package bind

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// FuzzQueryValues holds the query scanner to url.ParseQuery, which it replaces.
func FuzzQueryValues(f *testing.F) {
	for _, s := range []string{"", "a=1", "a=1&a=2", "a=&a", "b=2&a=%41", "a=x;y&a=1", "a=1+2", "%zz=1&a=2", "a", "=a", "a=1&&a=2"} {
		f.Add(s, "a")
	}
	f.Fuzz(func(t *testing.T, raw, name string) {
		want := url.Values{}
		for raw2 := raw; raw2 != ""; {
			var pair string
			pair, raw2, _ = strings.Cut(raw2, "&")
			if pair == "" || strings.Contains(pair, ";") {
				continue
			}
			k, v, _ := strings.Cut(pair, "=")
			k, err := url.QueryUnescape(k)
			if err != nil {
				continue
			}
			v, err = url.QueryUnescape(v)
			if err != nil {
				continue
			}
			want[k] = append(want[k], v)
		}
		if m, err := url.ParseQuery(raw); err == nil && !reflect.DeepEqual(m, want) {
			t.Fatalf("oracle disagrees with url.ParseQuery: %v vs %v", want, m)
		}
		first, values, found := queryValues(raw, name, true)
		if found != (len(want[name]) > 0) || !slices.Equal(values, want[name]) || (found && first != want[name][0]) {
			t.Fatalf("queryValues(%q, %q, all) = %q, %q, %v; want %q", raw, name, first, values, found, want[name])
		}
		first, values, found = queryValues(raw, name, false)
		if values != nil || found != (len(want[name]) > 0) || (found && first != want[name][0]) {
			t.Fatalf("queryValues(%q, %q) = %q, %q, %v; want %q", raw, name, first, values, found, want[name])
		}
	})
}

type fuzzBody struct {
	Name   string            `json:"name"`
	Age    int               `json:"age"`
	Score  *float64          `json:"score"`
	Tags   []string          `json:"tags"`
	Meta   map[string]any    `json:"meta"`
	When   time.Time         `json:"when"`
	Child  *fuzzBody         `json:"child"`
	Nested struct{ OK bool } `json:"nested"`
}

type fuzzJSONInput struct {
	ID   uint8 `path:"id"`
	Page *int  `query:"page" default:"1"`
	Body fuzzBody
}

// FuzzBindJSON binds arbitrary bodies and path values; every failure must be
// a bind Error with a 400, 413, or 422 status.
func FuzzBindJSON(f *testing.F) {
	for _, s := range []string{`{}`, `null`, ``, `{"name":"x","age":3,"tags":["a"],"when":"2024-01-02T03:04:05Z"}`, `{"age":"3"}`, `[1]`, `{"child":{"child":{}}}`, "\xff"} {
		f.Add(s, "7", "2")
	}
	plan, err := New(reflect.TypeFor[fuzzJSONInput](), []string{"id"})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, body, id, page string) {
		r := httptest.NewRequest(http.MethodPost, "/x?page="+url.QueryEscape(page), strings.NewReader(body))
		r.SetPathValue("id", id)
		r.Header.Set("Content-Type", "application/json")
		var in fuzzJSONInput
		checkBindError(t, plan.Bind(r, &in))
	})
}

type fuzzFormInput struct {
	Caption string                  `form:"caption"`
	Count   int                     `form:"count" default:"2"`
	Flags   []bool                  `form:"flag"`
	Photo   *multipart.FileHeader   `form:"photo"`
	Extra   []*multipart.FileHeader `form:"extra"`
}

// FuzzParseForm parses arbitrary URL-encoded and multipart bodies under the
// same body limit the route applies.
func FuzzParseForm(f *testing.F) {
	var seed bytes.Buffer
	w := multipart.NewWriter(&seed)
	_ = w.WriteField("caption", "hi")
	_ = w.WriteField("count", "3")
	part, _ := w.CreateFormFile("photo", "a.png")
	_, _ = part.Write([]byte("png"))
	_ = w.Close()
	f.Add(seed.Bytes(), w.Boundary(), true)
	f.Add([]byte("caption=hi&count=1&flag=true&flag=x"), "", false)
	f.Add([]byte("--b\r\nContent-Disposition: form-data; name=\"count\"\r\n\r\nnope\r\n--b--\r\n"), "b", true)
	f.Add([]byte("--b\r\n"), "b", true)
	plan, err := New(reflect.TypeFor[fuzzFormInput](), nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, body []byte, boundary string, multi bool) {
		r := httptest.NewRequest(http.MethodPost, "/x?count=9", bytes.NewReader(body))
		if multi {
			r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		} else {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		r.Body = http.MaxBytesReader(nil, r.Body, 1<<16)
		var in fuzzFormInput
		err := plan.Bind(r, &in)
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}
		checkBindError(t, err)
		if err == nil && in.Count == 9 {
			t.Fatal("form value read from the query string")
		}
	})
}

func checkBindError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var be *Error
	if !errors.As(err, &be) {
		t.Fatalf("Bind returned %T %v, want *Error", err, err)
	}
	switch be.Status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusUnsupportedMediaType:
	default:
		t.Fatalf("Bind returned status %d: %v", be.Status, err)
	}
}

// FuzzSplitList checks the header list splitter never returns empty or padded items.
func FuzzSplitList(f *testing.F) {
	f.Add("a, b", "c")
	f.Add(",", " ")
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, part := range splitList([]string{a, b}) {
			if part == "" || strings.TrimSpace(part) != part || strings.Contains(part, ",") {
				t.Fatalf("splitList(%q, %q) returned %q", a, b, part)
			}
		}
	})
}

// FuzzPathParams checks the wildcard scanner on arbitrary patterns: every
// name it returns is spelled in the pattern, and {$} is never one.
func FuzzPathParams(f *testing.F) {
	for _, s := range []string{"/pets/{id}", "/files/{path...}", "/{$}", "/{a}/{b}", "/{", "/}{", "/{{a}}", "{......}", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		for _, name := range PathParams(pattern) {
			if name == "$" || (!strings.Contains(pattern, "{"+name+"}") && !strings.Contains(pattern, "{"+name+"...}")) {
				t.Fatalf("PathParams(%q) returned %q", pattern, name)
			}
		}
	})
}
