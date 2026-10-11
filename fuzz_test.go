package tack

import (
	"net/http"
	"strings"
	"testing"
	"unicode"

	"github.com/raven-clown/tack/internal/bind"
)

// FuzzSpecPath checks the ServeMux to OpenAPI path conversion and the
// operation ID derived from it on arbitrary routes: every wildcard keeps its
// name without the "..." suffix, {$} disappears, and the ID is an identifier.
func FuzzSpecPath(f *testing.F) {
	for _, s := range []string{"/pets/{id}", "/files/{path...}", "/pets/{$}", "/a-b/c_d/{x}", "/", "", "/{}", "/é/{ü}", "/{...}", "/9", "......}", "{{$}"} {
		f.Add("GET", s)
	}
	f.Fuzz(func(t *testing.T, method, route string) {
		p := specPath(route)
		id := operationID(method, route)
		if !muxAccepts(route) {
			return // the helpers only need to survive what ServeMux rejects
		}
		if strings.Contains(p, "{$}") {
			t.Fatalf("specPath(%q) = %q", route, p)
		}
		for _, name := range bind.PathParams(route) {
			if !strings.Contains(p, "{"+name+"}") {
				t.Fatalf("specPath(%q) = %q lacks {%s}", route, p, name)
			}
		}
		rest, ok := strings.CutPrefix(id, strings.ToLower(method))
		if !ok {
			t.Fatalf("operationID(%q, %q) = %q", method, route, id)
		}
		for _, r := range rest {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				t.Fatalf("operationID(%q, %q) = %q has %q", method, route, id, r)
			}
		}
	})
}

// muxAccepts reports whether http.ServeMux registers a GET route for pattern.
func muxAccepts(pattern string) (ok bool) {
	defer func() { ok = recover() == nil }()
	http.NewServeMux().Handle("GET "+pattern, http.NotFoundHandler())
	return true
}
