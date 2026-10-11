package futurefit

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/raven-clown/tack"
)

// Typed pagination. tack.Page[T] is a plain generic struct and PageInput a
// struct users embed in their input; both work with the public API as it is,
// so they can be added to the root package or to an add-on without touching
// an existing identifier.

// Page is what tack.Page[T] would be.
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty" doc:"Cursor of the next page; absent on the last one"`
}

// PageInput is the query half, embedded in a route's input.
type PageInput struct {
	Cursor string `query:"cursor" doc:"Where the previous page ended"`
	Limit  int    `query:"limit" default:"20" validate:"min=1,max=100"`
}

type listPetsInput struct {
	PageInput
	Kind string `query:"kind" validate:"omitempty,oneof=cat dog"`
}

func listPets(_ context.Context, in listPetsInput) (Page[Pet], error) {
	start, _ := strconv.Atoi(in.Cursor)
	page := Page[Pet]{Items: []Pet{}}
	for i := start; i < start+in.Limit && i < 5; i++ {
		page.Items = append(page.Items, Pet{ID: i + 1, Name: "pet"})
	}
	if end := start + in.Limit; end < 5 {
		page.Next = strconv.Itoa(end)
	}
	return page, nil
}

func TestPagination(t *testing.T) {
	app := newApp()
	tack.Get(app, "/pets", listPets)

	status, body, _ := do(t, app, http.MethodGet, "/pets?limit=2", "", nil)
	if status != 200 || !strings.Contains(body, `"next":"2"`) {
		t.Fatalf("first page: %d %s", status, body)
	}
	status, body, _ = do(t, app, http.MethodGet, "/pets?limit=2&cursor=4", "", nil)
	if status != 200 || strings.Contains(body, `"next"`) {
		t.Fatalf("last page: %d %s", status, body)
	}
	status, body, _ = do(t, app, http.MethodGet, "/pets?limit=0", "", nil)
	if status != 422 || !strings.Contains(body, `"query.limit"`) {
		t.Fatalf("embedded validation: %d %s", status, body)
	}

	doc := app.OpenAPI()
	op := operation(t, doc, http.MethodGet, "/pets")
	var names []string
	for _, p := range op.Parameters {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "cursor,limit,kind" {
		t.Errorf("parameters %s, want cursor,limit,kind", got)
	}
	if _, ok := doc.Components.Schemas["PagePet"]; !ok {
		t.Errorf("no PagePet schema; have %v", keys(doc.Components.Schemas))
	}
	if client := generates(t, doc); !strings.Contains(client, "export interface PagePet") {
		t.Error("client lacks PagePet")
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
