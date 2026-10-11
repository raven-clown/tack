package tack

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func BenchmarkEmptyHandler(b *testing.B) {
	b.Run("net/http", func(b *testing.B) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		})
		run(b, mux, http.MethodGet, "/ping", "")
	})
	b.Run("tack", func(b *testing.B) {
		app := New(Config{})
		Get(app, "/ping", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })
		run(b, app, http.MethodGet, "/ping", "")
	})
	b.Run("tack/path+query+validate", func(b *testing.B) {
		run(b, benchApp(), http.MethodGet, "/pets/7?limit=5", "")
	})
	b.Run("tack/json-body", func(b *testing.B) {
		run(b, benchApp(), http.MethodPost, "/pets", `{"name":"Milo","age":3}`)
	})
	b.Run("tack/validation-error", func(b *testing.B) {
		run(b, benchApp(), http.MethodGet, "/pets/0?limit=5", "")
	})
}

type benchPet struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// benchApp has one route per benchmarked shape.
func benchApp() *App {
	app := New(Config{Logger: slog.New(slog.DiscardHandler)})
	Get(app, "/pets/{id}", func(_ context.Context, in struct {
		ID    int `path:"id" validate:"min=1"`
		Limit int `query:"limit" default:"10"`
	}) (int, error) {
		return in.ID, nil
	})
	Post(app, "/pets", func(_ context.Context, in struct{ Body benchPet }) (benchPet, error) {
		return in.Body, nil
	}, Status(http.StatusCreated))
	return app
}

// discard is the cheapest ResponseWriter possible, so the benchmarks measure
// the handlers rather than httptest.ResponseRecorder.
type discard struct {
	header http.Header
	status int
}

func (d *discard) Header() http.Header         { return d.header }
func (d *discard) Write(p []byte) (int, error) { return len(p), nil }
func (d *discard) WriteHeader(status int)      { d.status = status }

// request builds the benchmark request; a body is rewound for every call.
func request(method, url, body string) (*http.Request, func()) {
	if body == "" {
		return httptest.NewRequest(method, url, nil), func() {}
	}
	r := strings.NewReader(body)
	req := httptest.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	return req, func() {
		r.Reset(body)
		req.Body = readerBody{r}
	}
}

// readerBody keeps the body a plain reader so MaxBytesReader wraps only it.
type readerBody struct{ *strings.Reader }

func (readerBody) Close() error { return nil }

func run(b *testing.B, h http.Handler, method, url, body string) {
	req, rewind := request(method, url, body)
	w := &discard{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		rewind()
		h.ServeHTTP(w, req)
	}
	if w.status != 0 && w.status/100 != 2 && w.status != http.StatusUnprocessableEntity {
		b.Fatalf("status %d", w.status)
	}
}

// TestAllocations holds the request path to its allocation budget. CI runs
// it in its own step without the race detector, which allocates on its own.
func TestAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under the race detector")
	}
	app := benchApp()
	empty := New(Config{})
	Get(empty, "/ping", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })
	tests := []struct {
		name, method, url, body string
		app                     *App
		max                     float64
	}{
		{"empty", http.MethodGet, "/ping", "", empty, 0},
		{"path+query+validate", http.MethodGet, "/pets/7?limit=5", "", app, 1},
		{"json-body", http.MethodPost, "/pets", `{"name":"Milo","age":3}`, app, 3},
		{"validation-error", http.MethodGet, "/pets/0?limit=5", "", app, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, rewind := request(tt.method, tt.url, tt.body)
			w := &discard{header: http.Header{}}
			n := testing.AllocsPerRun(200, func() {
				clear(w.header)
				rewind()
				tt.app.ServeHTTP(w, req)
			})
			if n > tt.max {
				t.Errorf("%.0f allocations per request, budget %.0f", n, tt.max)
			}
		})
	}
}
