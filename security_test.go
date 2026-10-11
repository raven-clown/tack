package tack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raven-clown/tack/openapi"
)

func TestSecuritySchemes(t *testing.T) {
	tests := []struct {
		name string
		got  openapi.SecurityScheme
		want openapi.SecurityScheme
	}{
		{"bearer", Bearer("JWT"), openapi.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}},
		{"bearer no format", Bearer(""), openapi.SecurityScheme{Type: "http", Scheme: "bearer"}},
		{"basic", Basic(), openapi.SecurityScheme{Type: "http", Scheme: "basic"}},
		{"api key", APIKey("query", "key"), openapi.SecurityScheme{Type: "apiKey", In: "query", Name: "key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestSecurityPanics(t *testing.T) {
	nop := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	tests := []struct {
		name string
		do   func()
		want string
	}{
		{"empty name", func() { New(Config{Security: map[string]openapi.SecurityScheme{"": Bearer("")}}) }, "tack: Config.Security: scheme name must not be empty"},
		{"http without scheme", func() { New(Config{Security: map[string]openapi.SecurityScheme{"a": {Type: "http"}}}) }, `scheme "a": http schemes need Scheme`},
		{"api key bad in", func() { New(Config{Security: map[string]openapi.SecurityScheme{"a": APIKey("body", "k")}}) }, `scheme "a": In must be "header", "query", or "cookie", not "body"`},
		{"api key no name", func() { New(Config{Security: map[string]openapi.SecurityScheme{"a": APIKey("header", "")}}) }, `scheme "a": apiKey schemes need Name`},
		{"unsupported type", func() { New(Config{Security: map[string]openapi.SecurityScheme{"a": {Type: "oauth2"}}}) }, `scheme "a": type "oauth2" is not supported`},
		{"undeclared scheme", func() {
			Get(New(Config{}), "/x", nop, Security("bearer"))
		}, `tack: GET /x: Security("bearer") names a scheme that is not in Config.Security; add it there with tack.Bearer, tack.APIKey, or tack.Basic`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.do()
		})
	}
}

func TestSecurityInSpec(t *testing.T) {
	app := New(Config{Security: map[string]openapi.SecurityScheme{"bearer": Bearer("JWT"), "key": APIKey("header", "X-Key")}})
	nop := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	Get(app, "/open", nop)
	Get(app, "/either", nop, Security("bearer", "read", "write"), Security("key"))
	api := app.Group("/api").With(Security("bearer"))
	Get(api, "/me", nop)

	doc := app.OpenAPI()
	if len(doc.Components.SecuritySchemes) != 2 || doc.Components.SecuritySchemes["bearer"].BearerFormat != "JWT" {
		t.Fatalf("schemes = %+v", doc.Components.SecuritySchemes)
	}
	open := doc.Paths["/open"].Get
	if open.Security != nil || open.Responses["401"] != nil {
		t.Fatalf("open route should have no security: %+v", open.Security)
	}
	either := doc.Paths["/either"].Get
	want := []openapi.SecurityRequirement{{"bearer": {"read", "write"}}, {"key": {}}}
	if fmt.Sprint(either.Security) != fmt.Sprint(want) {
		t.Fatalf("security = %v, want %v", either.Security, want)
	}
	if either.Responses["401"].Content["application/problem+json"].Schema.Ref != "#/components/schemas/UnauthorizedProblem" {
		t.Fatalf("401 = %+v", either.Responses["401"])
	}
	me := doc.Paths["/api/me"].Get
	if fmt.Sprint(me.Security) != fmt.Sprint([]openapi.SecurityRequirement{{"bearer": {}}}) || me.Responses["401"] == nil {
		t.Fatalf("group security = %v", me.Security)
	}
	body := fetch(t, app, "/openapi.json").Body.String()
	if !strings.Contains(body, `"security":[{"bearer":["read","write"]},{"key":[]}]`) {
		t.Fatalf("spec JSON: %s", body)
	}
}

func TestResponsesAreNosniff(t *testing.T) {
	app := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	Get(app, "/ok", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })
	Get(app, "/fail", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, errors.New("boom") })
	Get(app, "/events", func(context.Context, struct{}) (Stream[int], error) {
		return func(yield func(int) bool) { yield(1) }, nil
	})
	for _, path := range []string{"/ok", "/fail", "/events", "/openapi.json", "/docs"} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options %q", path, got)
		}
	}
}
