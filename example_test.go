package tack_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/raven-clown/tack"
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
