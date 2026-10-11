package bench

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/raven-clown/tack"
)

var errPetNotFound = tack.DefineError(http.StatusNotFound, "pet_not_found", "pet not found")

type tackListInput struct {
	Kind  string `query:"kind" validate:"omitempty,oneof=cat dog bird"`
	Limit int    `query:"limit" default:"20" validate:"min=1,max=100"`
}

type tackIDInput struct {
	ID int `path:"id" validate:"min=1"`
}

type tackPetInput struct {
	Name string `json:"name" validate:"required,max=50"`
	Kind string `json:"kind" validate:"required,oneof=cat dog bird"`
}

type tackCreateInput struct {
	Body tackPetInput
}

type tackUpdateInput struct {
	ID   int `path:"id" validate:"min=1"`
	Body tackPetInput
}

// NewTack registers the routes on a tack.App with the defaults, including
// the OpenAPI document and the docs page every app serves.
func NewTack() http.Handler {
	app := tack.New(tack.Config{Title: "Pets", Version: "1.0.0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	tack.Get(app, "/pets", func(_ context.Context, in tackListInput) ([]Pet, error) {
		return listPets(in.Kind, in.Limit), nil
	})
	tack.Get(app, "/pets/{id}", func(_ context.Context, in tackIDInput) (Pet, error) {
		p, ok := getPet(in.ID)
		if !ok {
			return Pet{}, errPetNotFound
		}
		return p, nil
	}, tack.Errors(errPetNotFound))
	tack.Post(app, "/pets", func(_ context.Context, in tackCreateInput) (Pet, error) {
		return createPet(in.Body.Name, in.Body.Kind), nil
	}, tack.Status(http.StatusCreated))
	tack.Put(app, "/pets/{id}", func(_ context.Context, in tackUpdateInput) (Pet, error) {
		p, ok := updatePet(in.ID, in.Body.Name, in.Body.Kind)
		if !ok {
			return Pet{}, errPetNotFound
		}
		return p, nil
	}, tack.Errors(errPetNotFound))
	return app
}
