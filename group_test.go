package tack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ctxKey struct{}

// tag appends name to a trail stored in the request context.
func tag(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			trail, _ := r.Context().Value(ctxKey{}).(string)
			ctx := context.WithValue(r.Context(), ctxKey{}, trail+name+" ")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func trailHandler(context.Context, struct{}) (string, error) { return "", nil }

func TestMiddlewareOrder(t *testing.T) {
	app := New(Config{})
	app.Use(tag("app1"), tag("app2"))
	v1 := app.Group("/v1", tag("outer"))
	admin := v1.Group("/admin", tag("inner"))
	Get(admin, "/users", func(ctx context.Context, _ struct{}) (string, error) {
		trail, _ := ContextValue[string](ctx, ctxKey{})
		return trail + "handler", nil
	}, Middleware(tag("route1"), tag("route2")))

	rec := serveOnce(app, "GET", "/v1/admin/users", "")
	want := `"app1 app2 outer inner route1 route2 handler"`
	if rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("got %d %s, want %s", rec.Code, rec.Body, want)
	}
}

func TestGroupPrefixes(t *testing.T) {
	app := New(Config{})
	v1 := app.Group("/v1")
	orgs := v1.Group("/orgs/{org}")
	plain := app.Group("")
	echo := func(_ context.Context, in struct {
		Org string `path:"org"`
	}) (string, error) {
		return in.Org, nil
	}
	Get(v1, "", trailHandler)
	Get(v1, "/{$}", trailHandler, OperationID("v1Root"))
	Get(v1, "/pets", trailHandler, OperationID("v1Pets"))
	Get(orgs, "/name", echo)
	Get(plain, "/health", trailHandler)

	tests := []struct {
		url  string
		want int
		body string
	}{
		{"/v1", 200, `""`},
		{"/v1/", 200, `""`},
		{"/v1/pets", 200, `""`},
		{"/v1/orgs/acme/name", 200, `"acme"`},
		{"/health", 200, `""`},
		{"/pets", 404, ""},
		{"/v1/other", 404, ""},
	}
	for _, tt := range tests {
		rec := serveOnce(app, "GET", tt.url, "")
		if rec.Code != tt.want || (tt.body != "" && rec.Body.String() != tt.body) {
			t.Errorf("%s: got %d %s, want %d %s", tt.url, rec.Code, rec.Body, tt.want, tt.body)
		}
	}
	for _, p := range []string{"/v1", "/v1/", "/v1/pets", "/v1/orgs/{org}/name", "/health"} {
		if app.OpenAPI().Paths[p] == nil {
			t.Errorf("spec is missing %s", p)
		}
	}
}

func TestGroupUseAfterNesting(t *testing.T) {
	app := New(Config{})
	v1 := app.Group("/v1")
	admin := v1.Group("/admin")
	v1.Use(tag("late"))
	Get(admin, "/x", func(ctx context.Context, _ struct{}) (string, error) {
		trail, _ := ContextValue[string](ctx, ctxKey{})
		return trail, nil
	})
	if got := serveOnce(app, "GET", "/v1/admin/x", "").Body.String(); got != `"late "` {
		t.Fatalf("got %s", got)
	}
}

func TestGroupWithOptions(t *testing.T) {
	app := New(Config{})
	pets := app.Group("/pets").With(Tags("pets"), Deprecated())
	Get(pets, "", trailHandler, Tags("public"))
	op := app.OpenAPI().Paths["/pets"].Get
	if fmt.Sprint(op.Tags) != "[pets public]" || !op.Deprecated {
		t.Fatalf("tags %v deprecated %v", op.Tags, op.Deprecated)
	}
}

func TestAppUseWrapsEverything(t *testing.T) {
	app := New(Config{})
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Seen", "1")
			next.ServeHTTP(w, r)
		})
	})
	Get(app, "/ping", trailHandler)
	for _, url := range []string{"/ping", "/docs", "/openapi.json", "/missing"} {
		if rec := serveOnce(app, "GET", url, ""); rec.Header().Get("X-Seen") != "1" {
			t.Errorf("%s: middleware did not run", url)
		}
	}
	app.Use(tag("second"))
	if rec := serveOnce(app, "GET", "/ping", ""); rec.Header().Get("X-Seen") != "1" {
		t.Error("first middleware lost after second Use")
	}
}

func TestGroupPanics(t *testing.T) {
	tests := []struct {
		name string
		do   func(a *App)
		want string
	}{
		{"no leading slash", func(a *App) { a.Group("v1") }, `tack: group prefix "v1" must start with "/" and not end with one`},
		{"trailing slash", func(a *App) { a.Group("/v1/") }, `tack: group prefix "/v1/" must start`},
		{"nested bad prefix", func(a *App) { a.Group("/v1").Group("admin") }, `tack: group prefix "admin"`},
		{"slash route under group", func(a *App) { Get(a.Group("/v1"), "/", trailHandler) },
			`tack: GET /v1/: path "/" under a group matches every path below /v1/; use "/{$}" for /v1/ alone or "" for /v1`},
		{"missing path field in prefix", func(a *App) { Get(a.Group("/orgs/{org}"), "/pets", trailHandler) },
			"tack: GET /orgs/{org}/pets: path parameter {org} has no field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.do(New(Config{}))
		})
	}
}

func TestContextValue(t *testing.T) {
	type userKey struct{}
	ctx := context.WithValue(context.Background(), userKey{}, 42)
	tests := []struct {
		name string
		got  func() (any, bool)
		want any
		ok   bool
	}{
		{"present", func() (any, bool) { v, ok := ContextValue[int](ctx, userKey{}); return v, ok }, 42, true},
		{"wrong type", func() (any, bool) { v, ok := ContextValue[string](ctx, userKey{}); return v, ok }, "", false},
		{"absent", func() (any, bool) { v, ok := ContextValue[int](context.Background(), userKey{}); return v, ok }, 0, false},
		{"interface", func() (any, bool) { v, ok := ContextValue[any](ctx, userKey{}); return v, ok }, 42, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.got()
			if got != tt.want || ok != tt.ok {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestChainEmpty(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	rec := httptest.NewRecorder()
	chain(nil, h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusCreated {
		t.Fatal("empty chain should return h")
	}
}
