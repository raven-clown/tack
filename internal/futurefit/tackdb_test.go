package futurefit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raven-clown/tack"
)

// tackdb: error mapping, a transaction per request, and a health route.
// Everything below is built from exported identifiers, so tackdb can be its
// own module today. An app-wide mapper that runs before tack turns an unknown
// error into a 500 would be an additive Config field; until then handlers
// call dbErr the way they would call tackdb.Err.

var (
	errNotFound    = tack.DefineError(http.StatusNotFound, "not_found", "no such row")
	errConflict    = tack.DefineError(http.StatusConflict, "conflict", "a row with this key exists")
	errUnavailable = tack.DefineError(http.StatusServiceUnavailable, "unavailable", "database unreachable")
)

// errNoRows and uniqueError stand in for sql.ErrNoRows and a
// pgconn.PgError with SQLSTATE 23505.
var errNoRows = errors.New("no rows")

type uniqueError struct{ constraint string }

func (u uniqueError) Error() string { return "duplicate key: " + u.constraint }

// dbErr is tackdb.Err: driver errors become declared tack errors, anything
// else is returned as is and stays a hidden 500.
func dbErr(err error) error {
	var uv uniqueError
	switch {
	case errors.Is(err, errNoRows):
		return errNotFound
	case errors.As(err, &uv):
		return errConflict.WithDetail(fmt.Sprintf("%s already exists", uv.constraint))
	}
	return err
}

// Tx and DB are the slice of a driver the sketch needs.
type Tx interface {
	Commit() error
	Rollback() error
}

type DB interface {
	BeginTx(ctx context.Context) (Tx, error)
	Ping(ctx context.Context) error
}

type txKey struct{}

// txFrom is tackdb.From: the request's transaction, typed.
func txFrom(ctx context.Context) (Tx, bool) { return tack.ContextValue[Tx](ctx, txKey{}) }

// statusWriter records the status so the middleware knows how the handler
// ended; Unwrap keeps streams working behind it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// txMiddleware is tackdb.Tx: one transaction per request, committed on a
// success status and rolled back otherwise. It needs the app only to answer
// in the app's error format when the transaction cannot start.
func txMiddleware(app *tack.App, db DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tx, err := db.BeginTx(r.Context())
			if err != nil {
				app.WriteError(w, r, errUnavailable)
				return
			}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), txKey{}, tx)))
			if sw.status >= 400 {
				_ = tx.Rollback()
				return
			}
			_ = tx.Commit()
		})
	}
}

type health struct {
	Status string `json:"status"`
}

// dbHealth is tackdb.Health: a handler for tack.Get.
func dbHealth(db DB) func(context.Context, struct{}) (health, error) {
	return func(ctx context.Context, _ struct{}) (health, error) {
		if err := db.Ping(ctx); err != nil {
			return health{}, errUnavailable
		}
		return health{Status: "ok"}, nil
	}
}

type fakeTx struct{ committed, rolledBack *atomic.Int32 }

func (t fakeTx) Commit() error   { t.committed.Add(1); return nil }
func (t fakeTx) Rollback() error { t.rolledBack.Add(1); return nil }

type fakeDB struct {
	down                  bool
	committed, rolledBack atomic.Int32
}

func (d *fakeDB) BeginTx(context.Context) (Tx, error) {
	if d.down {
		return nil, errors.New("down")
	}
	return fakeTx{&d.committed, &d.rolledBack}, nil
}

func (d *fakeDB) Ping(context.Context) error {
	if d.down {
		return errors.New("down")
	}
	return nil
}

type createPetInput struct {
	Body struct {
		Name string `json:"name" validate:"required"`
	}
}

func TestTackdb(t *testing.T) {
	db := &fakeDB{}
	app := newApp()
	pets := app.Group("/pets", txMiddleware(app, db)).With(tack.Errors(errUnavailable))
	tack.Post(pets, "", func(ctx context.Context, in createPetInput) (Pet, error) {
		if _, ok := txFrom(ctx); !ok {
			return Pet{}, errors.New("no transaction in context")
		}
		if in.Body.Name == "taken" {
			return Pet{}, dbErr(uniqueError{"pets_name_key"})
		}
		return Pet{ID: 1, Name: in.Body.Name}, nil
	}, tack.Status(http.StatusCreated), tack.Errors(errConflict))
	tack.Get(pets, "/{id}", func(_ context.Context, in idInput) (Pet, error) {
		if in.ID > 100 {
			return Pet{}, dbErr(errNoRows)
		}
		return Pet{ID: in.ID, Name: "Milo"}, nil
	}, tack.Errors(errNotFound))
	tack.Get(app, "/health", dbHealth(db), tack.Errors(errUnavailable))

	if status, _, _ := do(t, app, http.MethodPost, "/pets", `{"name":"Milo"}`, nil); status != 201 || db.committed.Load() != 1 {
		t.Fatalf("create: %d, committed %d", status, db.committed.Load())
	}
	if status, body, _ := do(t, app, http.MethodPost, "/pets", `{"name":"taken"}`, nil); status != 409 || !strings.Contains(body, `"conflict"`) || db.rolledBack.Load() != 1 {
		t.Fatalf("conflict: %d %s, rolled back %d", status, body, db.rolledBack.Load())
	}
	if status, body, _ := do(t, app, http.MethodGet, "/pets/101", "", nil); status != 404 || !strings.Contains(body, `"not_found"`) {
		t.Fatalf("not found: %d %s", status, body)
	}
	if status, _, _ := do(t, app, http.MethodGet, "/health", "", nil); status != 200 {
		t.Fatalf("health: %d", status)
	}
	db.down = true
	if status, body, _ := do(t, app, http.MethodGet, "/health", "", nil); status != 503 || !strings.Contains(body, `"unavailable"`) {
		t.Fatalf("health down: %d %s", status, body)
	}
	if status, body, _ := do(t, app, http.MethodGet, "/pets/1", "", nil); status != 503 || !strings.Contains(body, `"unavailable"`) {
		t.Fatalf("tx down: %d %s", status, body)
	}

	doc := app.OpenAPI()
	if op := operation(t, doc, http.MethodPost, "/pets"); op.Responses["409"] == nil || op.Responses["503"] == nil {
		t.Errorf("POST /pets documents %v", keys(op.Responses))
	}
	if client := generates(t, doc); !strings.Contains(client, "ConflictProblem") || !strings.Contains(client, "UnavailableProblem") {
		t.Error("client lacks the database problems")
	}
}
