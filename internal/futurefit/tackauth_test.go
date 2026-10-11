package futurefit

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
)

// tackauth: middleware with typed claims and a Require(roles...) option.
// The middleware is plain func(http.Handler) http.Handler, claims travel
// through the context and come back typed with tack.ContextValue, and Require
// is tack.Middleware. The 401 is documented by tack.Security; the 403 needs
// tack.Errors next to Require, because an option cannot bundle two options
// today. A combinator such as tack.Options(opts...) would be additive and
// would let Require document its own 403.

// Claims is the user's own type; tackauth would be generic over it.
type Claims struct {
	Subject string
	Roles   []string
}

type claimsKey struct{}

var errForbidden = tack.DefineError(http.StatusForbidden, "forbidden", "missing role")

// authJWT is tackauth.JWT: verify is the user's token check, which a real
// add-on would do with a key set.
func authJWT(app *tack.App, verify func(token string) (Claims, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				app.WriteError(w, r, tack.ErrUnauthorized)
				return
			}
			claims, err := verify(token)
			if err != nil {
				app.WriteError(w, r, tack.ErrUnauthorized.WithDetail(err.Error()))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
		})
	}
}

// claimsFrom is tackauth.Claims[C].
func claimsFrom(ctx context.Context) (Claims, bool) {
	return tack.ContextValue[Claims](ctx, claimsKey{})
}

// authRequire is tackauth.Require.
func authRequire(app *tack.App, roles ...string) tack.RouteOption {
	return tack.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := claimsFrom(r.Context())
			if !ok {
				app.WriteError(w, r, tack.ErrUnauthorized)
				return
			}
			for _, role := range roles {
				if !slices.Contains(claims.Roles, role) {
					app.WriteError(w, r, errForbidden.WithDetail("needs role "+role))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
}

type whoami struct {
	Subject string `json:"subject"`
}

func TestTackauth(t *testing.T) {
	verify := func(token string) (Claims, error) {
		switch token {
		case "admin":
			return Claims{Subject: "ada", Roles: []string{"admin"}}, nil
		case "user":
			return Claims{Subject: "bob"}, nil
		}
		return Claims{}, errors.New("bad token")
	}
	app := tack.New(tack.Config{Security: map[string]openapi.SecurityScheme{"bearer": tack.Bearer("JWT")}})
	admin := app.Group("/admin", authJWT(app, verify)).With(tack.Security("bearer"), authRequire(app, "admin"), tack.Errors(errForbidden))
	tack.Get(admin, "/me", func(ctx context.Context, _ struct{}) (whoami, error) {
		claims, _ := claimsFrom(ctx)
		return whoami{Subject: claims.Subject}, nil
	})

	bearer := func(token string) http.Header { return http.Header{"Authorization": {"Bearer " + token}} }
	if status, body, _ := do(t, app, http.MethodGet, "/admin/me", "", nil); status != 401 || !strings.Contains(body, `"unauthorized"`) {
		t.Fatalf("no token: %d %s", status, body)
	}
	if status, body, _ := do(t, app, http.MethodGet, "/admin/me", "", bearer("nope")); status != 401 {
		t.Fatalf("bad token: %d %s", status, body)
	}
	if status, body, _ := do(t, app, http.MethodGet, "/admin/me", "", bearer("user")); status != 403 || !strings.Contains(body, `"forbidden"`) {
		t.Fatalf("wrong role: %d %s", status, body)
	}
	if status, body, _ := do(t, app, http.MethodGet, "/admin/me", "", bearer("admin")); status != 200 || !strings.Contains(body, `"ada"`) {
		t.Fatalf("admin: %d %s", status, body)
	}

	doc := app.OpenAPI()
	op := operation(t, doc, http.MethodGet, "/admin/me")
	if op.Responses["401"] == nil || op.Responses["403"] == nil || len(op.Security) != 1 {
		t.Errorf("documented %v with security %v", keys(op.Responses), op.Security)
	}
	if client := generates(t, doc); !strings.Contains(client, "ForbiddenProblem") {
		t.Error("client lacks ForbiddenProblem")
	}
}
