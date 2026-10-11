package tack

import (
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"
)

const defaultMaxBodyBytes = 1 << 20

// Config configures an App.
type Config struct {
	// Title and Version describe the API.
	Title   string
	Version string

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
