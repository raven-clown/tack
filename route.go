package tack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"

	"github.com/raven-clown/tack/internal/bind"
	"github.com/raven-clown/tack/openapi"
)

// RouteOption configures a single route, or every route of a group through
// Group.With.
type RouteOption func(*routeConfig)

type routeConfig struct {
	status      int
	summary     string
	description string
	tags        []string
	operationID string
	deprecated  bool
	middleware  []func(http.Handler) http.Handler
	security    []openapi.SecurityRequirement
	errs        []*Error
}

// route is what a registered handler knows about itself at request time.
type route struct {
	name     string
	declared map[string]bool
	statuses map[int]bool
}

// Status sets the status code of successful responses. The default is 200.
func Status(code int) RouteOption {
	return func(c *routeConfig) { c.status = code }
}

// Summary sets the operation summary in the OpenAPI document.
func Summary(s string) RouteOption {
	return func(c *routeConfig) { c.summary = s }
}

// Description sets the operation description in the OpenAPI document.
func Description(s string) RouteOption {
	return func(c *routeConfig) { c.description = s }
}

// Tags groups the operation under tags in the OpenAPI document.
func Tags(tags ...string) RouteOption {
	return func(c *routeConfig) { c.tags = append(c.tags, tags...) }
}

// OperationID overrides the generated operation ID, such as "getPetsById".
func OperationID(id string) RouteOption {
	return func(c *routeConfig) { c.operationID = id }
}

// Deprecated marks the operation as deprecated in the OpenAPI document.
func Deprecated() RouteOption {
	return func(c *routeConfig) { c.deprecated = true }
}

// Middleware wraps this route's handler in mw, inside any app and group
// middleware. The first middleware runs outermost.
func Middleware(mw ...func(http.Handler) http.Handler) RouteOption {
	return func(c *routeConfig) { c.middleware = append(c.middleware, mw...) }
}

// Security declares that the route needs the named scheme from
// Config.Security, with the given scopes. Several Security options mean any
// one of them is enough. The route documents a 401 response; checking the
// credentials is up to middleware, which can answer with ErrUnauthorized.
func Security(name string, scopes ...string) RouteOption {
	if scopes == nil {
		scopes = []string{}
	}
	return func(c *routeConfig) {
		c.security = append(c.security, openapi.SecurityRequirement{name: scopes})
	}
}

// Errors declares the typed errors, made with DefineError, that the route can
// return. Each appears in the OpenAPI document under its status with its code,
// next to the validation and internal errors every route has.
func Errors(errs ...*Error) RouteOption {
	return func(c *routeConfig) { c.errs = append(c.errs, errs...) }
}

