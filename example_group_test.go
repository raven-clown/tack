package tack_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/raven-clown/tack"
)

// logging is a plain net/http middleware.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("request:", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

type userKey struct{}

// withUser stores the caller's name in the request context.
func withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userKey{}, r.Header.Get("X-User"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ExampleApp_Group() {
	app := tack.New(tack.Config{})
	v1 := app.Group("/v1")
	tack.Get(v1, "/pets", func(context.Context, struct{}) ([]Pet, error) {
		return []Pet{{ID: 1, Name: "Milo"}}, nil
	})

	serve(app, "GET", "/v1/pets", "")
	// Output:
	// 200 [{"id":1,"name":"Milo"}]
}

func ExampleApp_Use() {
	app := tack.New(tack.Config{})
	app.Use(logging)
	tack.Get(app, "/ping", func(context.Context, struct{}) (string, error) { return "pong", nil })

	serve(app, "GET", "/ping", "")
	// Output:
	// request: GET /ping
	// 200 "pong"
}

func ExampleGroup_Group() {
	app := tack.New(tack.Config{})
	v1 := app.Group("/v1")
	admin := v1.Group("/admin", withUser)
	tack.Get(admin, "/whoami", func(ctx context.Context, _ struct{}) (string, error) {
		user, _ := tack.ContextValue[string](ctx, userKey{})
		return user, nil
	})

	fmt.Println(app.OpenAPI().Paths["/v1/admin/whoami"].Get.OperationID)
	// Output:
	// getV1AdminWhoami
}

func ExampleGroup_Use() {
	app := tack.New(tack.Config{})
	api := app.Group("/api")
	api.Use(logging)
	tack.Get(api, "/ping", func(context.Context, struct{}) (string, error) { return "pong", nil })

	serve(app, "GET", "/api/ping", "")
	// Output:
	// request: GET /api/ping
	// 200 "pong"
}

func ExampleGroup_With() {
	app := tack.New(tack.Config{})
	pets := app.Group("/pets").With(tack.Tags("pets"))
	tack.Get(pets, "", func(context.Context, struct{}) ([]Pet, error) { return nil, nil })
	tack.Get(pets, "/{id}", func(_ context.Context, in struct {
		ID int `path:"id"`
	}) (Pet, error) {
		return Pet{ID: in.ID}, nil
	})

	doc := app.OpenAPI()
	fmt.Println(doc.Paths["/pets"].Get.Tags, doc.Paths["/pets/{id}"].Get.Tags)
	// Output:
	// [pets] [pets]
}

func ExampleMiddleware() {
	app := tack.New(tack.Config{})
	tack.Get(app, "/ping", func(context.Context, struct{}) (string, error) { return "pong", nil }, tack.Middleware(logging))

	serve(app, "GET", "/ping", "")
	// Output:
	// request: GET /ping
	// 200 "pong"
}

func ExampleContextValue() {
	app := tack.New(tack.Config{})
	app.Use(withUser)
	tack.Get(app, "/me", func(ctx context.Context, _ struct{}) (string, error) {
		user, ok := tack.ContextValue[string](ctx, userKey{})
		if !ok || user == "" {
			return "", tack.NewError(http.StatusUnauthorized, "who are you?")
		}
		return "hello " + user, nil
	})

	serve(app, "GET", "/me", "")
	serveWithHeader(app, "GET", "/me", "X-User", "ada")
	// Output:
	// 401 {"type":"about:blank","title":"Unauthorized","status":401,"detail":"who are you?"}
	// 200 "hello ada"
}
