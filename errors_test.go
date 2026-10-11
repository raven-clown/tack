package tack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var (
	errGone    = DefineError(http.StatusGone, "pet_gone", "pet is gone")
	errTeapot  = DefineError(http.StatusTeapot, "teapot", "short and stout")
	errRetired = DefineError(http.StatusGone, "pet_retired", "pet has retired")
)

func TestDefineErrorPanics(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		want   string
	}{
		{"low status", 200, "ok", "tack: DefineError(200, \"ok\"): status must be 400-599"},
		{"high status", 600, "x", "status must be 400-599"},
		{"empty code", 404, "", "code must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			_ = DefineError(tt.status, tt.code, "detail")
		})
	}
}

func TestErrorIsAndWithDetail(t *testing.T) {
	detailed := errGone.WithDetail("pet 7 is gone")
	wrapped := fmt.Errorf("lookup: %w", detailed)
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{"same value", errGone, errGone, true},
		{"nil target", errGone, (*Error)(nil), false},
		{"copy with detail", detailed, errGone, true},
		{"wrapped copy", wrapped, errGone, true},
		{"other code", errRetired, errGone, false},
		{"other status same code", &Error{Status: 404, Code: "pet_gone"}, errGone, false},
		{"no code", NewError(410, "a"), NewError(410, "a"), false},
		{"not an Error", errGone, errors.New("x"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Fatalf("errors.Is = %v, want %v", got, tt.want)
			}
		})
	}
	if detailed.Detail != "pet 7 is gone" || detailed.Code != "pet_gone" || detailed.Status != 410 || errGone.Detail != "pet is gone" {
		t.Fatalf("WithDetail changed the wrong thing: %+v / %+v", detailed, errGone)
	}
	if errGone.Unwrap() != nil {
		t.Fatal("a defined error has no cause")
	}
	if !errors.Is(problem(errGone).Unwrap(), errGone) {
		t.Fatal("problem() should keep the returned error as the cause")
	}
}

func TestProblem(t *testing.T) {
	cause := errors.New("db down")
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantDetail string
		wantCause  error
	}{
		{"defined", errGone, 410, "pet_gone", "pet is gone", errGone},
		{"typed nil", (*Error)(nil), 500, "internal", "", (*Error)(nil)},
		{"wrapped defined", fmt.Errorf("x: %w", errGone), 410, "pet_gone", "pet is gone", fmt.Errorf("x: %w", errGone)},
		{"status interface", notFoundError{}, 404, "", "pet is gone", notFoundError{}},
		{"status interface 5xx", unavailableError{}, 503, "", "", unavailableError{}},
		{"unknown", cause, 500, "internal", "", cause},
		{"bad status", &Error{Status: 299}, 500, "internal", "", &Error{Status: 299}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := problem(tt.err)
			if e.Status != tt.wantStatus || e.Code != tt.wantCode || e.Detail != tt.wantDetail {
				t.Fatalf("got %d %q %q", e.Status, e.Code, e.Detail)
			}
			if tt.name == "typed nil" {
				if e.Unwrap() == nil {
					t.Fatal("typed nil cause lost")
				}
				return
			}
			if (tt.wantCause == nil) != (e.Unwrap() == nil) || (tt.wantCause != nil && e.Unwrap().Error() != tt.wantCause.Error()) {
				t.Fatalf("cause = %v, want %v", e.Unwrap(), tt.wantCause)
			}
		})
	}
}

