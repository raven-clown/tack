package mock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/raven-clown/tack/openapi"
)

func goldenHandler(t *testing.T) http.Handler {
	t.Helper()
	b, err := os.ReadFile("../../testdata/openapi.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	h, err := Handler(&doc)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(h http.Handler, method, url string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler(t *testing.T) {
	h := goldenHandler(t)
	tests := []struct {
		name, method, url string
		headers           []string
		status            int
		contentType       string
		body              string
	}{
		{"list", "GET", "/pets", nil, 200, "application/json",
			`[{"born":"2026-01-02T15:04:05Z","id":7,"kind":"cat","name":"name","nicknames":["nicknames"],"owner":{"email":"user@example.com","name":"name"}}]`},
		{"one", "GET", "/pets/5", nil, 200, "application/json",
			`{"born":"2026-01-02T15:04:05Z","id":7,"kind":"cat","name":"name","nicknames":["nicknames"],"owner":{"email":"user@example.com","name":"name"}}`},
		{"created", "POST", "/admin/animals", nil, 201, "application/json", ""},
		{"no content", "DELETE", "/pets/5", nil, 204, "", ""},
		{"map body", "PUT", "/files/a", nil, 200, "application/json", `{"key":1}`},
		{"preferred error", "GET", "/admin/animals/5", []string{"Prefer", "code=404"}, 404, "application/problem+json",
			`{"code":"animal_not_found","detail":"animal not found","errors":[{"location":"location","message":"message"}],"instance":"instance","status":404,"title":"Not Found","type":"type"}`},
		{"preferred with spaces", "GET", "/admin/animals/5", []string{"Prefer", "respond-async, code = 401"}, 401, "application/problem+json",
			`{"code":"unauthorized","detail":"authentication required","errors":[{"location":"location","message":"message"}],"instance":"instance","status":401,"title":"Unauthorized","type":"type"}`},
		{"undocumented status", "GET", "/pets", []string{"Prefer", "code=418"}, 400, "application/problem+json",
			`{"code":"undocumented_status","detail":"status 418 is not documented for this operation; documented: 200, 400, 422, 500","status":400,"title":"Bad Request","type":"about:blank"}`},
		{"ignored prefer", "GET", "/pets", []string{"Prefer", "wait=10"}, 200, "application/json", ""},
		{"root", "GET", "/", nil, 200, "application/json", ""},
		{"unknown path", "GET", "/nope", nil, 404, "application/problem+json",
			`{"code":"not_found","detail":"no operation for GET /nope in the document","status":404,"title":"Not Found","type":"about:blank"}`},
		{"wrong method", "POST", "/pets/5", nil, 404, "application/problem+json", ""},
		{"spec", "GET", "/openapi.json", nil, 200, "application/json", ""},
		{"trailing slash is exact", "GET", "/pets/", nil, 404, "application/problem+json", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(h, tt.method, tt.url, tt.headers...)
			if rec.Code != tt.status || rec.Header().Get("Content-Type") != tt.contentType {
				t.Fatalf("got %d %q, want %d %q\n%s", rec.Code, rec.Header().Get("Content-Type"), tt.status, tt.contentType, rec.Body)
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Error("missing CORS header")
			}
			if tt.body != "" && strings.TrimSpace(rec.Body.String()) != tt.body {
				t.Fatalf("body:\n%s\nwant:\n%s", rec.Body, tt.body)
			}
		})
	}

	rec := do(h, "POST", "/admin/animals")
	for name, want := range map[string]string{"Location": "Location", "X-Count": "1", "X-Since": "2026-01-02T15:04:05Z", "Warning": "Warning", "ETag": "ETag"} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(do(h, "GET", "/openapi.json").Body.String(), `"openapi":"3.1.0"`) {
		t.Error("spec not served")
	}
	rec = do(h, "OPTIONS", "/pets")
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Header().Get("Access-Control-Allow-Headers") != "*" {
		t.Errorf("preflight: %d %v", rec.Code, rec.Header())
	}
	routes := Routes(h)
	if len(routes) != 9 || routes[0] != "GET /" || routes[len(routes)-1] != "DELETE /pets/{id}" {
		t.Errorf("routes: %v", routes)
	}
	if Routes(http.NotFoundHandler()) != nil {
		t.Error("Routes of another handler should be nil")
	}
}

func TestHandlerErrors(t *testing.T) {
	if _, err := Handler(&openapi.Document{}); err == nil || !strings.Contains(err.Error(), "no paths") {
		t.Fatalf("empty: %v", err)
	}
	doc := &openapi.Document{Paths: map[string]*openapi.PathItem{
		"/a": {Get: &openapi.Operation{Responses: map[string]*openapi.Response{"404": {}, "default": {}}}},
		"/b": nil,
	}}
	if _, err := Handler(doc); err == nil || !strings.Contains(err.Error(), "GET /a: no 2xx or 3xx response") {
		t.Fatalf("no success: %v", err)
	}
}

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }

