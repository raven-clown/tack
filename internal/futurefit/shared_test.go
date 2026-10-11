package futurefit

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

// Pet is the resource every sketch serves.
type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name" validate:"required,max=50"`
}

type idInput struct {
	ID int `path:"id" validate:"min=1"`
}

var errPetNotFound = tack.DefineError(http.StatusNotFound, "pet_not_found", "pet not found")

func getPet(_ context.Context, in idInput) (Pet, error) {
	if in.ID > 100 {
		return Pet{}, errPetNotFound
	}
	return Pet{ID: in.ID, Name: "Milo"}, nil
}

func newApp() *tack.App {
	return tack.New(tack.Config{Title: "Future fit", Version: "0.9.0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

// do sends one request to h and returns the status and body.
func do(t *testing.T, h http.Handler, method, target, body string, header http.Header) (int, string, http.Header) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

// generates checks that the document still produces a client and schemas,
// and returns the client source for further checks.
func generates(t *testing.T, doc *openapi.Document) string {
	t.Helper()
	ts, err := tsgen.Generate(doc)
	if err != nil {
		t.Fatalf("tsgen.Generate: %v", err)
	}
	if _, err := tsgen.GenerateZod(doc); err != nil {
		t.Fatalf("tsgen.GenerateZod: %v", err)
	}
	return string(ts)
}

func operation(t *testing.T, doc *openapi.Document, method, path string) *openapi.Operation {
	t.Helper()
	item := doc.Paths[path]
	if item == nil {
		t.Fatalf("no path %s in the document", path)
	}
	var op *openapi.Operation
	switch method {
	case http.MethodGet:
		op = item.Get
	case http.MethodPost:
		op = item.Post
	case http.MethodPut:
		op = item.Put
	case http.MethodDelete:
		op = item.Delete
	case http.MethodPatch:
		op = item.Patch
	}
	if op == nil {
		t.Fatalf("no %s %s in the document", method, path)
	}
	return op
}
