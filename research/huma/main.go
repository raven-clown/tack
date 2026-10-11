// Command huma is the Huma variant of the comparison API.
package main

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Pet is a pet in the store.
type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name" maxLength:"50"`
	Kind string `json:"kind" enum:"cat,dog,bird"`
}

// PetInput is the writable part of a Pet.
type PetInput struct {
	Name string `json:"name" minLength:"1" maxLength:"50"`
	Kind string `json:"kind" enum:"cat,dog,bird"`
}

type listInput struct {
	Kind  string `query:"kind" enum:"cat,dog,bird" doc:"Only pets of this kind"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of pets"`
}

type idInput struct {
	ID int `path:"id" minimum:"1" doc:"Pet ID"`
}

type createInput struct {
	Body PetInput
}

type updateInput struct {
	ID   int `path:"id" minimum:"1" doc:"Pet ID"`
	Body PetInput
}

type petOutput struct {
	Body Pet
}

type petsOutput struct {
	Body []Pet
}

type store struct {
	mu   sync.Mutex
	pets map[int]Pet
	next int
}

func main() {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Pet Store", "1.0.0"))
	s := &store{pets: map[int]Pet{}}
	huma.Get(api, "/pets", s.list)
	huma.Get(api, "/pets/{id}", s.get)
	huma.Post(api, "/pets", s.create, func(o *huma.Operation) { o.DefaultStatus = http.StatusCreated })
	huma.Put(api, "/pets/{id}", s.update)
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *store) list(_ context.Context, in *listInput) (*petsOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pets := make([]Pet, 0, len(s.pets))
	for _, p := range s.pets {
		if in.Kind == "" || p.Kind == in.Kind {
			pets = append(pets, p)
		}
	}
	sort.Slice(pets, func(i, j int) bool { return pets[i].ID < pets[j].ID })
	if len(pets) > in.Limit {
		pets = pets[:in.Limit]
	}
	return &petsOutput{Body: pets}, nil
}

func (s *store) get(_ context.Context, in *idInput) (*petOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pets[in.ID]
	if !ok {
		return nil, huma.Error404NotFound("pet not found")
	}
	return &petOutput{Body: p}, nil
}

func (s *store) create(_ context.Context, in *createInput) (*petOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	p := Pet{ID: s.next, Name: in.Body.Name, Kind: in.Body.Kind}
	s.pets[p.ID] = p
	return &petOutput{Body: p}, nil
}

func (s *store) update(_ context.Context, in *updateInput) (*petOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pets[in.ID]; !ok {
		return nil, huma.Error404NotFound("pet not found")
	}
	p := Pet{ID: in.ID, Name: in.Body.Name, Kind: in.Body.Kind}
	s.pets[in.ID] = p
	return &petOutput{Body: p}, nil
}