// Get registers h for GET requests to path. In and Out are inferred from h.
//
// In must be a struct whose fields use path, query, header, or cookie tags, or
// a field named Body for the JSON body. Out is encoded as JSON, unless it is a
// struct with a field named Body: then Body is encoded, an int field named
// Status overrides the status code when not zero (it must be 2xx or 3xx), and
// header-tagged fields become response headers. Get panics if In does not
// match path or Out is not a valid output type.
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
	if prefix := r.prefix(); prefix != "" {
		switch {
		case path == "/":
			panic(fmt.Sprintf("tack: %s %s/: path \"/\" under a group matches every path below %s/; use \"/{$}\" for %s/ alone or \"\" for %s", method, prefix, prefix, prefix, prefix))
		case path != "" && !strings.HasPrefix(path, "/"):
			panic(fmt.Sprintf("tack: %s %s: path %q under group %s must start with \"/\"", method, prefix+path, path, prefix))
		}
		path = prefix + path
	}
	name := method + " " + path
	if h == nil {
		panic(fmt.Sprintf("tack: %s: handler is nil", name))
	}

	cfg := routeConfig{status: http.StatusOK}
	for _, o := range slices.Concat(r.options(), opts) {
		o(&cfg)
	}
	if cfg.status < 200 || cfg.status > 399 {
		panic(fmt.Sprintf("tack: %s: Status(%d) must be a 2xx or 3xx code", name, cfg.status))
	}
	for _, s := range cfg.security {
		for scheme := range s {
			if _, ok := a.security[scheme]; !ok {
				panic(fmt.Sprintf("tack: %s: Security(%q) names a scheme that is not in Config.Security; add it there with tack.Bearer, tack.APIKey, or tack.Basic", name, scheme))
			}
		}
	}
	for _, e := range cfg.errs {
		if e == nil || e.Code == "" || !validStatus(e.Status) {
			panic(fmt.Sprintf("tack: %s: Errors needs errors made with tack.DefineError, got %v", name, e))
		}
	}

	out, err := newOutput(reflect.TypeFor[Out]())
	if err != nil {
		panic(fmt.Sprintf("tack: %s: %v", name, err))
	}
	if noBody(cfg.status) && (out.stream || out.typ != reflect.TypeFor[struct{}]()) {
		shown := out.typ
		if out.stream {
			shown = reflect.TypeFor[Out]()
		}
		panic(fmt.Sprintf("tack: %s: Status(%d) sends no body, but the handler returns %s; return struct{} instead", name, cfg.status, shown))
	}
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("tack: %s: path must start with \"/\"", name))
	}

	t := reflect.TypeFor[In]()
	plan, err := bind.New(t, bind.PathParams(path))
	if err != nil {
		panic(fmt.Sprintf("tack: %s: %v", name, err))
	}
	if err := checkDive(t, "", map[reflect.Type]bool{}); err != nil {
		panic(fmt.Sprintf("tack: %s: %v", name, err))
	}
	check := bind.HasValidateTags(t, map[reflect.Type]bool{})
	checks, compiled := bind.Compile(plan, t)
	if compiled && len(checks) == 0 {
		check = false // nothing to validate at all
	}
	if check && !compiled {
		if err := probeValidator(a.validate, t); err != nil {
			panic(fmt.Sprintf("tack: %s: %v", name, err))
		}
	}
	locs := plan.Locations()
	prefix := ""
	if t.Name() != "" {
		prefix = t.Name() + "."
	}

	errs := documentedErrors(plan, cfg)
	rt := &route{name: name, declared: map[string]bool{}, statuses: map[int]bool{cfg.status: true}}
	for _, e := range errs {
		rt.declared[e.Code] = true
		rt.statuses[e.Status] = true
	}
	hasForm, _ := plan.HasForm()
	if hasForm && method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch {
		panic(fmt.Sprintf("tack: %s: form fields need a request body, which net/http reads for POST, PUT, and PATCH only; use query tags or change the method", name))
	}
	// Inputs are pooled: binding and validation need a pointer, which would
	// otherwise move every request's input to the heap. The handler gets a
	// copy, so the pooled value is never shared. An input with nothing to
	// bind skips all of that.
	inputs := sync.Pool{New: func() any { return new(In) }}
	// Outputs are pooled too: the encoder needs an addressable value, and
	// would otherwise copy every result to the heap. Streams are sent as is.
	outputs := sync.Pool{New: func() any { return new(Out) }}
	finish := func(w http.ResponseWriter, req *http.Request, res Out, err error) {
		if out.stream || err != nil {
			a.finish(w, req, rt, out, cfg.status, res, err)
			return
		}
		op := outputs.Get().(*Out)
		*op = res
		a.finish(w, req, rt, out, cfg.status, op, nil)
		outputs.Put(op)
	}
	noInput := len(plan.Params()) == 0 && !plan.HasBody() && !hasForm
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if noInput {
			var in In
			res, err := h(req.Context(), in)
			finish(w, req, res, err)
			return
		}
		inp := inputs.Get().(*In)
		*inp = *new(In)
		defer inputs.Put(inp)
		if plan.HasBody() || hasForm {
			if req.ContentLength > a.maxBody {
				a.respond(w, req, &Error{Status: http.StatusRequestEntityTooLarge, Code: codeTooLarge, Detail: fmt.Sprintf("request body exceeds %d bytes", a.maxBody)})
				return
			}
			req.Body = http.MaxBytesReader(w, req.Body, a.maxBody)
		}
		if hasForm {
			defer func() {
				if req.MultipartForm != nil {
					_ = req.MultipartForm.RemoveAll()
				}
			}()
		}
		if err := plan.Bind(req, inp); err != nil {
			a.writeBindError(w, req, err)
			return
		}
		if check {
			var details []ErrorDetail
			if compiled {
				details = detailsOf(bind.Run(reflect.ValueOf(inp), checks))
			} else {
				details = a.validateInput(inp, prefix, locs)
			}
			if details != nil {
				a.respond(w, req, &Error{Status: http.StatusUnprocessableEntity, Code: codeValidation, Detail: "validation failed", Errors: details})
				return
			}
		}
		res, err := h(req.Context(), *inp)
		finish(w, req, res, err)
	})
	mws := slices.Concat(r.middleware(), cfg.middleware)
	if err := register(a.mux, name, chain(mws, handler)); err != nil {
		panic(fmt.Sprintf("tack: %s: %v; wildcards must be whole segments like {id}, and each method and path can be registered once", name, err))
	}
	if err := a.addOperation(method, path, plan, out, cfg, errs); err != nil {
		panic(fmt.Sprintf("tack: %s: %v", name, err))
	}
}

