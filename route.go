package tack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/raven-clown/tack/internal/bind"
)

// RouteOption configures a single route.
type RouteOption func(*routeConfig)

type routeConfig struct {
	status int
}

// Status sets the status code of successful responses. The default is 200.
func Status(code int) RouteOption {
	return func(c *routeConfig) { c.status = code }
}

// Get registers h for GET requests to path. In and Out are inferred from h.
//
// In must be a struct whose fields use path, query, header, or cookie tags, or
// a field named Body for the JSON body. Out is encoded as JSON. Get panics if
// In does not match path.
func Get[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption) {
	handle(r, http.MethodGet, path, h, opts)
}

// Post registers h for POST requests to path. See Get.
func Post[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption) {
	handle(r, http.MethodPost, path, h, opts)
}

// Put registers h for PUT requests to path. See Get.
func Put[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption) {
	handle(r, http.MethodPut, path, h, opts)
}

// Patch registers h for PATCH requests to path. See Get.
func Patch[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption) {
	handle(r, http.MethodPatch, path, h, opts)
}

// Delete registers h for DELETE requests to path. See Get.
func Delete[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption) {
	handle(r, http.MethodDelete, path, h, opts)
}

func handle[In, Out any](r Router, method, path string, h func(context.Context, In) (Out, error), opts []RouteOption) {
	a := r.app()
	route := method + " " + path
	if h == nil {
		panic(fmt.Sprintf("tack: %s: handler is nil", route))
	}

	cfg := routeConfig{status: http.StatusOK}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.status < 200 || cfg.status > 399 {
		panic(fmt.Sprintf("tack: %s: Status(%d) must be a 2xx or 3xx code", route, cfg.status))
	}

	t := reflect.TypeFor[In]()
	plan, err := bind.New(t, bind.PathParams(path))
	if err != nil {
		panic(fmt.Sprintf("tack: %s: %v", route, err))
	}
	check := hasValidateTags(t, map[reflect.Type]bool{})
	locs := plan.Locations()

	a.mux.HandleFunc(route, func(w http.ResponseWriter, req *http.Request) {
		var in In
		if plan.HasBody() {
			req.Body = http.MaxBytesReader(w, req.Body, a.maxBody)
		}
		if err := plan.Bind(req, &in); err != nil {
			writeBindError(w, err)
			return
		}
		if check {
			if details := a.validateInput(in, locs); details != nil {
				writeProblem(w, &Error{Status: http.StatusUnprocessableEntity, Detail: "validation failed", Errors: details})
				return
			}
		}
		out, err := h(req.Context(), in)
		if err != nil {
			a.writeError(w, req, route, err)
			return
		}
		a.writeJSON(w, req, route, cfg.status, out)
	})
}

func writeBindError(w http.ResponseWriter, err error) {
	var be *bind.Error
	if !errors.As(err, &be) {
		writeProblem(w, &Error{Status: http.StatusBadRequest, Detail: err.Error()})
		return
	}
	e := &Error{Status: be.Status, Detail: be.Detail}
	for _, f := range be.Fields {
		e.Errors = append(e.Errors, ErrorDetail(f))
	}
	writeProblem(w, e)
}

func (a *App) writeJSON(w http.ResponseWriter, r *http.Request, route string, status int, v any) {
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		a.writeError(w, r, route, fmt.Errorf("encoding response: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
