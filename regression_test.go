package tack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type Item struct {
	Name string `json:"name" validate:"required,max=3"`
}

type Generic[T any] struct {
	ID   int `path:"id" validate:"min=1"`
	Body T
}

func serveOnce(app *App, method, url, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestValidationLocations(t *testing.T) {
	app := New(Config{})
	Post(app, "/anon/{id}", func(_ context.Context, in struct {
		ID   int `path:"id" validate:"min=1"`
		Body Item
	}) (Item, error) {
		return in.Body, nil
	})
	Post(app, "/generic/{id}", func(_ context.Context, in Generic[Item]) (Item, error) {
		return in.Body, nil
	})
	Post(app, "/list", func(_ context.Context, in struct {
		Body []Item `validate:"dive"`
	}) (int, error) {
		return len(in.Body), nil
	})

	want := `"errors":[{"location":"path.id","message":"must be at least 1"},{"location":"body.name","message":"is required"}]`
	for _, url := range []string{"/anon/0", "/generic/0"} {
		if got := serveOnce(app, "POST", url, `{}`).Body.String(); !strings.Contains(got, want) {
			t.Errorf("%s: %s", url, got)
		}
	}
	got := serveOnce(app, "POST", "/list", `[{"name":"ok"},{"name":"toolong"}]`).Body.String()
	if !strings.Contains(got, `{"location":"body[1].name","message":"must be at most 3 characters long"}`) {
		t.Errorf("list: %s", got)
	}
}

func TestValidationErrorCap(t *testing.T) {
	app := New(Config{MaxBodyBytes: 1 << 20})
	Post(app, "/list", func(context.Context, struct {
		Body []Item `validate:"dive"`
	}) (int, error) {
		return 0, nil
	})
	body := "[" + strings.TrimSuffix(strings.Repeat(`{},`, 80), ",") + "]"
	got := serveOnce(app, "POST", "/list", body).Body.String()
	if n := strings.Count(got, `"location"`); n != maxErrors+1 || !strings.Contains(got, "and 30 more errors") {
		t.Fatalf("got %d details: %s", n, got)
	}
}

func TestUnsupportedMediaType(t *testing.T) {
	app := New(Config{})
	Post(app, "/items", func(_ context.Context, in struct{ Body Item }) (Item, error) { return in.Body, nil })
	if rec := serveOnce(app, "POST", "/items", `{"name":"a"}`, "Content-Type", "text/plain"); rec.Code != 415 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if rec := serveOnce(app, "POST", "/items", `{"name":"a"}`, "Content-Type", "application/json"); rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
}

func TestResetContent(t *testing.T) {
	app := New(Config{})
	Post(app, "/reset", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }, Status(http.StatusResetContent))
	if rec := serveOnce(app, "POST", "/reset", ""); rec.Code != 205 || rec.Body.Len() != 0 {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

func TestMoreRegistrationPanics(t *testing.T) {
	nop := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	tests := []struct {
		name     string
		register func(a *App)
		want     string
	}{
		{"no leading slash", func(a *App) { Get(a, "pets", nop) }, `tack: GET pets: path must start with "/"`},
		{"partial wildcard", func(a *App) {
			Get(a, "/pets/x{id}", func(context.Context, struct {
				ID int `path:"id"`
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "tack: GET /pets/x{id}:"},
		{"duplicate route", func(a *App) {
			Get(a, "/dup", nop, OperationID("a"))
			Get(a, "/dup", nop, OperationID("b"))
		}, "tack: GET /dup:"},
		{"spec path clash", func(a *App) { Get(a, "/openapi.json", nop) }, "tack: GET /openapi.json:"},
		{"missing dive", func(a *App) {
			Post(a, "/list", func(context.Context, struct{ Body []Item }) (struct{}, error) { return struct{}{}, nil })
		}, `field Body: elements of []tack.Item have validate rules that are skipped without dive; add validate:"dive" to Body`},
		{"nested missing dive", func(a *App) {
			Post(a, "/nested", func(context.Context, struct {
				Body struct {
					Items []Item `json:"items"`
				}
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "field Body.Items: elements of []tack.Item"},
		{"body on no content", func(a *App) {
			Delete(a, "/x", func(context.Context, struct{}) (Item, error) { return Item{}, nil }, Status(http.StatusNoContent))
		}, "Status(204) sends no body, but the handler returns tack.Item; return struct{} instead"},
		{"body default", func(a *App) {
			Post(a, "/d", func(context.Context, struct {
				Body struct {
					N int `json:"n" default:"3"`
				}
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "default tags only apply to path, query, header, and cookie values"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(Config{})
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.register(app)
		})
	}
}

func TestFailedRegistrationLeavesSpecClean(t *testing.T) {
	app := New(Config{})
	nop := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	Get(app, "/dup", nop, Summary("first"))
	func() {
		defer func() { _ = recover() }()
		Get(app, "/dup", nop, OperationID("other"), Summary("second"))
	}()
	if got := app.OpenAPI().Paths["/dup"].Get.Summary; got != "first" {
		t.Fatalf("summary = %q, want first", got)
	}
}

func TestParamRequiredWhenZeroFails(t *testing.T) {
	app := New(Config{})
	Get(app, "/p", func(context.Context, struct {
		Page  int     `query:"page" validate:"min=1"`
		Size  int     `query:"size" default:"10" validate:"min=1"`
		Sort  string  `query:"sort" validate:"omitempty,oneof=a b"`
		Token *string `header:"X-Token" validate:"required"`
	}) (struct{}, error) {
		return struct{}{}, nil
	})
	got := map[string]bool{}
	for _, p := range app.OpenAPI().Paths["/p"].Get.Parameters {
		got[p.Name] = p.Required
	}
	want := map[string]bool{"page": true, "size": false, "sort": false, "X-Token": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s required = %v, want %v", k, got[k], v)
		}
	}
}
