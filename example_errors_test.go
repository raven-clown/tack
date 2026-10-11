package tack_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
)

var ErrPetNotFound = tack.DefineError(http.StatusNotFound, "pet_not_found", "pet not found")

func ExampleDefineError() {
	app := tack.New(tack.Config{})
	tack.Get(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id"`
	}) (Pet, error) {
		return Pet{}, ErrPetNotFound
	}, tack.Errors(ErrPetNotFound))

	serve(app, "GET", "/pets/7", "")
	// Output:
	// 404 {"type":"about:blank","title":"Not Found","status":404,"code":"pet_not_found","detail":"pet not found"}
}

func ExampleError_WithDetail() {
	app := tack.New(tack.Config{})
	tack.Get(app, "/pets/{id}", func(_ context.Context, in struct {
		ID int `path:"id"`
	}) (Pet, error) {
		return Pet{}, ErrPetNotFound.WithDetail(fmt.Sprintf("pet %d not found", in.ID))
	}, tack.Errors(ErrPetNotFound))

	serve(app, "GET", "/pets/7", "")
	// Output:
	// 404 {"type":"about:blank","title":"Not Found","status":404,"code":"pet_not_found","detail":"pet 7 not found"}
}

func ExampleError_Is() {
	err := fmt.Errorf("loading pet: %w", ErrPetNotFound.WithDetail("pet 7 not found"))
	fmt.Println(errors.Is(err, ErrPetNotFound))
	fmt.Println(errors.Is(tack.NewError(http.StatusNotFound, "pet not found"), ErrPetNotFound))
	// Output:
	// true
	// false
}

func ExampleError_Unwrap() {
	app := tack.New(tack.Config{
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e *tack.Error) {
			fmt.Println("cause:", e.Unwrap())
			w.WriteHeader(e.Status)
		},
	})
	tack.Get(app, "/pets", func(context.Context, struct{}) (Pet, error) {
		return Pet{}, fmt.Errorf("querying store: %w", ErrPetNotFound)
	})

	serve(app, "GET", "/pets", "")
	// Output:
	// cause: querying store: 404 Not Found: pet not found
	// 404
}

func ExampleErrors() {
	var ErrNameTaken = tack.DefineError(http.StatusConflict, "name_taken", "another pet has this name")

	app := tack.New(tack.Config{})
	tack.Post(app, "/pets", func(_ context.Context, in struct{ Body Pet }) (Pet, error) {
		return in.Body, nil
	}, tack.Errors(ErrPetNotFound, ErrNameTaken))

	op := app.OpenAPI().Paths["/pets"].Post
	for _, status := range []string{"404", "409", "422", "500"} {
		fmt.Println(status, op.Responses[status].Content["application/problem+json"].Schema.Ref)
	}
	// Output:
	// 404 #/components/schemas/PetNotFoundProblem
	// 409 #/components/schemas/NameTakenProblem
	// 422 #/components/schemas/ValidationFailedProblem
	// 500 #/components/schemas/InternalProblem
}

func ExampleNew_errorHandler() {
	app := tack.New(tack.Config{
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e *tack.Error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(e.Status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": e.Code, "message": e.Detail})
		},
	})
	tack.Get(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id" validate:"min=1"`
	}) (Pet, error) {
		return Pet{}, ErrPetNotFound
	})

	serve(app, "GET", "/pets/0", "")
	serve(app, "GET", "/pets/1", "")
	// Output:
	// 422 {"error":"validation_failed","message":"validation failed"}
	//
	// 404 {"error":"pet_not_found","message":"pet not found"}
	//
}

func ExampleApp_WriteError() {
	app := tack.New(tack.Config{Security: map[string]openapi.SecurityScheme{"bearer": tack.Bearer("JWT")}})
	requireToken := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				app.WriteError(w, r, tack.ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	me := app.Group("", requireToken).With(tack.Security("bearer"))
	tack.Get(me, "/me", func(context.Context, struct{}) (string, error) { return "ada", nil })

	serve(app, "GET", "/me", "")
	serveWithHeader(app, "GET", "/me", "Authorization", "Bearer token")
	// Output:
	// 401 {"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized","detail":"authentication required"}
	// 200 "ada"
}

func ExampleSecurity() {
	app := tack.New(tack.Config{Security: map[string]openapi.SecurityScheme{
		"bearer": tack.Bearer("JWT"),
		"apiKey": tack.APIKey("header", "X-API-Key"),
	}})
	tack.Get(app, "/pets", func(context.Context, struct{}) ([]Pet, error) { return nil, nil },
		tack.Security("bearer", "pets:read"), tack.Security("apiKey"))

	op := app.OpenAPI().Paths["/pets"].Get
	fmt.Println(op.Security)
	fmt.Println(op.Responses["401"].Description)
	// Output:
	// [map[bearer:[pets:read]] map[apiKey:[]]]
	// Unauthorized
}

func ExampleBearer() {
	fmt.Printf("%+v\n", tack.Bearer("JWT"))
	// Output:
	// {Type:http Description: Scheme:bearer BearerFormat:JWT In: Name:}
}

func ExampleBasic() {
	fmt.Printf("%+v\n", tack.Basic())
	// Output:
	// {Type:http Description: Scheme:basic BearerFormat: In: Name:}
}

func ExampleAPIKey() {
	fmt.Printf("%+v\n", tack.APIKey("header", "X-API-Key"))
	// Output:
	// {Type:apiKey Description: Scheme: BearerFormat: In:header Name:X-API-Key}
}

// CreatePetOutput sets the status, a header, and the body of one response.
type CreatePetOutput struct {
	Status   int
	Location string `header:"Location" doc:"URL of the new pet"`
	Body     Pet
}

func ExamplePost_responseHeaders() {
	app := tack.New(tack.Config{})
	tack.Post(app, "/pets", func(_ context.Context, in struct{ Body Pet }) (CreatePetOutput, error) {
		in.Body.ID = 1
		return CreatePetOutput{Status: http.StatusCreated, Location: "/pets/1", Body: in.Body}, nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pets", strings.NewReader(`{"name":"Rex"}`)))
	fmt.Println(rec.Code, rec.Header().Get("Location"), rec.Body.String())
	fmt.Println(app.OpenAPI().Paths["/pets"].Post.Responses["200"].Headers["Location"].Description)
	// Output:
	// 201 /pets/1 {"id":1,"name":"Rex"}
	// URL of the new pet
}
