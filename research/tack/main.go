// Command tack is the tack variant of the comparison API.
package main

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"

	"github.com/raven-clown/tack"
)

// Pet is a pet in the store.
type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name" validate:"required,max=50"`
	Kind string `json:"kind" validate:"required,oneof=cat dog bird"`
}

// PetInput is the writable part of a Pet.
type PetInput struct {
	Name string `json:"name" validate:"required,max=50"`
	Kind string `json:"kind" validate:"required,oneof=cat dog bird"`
}

type listInput struct {
	Kind  string `query:"kind" validate:"omitempty,oneof=cat dog bird" doc:"Only pets of this kind"`
	Limit int    `query:"limit" default:"20" validate:"min=1,max=100" doc:"Maximum number of pets"`
}

type idInput struct {
	ID int `path:"id" validate:"min=1" doc:"Pet ID"`
}

type createInput struct {
	Body PetInput
}

type updateInput struct {
	ID   int `path:"id" validate:"min=1" doc:"Pet ID"`
	Body PetInput
}

type store struct {
	mu   sync.Mutex
	pets map[int]Pet
	next int
}

func main() {
	app := tack.New(tack.Config{Title: "Pet Store", Version: "1.0.0"})
	s := &store{pets: map[int]Pet{}}
	tack.Get(app, "/pets", s.list, tack.Summary("List pets"))
	tack.Get(app, "/pets/{id}", s.get, tack.Summary("Get a pet"))
	tack.Post(app, "/pets", s.create, tack.Summary("Create a pet"), tack.Status(http.StatusCreated))
	tack.Put(app, "/pets/{id}", s.update, tack.Summary("Update a pet"))
	log.Fatal(http.ListenAndServe(":8080", app))
}

func (s *store) list(_ context.Context, in listInput) ([]Pet, error) {
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
	return pets, nil
}

func (s *store) get(_ context.Context, in idInput) (Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pets[in.ID]
	if !ok {
		return Pet{}, tack.NewError(http.StatusNotFound, "pet not found")
	}
	return p, nil
}

func (s *store) create(_ context.Context, in createInput) (Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	p := Pet{ID: s.next, Name: in.Body.Name, Kind: in.Body.Kind}
	s.pets[p.ID] = p
	return p, nil
}

func (s *store) update(_ context.Context, in updateInput) (Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pets[in.ID]; !ok {
		return Pet{}, tack.NewError(http.StatusNotFound, "pet not found")
	}
	p := Pet{ID: in.ID, Name: in.Body.Name, Kind: in.Body.Kind}
	s.pets[in.ID] = p
	return p, nil
}
