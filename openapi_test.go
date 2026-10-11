package tack

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/raven-clown/tack/openapi"
)

var update = flag.Bool("update", false, "update golden files")

type Owner struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email,omitempty" validate:"omitempty,email"`
}

type Animal struct {
	ID        int       `json:"id" doc:"Pet ID" example:"7"`
	Name      string    `json:"name" validate:"required,max=50"`
	Kind      string    `json:"kind" validate:"oneof=cat dog"`
	Owner     *Owner    `json:"owner,omitempty"`
	Born      time.Time `json:"born"`
	Nicknames []string  `json:"nicknames,omitempty" validate:"max=3,dive,min=1"`
}

func specApp() *App {
	app := New(Config{
		Title:       "Pet API",
		Version:     "1.2.3",
		Description: "Manage pets.",
		Servers:     []openapi.Server{{URL: "https://api.example.com"}},
	})
	nop := func(context.Context, struct{}) ([]Animal, error) { return nil, nil }
	Get(app, "/pets", func(context.Context, struct {
		Limit  int      `query:"limit" default:"20" validate:"min=1,max=100" doc:"Page size"`
		Kind   *string  `query:"kind" validate:"omitempty,oneof=cat dog"`
		Tags   []string `query:"tag"`
		APIKey string   `header:"X-API-Key" validate:"required"`
	}) ([]Animal, error) {
		return nil, nil
	}, Summary("List pets"), Tags("pets"))
	Get(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id" validate:"min=1"`
	}) (Animal, error) {
		return Animal{}, nil
	}, Summary("Get a pet"), Description("Returns one pet."), Tags("pets"))
	Post(app, "/pets", func(context.Context, struct {
		Body Animal `doc:"The pet to create"`
	}) (Animal, error) {
		return Animal{}, nil
	}, Status(http.StatusCreated), Tags("pets"))
	Patch(app, "/pets/{id}", func(context.Context, struct {
		ID      int    `path:"id"`
		Session string `cookie:"session"`
		Body    *Owner
	}) (Animal, error) {
		return Animal{}, nil
	}, OperationID("setOwner"))
	Delete(app, "/pets/{id}", func(context.Context, struct {
		ID int `path:"id"`
	}) (struct{}, error) {
		return struct{}{}, nil
	}, Status(http.StatusNoContent), Deprecated())
	Put(app, "/files/{path...}", func(context.Context, struct {
		Path string `path:"path"`
		Body map[string]any
	}) (map[string]int, error) {
		return nil, nil
	})
	Get(app, "/{$}", nop)
	return app
}

func fetch(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestOpenAPISpec(t *testing.T) {
	rec := fetch(t, specApp(), "/openapi.json")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("invalid OpenAPI document: %v", err)
	}

	var pretty any
	if err := json.Unmarshal(rec.Body.Bytes(), &pretty); err != nil {
		t.Fatal(err)
	}
	got, _ := json.MarshalIndent(pretty, "", "  ")
	got = append(got, '\n')
	path := filepath.Join("testdata", "openapi.golden.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("spec differs from %s (run go test . -update)\ngot:\n%s", path, got)
	}
}

func TestOpenAPIDeterministic(t *testing.T) {
	a := fetch(t, specApp(), "/openapi.json").Body.String()
	b := fetch(t, specApp(), "/openapi.json").Body.String()
	if a != b {
		t.Fatal("spec output is not deterministic")
	}
}

func TestOpenAPICachedAndMutable(t *testing.T) {
	app := specApp()
	first := fetch(t, app, "/openapi.json").Body.String()
	if again := fetch(t, app, "/openapi.json").Body.String(); again != first {
		t.Fatal("cached spec changed")
	}
	app.OpenAPI().Info.Title = "Renamed"
	if !strings.Contains(fetch(t, app, "/openapi.json").Body.String(), `"title":"Renamed"`) {
		t.Fatal("changes from OpenAPI() not served")
	}
	Get(app, "/late", func(context.Context, struct{}) (int, error) { return 0, nil })
	if !strings.Contains(fetch(t, app, "/openapi.json").Body.String(), `"/late"`) {
		t.Fatal("route registered after first request missing from spec")
	}
}

func TestDocsPage(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		docs     string
		wantSpec string
	}{
		{"defaults", Config{Title: "Pet <API>"}, "/docs", `url: "openapi.json"`},
		{"nested", Config{DocsPath: "/api/v1/docs", SpecPath: "/api/v1/spec.json"}, "/api/v1/docs", `url: "../../api/v1/spec.json"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := fetch(t, New(tt.cfg), tt.docs)
			body := rec.Body.String()
			if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
			}
			if !strings.Contains(body, tt.wantSpec) || !strings.Contains(body, scalarURL) {
				t.Fatalf("docs page:\n%s", body)
			}
			if strings.Contains(body, "<API>") {
				t.Fatal("title not escaped")
			}
		})
	}
}

func TestDisabledPaths(t *testing.T) {
	app := New(Config{SpecPath: "-"})
	if fetch(t, app, "/openapi.json").Code != 404 || fetch(t, app, "/docs").Code != 404 {
		t.Fatal("spec and docs should be disabled")
	}
	app = New(Config{DocsPath: "-"})
	if fetch(t, app, "/openapi.json").Code != 200 || fetch(t, app, "/docs").Code != 404 {
		t.Fatal("only docs should be disabled")
	}
	if doc := app.OpenAPI(); doc.Info.Title != "API" || doc.Info.Version != "0.0.0" {
		t.Fatalf("default info = %+v", doc.Info)
	}
}

func TestOperationID(t *testing.T) {
	tests := map[string]string{
		"GET /":                    "get",
		"GET /pets":                "getPets",
		"GET /pets/{id}":           "getPetsById",
		"POST /orgs/{org}/pets":    "postOrgsByOrgPets",
		"PUT /files/{path...}":     "putFilesByPath",
		"DELETE /user-profiles/me": "deleteUserProfilesMe",
		"GET /{$}":                 "get",
	}
	for in, want := range tests {
		method, route, _ := strings.Cut(in, " ")
		if got := operationID(method, route); got != want {
			t.Errorf("operationID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpecPathAndRelURL(t *testing.T) {
	for in, want := range map[string]string{"/files/{path...}": "/files/{path}", "/{$}": "/", "/a/{$}": "/a/", "/pets/{id}": "/pets/{id}"} {
		if got := specPath(in); got != want {
			t.Errorf("specPath(%q) = %q, want %q", in, got, want)
		}
	}
	for _, tt := range [][3]string{{"/docs", "/openapi.json", "openapi.json"}, {"/api/docs", "/api/openapi.json", "../api/openapi.json"}} {
		if got := relURL(tt[0], tt[1]); got != tt[2] {
			t.Errorf("relURL(%q, %q) = %q, want %q", tt[0], tt[1], got, tt[2])
		}
	}
}
