// Command auth is a small API where POST /login returns a JWT and GET /me
// requires it. tack documents the bearer scheme; the middleware checks it.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
)

// ErrBadCredentials is returned by POST /login for an unknown user or a
// wrong password.
var ErrBadCredentials = tack.DefineError(http.StatusUnauthorized, "invalid_credentials", "unknown user or wrong password")

// User is an account that can log in.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type claims struct {
	jwt.RegisteredClaims
	Name string `json:"name"`
}

type userKey struct{}

// auth holds the accounts, their bcrypt password hashes, and the signing secret.
type auth struct {
	users  map[string]User
	hashes map[string][]byte
	secret []byte
	now    func() time.Time
}

// dummyHash keeps the login timing the same for unknown users.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("unknown"), bcrypt.DefaultCost)

type loginInput struct {
	Body struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}
}

// Token is a signed JWT and the time it stops working.
type Token struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (a *auth) login(_ context.Context, in loginInput) (Token, error) {
	u, ok := a.users[in.Body.Username]
	hash := a.hashes[in.Body.Username]
	if !ok {
		hash = dummyHash
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(in.Body.Password)); err != nil || !ok {
		return Token{}, ErrBadCredentials
	}
	exp := a.now().Add(time.Hour)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: u.ID, ExpiresAt: jwt.NewNumericDate(exp), IssuedAt: jwt.NewNumericDate(a.now())},
		Name:             u.Name,
	})
	signed, err := t.SignedString(a.secret)
	if err != nil {
		return Token{}, fmt.Errorf("signing token: %w", err)
	}
	return Token{Token: signed, ExpiresAt: exp}, nil
}

// requireJWT checks the bearer token and stores the User in the context.
func (a *auth) requireJWT(app *tack.App) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, raw, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") || raw == "" {
				w.Header().Set("WWW-Authenticate", "Bearer")
				app.WriteError(w, r, tack.ErrUnauthorized)
				return
			}
			var c claims
			_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return a.secret, nil },
				jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(a.now))
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				app.WriteError(w, r, tack.ErrUnauthorized.WithDetail("invalid or expired token"))
				return
			}
			ctx := context.WithValue(r.Context(), userKey{}, User{ID: c.Subject, Name: c.Name})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func getMe(ctx context.Context, _ struct{}) (User, error) {
	u, ok := tack.ContextValue[User](ctx, userKey{})
	if !ok {
		return User{}, tack.ErrUnauthorized
	}
	return u, nil
}

func newApp(a *auth) *tack.App {
	app := tack.New(tack.Config{
		Title:    "Auth example",
		Version:  "1.0.0",
		Security: map[string]openapi.SecurityScheme{"bearer": tack.Bearer("JWT")},
	})
	tack.Post(app, "/login", a.login, tack.Summary("Log in"), tack.Errors(ErrBadCredentials))

	private := app.Group("", a.requireJWT(app)).With(tack.Security("bearer"))
	tack.Get(private, "/me", getMe, tack.Summary("Current user"))
	return app
}

func main() {
	secret := []byte(os.Getenv("AUTH_SECRET"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	// AUTH_PASSWORD sets ada's password; the default is for trying the demo.
	password := os.Getenv("AUTH_PASSWORD")
	if password == "" {
		password = "hunter2"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	a := &auth{
		users:  map[string]User{"ada": {ID: "ada", Name: "Ada Lovelace"}},
		hashes: map[string][]byte{"ada": hash},
		secret: secret,
		now:    time.Now,
	}
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           newApp(a),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on %s; log in as ada with the password from AUTH_PASSWORD (default hunter2)", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
