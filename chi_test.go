package tack

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// TestChiMiddleware checks that stock net/http middleware from chi works on
// an App without adapters.
func TestChiMiddleware(t *testing.T) {
	var logs bytes.Buffer
	middleware.DefaultLogger = middleware.RequestLogger(&middleware.DefaultLogFormatter{Logger: log.New(&logs, "", 0), NoColor: true})

	app := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	app.Use(middleware.RequestID, middleware.Logger, middleware.Recoverer)
	Get(app, "/id", func(ctx context.Context, _ struct{}) (string, error) {
		return middleware.GetReqID(ctx), nil
	})
	Get(app, "/panic", func(context.Context, struct{}) (string, error) {
		panic("boom")
	})

	tests := []struct {
		name, url  string
		wantStatus int
		wantLog    string
	}{
		{"request id reaches the handler", "/id", 200, `"GET http://example.com/id HTTP/1.1" from 192.0.2.1:1234 - 200`},
		{"recoverer turns a panic into 500", "/panic", 500, `"GET http://example.com/panic HTTP/1.1" from 192.0.2.1:1234 - 500`},
		{"middleware also wraps unmatched routes", "/missing", 404, "- 404"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			rec := serveOnce(app, "GET", tt.url, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Fatalf("log = %q, want it to contain %q", logs.String(), tt.wantLog)
			}
			if tt.url == "/id" {
				id := strings.Trim(rec.Body.String(), `"`)
				if id == "" || !strings.Contains(logs.String(), id) {
					t.Fatalf("request id %q missing from handler output or log %q", id, logs.String())
				}
			}
		})
	}
}
