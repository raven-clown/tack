package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func testApp(now func() time.Time) (*httptest.Server, func()) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	a := &auth{
		users:  map[string]User{"ada": {ID: "ada", Name: "Ada Lovelace"}},
		hashes: map[string][]byte{"ada": hash},
		secret: []byte("test secret"),
		now:    now,
	}
	srv := httptest.NewServer(newApp(a))
	return srv, srv.Close
}

func do(t *testing.T, srv *httptest.Server, method, path, body, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func TestLoginAndMe(t *testing.T) {
	clock := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	srv, stop := testApp(func() time.Time { return clock })
	defer stop()

	tests := []struct {
		name, body string
		wantStatus int
		wantBody   string
	}{
		{"wrong password", `{"username":"ada","password":"nope"}`, 401, `{"type":"about:blank","title":"Unauthorized","status":401,"code":"invalid_credentials","detail":"unknown user or wrong password"}`},
		{"unknown user", `{"username":"bob","password":"hunter2"}`, 401, `{"type":"about:blank","title":"Unauthorized","status":401,"code":"invalid_credentials","detail":"unknown user or wrong password"}`},
		{"missing fields", `{}`, 422, `{"type":"about:blank","title":"Unprocessable Entity","status":422,"code":"validation_failed","detail":"validation failed","errors":[{"location":"body.username","message":"is required"},{"location":"body.password","message":"is required"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := do(t, srv, http.MethodPost, "/login", tt.body, "")
			if status != tt.wantStatus || body != tt.wantBody {
				t.Fatalf("got %d %s\nwant %d %s", status, body, tt.wantStatus, tt.wantBody)
			}
		})
	}

	status, body := do(t, srv, http.MethodPost, "/login", `{"username":"ada","password":"hunter2"}`, "")
	var tok Token
	if err := json.Unmarshal([]byte(body), &tok); err != nil || status != 200 || tok.Token == "" || !tok.ExpiresAt.Equal(clock.Add(time.Hour)) {
		t.Fatalf("login: %d %s (%v)", status, body, err)
	}

	meTests := []struct {
		name, token string
		wantStatus  int
		wantBody    string
	}{
		{"no token", "", 401, `{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized","detail":"authentication required"}`},
		{"garbage token", "not.a.jwt", 401, `{"type":"about:blank","title":"Unauthorized","status":401,"code":"unauthorized","detail":"invalid or expired token"}`},
		{"valid token", tok.Token, 200, `{"id":"ada","name":"Ada Lovelace"}`},
	}
	for _, tt := range meTests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := do(t, srv, http.MethodGet, "/me", "", tt.token)
			if status != tt.wantStatus || body != tt.wantBody {
				t.Fatalf("got %d %s\nwant %d %s", status, body, tt.wantStatus, tt.wantBody)
			}
		})
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/me", nil)
	req.Header.Set("Authorization", "bearer "+tok.Token)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("lower-case scheme: %d", res.StatusCode)
	}
	res, err = srv.Client().Get(srv.URL + "/me")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", res.Header.Get("WWW-Authenticate"))
	}

	clock = clock.Add(2 * time.Hour)
	if status, body := do(t, srv, http.MethodGet, "/me", "", tok.Token); status != 401 || !strings.Contains(body, "invalid or expired token") {
		t.Fatalf("expired token: %d %s", status, body)
	}
}

func TestSpecDocumentsSecurity(t *testing.T) {
	srv, stop := testApp(time.Now)
	defer stop()
	_, body := do(t, srv, http.MethodGet, "/openapi.json", "", "")
	for _, want := range []string{
		`"securitySchemes":{"bearer":{"type":"http","scheme":"bearer","bearerFormat":"JWT"}}`,
		`"security":[{"bearer":[]}]`,
		`"$ref":"#/components/schemas/InvalidCredentialsProblem"`,
		`"$ref":"#/components/schemas/UnauthorizedProblem"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("spec is missing %s:\n%s", want, body)
		}
	}
}
