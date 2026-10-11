package bench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// benchCases are the timed requests: a path parameter, a query with
// validation and a default, a JSON body in and out, and a validation error.
var benchCases = []requestCase{
	{name: "get-by-id", method: http.MethodGet, path: "/pets/7"},
	{name: "list-query", method: http.MethodGet, path: "/pets?kind=cat&limit=10"},
	{name: "create-json", method: http.MethodPost, path: "/pets", body: `{"name":"Milo","kind":"cat"}`},
	{name: "validation-error", method: http.MethodPost, path: "/pets", body: `{"name":"","kind":"fish"}`},
}

// discard is the cheapest ResponseWriter possible, so the numbers measure
// the frameworks rather than httptest.ResponseRecorder.
type discard struct {
	header http.Header
	status int
}

func (d *discard) Header() http.Header         { return d.header }
func (d *discard) Write(p []byte) (int, error) { return len(p), nil }
func (d *discard) WriteHeader(status int)      { d.status = status }

type readerBody struct{ *strings.Reader }

func (readerBody) Close() error { return nil }

func BenchmarkPets(b *testing.B) {
	for _, c := range benchCases {
		for _, f := range frameworks {
			b.Run(c.name+"/"+f.name, func(b *testing.B) {
				h := f.new()
				var r *strings.Reader
				var req *http.Request
				if c.body != "" {
					r = strings.NewReader(c.body)
					req = httptest.NewRequest(c.method, c.path, r)
					req.Header.Set("Content-Type", "application/json")
				} else {
					req = httptest.NewRequest(c.method, c.path, nil)
				}
				w := &discard{header: http.Header{}}
				b.ReportAllocs()
				for b.Loop() {
					clear(w.header)
					if r != nil {
						r.Reset(c.body)
						req.Body = readerBody{r}
					}
					h.ServeHTTP(w, req)
				}
				if w.status/100 != 2 && w.status != http.StatusUnprocessableEntity && w.status != http.StatusBadRequest {
					b.Fatalf("status %d", w.status)
				}
			})
		}
	}
}
