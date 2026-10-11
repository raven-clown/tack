package tack_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
)

type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name" validate:"required,max=50"`
}

func serve(app *tack.App, method, url, body string) {
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(method, url, strings.NewReader(body)))
	fmt.Println(rec.Code, rec.Body.String())
}

func serveWithHeader(app *tack.App, method, url, key, value string) {
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set(key, value)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())
}

func ExampleGet() {
	app := tack.New(tack.Config{Title: "Pet API", Version: "1.0.0"})

	type GetPetInput struct {
		ID int `path:"id" validate:"min=1"`
	}
	tack.Get(app, "/pets/{id}", func(_ context.Context, in GetPetInput) (Pet, error) {
		return Pet{ID: in.ID, Name: "Milo"}, nil
	})

	serve(app, "GET", "/pets/7", "")
	serve(app, "GET", "/pets/0", "")
	// Output:
	// 200 {"id":7,"name":"Milo"}
	// 422 {"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"path.id","message":"must be at least 1"}]}
}

func ExamplePost() {
	app := tack.New(tack.Config{Title: "Pet API", Version: "1.0.0"})

	type CreatePetInput struct {
		Body Pet
	}
	tack.Post(app, "/pets", func(_ context.Context, in CreatePetInput) (Pet, error) {
		in.Body.ID = 1
		return in.Body, nil
	}, tack.Status(http.StatusCreated))

	serve(app, "POST", "/pets", `{"name":"Rex"}`)
	serve(app, "POST", "/pets", `{"name":""}`)
	// Output:
	// 201 {"id":1,"name":"Rex"}
	// 422 {"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"body.name","message":"is required"}]}
}

func ExampleNewError() {
	app := tack.New(tack.Config{})
	tack.Delete(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id"`
	}) (struct{}, error) {
		return struct{}{}, tack.NewError(http.StatusNotFound, "pet not found")
	})

	serve(app, "DELETE", "/pets/9", "")
	// Output:
	// 404 {"type":"about:blank","title":"Not Found","status":404,"detail":"pet not found"}
}

func ExampleStatus() {
	app := tack.New(tack.Config{})
	tack.Put(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id"`
	}) (struct{}, error) {
		return struct{}{}, nil
	}, tack.Status(http.StatusNoContent))

	serve(app, "PUT", "/pets/9", "")
	// Output:
	// 204
}

func ExampleNew() {
	app := tack.New(tack.Config{Title: "Pet API", Version: "1.0.0", DocsPath: "-"})
	fmt.Println(app.OpenAPI().Info.Title, app.OpenAPI().Info.Version)
	// Output:
	// Pet API 1.0.0
}

func ExampleApp_ServeHTTP() {
	app := tack.New(tack.Config{})
	tack.Get(app, "/ping", func(context.Context, struct{}) (string, error) { return "pong", nil })

	srv := httptest.NewServer(app)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/ping")
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	fmt.Println(res.StatusCode, res.Header.Get("Content-Type"))
	// Output:
	// 200 application/json
}

func ExampleApp_OpenAPI() {
	app := tack.New(tack.Config{Title: "Pet API"})
	tack.Get(app, "/pets/{id}", func(_ context.Context, in struct {
		ID int `path:"id" doc:"Pet ID"`
	}) (Pet, error) {
		return Pet{ID: in.ID}, nil
	})

	op := app.OpenAPI().Paths["/pets/{id}"].Get
	fmt.Println(op.OperationID, op.Parameters[0].Name, op.Parameters[0].Description)
	// Output:
	// getPetsById id Pet ID
}

func ExamplePut() {
	app := tack.New(tack.Config{})
	tack.Put(app, "/pets/{id}", func(_ context.Context, in struct {
		ID   int `path:"id"`
		Body Pet
	}) (Pet, error) {
		in.Body.ID = in.ID
		return in.Body, nil
	})

	serve(app, "PUT", "/pets/3", `{"name":"Rex"}`)
	// Output:
	// 200 {"id":3,"name":"Rex"}
}

func ExamplePatch() {
	app := tack.New(tack.Config{})
	tack.Patch(app, "/pets/{id}", func(_ context.Context, in struct {
		ID   int `path:"id"`
		Body *struct {
			Name *string `json:"name"`
		}
	}) (string, error) {
		if in.Body == nil || in.Body.Name == nil {
			return "unchanged", nil
		}
		return "renamed to " + *in.Body.Name, nil
	})

	serve(app, "PATCH", "/pets/3", `{"name":"Rex"}`)
	serve(app, "PATCH", "/pets/3", ``)
	// Output:
	// 200 "renamed to Rex"
	// 200 "unchanged"
}

func ExampleDelete() {
	app := tack.New(tack.Config{})
	tack.Delete(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id"`
	}) (struct{}, error) {
		return struct{}{}, nil
	}, tack.Status(http.StatusNoContent))

	serve(app, "DELETE", "/pets/3", "")
	// Output:
	// 204
}

func operation(opts ...tack.RouteOption) *openapi.Operation {
	app := tack.New(tack.Config{})
	tack.Get(app, "/pets", func(context.Context, struct{}) ([]Pet, error) { return nil, nil }, opts...)
	return app.OpenAPI().Paths["/pets"].Get
}

func ExampleSummary() {
	fmt.Println(operation(tack.Summary("List pets")).Summary)
	// Output:
	// List pets
}

func ExampleDescription() {
	fmt.Println(operation(tack.Description("Returns every pet, oldest first.")).Description)
	// Output:
	// Returns every pet, oldest first.
}

func ExampleTags() {
	fmt.Println(operation(tack.Tags("pets", "public")).Tags)
	// Output:
	// [pets public]
}

func ExampleOperationID() {
	fmt.Println(operation(tack.OperationID("listAllPets")).OperationID)
	// Output:
	// listAllPets
}

func ExampleDeprecated() {
	fmt.Println(operation(tack.Deprecated()).Deprecated)
	// Output:
	// true
}
