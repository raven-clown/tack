// Command petstore is a small CRUD API built with tack.
package main

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"github.com/raven-clown/tack"
)

// ErrPetNotFound is returned when no pet has the requested ID.
var ErrPetNotFound = tack.DefineError(http.StatusNotFound, "pet_not_found", "pet not found")

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
	mu        sync.Mutex
	pets      map[int]Pet
	nextID    int
	listeners map[chan Change]struct{}
}

// notify sends c to every open event stream.
func (s *store) notify(c Change) {
	for ch := range s.listeners {
		select {
		case ch <- c:
		default: // A slow listener misses the event rather than blocking the API.
		}
	}
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

// createOutput adds a Location header to the created pet.
type createOutput struct {
	Location string `header:"Location" doc:"URL of the new pet"`
	Body     Pet
}

type updateInput struct {
	ID   int `path:"id" validate:"min=1"`
	Body PetInput
}

// photoInput is a multipart form: a file and a caption.
type photoInput struct {
	ID      int                   `path:"id" validate:"min=1"`
	Photo   *multipart.FileHeader `form:"photo" validate:"required" doc:"The image file"`
	Caption string                `form:"caption" validate:"max=100"`
}

// Photo describes an uploaded photo.
type Photo struct {
	PetID   int    `json:"petId"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Caption string `json:"caption,omitempty"`
}

// Change is one event on the store's event stream.
type Change struct {
	Action string `json:"action" validate:"oneof=created updated deleted"`
	Pet    Pet    `json:"pet"`
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
		return Pet{}, ErrPetNotFound
	}
	return p, nil
}

func (s *store) create(_ context.Context, in createInput) (createOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	p := Pet{ID: s.nextID, Name: in.Body.Name, Kind: in.Body.Kind}
	s.pets[p.ID] = p
	s.notify(Change{Action: "created", Pet: p})
	return createOutput{Location: fmt.Sprintf("/pets/%d", p.ID), Body: p}, nil
}

func (s *store) update(_ context.Context, in updateInput) (Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pets[in.ID]; !ok {
		return Pet{}, ErrPetNotFound.WithDetail(fmt.Sprintf("pet %d not found", in.ID))
	}
	p := Pet{ID: in.ID, Name: in.Body.Name, Kind: in.Body.Kind}
	s.pets[p.ID] = p
	s.notify(Change{Action: "updated", Pet: p})
	return p, nil
}

func (s *store) delete(_ context.Context, in idInput) (struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pets[in.ID]
	if !ok {
		return struct{}{}, ErrPetNotFound
	}
	delete(s.pets, in.ID)
	s.notify(Change{Action: "deleted", Pet: p})
	return struct{}{}, nil
}

// upload stores nothing; it shows how a multipart form arrives.
func (s *store) upload(_ context.Context, in photoInput) (Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pets[in.ID]; !ok {
		return Photo{}, ErrPetNotFound
	}
	return Photo{PetID: in.ID, Name: in.Photo.Filename, Size: in.Photo.Size, Caption: in.Caption}, nil
}

// events streams every change until the client disconnects.
func (s *store) events(ctx context.Context, _ struct{}) (tack.Stream[tack.Event[Change]], error) {
	ch := make(chan Change, 16)
	s.mu.Lock()
	s.listeners[ch] = struct{}{}
	s.mu.Unlock()
	return func(yield func(tack.Event[Change]) bool) {
		defer func() {
			s.mu.Lock()
			delete(s.listeners, ch)
			s.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-ch:
				if !yield(tack.Event[Change]{Name: c.Action, Data: c}) {
					return
				}
			}
		}
	}, nil
}

func newApp() *tack.App {
	s := &store{pets: map[int]Pet{}, listeners: map[chan Change]struct{}{}}
	app := tack.New(tack.Config{Title: "Pet Store", Version: "1.0.0", Description: "A small CRUD API built with tack.", MaxBodyBytes: 8 << 20})
	pets := app.Group("/pets").With(tack.Tags("pets"))
	one := pets.With(tack.Errors(ErrPetNotFound))
	tack.Get(pets, "", s.list, tack.Summary("List pets"))
	tack.Post(pets, "", s.create, tack.Summary("Create a pet"), tack.Status(http.StatusCreated))
	tack.Get(pets, "/events", s.events, tack.Summary("Follow changes"), tack.Description("Server-sent events: one per created, updated, or deleted pet."))
	tack.Get(one, "/{id}", s.get, tack.Summary("Get a pet"))
	tack.Put(one, "/{id}", s.update, tack.Summary("Update a pet"))
	tack.Delete(one, "/{id}", s.delete, tack.Summary("Delete a pet"), tack.Status(http.StatusNoContent))
	tack.Post(one, "/{id}/photo", s.upload, tack.Summary("Upload a photo"), tack.Status(http.StatusCreated))
	return app
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newApp().Run(ctx, ":8080")
	stop()
	if err != nil {
		log.Fatal(err)
	}
}
