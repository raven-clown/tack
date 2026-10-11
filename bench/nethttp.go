package bench

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// NewNetHTTP is the hand-written floor: net/http routing, strconv, and
// encoding/json, with every rule checked by hand and all errors reported.
func NewNetHTTP() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /pets", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var errs []fieldError
		kind := q.Get("kind")
		if kind != "" && !validKind(kind) {
			errs = append(errs, fieldError{"query.kind", "must be one of cat, dog, bird"})
		}
		limit := 20
		if s := q.Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				writeProblem(w, http.StatusBadRequest, "malformed request", []fieldError{{"query.limit", "must be an integer"}})
				return
			}
			limit = n
		}
		if limit < 1 || limit > 100 {
			errs = append(errs, fieldError{"query.limit", "must be between 1 and 100"})
		}
		if errs != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "validation failed", errs)
			return
		}
		writeJSON(w, http.StatusOK, listPets(kind, limit))
	})
	mux.HandleFunc("GET /pets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		p, found := getPet(id)
		if !found {
			writeProblem(w, http.StatusNotFound, "pet not found", nil)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /pets", func(w http.ResponseWriter, r *http.Request) {
		in, ok := readPetInput(w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusCreated, createPet(in.Name, in.Kind))
	})
	mux.HandleFunc("PUT /pets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		in, ok := readPetInput(w, r)
		if !ok {
			return
		}
		p, found := updatePet(id, in.Name, in.Kind)
		if !found {
			writeProblem(w, http.StatusNotFound, "pet not found", nil)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	return mux
}

type petInput struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type fieldError struct {
	Location string `json:"location"`
	Message  string `json:"message"`
}

type problem struct {
	Status int          `json:"status"`
	Detail string       `json:"detail"`
	Errors []fieldError `json:"errors,omitempty"`
}

func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "malformed request", []fieldError{{"path.id", "must be an integer"}})
		return 0, false
	}
	if id < 1 {
		writeProblem(w, http.StatusUnprocessableEntity, "validation failed", []fieldError{{"path.id", "must be at least 1"}})
		return 0, false
	}
	return id, true
}

func readPetInput(w http.ResponseWriter, r *http.Request) (petInput, bool) {
	var in petInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeProblem(w, http.StatusBadRequest, "malformed request", []fieldError{{"body", err.Error()}})
		return in, false
	}
	var errs []fieldError
	if in.Name == "" {
		errs = append(errs, fieldError{"body.name", "is required"})
	} else if len(in.Name) > 50 {
		errs = append(errs, fieldError{"body.name", "must be at most 50 characters"})
	}
	if !validKind(in.Kind) {
		errs = append(errs, fieldError{"body.kind", "must be one of cat, dog, bird"})
	}
	if errs != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "validation failed", errs)
		return in, false
	}
	return in, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, detail string, errs []fieldError) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Status: status, Detail: detail, Errors: errs})
}
