package tack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkEmptyHandler(b *testing.B) {
	b.Run("net/http", func(b *testing.B) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		})
		run(b, mux, "/ping")
	})
	b.Run("tack", func(b *testing.B) {
		app := New(Config{})
		Get(app, "/ping", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })
		run(b, app, "/ping")
	})
	b.Run("tack/path+query+validate", func(b *testing.B) {
		app := New(Config{})
		Get(app, "/pets/{id}", func(_ context.Context, in struct {
			ID    int `path:"id" validate:"min=1"`
			Limit int `query:"limit" default:"10"`
		}) (int, error) {
			return in.ID, nil
		})
		run(b, app, "/pets/7?limit=5")
	})
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

func run(b *testing.B, h http.Handler, url string) {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := &discard{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		h.ServeHTTP(w, req)
	}
	if w.status != 0 && w.status != http.StatusOK {
		b.Fatalf("status %d", w.status)
	}
}
