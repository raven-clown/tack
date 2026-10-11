package tack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type Pet struct {
	ID   int      `json:"id"`
	Name string   `json:"name" validate:"required,max=10"`
	Tags []string `json:"tags,omitempty" validate:"max=2,dive,min=2"`
}

type getPetInput struct {
	ID int `path:"id" validate:"min=1"`
}

type createPetInput struct {
	Org  string `path:"org"`
	Body Pet
}

type listInput struct {
	Limit int      `query:"limit" default:"10" validate:"min=1,max=100"`
	Kind  string   `query:"kind" validate:"omitempty,oneof=cat dog"`
	Token string   `header:"X-Token" validate:"required"`
	Pets  []string `query:"pet"`
}

type notFoundError struct{}

func (notFoundError) Error() string   { return "pet is gone" }
func (notFoundError) HTTPStatus() int { return http.StatusNotFound }

type unavailableError struct{}

func (unavailableError) Error() string   { return "db down: secret" }
func (unavailableError) HTTPStatus() int { return http.StatusServiceUnavailable }

func newTestApp(t *testing.T, logs *bytes.Buffer) *App {
	t.Helper()
	app := New(Config{Title: "test", Version: "0.0.0", MaxBodyBytes: 256, Logger: slog.New(slog.NewTextHandler(logs, nil))})

	Get(app, "/pets/{id}", func(_ context.Context, in getPetInput) (Pet, error) {
		switch in.ID {
		case 404:
			return Pet{}, NewError(http.StatusNotFound, "pet not found")
		case 410:
			return Pet{}, fmt.Errorf("lookup: %w", notFoundError{})
		case 503:
			return Pet{}, unavailableError{}
		case 500:
			return Pet{}, errors.New("database password leaked")
		case 501:
			return Pet{}, NewError(http.StatusNotImplemented, "not yet")
		case 299:
			return Pet{}, &Error{Status: 299, Detail: "bad status"}
		}
		return Pet{ID: in.ID, Name: "Milo"}, nil
	})
	Post(app, "/orgs/{org}/pets", func(_ context.Context, in createPetInput) (Pet, error) {
		in.Body.ID = len(in.Org)
		return in.Body, nil
	}, Status(http.StatusCreated))
	Get(app, "/pets", func(_ context.Context, in listInput) (map[string]any, error) {
		return map[string]any{"limit": in.Limit, "kind": in.Kind, "pets": in.Pets}, nil
	})
	Put(app, "/pets/{id}", func(_ context.Context, in struct {
		ID   int `path:"id"`
		Body Pet
	}) (Pet, error) {
		in.Body.ID = in.ID
		return in.Body, nil
	})
	Patch(app, "/pets/{id}", func(_ context.Context, in struct {
		ID   int `path:"id"`
		Body *Pet
	}) (bool, error) {
		return in.Body != nil, nil
	})
	Delete(app, "/pets/{id}", func(context.Context, getPetInput) (struct{}, error) {
		return struct{}{}, nil
	}, Status(http.StatusNoContent))
	Get(app, "/bad-output", func(context.Context, struct{}) (any, error) {
		return func() {}, nil
	})
	return app
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name, method, url, body string
		header                  map[string]string
		wantStatus              int
		wantType                string
		wantBody                string
	}{
		{"get", "GET", "/pets/7", "", nil, 200, "application/json", `{"id":7,"name":"Milo"}`},
		{"post status", "POST", "/orgs/acme/pets", `{"name":"Rex","extra":true}`, nil, 201, "application/json", `{"id":4,"name":"Rex"}`},
		{"put", "PUT", "/pets/3", `{"name":"Rex"}`, nil, 200, "application/json", `{"id":3,"name":"Rex"}`},
		{"patch optional body empty", "PATCH", "/pets/3", "", nil, 200, "application/json", `false`},
		{"patch optional body", "PATCH", "/pets/3", `{"name":"Rex"}`, nil, 200, "application/json", `true`},
		{"delete no content", "DELETE", "/pets/3", "", nil, 204, "", ``},
		{"query defaults", "GET", "/pets?pet=a&pet=b", "", map[string]string{"X-Token": "t"}, 200, "application/json", `{"kind":"","limit":10,"pets":["a","b"]}`},

		{"bad path type", "GET", "/pets/abc", "", nil, 400, "application/problem+json",
			`{"type":"about:blank","title":"Bad Request","status":400,"detail":"malformed request","errors":[{"location":"path.id","message":"must be an integer"}]}`},
		{"path validation", "GET", "/pets/0", "", nil, 422, "application/problem+json",
			`{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"path.id","message":"must be at least 1"}]}`},
		{"all validation errors", "GET", "/pets?limit=500&kind=fish", "", nil, 422, "application/problem+json",
			`{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"query.limit","message":"must be at most 100"},{"location":"query.kind","message":"must be one of: cat, dog"},{"location":"header.X-Token","message":"is required"}]}`},
		{"body validation", "POST", "/orgs/acme/pets", `{"name":"","tags":["a","bb","cc"]}`, nil, 422, "application/problem+json",
			`{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"body.name","message":"is required"},{"location":"body.tags","message":"must have at most 2 items"}]}`},
		{"nested body validation", "POST", "/orgs/acme/pets", `{"name":"Rex","tags":["a"]}`, nil, 422, "application/problem+json",
			`{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"body.tags[0]","message":"must be at least 2 characters long"}]}`},
		{"malformed json", "POST", "/orgs/acme/pets", `{"name":`, nil, 400, "application/problem+json",
			`{"type":"about:blank","title":"Bad Request","status":400,"detail":"malformed request","errors":[{"location":"body","message":"is not valid JSON"}]}`},
		{"empty body", "POST", "/orgs/acme/pets", ``, nil, 422, "application/problem+json",
			`{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"location":"body","message":"is required"}]}`},
		{"body too large", "POST", "/orgs/acme/pets", `{"name":"` + strings.Repeat("x", 300) + `"}`, nil, 413, "application/problem+json",
			`{"type":"about:blank","title":"Request Entity Too Large","status":413,"detail":"request body exceeds 256 bytes"}`},

		{"tack error", "GET", "/pets/404", "", nil, 404, "application/problem+json",
			`{"type":"about:blank","title":"Not Found","status":404,"detail":"pet not found"}`},
		{"domain error", "GET", "/pets/410", "", nil, 404, "application/problem+json",
			`{"type":"about:blank","title":"Not Found","status":404,"detail":"pet is gone"}`},
		{"domain 5xx hides detail", "GET", "/pets/503", "", nil, 503, "application/problem+json",
			`{"type":"about:blank","title":"Service Unavailable","status":503}`},
		{"unknown error hides detail", "GET", "/pets/500", "", nil, 500, "application/problem+json",
			`{"type":"about:blank","title":"Internal Server Error","status":500}`},
		{"tack 5xx keeps detail", "GET", "/pets/501", "", nil, 501, "application/problem+json",
			`{"type":"about:blank","title":"Not Implemented","status":501,"detail":"not yet"}`},
		{"invalid error status", "GET", "/pets/299", "", nil, 500, "application/problem+json",
			`{"type":"about:blank","title":"Internal Server Error","status":500}`},
		{"unencodable output", "GET", "/bad-output", "", nil, 500, "application/problem+json",
			`{"type":"about:blank","title":"Internal Server Error","status":500}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			app := newTestApp(t, &logs)
			req := httptest.NewRequest(tt.method, tt.url, strings.NewReader(tt.body))
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body =\n%s\nwant\n%s", got, tt.wantBody)
			}
			if tt.wantStatus >= 500 && !strings.Contains(logs.String(), "tack: handler error") {
				t.Errorf("5xx not logged: %q", logs.String())
			}
			if tt.wantStatus < 500 && logs.Len() > 0 {
				t.Errorf("unexpected log: %q", logs.String())
			}
		})
	}
}

func TestRegistrationPanics(t *testing.T) {
	type idInput struct {
		ID int `path:"id"`
	}
	h := func(context.Context, idInput) (struct{}, error) { return struct{}{}, nil }
	h2 := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	tests := []struct {
		name     string
		register func(a *App)
		want     string
	}{
		{"missing path field", func(a *App) {
			Get(a, "/pets/{petID}", h)
		}, "tack: GET /pets/{petID}: path parameter {petID} has no field"},
		{"untagged field", func(a *App) {
			Post(a, "/pets", func(context.Context, struct{ Name string }) (struct{}, error) { return struct{}{}, nil })
		}, "tack: POST /pets: field Name has no source tag"},
		{"non-struct input", func(a *App) {
			Get(a, "/pets", func(context.Context, int) (struct{}, error) { return struct{}{}, nil })
		}, "must be a struct"},
		{"bad status", func(a *App) {
			Get(a, "/pets/{id}", h, Status(404))
		}, "tack: GET /pets/{id}: Status(404) must be a 2xx or 3xx code"},
		{"unsupported output", func(a *App) {
			Get(a, "/f", func(context.Context, struct{}) (func(), error) { return nil, nil })
		}, "tack: GET /f: output type func(): type func() is not supported"},
		{"duplicate operation ID", func(a *App) {
			Get(a, "/a", h2, OperationID("same"))
			Get(a, "/b", h2, OperationID("same"))
		}, `tack: GET /b: operation ID "same" is already used by GET /a`},
		{"nil handler", func(a *App) {
			Get[idInput, struct{}](a, "/pets/{id}", nil)
		}, "tack: GET /pets/{id}: handler is nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.register(New(Config{}))
		})
	}
}

func TestDefaults(t *testing.T) {
	app := New(Config{})
	if app.maxBody != defaultMaxBodyBytes || app.logger != slog.Default() {
		t.Fatalf("maxBody = %d, logger default = %v", app.maxBody, app.logger == slog.Default())
	}
}

func TestErrorString(t *testing.T) {
	if got, want := NewError(404, "pet not found").Error(), "404 Not Found: pet not found"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestCustomProblemFields(t *testing.T) {
	app := New(Config{})
	Get(app, "/x", func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, &Error{Type: "https://example.com/conflict", Title: "Conflict", Status: 409, Instance: "/x/1",
			Errors: []ErrorDetail{{Location: "body.name", Message: "is taken"}}}
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	var got Error
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "https://example.com/conflict" || got.Instance != "/x/1" || len(got.Errors) != 1 || rec.Code != 409 {
		t.Fatalf("got %+v (status %d)", got, rec.Code)
	}
}

func TestPointerDefaultIsolation(t *testing.T) {
	app := New(Config{})
	Get(app, "/n", func(_ context.Context, in struct {
		N *int `query:"n" default:"1"`
	}) (int, error) {
		*in.N++
		return *in.N, nil
	})
	for range 2 {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/n", nil))
		if b, _ := io.ReadAll(rec.Body); string(b) != "2" {
			t.Fatalf("body = %s, want 2", b)
		}
	}
}
