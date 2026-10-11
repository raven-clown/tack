package futurefit

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raven-clown/tack"
)

// tackcache: a route option with a TTL, and invalidation. The option is
// tack.Middleware under the hood, so it composes with every other option and
// an add-on can ship it today. What it cannot do today is document itself:
// a RouteOption does not learn which operation it was applied to, so the
// Cache-Control header the middleware sends is not in /docs. A registration
// hook such as an option that receives the operation would be additive.

type cacheEntry struct {
	status  int
	header  http.Header
	body    []byte
	expires time.Time
}

// cache is tackcache.Cache; New would take a store such as Redis.
type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	now     func() time.Time
}

func newCache() *cache {
	return &cache{entries: map[string]cacheEntry{}, now: time.Now}
}

// recorder captures a response so it can be stored; Unwrap keeps flushing
// handlers working through it.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// TTL is tackcache.TTL: a route option built from tack.Middleware.
func (c *cache) TTL(d time.Duration) tack.RouteOption {
	return tack.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}
			key := r.URL.RequestURI()
			c.mu.Lock()
			e, ok := c.entries[key]
			c.mu.Unlock()
			if ok && c.now().Before(e.expires) {
				for k, v := range e.header {
					w.Header()[k] = v
				}
				w.Header().Set("Age", strconv.Itoa(int(d.Seconds()-time.Until(e.expires).Seconds())))
				w.WriteHeader(e.status)
				_, _ = w.Write(e.body)
				return
			}
			w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(int(d.Seconds())))
			rec := &recorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == http.StatusOK {
				c.mu.Lock()
				c.entries[key] = cacheEntry{status: rec.status, header: w.Header().Clone(), body: rec.body.Bytes(), expires: c.now().Add(d)}
				c.mu.Unlock()
			}
		})
	})
}

// Invalidate is tackcache.Invalidate: drop every entry under a path prefix.
func (c *cache) Invalidate(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
}

func TestTackcache(t *testing.T) {
	c := newCache()
	var calls atomic.Int32
	app := newApp()
	pets := app.Group("/pets")
	tack.Get(pets, "/{id}", func(ctx context.Context, in idInput) (Pet, error) {
		calls.Add(1)
		return getPet(ctx, in)
	}, c.TTL(time.Minute), tack.Errors(errPetNotFound))
	tack.Put(pets, "/{id}", func(_ context.Context, in struct {
		ID   int `path:"id"`
		Body Pet
	}) (Pet, error) {
		c.Invalidate("/pets/" + strconv.Itoa(in.ID))
		return in.Body, nil
	})

	status, first, header := do(t, app, http.MethodGet, "/pets/1", "", nil)
	if status != 200 || header.Get("Cache-Control") != "max-age=60" {
		t.Fatalf("first: %d %v", status, header)
	}
	_, second, header := do(t, app, http.MethodGet, "/pets/1", "", nil)
	if second != first || header.Get("Age") == "" || calls.Load() != 1 {
		t.Fatalf("second: body equal %v, Age %q, calls %d", second == first, header.Get("Age"), calls.Load())
	}
	if status, _, _ := do(t, app, http.MethodGet, "/pets/101", "", nil); status != 404 {
		t.Fatalf("errors pass through: %d", status)
	}
	do(t, app, http.MethodPut, "/pets/1", `{"id":1,"name":"Max"}`, nil)
	if _, _, _ = do(t, app, http.MethodGet, "/pets/1", "", nil); calls.Load() != 3 {
		t.Fatalf("after invalidation calls = %d, want 3", calls.Load())
	}

	// The middleware is invisible to the document: the one gap.
	op := operation(t, app.OpenAPI(), http.MethodGet, "/pets/{id}")
	if op.Responses["200"].Headers["Cache-Control"] != nil {
		t.Error("the option documented a header; update FUTURE_FIT.md, the hook exists")
	}
	generates(t, app.OpenAPI())
}
