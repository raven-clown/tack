// Command oapi-codegen is the spec-first variant of the comparison API.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"sync"

	middleware "github.com/oapi-codegen/nethttp-middleware"

	"oapicodegen/api"
)

type store struct {
	mu   sync.Mutex
	pets map[int]api.Pet
	next int
}

func main() {
	spec, err := api.GetSwagger()
	if err != nil {
		log.Fatal(err)
	}
	spec.Servers = nil
	s := &store{pets: map[int]api.Pet{}}
	h := api.HandlerWithOptions(s, api.StdHTTPServerOptions{
		Middlewares: []api.MiddlewareFunc{middleware.OapiRequestValidator(spec)},
	})
	log.Fatal(http.ListenAndServe(":8080", h))
}

func (s *store) ListPets(w http.ResponseWriter, _ *http.Request, params api.ListPetsParams) {
	limit := 20
	if params.Limit != nil {
		limit = *params.Limit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pets := make([]api.Pet, 0, len(s.pets))
	for _, p := range s.pets {
		if params.Kind == nil || p.Kind == api.PetKind(*params.Kind) {
			pets = append(pets, p)
		}
	}
	sort.Slice(pets, func(i, j int) bool { return pets[i].Id < pets[j].Id })
	if len(pets) > limit {
		pets = pets[:limit]
	}
	writeJSON(w, http.StatusOK, pets)
}

func (s *store) GetPet(w http.ResponseWriter, _ *http.Request, id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pets[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, api.Problem{Status: http.StatusNotFound, Detail: "pet not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *store) CreatePet(w http.ResponseWriter, r *http.Request) {
	var in api.PetInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Problem{Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	p := api.Pet{Id: s.next, Name: in.Name, Kind: in.Kind}
	s.pets[p.Id] = p
	writeJSON(w, http.StatusCreated, p)
}

func (s *store) UpdatePet(w http.ResponseWriter, r *http.Request, id int) {
	var in api.PetInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, api.Problem{Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pets[id]; !ok {
		writeJSON(w, http.StatusNotFound, api.Problem{Status: http.StatusNotFound, Detail: "pet not found"})
		return
	}
	p := api.Pet{Id: id, Name: in.Name, Kind: in.Kind}
	s.pets[id] = p
	writeJSON(w, http.StatusOK, p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
