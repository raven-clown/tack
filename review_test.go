package tack

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func panics(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(r.(string), want) {
			t.Fatalf("panic %v, want one containing %q", r, want)
		}
	}()
	f()
}

func TestUnknownBodyRuleFailsAtRegistration(t *testing.T) {
	t.Parallel()
	panics(t, "validate tag", func() {
		Post(New(Config{}), "/x", func(_ context.Context, _ struct {
			Body struct {
				Name string `validate:"requird"`
			}
		}) (struct{}, error) {
			return struct{}{}, nil
		})
	})
}

func TestFormFieldsNeedABodyMethod(t *testing.T) {
	t.Parallel()
	panics(t, "form fields need a request body", func() {
		Get(New(Config{}), "/x", func(_ context.Context, _ struct {
			Caption string `form:"caption"`
		}) (struct{}, error) {
			return struct{}{}, nil
		})
	})
}

func TestUndocumentedStatusWarnsInDev(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	app := New(Config{Dev: true, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	Get(app, "/x", func(context.Context, struct{}) (struct {
		Status int
		Body   string
	}, error) {
		return struct {
			Status int
			Body   string
		}{Status: http.StatusAccepted, Body: "later"}, nil
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusAccepted || !strings.Contains(logs.String(), "undocumented status") || !strings.Contains(logs.String(), "status=202") {
		t.Fatalf("got %d and logs %q", rec.Code, logs.String())
	}
}

func TestEncodeFailureDropsOutputHeaders(t *testing.T) {
	t.Parallel()
	app := New(Config{Logger: slog.New(slog.DiscardHandler)})
	Get(app, "/x", func(context.Context, struct{}) (struct {
		Location string `header:"Location"`
		Body     any
	}, error) {
		return struct {
			Location string `header:"Location"`
			Body     any
		}{Location: "/elsewhere", Body: func() {}}, nil
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Location") != "" || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("got %d %v", rec.Code, rec.Header())
	}
}

// noFlush is a ResponseWriter wrapper that neither flushes nor unwraps.
type noFlush struct{ w http.ResponseWriter }

func (n noFlush) Header() http.Header         { return n.w.Header() }
func (n noFlush) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n noFlush) WriteHeader(status int)      { n.w.WriteHeader(status) }

func TestStreamNeedsAFlusher(t *testing.T) {
	t.Parallel()
	app := New(Config{Logger: slog.New(slog.DiscardHandler)})
	Get(app, "/x", func(context.Context, struct{}) (Stream[int], error) {
		return func(yield func(int) bool) { yield(1) }, nil
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(noFlush{rec}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestStreamItemMustBeEventItself(t *testing.T) {
	t.Parallel()
	type wrapped struct {
		Event[int]
		N int
	}
	if _, err := newOutput(reflect.TypeFor[Stream[wrapped]]()); err == nil || !strings.Contains(err.Error(), "embeds or points to tack.Event") {
		t.Fatalf("embedded Event: %v", err)
	}
	if _, err := newOutput(reflect.TypeFor[Stream[*Event[int]]]()); err == nil || !strings.Contains(err.Error(), "embeds or points to tack.Event") {
		t.Fatalf("pointer to Event: %v", err)
	}
}

func TestServeReturnsNilWhenShutDownElsewhere(t *testing.T) {
	t.Parallel()
	app := New(Config{Logger: slog.New(slog.DiscardHandler)})
	srv := &http.Server{Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Addr = ln.Addr().String()
	_ = ln.Close()
	done := make(chan error, 1)
	go func() { done <- app.Serve(context.Background(), srv) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := net.Dial("tcp", srv.Addr); err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v after an external Shutdown", err)
	}
}

func TestPooledBodyBuffersAreNotRetained(t *testing.T) {
	t.Parallel()
	app := New(Config{})
	var seen []json.RawMessage
	Post(app, "/x", func(_ context.Context, in struct{ Body json.RawMessage }) (int, error) {
		seen = append(seen, in.Body)
		return len(in.Body), nil
	})
	for _, body := range []string{`{"first":1}`, `{"second":22222}`} {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if string(seen[0]) != `{"first":1}` || string(seen[1]) != `{"second":22222}` {
		t.Fatalf("bodies were aliased: %s", seen)
	}
}