// finish sends the handler's result: an error, a stream, a wrapped output
// with status and headers, or a JSON body. res is a pointer to the result
// unless it is a stream.
func (a *App) finish(w http.ResponseWriter, req *http.Request, rt *route, out *output, status int, res any, err error) {
	if err != nil {
		a.writeError(w, req, err, rt)
		return
	}
	if out.stream {
		if err := res.(streamer).serve(w, req, status); err != nil {
			a.writeError(w, req, err, rt)
		}
		return
	}
	if !out.wrapped() {
		a.writeJSON(w, req, rt, status, res)
		return
	}
	status, headers, body, err := out.split(res, status)
	if err != nil {
		a.writeError(w, req, err, rt)
		return
	}
	if status < 200 || status > 399 {
		a.writeError(w, req, fmt.Errorf("output Status %d is not a 2xx or 3xx code; return an error for failures", status), rt)
		return
	}
	if a.dev && !rt.statuses[status] {
		a.logger.WarnContext(req.Context(), "tack: undocumented status", slog.String("route", rt.name), slog.Int("status", status),
			slog.String("fix", "declare it with tack.Status so it appears in the docs and the generated client accepts it"))
	}
	// Encode before adding headers, so a failure's problem response does not
	// carry the handler's headers.
	var e *encoder
	if !noBody(status) {
		var err error
		if e, err = encodeJSON(body); err != nil {
			a.writeError(w, req, fmt.Errorf("encoding response: %w", err), rt)
			return
		}
	}
	for i, values := range headers {
		for _, v := range values {
			w.Header().Add(out.headers[i].name, v)
		}
	}
	if e == nil {
		w.WriteHeader(status)
		return
	}
	e.send(w, status)
}

// probeValidator runs the validator once on a zero input so a rule it does
// not know fails at registration rather than on the first request.
func probeValidator(v *validator.Validate, t reflect.Type) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("validate tag: %v; fix the rule or remove it", r)
		}
	}()
	_ = v.Struct(reflect.New(t).Interface())
	return nil
}

// jsonContentType and nosniff are shared between responses. Their length
// equals their capacity, so a later Header.Add copies rather than writes into
// them.
var (
	jsonContentType = []string{"application/json"}
	nosniff         = []string{"nosniff"}
)

