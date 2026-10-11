package futurefit

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
)

// API versioning. Today a version is a group. App.Version(v) would return
// such a group with the version tag set, and a per-version document would be
// a new method on App built from the one document the way versionDocument
// does below. Both are additive.

// PetV2 renames a field; the two versions serve side by side.
type PetV2 struct {
	ID       int    `json:"id"`
	FullName string `json:"fullName" validate:"required,max=50"`
}

func getPetV2(_ context.Context, in idInput) (PetV2, error) {
	return PetV2{ID: in.ID, FullName: "Milo"}, nil
}

// versionDocument is what app.OpenAPIVersion("v2") would return: the paths
// under one prefix, with the schemas they reach. The document is a value the
// public API already hands out, so an add-on can build this today.
func versionDocument(doc *openapi.Document, prefix string) *openapi.Document {
	out := *doc
	out.Paths = map[string]*openapi.PathItem{}
	for p, item := range doc.Paths {
		if strings.HasPrefix(p, prefix+"/") {
			out.Paths[p] = item
		}
	}
	return &out
}

func TestVersioning(t *testing.T) {
	app := newApp()
	v1 := app.Group("/v1").With(tack.Tags("v1"), tack.Deprecated())
	v2 := app.Group("/v2").With(tack.Tags("v2"))
	tack.Get(v1, "/pets/{id}", getPet, tack.Errors(errPetNotFound))
	tack.Get(v2, "/pets/{id}", getPetV2)

	if status, body, _ := do(t, app, http.MethodGet, "/v1/pets/1", "", nil); status != 200 || !strings.Contains(body, `"name"`) {
		t.Fatalf("v1: %d %s", status, body)
	}
	if status, body, _ := do(t, app, http.MethodGet, "/v2/pets/1", "", nil); status != 200 || !strings.Contains(body, `"fullName"`) {
		t.Fatalf("v2: %d %s", status, body)
	}

	doc := app.OpenAPI()
	if op := operation(t, doc, http.MethodGet, "/v1/pets/{id}"); !op.Deprecated || op.Tags[0] != "v1" {
		t.Errorf("v1 operation: deprecated=%v tags=%v", op.Deprecated, op.Tags)
	}
	v2doc := versionDocument(doc, "/v2")
	if len(v2doc.Paths) != 1 {
		t.Errorf("v2 document has %d paths, want 1", len(v2doc.Paths))
	}
	client := generates(t, doc)
	for _, want := range []string{"getV1PetsById", "getV2PetsById", "PetV2"} {
		if !strings.Contains(client, want) {
			t.Errorf("client lacks %s", want)
		}
	}
}
