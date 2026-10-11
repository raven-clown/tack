package bench

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type humaListInput struct {
	Kind  string `query:"kind" enum:"cat,dog,bird"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100"`
}

type humaIDInput struct {
	ID int `path:"id" minimum:"1"`
}

type humaPetInput struct {
	Name string `json:"name" minLength:"1" maxLength:"50"`
	Kind string `json:"kind" enum:"cat,dog,bird"`
}

type humaCreateInput struct {
	Body humaPetInput
}

type humaUpdateInput struct {
	ID   int `path:"id" minimum:"1"`
	Body humaPetInput
}

type humaPetOutput struct {
	Body Pet
}

type humaPetsOutput struct {
	Body []Pet
}

// NewHuma registers the routes on a net/http ServeMux through humago with
// huma.DefaultConfig, which is what the Huma documentation starts from. Huma
// validates with its own JSON Schema validator and answers 422.
func NewHuma() http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Pets", "1.0.0"))
	huma.Get(api, "/pets", func(_ context.Context, in *humaListInput) (*humaPetsOutput, error) {
		return &humaPetsOutput{Body: listPets(in.Kind, in.Limit)}, nil
	})
	huma.Get(api, "/pets/{id}", func(_ context.Context, in *humaIDInput) (*humaPetOutput, error) {
		p, ok := getPet(in.ID)
		if !ok {
			return nil, huma.Error404NotFound("pet not found")
		}
		return &humaPetOutput{Body: p}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID:   "create-pet",
		Method:        http.MethodPost,
		Path:          "/pets",
		DefaultStatus: http.StatusCreated,
	}, func(_ context.Context, in *humaCreateInput) (*humaPetOutput, error) {
		return &humaPetOutput{Body: createPet(in.Body.Name, in.Body.Kind)}, nil
	})
	huma.Put(api, "/pets/{id}", func(_ context.Context, in *humaUpdateInput) (*humaPetOutput, error) {
		p, ok := updatePet(in.ID, in.Body.Name, in.Body.Kind)
		if !ok {
			return nil, huma.Error404NotFound("pet not found")
		}
		return &humaPetOutput{Body: p}, nil
	})
	return mux
}