// documentedErrors lists the errors an operation documents: tack's own, a
// 401 when the route has security, and the ones declared with Errors.
func documentedErrors(plan *bind.Plan, cfg routeConfig) []*Error {
	errs := []*Error{{Status: http.StatusInternalServerError, Code: codeInternal, Detail: "internal error"}}
	_, _, hasBody := plan.Body()
	hasForm, _ := plan.HasForm()
	if hasBody || hasForm || len(plan.Params()) > 0 {
		errs = append(errs,
			&Error{Status: http.StatusBadRequest, Code: codeMalformed, Detail: "malformed request"},
			&Error{Status: http.StatusUnprocessableEntity, Code: codeValidation, Detail: "validation failed"})
	}
	if hasBody || hasForm {
		errs = append(errs,
			&Error{Status: http.StatusRequestEntityTooLarge, Code: codeTooLarge, Detail: "request body too large"},
			&Error{Status: http.StatusUnsupportedMediaType, Code: codeMediaType, Detail: "unsupported content type"})
	}
	if len(cfg.security) > 0 {
		errs = append(errs, ErrUnauthorized)
	}
	return append(errs, cfg.errs...)
}

// register turns ServeMux panics into errors.
func register(mux *http.ServeMux, pattern string, h http.Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	mux.Handle(pattern, h)
	return nil
}

func noBody(status int) bool {
	return status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified
}

var bindCodes = map[int]string{
	http.StatusBadRequest:            codeMalformed,
	http.StatusRequestEntityTooLarge: codeTooLarge,
	http.StatusUnsupportedMediaType:  codeMediaType,
	http.StatusUnprocessableEntity:   codeValidation,
}

// detailsOf converts bind errors; it allocates only when there are some.
func detailsOf(fields []bind.FieldError) []ErrorDetail {
	if len(fields) == 0 {
		return nil
	}
	details := make([]ErrorDetail, len(fields))
	for i, f := range fields {
		details[i] = ErrorDetail(f)
	}
	return details
}

func (a *App) writeBindError(w http.ResponseWriter, r *http.Request, err error) {
	var be *bind.Error
	if !errors.As(err, &be) {
		a.respond(w, r, &Error{Status: http.StatusBadRequest, Code: codeMalformed, Detail: err.Error()})
		return
	}
	e := &Error{Status: be.Status, Code: bindCodes[be.Status], Detail: be.Detail}
	for _, f := range be.Fields {
		e.Errors = append(e.Errors, ErrorDetail(f))
	}
	a.respond(w, r, e)
}

// encoder is a buffer and a JSON encoder bound to it, pooled so that a
// response is encoded without allocating.
type encoder struct {
	buf bytes.Buffer
	enc *json.Encoder
}

var encoders = sync.Pool{New: func() any {
	e := &encoder{}
	e.enc = json.NewEncoder(&e.buf)
	return e
}}

// maxPooledBuffer keeps the pool from holding on to one huge response.
const maxPooledBuffer = 64 << 10

func (a *App) writeJSON(w http.ResponseWriter, r *http.Request, rt *route, status int, v any) {
	if noBody(status) {
		w.WriteHeader(status)
		return
	}
	e, err := encodeJSON(v)
	if err != nil {
		a.writeError(w, r, fmt.Errorf("encoding response: %w", err), rt)
		return
	}
	e.send(w, status)
}

// encodeJSON encodes v into a pooled encoder, which send returns to the pool.
func encodeJSON(v any) (*encoder, error) {
	e := encoders.Get().(*encoder)
	e.buf.Reset()
	if err := e.enc.Encode(v); err != nil {
		encoders.Put(e)
		return nil, err
	}
	return e, nil
}

// send writes the encoded JSON with contentType and releases the encoder.
func (e *encoder) send(w http.ResponseWriter, status int) {
	e.sendAs(w, status, jsonContentType)
}

func (e *encoder) sendAs(w http.ResponseWriter, status int, contentType []string) {
	b := e.buf.Bytes()
	b = b[:len(b)-1] // Encode ends with a newline.
	h := w.Header()
	h["Content-Type"] = contentType
	h["X-Content-Type-Options"] = nosniff
	w.WriteHeader(status)
	_, _ = w.Write(b) // #nosec G705 -- JSON under an explicit Content-Type
	if e.buf.Cap() <= maxPooledBuffer {
		encoders.Put(e)
	}
}