func TestExample(t *testing.T) {
	m := &mock{doc: &openapi.Document{Components: &openapi.Components{Schemas: map[string]*openapi.Schema{
		"Node": {Type: "object", Properties: map[string]*openapi.Schema{
			"name": {Type: "string"},
			"next": {Ref: "#/components/schemas/Node"},
		}, Required: []string{"name"}},
		"Kind": {Type: "string", Enum: []any{nil, "cat"}},
	}}}}
	tests := []struct {
		name string
		in   *openapi.Schema
		want string
	}{
		{"nil", nil, "null"},
		{"any", &openapi.Schema{}, "null"},
		{"example wins", &openapi.Schema{Type: "integer", Examples: []any{42}, Default: 1}, "42"},
		{"default", &openapi.Schema{Type: "integer", Default: 20}, "20"},
		{"enum", &openapi.Schema{Type: "string", Enum: []any{"a", "b"}}, `"a"`},
		{"enum via ref", &openapi.Schema{Ref: "#/components/schemas/Kind"}, `"cat"`},
		{"null enum", &openapi.Schema{Enum: []any{nil}}, "null"},
		{"string", &openapi.Schema{Type: "string"}, `"string"`},
		{"string min", &openapi.Schema{Type: "string", MinLength: i(10)}, `"stringstring"`},
		{"string max", &openapi.Schema{Type: "string", MaxLength: i(3)}, `"str"`},
		{"date", &openapi.Schema{Type: "string", Format: "date"}, `"2026-01-02"`},
		{"uuid", &openapi.Schema{Type: "string", Format: "uuid"}, `"123e4567-e89b-12d3-a456-426614174000"`},
		{"bytes", &openapi.Schema{Type: "string", ContentEncoding: "base64"}, `"aGVsbG8="`},
		{"integer", &openapi.Schema{Type: "integer"}, "1"},
		{"integer min", &openapi.Schema{Type: "integer", Minimum: f(5)}, "5"},
		{"integer exclusive", &openapi.Schema{Type: "integer", ExclusiveMinimum: f(5), ExclusiveMaximum: f(7)}, "6"},
		{"integer max below one", &openapi.Schema{Type: "integer", Maximum: f(-3)}, "-3"},
		{"integer exclusive max", &openapi.Schema{Type: "integer", ExclusiveMaximum: f(1)}, "0"},
		{"number", &openapi.Schema{Type: "number", Minimum: f(0.5)}, "0.5"},
		{"boolean", &openapi.Schema{Type: "boolean"}, "true"},
		{"null", &openapi.Schema{Type: "null"}, "null"},
		{"array", &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "integer"}}, "[1]"},
		{"array min", &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "boolean"}, MinItems: i(2)}, "[true,true]"},
		{"array empty", &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "boolean"}, MaxItems: i(0)}, "[]"},
		{"map", &openapi.Schema{Type: "object", AdditionalProperties: &openapi.Schema{Type: "string"}}, `{"key":"key"}`},
		{"empty object", &openapi.Schema{Type: "object"}, "{}"},
		{"object", &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{"b": {Type: "string"}, "a": {Type: "integer"}}}, `{"a":1,"b":"b"}`},
		{"allOf", &openapi.Schema{AllOf: []*openapi.Schema{
			{Type: "object", Properties: map[string]*openapi.Schema{"a": {Type: "integer"}}},
			{Type: "object", Properties: map[string]*openapi.Schema{"a": {Type: "integer", Examples: []any{2}}, "b": {Type: "string"}}},
		}}, `{"a":2,"b":"b"}`},
		{"oneOf", &openapi.Schema{OneOf: []*openapi.Schema{{Type: "boolean"}, {Type: "string"}}}, "true"},
		{"anyOf", &openapi.Schema{AnyOf: []*openapi.Schema{{Type: "null"}, {Type: "string"}}}, "null"},
		{"recursive", &openapi.Schema{Ref: "#/components/schemas/Node"},
			`{"name":"name","next":{"name":"name","next":{"name":"name"}}}`},
		{"missing ref", &openapi.Schema{Ref: "#/components/schemas/Nope"}, "null"},
		{"unknown type", &openapi.Schema{Type: "file"}, "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(m.example(tt.in, "", 0))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("got %s, want %s", b, tt.want)
			}
		})
	}
	if v := headerValue([]any{1, "a", true}); v != "1, a, true" {
		t.Errorf("headerValue: %q", v)
	}
	if preferredCode("code=99") != 0 || preferredCode("code=abc") != 0 || preferredCode("") != 0 {
		t.Error("preferredCode should ignore invalid codes")
	}
}
