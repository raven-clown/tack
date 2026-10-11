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

func run(b *testing.B, h http.Handler, url string) {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
