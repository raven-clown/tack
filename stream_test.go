package tack

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type tick struct {
	N int `json:"n"`
}

type badJSON struct{}

func (badJSON) MarshalJSON() ([]byte, error) { return nil, errors.New("nope") }

func streamApp(t *testing.T) (*App, chan struct{}) {
	t.Helper()
	ended := make(chan struct{}, 1)
	app := New(Config{})
	Get(app, "/ticks", func(_ context.Context, in struct {
		Count int `query:"count" default:"3"`
	}) (Stream[tick], error) {
		return func(yield func(tick) bool) {
			for i := range in.Count {
				if !yield(tick{N: i}) {
					return
				}
			}
		}, nil
	})
	Get(app, "/named", func(context.Context, struct{}) (Stream[Event[tick]], error) {
		return func(yield func(Event[tick]) bool) {
			_ = yield(Event[tick]{Name: "tick", ID: "1", Data: tick{N: 1}}) &&
				yield(Event[tick]{Name: "multi\nline", ID: "a\r\nb", Data: tick{N: 2}}) &&
				yield(Event[tick]{Data: tick{N: 3}})
		}, nil
	})
	Get(app, "/forever", func(ctx context.Context, _ struct{}) (Stream[tick], error) {
		return func(yield func(tick) bool) {
			defer func() { ended <- struct{}{} }()
			for i := 0; yield(tick{N: i}); i++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}, nil
	})
	Get(app, "/nil", func(context.Context, struct{}) (Stream[tick], error) { return nil, nil })
	Get(app, "/fail", func(context.Context, struct{}) (Stream[tick], error) {
		return nil, NewError(http.StatusNotFound, "no such stream")
	})
	Get(app, "/bad", func(context.Context, struct{}) (Stream[badJSON], error) {
		return func(yield func(badJSON) bool) { yield(badJSON{}); yield(badJSON{}) }, nil
	})
	return app, ended
}

func TestStream(t *testing.T) {
	app, _ := streamApp(t)
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}
	tests := []struct {
		url, contentType, body string
		status                 int
	}{
		{"/ticks?count=2", "text/event-stream", "data: {\"n\":0}\n\ndata: {\"n\":1}\n\n", 200},
		{"/ticks?count=0", "text/event-stream", "", 200},
		{"/named", "text/event-stream", "event: tick\nid: 1\ndata: {\"n\":1}\n\nevent: multi line\nid: a b\ndata: {\"n\":2}\n\ndata: {\"n\":3}\n\n", 200},
		{"/nil", "application/problem+json", `"code":"internal"`, 500},
		{"/fail", "application/problem+json", `"detail":"no such stream"`, 404},
		{"/bad", "text/event-stream", "event: error\ndata: \"encoding event: json: error calling MarshalJSON for type tack.badJSON: nope\"\n\n", 200},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			rec := get(tt.url)
			if rec.Code != tt.status || rec.Header().Get("Content-Type") != tt.contentType || !strings.Contains(rec.Body.String(), tt.body) {
				t.Fatalf("got %d %q\n%s\nwant %d %q containing %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body, tt.status, tt.contentType, tt.body)
			}
		})
	}
	rec := get("/ticks")
	if rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("X-Accel-Buffering") != "no" || !rec.Flushed {
		t.Errorf("stream headers: %v, flushed %v", rec.Header(), rec.Flushed)
	}
}

func TestStreamStopsWhenClientLeaves(t *testing.T) {
	app, ended := streamApp(t)
	srv := httptest.NewServer(app)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/forever", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil || line != "data: {\"n\":0}\n" {
		t.Fatalf("first line %q, %v", line, err)
	}
	cancel()
	res.Body.Close()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler kept running after the client left")
	}
}

func TestStreamRegistration(t *testing.T) {
	out, err := newOutput(reflect.TypeFor[Stream[Event[tick]]]())
	if err != nil || !out.stream || out.typ != reflect.TypeFor[tick]() {
		t.Fatalf("Stream[Event[tick]]: %+v, %v", out, err)
	}
	out, err = newOutput(reflect.TypeFor[Stream[[]tick]]())
	if err != nil || !out.stream || out.typ != reflect.TypeFor[[]tick]() {
		t.Fatalf("Stream[[]tick]: %+v, %v", out, err)
	}
	tests := []struct {
		name string
		reg  func(*App)
		want string
	}{
		{"stream in wrapper", func(app *App) {
			Get(app, "/a", func(context.Context, struct{}) (struct {
				Location string `header:"Location"`
				Body     Stream[tick]
			}, error) {
				return struct {
					Location string `header:"Location"`
					Body     Stream[tick]
				}{}, nil
			})
		}, "Body is a Stream; return the Stream itself"},
		{"stream with 204", func(app *App) {
			Get(app, "/a", func(context.Context, struct{}) (Stream[tick], error) { return nil, nil }, Status(http.StatusNoContent))
		}, "Status(204) sends no body, but the handler returns tack.Stream["},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(r.(string), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.reg(New(Config{}))
		})
	}
}
