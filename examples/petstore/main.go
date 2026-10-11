// Command petstore is a small CRUD API built with tack.
package main

import (
	"context"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

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

type store struct {
	mu     sync.Mutex
	pets   map[int]Pet
	nextID int
}

type listInput struct {
	Kind  string `query:"kind" validate:"omitempty,oneof=cat dog bird" doc:"Only return pets of this kind"`
	Limit int    `query:"limit" default:"20" validate:"min=1,max=100" doc:"Maximum number of pets"`
}

type idInput struct {
	ID int `path:"id" validate:"min=1" doc:"Pet ID"`
}

type createInput struct {
	Body PetInput
}

type updateInput struct {
	ID   int `path:"id" validate:"min=1"`
	Body PetInput
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
	slices.SortFunc(pets, func(a, b Pet) int { return a.ID - b.ID })
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
	s.nextID++
	p := Pet{ID: s.nextID, Name: in.Body.Name, Kind: in.Body.Kind}
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
	s.pets[p.ID] = p
	return p, nil
}

func (s *store) delete(_ context.Context, in idInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pets[in.ID]; !ok {
		return struct{}{}, tack.NewError(http.StatusNotFound, "pet not found")
	}
	delete(s.pets, in.ID)
	return struct{}{}, nil
}

func newApp() *tack.App {
	s := &store{pets: map[int]Pet{}}
	app := tack.New(tack.Config{Title: "Pet Store", Version: "1.0.0", Description: "A small CRUD API built with tack."})
	pets := tack.Tags("pets")
	tack.Get(app, "/pets", s.list, tack.Summary("List pets"), pets)
	tack.Get(app, "/pets/{id}", s.get, tack.Summary("Get a pet"), pets)
	tack.Post(app, "/pets", s.create, tack.Summary("Create a pet"), tack.Status(http.StatusCreated), pets)
	tack.Put(app, "/pets/{id}", s.update, tack.Summary("Update a pet"), pets)
	tack.Delete(app, "/pets/{id}", s.delete, tack.Summary("Delete a pet"), tack.Status(http.StatusNoContent), pets)
	return app
}

func main() {
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           newApp(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
