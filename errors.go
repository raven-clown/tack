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
type Error struct {
	Type     string        `json:"type"`
	Title    string        `json:"title"`
	Status   int           `json:"status"`
	Detail   string        `json:"detail,omitempty"`
	Instance string        `json:"instance,omitempty"`
	Errors   []ErrorDetail `json:"errors,omitempty"`
}

// ErrorDetail locates one problem in a request, e.g. "body.name" or "query.page".
type ErrorDetail struct {
	Location string `json:"location"`
	Message  string `json:"message"`
}

// NewError returns an *Error with the given HTTP status and detail.
//
// Errors that are not an *Error can still choose their status by implementing
// HTTPStatus() int. Any other error becomes a 500 with its details hidden.
func NewError(status int, detail string) *Error {
	return &Error{Status: status, Detail: detail}
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s: %s", e.Status, http.StatusText(e.Status), e.Detail)
}

type httpStatuser interface {
	HTTPStatus() int
}

func (a *App) writeError(w http.ResponseWriter, r *http.Request, route string, err error) {
	var te *Error
	if errors.As(err, &te) && validStatus(te.Status) {
		if te.Status >= 500 {
			a.logError(r, route, err)
		}
		writeProblem(w, te)
		return
	}
	var hs httpStatuser
	if errors.As(err, &hs) && validStatus(hs.HTTPStatus()) {
		s := hs.HTTPStatus()
		if s < 500 {
			writeProblem(w, &Error{Status: s, Detail: err.Error()})
			return
		}
		a.logError(r, route, err)
		writeProblem(w, &Error{Status: s})
		return
	}
	a.logError(r, route, err)
	writeProblem(w, &Error{Status: http.StatusInternalServerError})
}

func (a *App) logError(r *http.Request, route string, err error) {
	a.logger.ErrorContext(r.Context(), "tack: handler error", slog.String("route", route), slog.Any("error", err))
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
		b = []byte(`{"type":"about:blank","title":"Internal Server Error","status":500}`)
		p.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_, _ = w.Write(b)
}
