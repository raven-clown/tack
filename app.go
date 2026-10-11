package tack

import (
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-playground/validator/v10"

	"github.com/raven-clown/tack/internal/schema"
	"github.com/raven-clown/tack/openapi"
)

const defaultMaxBodyBytes = 1 << 20

// Config configures an App.
type Config struct {
	// Title, Version, and Description describe the API in the OpenAPI
	// document. Title defaults to "API" and Version to "0.0.0".
	Title       string
	Version     string
	Description string

	// Servers lists the base URLs the API is served from.
	Servers []openapi.Server

	// Security declares the authentication schemes routes can require with
	// the Security option, keyed by the name shown in the docs. Build them
	// with Bearer, APIKey, and Basic. tack documents them; checking
	// credentials is left to middleware.
	Security map[string]openapi.SecurityScheme

	// SpecPath and DocsPath serve the OpenAPI document and the docs page.
	// They default to "/openapi.json" and "/docs"; "-" disables one. The docs
	// page needs the spec, so disabling SpecPath disables both.
	SpecPath string
	DocsPath string

	// MaxBodyBytes limits request bodies. Zero or less means 1 MiB.
	MaxBodyBytes int64

	// Logger receives handler errors. Nil means slog.Default().
	Logger *slog.Logger

	// ErrorHandler writes every error response, replacing the
	// application/problem+json format for the whole app. tack has already
	// mapped the failure to e (status, code, detail) and logged it when it is
	// a 5xx; e.Unwrap returns the handler's original error, if any.
	ErrorHandler func(w http.ResponseWriter, r *http.Request, e *Error)

	// Dev turns on development checks, such as a warning when a handler
	// returns a typed error the route did not declare with Errors.
	Dev bool
}

// App is an http.Handler that serves typed routes.
type App struct {
	mux          *http.ServeMux
	handler      atomic.Pointer[http.Handler]
	maxBody      int64
	logger       *slog.Logger
	validate     *validator.Validate
	errorHandler func(http.ResponseWriter, *http.Request, *Error)
	dev          bool
	security     map[string]openapi.SecurityScheme

	mu       sync.Mutex
	mw       []func(http.Handler) http.Handler
	registry *schema.Registry
	doc      *openapi.Document
	spec     []byte
	opIDs    map[string]string
	codes    map[string]int
}

// New returns an App configured by c. It panics when c.Security holds an
// invalid scheme.
func New(c Config) *App {
	a := &App{
		mux:          http.NewServeMux(),
		maxBody:      c.MaxBodyBytes,
		logger:       c.Logger,
		validate:     newValidate(),
		errorHandler: c.ErrorHandler,
		dev:          c.Dev,
		security:     c.Security,
	}
	if a.maxBody <= 0 {
		a.maxBody = defaultMaxBodyBytes
	}
	if a.logger == nil {
		a.logger = slog.Default()
	}
	a.initOpenAPI(c)
	return a
}

// ServeHTTP implements http.Handler.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := a.handler.Load(); h != nil {
		(*h).ServeHTTP(w, r)
		return
	}
	a.mux.ServeHTTP(w, r)
}

// Use wraps the whole app, including the docs and unmatched routes, in mw.
// The first middleware runs outermost; group and route middleware run inside.
func (a *App) Use(mw ...func(http.Handler) http.Handler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mw = append(a.mw, mw...)
	h := chain(a.mw, a.mux)
	a.handler.Store(&h)
}

// Group returns a group of routes under prefix, each wrapped in mw. prefix is
// "" or starts with "/" and does not end with one; it may hold wildcards such
// as /orgs/{org}, which the routes' input structs must then bind.
func (a *App) Group(prefix string, mw ...func(http.Handler) http.Handler) *Group {
	return newGroup(a, prefix, mw)
}

// Router is something routes can be registered on: an *App or a *Group.
type Router interface {
	app() *App
	prefix() string
	middleware() []func(http.Handler) http.Handler
	options() []RouteOption
}

func (a *App) app() *App                                   { return a }
func (*App) prefix() string                                { return "" }
func (*App) middleware() []func(http.Handler) http.Handler { return nil }
func (*App) options() []RouteOption                        { return nil }

// chain wraps h so that mw[0] runs first.
func chain(mw []func(http.Handler) http.Handler, h http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}
