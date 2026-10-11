package tack

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type photoInput struct {
	ID      int                     `path:"id"`
	Caption string                  `form:"caption" validate:"max=5"`
	Photo   *multipart.FileHeader   `form:"photo" validate:"required"`
	Extras  []*multipart.FileHeader `form:"extras"`
	Tags    []string                `form:"tag"`
	Limit   int                     `form:"limit" default:"3"`
}

func formApp(maxBody int64) *App {
	app := New(Config{MaxBodyBytes: maxBody})
	Post(app, "/pets/{id}/photo", func(_ context.Context, in photoInput) (map[string]any, error) {
		f, err := in.Photo.Open()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		content, _ := io.ReadAll(f)
		return map[string]any{"id": in.ID, "caption": in.Caption, "photo": in.Photo.Filename, "content": string(content), "extras": len(in.Extras), "tags": in.Tags, "limit": in.Limit}, nil
	})
	Post(app, "/login", func(_ context.Context, in struct {
		User     string `form:"user" validate:"required"`
		Password string `form:"password" validate:"required"`
	}) (map[string]string, error) {
		return map[string]string{"user": in.User}, nil
	})
	return app
}

// multipartBody builds a form with the given values and files (name, filename, content).
func multipartBody(t *testing.T, values map[string][]string, files [][3]string) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, vs := range values {
		for _, v := range vs {
			if err := w.WriteField(name, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, f := range files {
		part, err := w.CreateFormFile(f[0], f[1])
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(f[2]))
	}
	_ = w.Close()
	return w.FormDataContentType(), &buf
}

func TestFormUpload(t *testing.T) {
	app := formApp(0)
	post := func(contentType string, body io.Reader, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	ct, body := multipartBody(t, map[string][]string{"caption": {"hi"}, "tag": {"a", "b"}, "limit": {"7"}},
		[][3]string{{"photo", "cat.jpg", "JPEG"}, {"extras", "1.png", "x"}, {"extras", "2.png", "y"}})
	rec := post(ct, body, "/pets/4/photo")
	want := `{"caption":"hi","content":"JPEG","extras":2,"id":4,"limit":7,"photo":"cat.jpg","tags":["a","b"]}`
	if rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}

	ct, body = multipartBody(t, nil, [][3]string{{"photo", "c.jpg", "x"}})
	rec = post(ct, body, "/pets/4/photo")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"limit":3`) || !strings.Contains(rec.Body.String(), `"tags":null`) {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
	}

	tests := []struct {
		name        string
		contentType string
		body        string
		path        string
		status      int
		want        string
	}{
		{"missing file", "", "", "/pets/4/photo", 422, `{"location":"form.photo","message":"is required"}`},
		{"caption too long", "", "", "/pets/4/photo", 422, `{"location":"form.caption","message":"must be at most 5 characters long"}`},
		{"urlencoded without file", "application/x-www-form-urlencoded", "caption=hi", "/pets/4/photo", 422, `"location":"form.photo"`},
		{"wrong content type", "text/plain", "hello", "/pets/4/photo", 415, `is not supported; send multipart/form-data`},
		{"unparsable content type", "multipart/form-data; boundary", "x", "/pets/4/photo", 415, `is not supported; send multipart/form-data`},
		{"bad multipart", "multipart/form-data; boundary=zzz", "garbage", "/pets/4/photo", 400, `"location":"form"`},
		{"login ok", "application/x-www-form-urlencoded", "user=ann&password=pw", "/login", 200, `{"user":"ann"}`},
		{"login missing", "application/x-www-form-urlencoded", "user=ann", "/login", 422, `{"location":"form.password","message":"is required"}`},
		{"login multipart", "", "", "/login", 200, `{"user":"bob"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, body := tt.contentType, io.Reader(strings.NewReader(tt.body))
			switch tt.name {
			case "missing file":
				ct, body = multipartBody(t, map[string][]string{"caption": {"hi"}}, nil)
			case "caption too long":
				ct, body = multipartBody(t, map[string][]string{"caption": {"too long"}}, [][3]string{{"photo", "c.jpg", "x"}})
			case "login multipart":
				ct, body = multipartBody(t, map[string][]string{"user": {"bob"}, "password": {"pw"}}, nil)
			}
			rec := post(ct, body, tt.path)
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
				t.Fatalf("got %d %s\nwant %d containing %s", rec.Code, rec.Body, tt.status, tt.want)
			}
		})
	}

	// A body over the limit is refused before it is read when its length is
	// known, and while it is parsed otherwise.
	small := formApp(1024)
	for _, known := range []bool{true, false} {
		ct, body := multipartBody(t, nil, [][3]string{{"photo", "big.jpg", strings.Repeat("x", 2000)}})
		req := httptest.NewRequest(http.MethodPost, "/pets/4/photo", body)
		req.Header.Set("Content-Type", ct)
		if !known {
			req.ContentLength = -1
		}
		rec := httptest.NewRecorder()
		small.ServeHTTP(rec, req)
		if rec.Code != 413 || !strings.Contains(rec.Body.String(), "body_too_large") {
			t.Fatalf("too large (known length %v): %d %s", known, rec.Code, rec.Body)
		}
	}
}

func TestFormPanics(t *testing.T) {
	tests := []struct {
		name string
		reg  func(*App)
		want string
	}{
		{"body and form", func(app *App) {
			Post(app, "/a", func(context.Context, struct {
				Body struct{}
				X    string `form:"x"`
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "has both a Body field and form fields"},
		{"file in query", func(app *App) {
			Get(app, "/a", func(context.Context, struct {
				F *multipart.FileHeader `query:"f"`
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "files come from multipart forms; use a form tag instead of query"},
		{"file with default", func(app *App) {
			Post(app, "/a", func(context.Context, struct {
				F *multipart.FileHeader `form:"f" default:"x"`
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "default is not supported on files"},
		{"file by value", func(app *App) {
			Post(app, "/a", func(context.Context, struct {
				F multipart.FileHeader `form:"f"`
			}) (struct{}, error) {
				return struct{}{}, nil
			})
		}, "*multipart.FileHeader in a form"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(r.(string), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.reg(New(Config{}))
		})
	}
}
