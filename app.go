package tack

import (
	"log/slog"
	"net/http"
	"sync"

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

	// SpecPath and DocsPath serve the OpenAPI document and the docs page.
	// They default to "/openapi.json" and "/docs"; "-" disables one. The docs
	// page needs the spec, so disabling SpecPath disables both.
	SpecPath string
	DocsPath string

	// MaxBodyBytes limits request bodies. Zero or less means 1 MiB.
	MaxBodyBytes int64

	// Logger receives handler errors. Nil means slog.Default().
	Logger *slog.Logger
}

// App is an http.Handler that serves typed routes.
type App struct {
	mux      *http.ServeMux
	maxBody  int64
	logger   *slog.Logger
	validate *validator.Validate

	mu       sync.Mutex
	registry *schema.Registry
	doc      *openapi.Document
	spec     []byte
	opIDs    map[string]string
}

// New returns an App configured by c.
func New(c Config) *App {
	a := &App{
		mux:      http.NewServeMux(),
		maxBody:  c.MaxBodyBytes,
		logger:   c.Logger,
		validate: newValidate(),
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
	a.mux.ServeHTTP(w, r)
}

// Router is something routes can be registered on. It is implemented by *App.
type Router interface {
	app() *App
}

func (a *App) app() *App { return a }
