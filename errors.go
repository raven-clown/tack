package tack

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Error is an RFC 9457 problem details response. Return one from a handler to
// send that status and detail to the client.
//
// Code identifies the kind of problem independently of the message, such as
// "pet_not_found". Define errors with codes once with DefineError, declare
// them on routes with Errors, and they appear in the OpenAPI document and in
// generated clients.
type Error struct {
	Type     string        `json:"type"`
	Title    string        `json:"title"`
	Status   int           `json:"status"`
	Code     string        `json:"code,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Instance string        `json:"instance,omitempty"`
	Errors   []ErrorDetail `json:"errors,omitempty"`

	cause error
}

// ErrorDetail locates one problem in a request, e.g. "body.name" or "query.page".
type ErrorDetail struct {
	Location string `json:"location"`
	Message  string `json:"message"`
}

// Codes of the errors tack itself sends.
const (
	codeMalformed   = "malformed_request"
	codeTooLarge    = "body_too_large"
	codeMediaType   = "unsupported_media_type"
	codeValidation  = "validation_failed"
	codeInternal    = "internal"
	codeUnauthorize = "unauthorized"
)

// ErrUnauthorized is the 401 that routes with a Security option document.
// Middleware that checks credentials can send it with App.WriteError.
var ErrUnauthorized = DefineError(http.StatusUnauthorized, codeUnauthorize, "authentication required")

// NewError returns an *Error with the given HTTP status (400-599) and detail.
//
// Errors that are not an *Error can still choose their status by implementing
// HTTPStatus() int; only that error's own message reaches the client. Any
// other error, or a status outside 400-599, becomes a 500 with details hidden.
func NewError(status int, detail string) *Error {
	return &Error{Status: status, Detail: detail}
}

// DefineError returns an *Error with a status (400-599), a code, and a default
// detail, meant for a package-level variable:
//
//	var ErrPetNotFound = tack.DefineError(404, "pet_not_found", "pet not found")
//
// Handlers return it as is, or with WithDetail for a per-request message.
// Routes list it with Errors so the code appears in their OpenAPI responses.
// DefineError panics when status or code is invalid.
func DefineError(status int, code, detail string) *Error {
	if !validStatus(status) {
		panic(fmt.Sprintf("tack: DefineError(%d, %q): status must be 400-599", status, code))
	}
	if code == "" {
		panic(fmt.Sprintf("tack: DefineError(%d, %q): code must not be empty; use a stable name such as \"pet_not_found\"", status, code))
	}
	return &Error{Status: status, Code: code, Detail: detail}
}

// WithDetail returns a copy of e with the given detail. The copy keeps the
// status and code, so errors.Is(copy, e) stays true.
func (e *Error) WithDetail(detail string) *Error {
	c := *e
	c.Detail = detail
	return &c
}

// Error formats the status and detail, e.g. "404 Not Found: pet not found".
func (e *Error) Error() string {
	return fmt.Sprintf("%d %s: %s", e.Status, http.StatusText(e.Status), e.Detail)
}

// Is reports whether target is an *Error with the same code and status.
// Errors without a code only match themselves.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && e.Code != "" && e.Code == t.Code && e.Status == t.Status
}

// Unwrap returns the handler's original error when tack built e from it,
// which lets an ErrorHandler inspect domain errors.
func (e *Error) Unwrap() error { return e.cause }

type httpStatuser interface {
	HTTPStatus() int
}

// problem maps any error to the *Error tack sends for it.
func problem(err error) *Error {
	var te *Error
	if errors.As(err, &te) && validStatus(te.Status) {
		c := *te
		if err != error(te) { //nolint:errorlint // identity check: the cause is only kept when err wraps te
			c.cause = err
		}
		return &c
	}
	var hs httpStatuser
	if errors.As(err, &hs) && validStatus(hs.HTTPStatus()) {
		e := &Error{Status: hs.HTTPStatus(), cause: err}
		if e.Status < 500 {
			e.Detail = err.Error()
			if he, ok := hs.(error); ok {
				e.Detail = he.Error()
			}
		}
		return e
	}
	return &Error{Status: http.StatusInternalServerError, Code: codeInternal, cause: err}
}

// WriteError sends err as an error response the same way a handler's returned
// error would be sent, using Config.ErrorHandler when set. Middleware uses it
// to answer in the app's error format, for example with ErrUnauthorized.
func (a *App) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	a.writeError(w, r, err, nil)
}

func (a *App) writeError(w http.ResponseWriter, r *http.Request, err error, rt *route) {
	e := problem(err)
	if e.Status >= 500 {
		a.logger.ErrorContext(r.Context(), "tack: handler error", slog.String("route", r.Pattern), slog.Any("error", err))
	}
	if a.dev && rt != nil && e.Code != "" && !rt.declared[e.Code] {
		a.logger.WarnContext(r.Context(), "tack: undeclared error", slog.String("route", rt.name), slog.String("code", e.Code), slog.Int("status", e.Status),
			slog.String("fix", "add it to the route with tack.Errors so it appears in the docs"))
	}
	a.respond(w, r, e)
}

func (a *App) respond(w http.ResponseWriter, r *http.Request, e *Error) {
	if a.errorHandler != nil {
		a.errorHandler(w, r, e)
		return
	}
	writeProblem(w, e)
}

func validStatus(s int) bool { return s >= 400 && s <= 599 }

func writeProblem(w http.ResponseWriter, e *Error) {
	p := *e
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	b, err := json.Marshal(p)
	if err != nil {
		b = []byte(`{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`)
		p.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_, _ = w.Write(b)
}
