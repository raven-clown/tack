package bench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// frameworks lists every variant in the order the tables use.
var frameworks = []struct {
	name string
	new  func() http.Handler
}{
	{"net/http", NewNetHTTP},
	{"gin", NewGin},
	{"echo", NewEcho},
	{"huma", NewHuma},
	{"tack", NewTack},
}

// requestCase is one request every variant must answer the same way, up to
// each framework's convention for validation errors (400 or 422).
type requestCase struct {
	name, method, path, body string
	want                     map[string]int // framework -> status
	mentions                 []string       // substrings the error body must contain, case-insensitively
}

func all(status int) map[string]int {
	m := map[string]int{}
	for _, f := range frameworks {
		m[f.name] = status
	}
	return m
}

func validationError(exceptions map[string]int) map[string]int {
	m := all(http.StatusUnprocessableEntity)
	for k, v := range exceptions {
		m[k] = v
	}
	return m
}

var cases = []requestCase{
	{name: "get-by-id", method: http.MethodGet, path: "/pets/7", want: all(http.StatusOK), mentions: []string{`"Nala"`}},
	{name: "list-query", method: http.MethodGet, path: "/pets?kind=cat&limit=10", want: all(http.StatusOK), mentions: []string{`"Milo"`, `"Cleo"`}},
	{name: "list-default-limit", method: http.MethodGet, path: "/pets", want: all(http.StatusOK), mentions: []string{`"Sky"`}},
	{name: "create-json", method: http.MethodPost, path: "/pets", body: `{"name":"Milo","kind":"cat"}`, want: all(http.StatusCreated), mentions: []string{`"id":11`}},
	{name: "update-json", method: http.MethodPut, path: "/pets/3", body: `{"name":"Max","kind":"dog"}`, want: all(http.StatusOK), mentions: []string{`"id":3`}},
	{name: "validation-error", method: http.MethodPost, path: "/pets", body: `{"name":"","kind":"fish"}`, want: validationError(map[string]int{"gin": http.StatusBadRequest}), mentions: []string{"name", "kind"}},
	{name: "limit-too-large", method: http.MethodGet, path: "/pets?limit=500", want: validationError(map[string]int{"gin": http.StatusBadRequest}), mentions: []string{"limit"}},
	{name: "bad-kind", method: http.MethodGet, path: "/pets?kind=fish", want: validationError(map[string]int{"gin": http.StatusBadRequest}), mentions: []string{"kind"}},
	{name: "not-found", method: http.MethodGet, path: "/pets/999", want: all(http.StatusNotFound)},
	{name: "update-not-found", method: http.MethodPut, path: "/pets/999", body: `{"name":"Max","kind":"dog"}`, want: all(http.StatusNotFound)},
	{name: "non-integer-id", method: http.MethodGet, path: "/pets/abc", want: validationError(map[string]int{"net/http": 400, "gin": 400, "echo": 400, "tack": 400})},
	{name: "malformed-json", method: http.MethodPost, path: "/pets", body: `{"name":`, want: all(http.StatusBadRequest)},
}

func send(h http.Handler, c requestCase) *httptest.ResponseRecorder {
	var req *http.Request
	if c.body != "" {
		req = httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(c.method, c.path, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestEquivalence is the fairness check: every variant enforces the same
// rules on the same requests before any of them is timed.
func TestEquivalence(t *testing.T) {
	for _, f := range frameworks {
		h := f.new()
		for _, c := range cases {
			t.Run(f.name+"/"+c.name, func(t *testing.T) {
				rec := send(h, c)
				if rec.Code != c.want[f.name] {
					t.Fatalf("status %d, want %d; body %s", rec.Code, c.want[f.name], rec.Body)
				}
				body := strings.ToLower(rec.Body.String())
				for _, m := range c.mentions {
					if !strings.Contains(body, strings.ToLower(m)) {
						t.Errorf("body lacks %q: %s", m, rec.Body)
					}
				}
			})
		}
	}
}