func TestTypedErrorResponse(t *testing.T) {
	var logs bytes.Buffer
	app := New(Config{Dev: true, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	Get(app, "/pets/{id}", func(_ context.Context, in struct {
		ID int `path:"id"`
	}) (Pet, error) {
		switch in.ID {
		case 1:
			return Pet{}, errGone
		case 2:
			return Pet{}, errGone.WithDetail("pet 2 is gone")
		case 3:
			return Pet{}, errTeapot
		case 4:
			return Pet{}, NewError(http.StatusConflict, "no code, undocumented status")
		case 5:
			return Pet{}, NewError(http.StatusGone, "no code, documented status")
		case 6:
			var typedNil *Error
			return Pet{}, typedNil
		}
		return Pet{ID: in.ID}, nil
	}, Errors(errGone))

	tests := []struct {
		url      string
		wantBody string
		wantWarn bool
	}{
		{"/pets/1", `{"type":"about:blank","title":"Gone","status":410,"code":"pet_gone","detail":"pet is gone"}`, false},
		{"/pets/2", `{"type":"about:blank","title":"Gone","status":410,"code":"pet_gone","detail":"pet 2 is gone"}`, false},
		{"/pets/3", `{"type":"about:blank","title":"I'm a teapot","status":418,"code":"teapot","detail":"short and stout"}`, true},
		{"/pets/4", `{"type":"about:blank","title":"Conflict","status":409,"detail":"no code, undocumented status"}`, true},
		{"/pets/5", `{"type":"about:blank","title":"Gone","status":410,"detail":"no code, documented status"}`, false},
		{"/pets/6", `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			logs.Reset()
			rec := serveOnce(app, "GET", tt.url, "")
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %s\nwant %s", rec.Body, tt.wantBody)
			}
			warned := strings.Contains(logs.String(), "tack: undeclared error")
			if warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v: %s", warned, tt.wantWarn, logs.String())
			}
		})
	}

	// Without Dev nothing is logged for undeclared 4xx errors.
	quiet := New(Config{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	Get(quiet, "/x", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, errTeapot })
	logs.Reset()
	serveOnce(quiet, "GET", "/x", "")
	if logs.Len() > 0 {
		t.Fatalf("unexpected log without Dev: %s", logs.String())
	}
}

func TestErrorsOptionPanics(t *testing.T) {
	nop := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	tests := []struct {
		name string
		do   func(a *App)
		want string
	}{
		{"nil", func(a *App) { Get(a, "/x", nop, Errors(nil)) }, "tack: GET /x: Errors needs errors made with tack.DefineError, got tack: nil *Error"},
		{"no code", func(a *App) { Get(a, "/x", nop, Errors(NewError(404, "x"))) }, "Errors needs errors made with tack.DefineError"},
		{"bad status", func(a *App) { Get(a, "/x", nop, Errors(&Error{Status: 200, Code: "ok"})) }, "Errors needs errors made with tack.DefineError"},
		{"conflicting definition", func(a *App) {
			Get(a, "/a", nop, Errors(errGone))
			Get(a, "/b", nop, Errors(&Error{Status: http.StatusNotFound, Code: "pet_gone", Detail: "pet is gone"}))
		}, `tack: GET /b: error code "pet_gone" was defined with status 410 and detail "pet is gone"; pass the same tack.DefineError value to Errors, not a copy`},
		{"detail copy", func(a *App) {
			Get(a, "/a", nop, Errors(errGone))
			Get(a, "/b", nop, Errors(errGone.WithDetail("pet 7 is gone")))
		}, `tack: GET /b: error code "pet_gone" was defined with status 410 and detail "pet is gone"; pass the same tack.DefineError value to Errors, not a copy`},
		{"reserved code", func(a *App) {
			Get(a, "/a", nop, Errors(DefineError(400, "internal", "mine")))
		}, `error code "internal" was defined with status 500`},
		{"schema name taken by a type", func(a *App) {
			type InternalProblem struct {
				X int `json:"x"`
			}
			Get(a, "/a", func(context.Context, struct{}) (InternalProblem, error) { return InternalProblem{}, nil })
		}, `tack: GET /a: error code "internal": schema "InternalProblem" is already defined differently; rename the type that uses that schema name`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.do(New(Config{}))
		})
	}
}

func TestErrorsSameDefinitionTwice(t *testing.T) {
	app := New(Config{})
	nop := func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }
	Get(app, "/a", nop, Errors(errGone, errGone))
	Get(app, "/b", nop, Errors(errGone, ErrUnauthorized))
	doc := app.OpenAPI()
	if doc.Paths["/a"].Get.Responses["410"].Content["application/problem+json"].Schema.Ref != "#/components/schemas/PetGoneProblem" {
		t.Fatalf("410 response: %+v", doc.Paths["/a"].Get.Responses["410"])
	}
	if doc.Paths["/b"].Get.Responses["401"] == nil {
		t.Fatal("ErrUnauthorized declared with Errors should document 401")
	}
}

func TestErrorHandler(t *testing.T) {
	var seen []string
	app := New(Config{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e *Error) {
			seen = append(seen, fmt.Sprintf("%d %s cause=%v", e.Status, e.Code, e.Unwrap()))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(e.Status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": e.Code, "message": e.Detail, "fields": len(e.Errors)})
		},
	})
	Post(app, "/pets/{id}", func(_ context.Context, in struct {
		ID   int `path:"id" validate:"min=1"`
		Body Pet
	}) (Pet, error) {
		switch in.ID {
		case 1:
			return Pet{}, fmt.Errorf("wrapped: %w", errGone)
		case 2:
			return Pet{}, errors.New("boom")
		}
		return in.Body, nil
	})

	tests := []struct {
		name, url, body string
		wantStatus      int
		wantBody        string
	}{
		{"bind", "/pets/abc", `{"name":"a"}`, 400, `{"error":"malformed_request","fields":1,"message":"malformed request"}`},
		{"validation", "/pets/0", `{"name":""}`, 422, `{"error":"validation_failed","fields":2,"message":"validation failed"}`},
		{"typed", "/pets/1", `{"name":"a"}`, 410, `{"error":"pet_gone","fields":0,"message":"pet is gone"}`},
		{"unknown", "/pets/2", `{"name":"a"}`, 500, `{"error":"internal","fields":0,"message":""}`},
		{"ok", "/pets/3", `{"name":"a"}`, 200, `{"id":0,"name":"a"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveOnce(app, "POST", tt.url, tt.body)
			if rec.Code != tt.wantStatus || strings.TrimSpace(rec.Body.String()) != tt.wantBody {
				t.Fatalf("got %d %s\nwant %d %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
		})
	}
	want := "410 pet_gone cause=wrapped: 410 Gone: pet is gone"
	if len(seen) != 4 || seen[2] != want || seen[3] != "500 internal cause=boom" {
		t.Fatalf("seen = %q", seen)
	}
	direct := New(Config{ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e *Error) {
		seen = append(seen, fmt.Sprintf("direct %v", errors.Is(e, errGone) && e.Unwrap() != nil))
		w.WriteHeader(e.Status)
	}})
	Get(direct, "/x", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, errGone })
	serveOnce(direct, "GET", "/x", "")
	if seen[len(seen)-1] != "direct true" {
		t.Fatalf("a directly returned sentinel must reach ErrorHandler as the cause: %q", seen)
	}
}

func TestWriteError(t *testing.T) {
	var logs bytes.Buffer
	app := New(Config{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get("Authorization") {
			case "":
				app.WriteError(w, r, ErrUnauthorized)
			case "crash":
				app.WriteError(w, r, errors.New("store offline"))
			default:
				next.ServeHTTP(w, r)
			}
		})
	})
	Get(app, "/me", func(context.Context, struct{}) (string, error) { return "me", nil })

	tests := []struct {
		name, auth string
		wantStatus int
		wantBody   string
		wantLog    string
	}{
		{"unauthorized", "", 401, `{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized","detail":"authentication required"}`, ""},
		{"internal logged", "crash", 500, `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`, "store offline"},
		{"ok", "Bearer x", 200, `"me"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			rec := serveOnce(app, "GET", "/me", "", "Authorization", tt.auth)
			if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
				t.Fatalf("got %d %s\nwant %d %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
			if (tt.wantLog == "") != (logs.Len() == 0) || !strings.Contains(logs.String(), tt.wantLog) {
				t.Fatalf("logs = %q", logs.String())
			}
		})
	}
}

func TestWriteProblemFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProblem(rec, &Error{Status: 500, Code: "internal"})
	if rec.Code != 500 || rec.Body.String() != `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestProblemName(t *testing.T) {
	for in, want := range map[string]string{"pet_not_found": "PetNotFoundProblem", "internal": "InternalProblem", "rate-limit.hit": "RateLimitHitProblem", "x1_y2": "X1Y2Problem"} {
		if got := problemName(in); got != want {
			t.Errorf("problemName(%q) = %q, want %q", in, got, want)
		}
	}
}
