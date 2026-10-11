// Package bench serves the same pet API with net/http, Gin, Echo, Huma, and
// tack, so one harness can measure them on identical requests.
//
// Every variant has the same four routes and the same rules:
//
//	GET  /pets          kind query, one of cat/dog/bird; limit query, 1-100, default 20
//	GET  /pets/{id}     id path, integer >= 1; 404 when missing
//	POST /pets          JSON body: name required, <= 50 chars; kind one of cat/dog/bird; 201
//	PUT  /pets/{id}     same body as POST; 404 when missing
//
// The store is fixed and read-only, so a benchmark does not grow it. Each
// variant uses its framework's own binding and validation, configured the way
// its documentation shows, with no logging or recovery middleware.
package bench

import "slices"

// Pet is a pet in the store.
type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

var kinds = []string{"cat", "dog", "bird"}

var pets = []Pet{
	{1, "Milo", "cat"}, {2, "Rex", "dog"}, {3, "Tweety", "bird"}, {4, "Luna", "cat"}, {5, "Max", "dog"},
	{6, "Kiwi", "bird"}, {7, "Nala", "cat"}, {8, "Buddy", "dog"}, {9, "Sky", "bird"}, {10, "Cleo", "cat"},
}

func validKind(kind string) bool { return slices.Contains(kinds, kind) }

func listPets(kind string, limit int) []Pet {
	// The store is fixed, so the result never needs more room than it has,
	// and the request's limit never reaches the allocation.
	out := make([]Pet, 0, len(pets))
	for _, p := range pets {
		if kind == "" || p.Kind == kind {
			out = append(out, p)
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

func getPet(id int) (Pet, bool) {
	if id < 1 || id > len(pets) {
		return Pet{}, false
	}
	return pets[id-1], true
}

func createPet(name, kind string) Pet {
	return Pet{ID: len(pets) + 1, Name: name, Kind: kind}
}

func updatePet(id int, name, kind string) (Pet, bool) {
	if _, ok := getPet(id); !ok {
		return Pet{}, false
	}
	return Pet{ID: id, Name: name, Kind: kind}, true
}
