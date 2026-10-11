package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
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

	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	_ = w.WriteField("caption", "sleeping")
	part, _ := w.CreateFormFile("photo", "milo.jpg")
	_, _ = part.Write([]byte("JPEG"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/pets/1/photo", &form)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if want := `{"petId":1,"name":"milo.jpg","size":4,"caption":"sleeping"}`; rec.Code != 201 || rec.Body.String() != want {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
}

func TestEvents(t *testing.T) {
	app := newApp()
	srv := httptest.NewServer(app)
	defer srv.Close()
	res, err := srv.Client().Get(srv.URL + "/pets/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	// The listener is registered before the stream's headers are sent, so a
	// change made now reaches it.
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pets", strings.NewReader(`{"name":"Milo","kind":"cat"}`)))
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	buf := make([]byte, 256)
	n, err := res.Body.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "event: created\ndata: {\"action\":\"created\",\"pet\":{\"id\":1,\"name\":\"Milo\",\"kind\":\"cat\"}}\n\n" {
		t.Fatalf("event: %q", got)
	}
}
