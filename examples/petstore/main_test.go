package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPetstore(t *testing.T) {
	app := newApp()
	steps := []struct {
		method, url, body string
		wantStatus        int
		wantBody          string
	}{
		{"POST", "/pets", `{"name":"Milo","kind":"cat"}`, 201, `{"id":1,"name":"Milo","kind":"cat"}`},
		{"POST", "/pets", `{"name":"Rex","kind":"dog"}`, 201, `{"id":2,"name":"Rex","kind":"dog"}`},
		{"POST", "/pets", `{"name":"","kind":"fish"}`, 422, `{"type":"about:blank","title":"Unprocessable Entity","status":422,"code":"validation_failed","detail":"validation failed","errors":[{"location":"body.name","message":"is required"},{"location":"body.kind","message":"must be one of: cat, dog, bird"}]}`},
		{"GET", "/pets", "", 200, `[{"id":1,"name":"Milo","kind":"cat"},{"id":2,"name":"Rex","kind":"dog"}]`},
		{"GET", "/pets?kind=dog", "", 200, `[{"id":2,"name":"Rex","kind":"dog"}]`},
		{"GET", "/pets?limit=1", "", 200, `[{"id":1,"name":"Milo","kind":"cat"}]`},
		{"GET", "/pets/1", "", 200, `{"id":1,"name":"Milo","kind":"cat"}`},
		{"PUT", "/pets/1", `{"name":"Mila","kind":"cat"}`, 200, `{"id":1,"name":"Mila","kind":"cat"}`},
		{"PUT", "/pets/9", `{"name":"Mila","kind":"cat"}`, 404, `{"type":"about:blank","title":"Not Found","status":404,"code":"pet_not_found","detail":"pet 9 not found"}`},
		{"DELETE", "/pets/2", "", 204, ``},
		{"DELETE", "/pets/2", "", 404, `{"type":"about:blank","title":"Not Found","status":404,"code":"pet_not_found","detail":"pet not found"}`},
		{"GET", "/pets/2", "", 404, `{"type":"about:blank","title":"Not Found","status":404,"code":"pet_not_found","detail":"pet not found"}`},
	}
	for _, s := range steps {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(s.method, s.url, strings.NewReader(s.body)))
		if rec.Code != s.wantStatus || rec.Body.String() != s.wantBody {
			t.Fatalf("%s %s: got %d %s\nwant %d %s", s.method, s.url, rec.Code, rec.Body, s.wantStatus, s.wantBody)
		}
	}
}
