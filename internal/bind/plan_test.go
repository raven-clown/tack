package bind

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type Paging struct {
	Page  int `query:"page" default:"1"`
	Limit int `query:"limit" default:"20"`
}

type pet struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type fullInput struct {
	Paging
	ID      int      `path:"id"`
	Tags    []string `query:"tag"`
	Debug   *bool    `query:"debug"`
	Trace   string   `header:"x-trace-id"`
	Accept  []string `header:"Accept"`
	Session string   `cookie:"session"`
	Body    pet
	Ignored string `json:"-"`
	hidden  string
}

func mustPlan[T any](t *testing.T, params ...string) *Plan {
	t.Helper()
	p, err := New(reflect.TypeFor[T](), params)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBindAllSources(t *testing.T) {
	p := mustPlan[fullInput](t, "id")
	r := httptest.NewRequest(http.MethodPost, "/pets/7?tag=a&tag=b&debug=true&limit=50", strings.NewReader(`{"name":"Milo","age":3}`))
	r.SetPathValue("id", "7")
	r.Header.Set("X-Trace-Id", "abc")
	r.Header.Add("Accept", "a")
	r.Header.Add("Accept", "b")
	r.AddCookie(&http.Cookie{Name: "session", Value: "s1"})

	var in fullInput
	if err := p.Bind(r, &in); err != nil {
		t.Fatal(err)
	}
	if !p.HasBody() {
		t.Fatal("HasBody = false")
	}
	want := fullInput{
		Paging:  Paging{Page: 1, Limit: 50},
		ID:      7,
		Tags:    []string{"a", "b"},
		Debug:   in.Debug,
		Trace:   "abc",
		Accept:  []string{"a", "b"},
		Session: "s1",
		Body:    pet{Name: "Milo", Age: 3},
	}
	if in.hidden != "" {
		t.Fatal("unexported field was bound")
	}
	if in.Debug == nil || !*in.Debug {
		t.Fatalf("Debug = %v, want true", in.Debug)
	}
	if !reflect.DeepEqual(in, want) {
		t.Fatalf("got %+v\nwant %+v", in, want)
	}

	locs := p.Locations()
	for k, v := range map[string]string{"ID": "path.id", "Paging.Page": "query.page", "Trace": "header.x-trace-id", "Body": "body", "Session": "cookie.session"} {
		if locs[k] != v {
			t.Errorf("Locations()[%q] = %q, want %q", k, locs[k], v)
		}
	}
}

func TestBindDefaultsAndAbsent(t *testing.T) {
	type in struct {
		Page *int   `query:"page" default:"2"`
		Sort string `query:"sort"`
		N    int    `query:"n" default:"5"`
	}
	p := mustPlan[in](t)
	var a, b in
	if err := p.Bind(httptest.NewRequest(http.MethodGet, "/?n=", nil), &a); err != nil {
		t.Fatal(err)
	}
	if a.Page == nil || *a.Page != 2 || a.N != 5 || a.Sort != "" {
		t.Fatalf("got %+v", a)
	}
	*a.Page = 99
	if err := p.Bind(httptest.NewRequest(http.MethodGet, "/", nil), &b); err != nil {
		t.Fatal(err)
	}
	if *b.Page != 2 {
		t.Fatalf("pointer default shared between requests: got %d", *b.Page)
	}
}

func TestBindErrors(t *testing.T) {
	type in struct {
		N    int   `query:"n"`
		IDs  []int `query:"id"`
		Body *pet
	}
	type required struct {
		Body pet
	}
	tests := []struct {
		name       string
		plan       *Plan
		url, body  string
		limit      int64
		wantStatus int
		wantFields []FieldError
	}{
		{"bad query", mustPlan[in](t), "/?n=x&id=1&id=y", "", 0, 400, []FieldError{{"query.n", "must be an integer"}, {"query.id[1]", "must be an integer"}}},
		{"bad json", mustPlan[in](t), "/", `{"name":`, 0, 400, []FieldError{{"body", "is not valid JSON"}}},
		{"wrong json type", mustPlan[in](t), "/", `{"age":"old"}`, 0, 400, []FieldError{{"body.age", "must be an integer"}}},
		{"two values", mustPlan[in](t), "/", `{} {}`, 0, 400, []FieldError{{"body", "is not valid JSON"}}},
		{"query and body errors", mustPlan[in](t), "/?n=x", `[1]`, 0, 400, []FieldError{{"query.n", "must be an integer"}, {"body", "must be an object"}}},
		{"too large", mustPlan[in](t), "/", `{"name":"` + strings.Repeat("x", 100) + `"}`, 10, 413, nil},
		{"too large after value", mustPlan[in](t), "/", `{}` + strings.Repeat(" ", 100) + "x", 10, 413, nil},
		{"missing body", mustPlan[required](t), "/", "", 0, 422, []FieldError{{"body", "is required"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tt.url, strings.NewReader(tt.body))
			if tt.limit > 0 {
				r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, tt.limit)
			}
			dst := reflect.New(reflect.TypeFor[in]())
			if tt.wantStatus == 422 {
				dst = reflect.New(reflect.TypeFor[required]())
			}
			err := tt.plan.Bind(r, dst.Interface())
			var be *Error
			if !errors.As(err, &be) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if be.Status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", be.Status, tt.wantStatus)
			}
			if !reflect.DeepEqual(be.Fields, tt.wantFields) {
				t.Fatalf("fields = %+v, want %+v", be.Fields, tt.wantFields)
			}
			if be.Error() == "" {
				t.Fatal("empty Error()")
			}
		})
	}
}

func TestBindOptionalBody(t *testing.T) {
	type in struct{ Body *pet }
	p := mustPlan[in](t)
	var empty, full in
	if err := p.Bind(httptest.NewRequest(http.MethodPost, "/", nil), &empty); err != nil || empty.Body != nil {
		t.Fatalf("empty: err=%v body=%v", err, empty.Body)
	}
	if err := p.Bind(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Rex"}`)), &full); err != nil || full.Body == nil || full.Body.Name != "Rex" {
		t.Fatalf("full: err=%v body=%v", err, full.Body)
	}
}

func TestNewErrors(t *testing.T) {
	tests := []struct {
		name   string
		typ    reflect.Type
		params []string
		want   string
	}{
		{"not a struct", reflect.TypeFor[int](), nil, "must be a struct"},
		{"untagged", reflect.TypeFor[struct{ Name string }](), nil, "field Name has no source tag"},
		{"two tags", reflect.TypeFor[struct {
			A string `query:"a" header:"a"`
		}](), nil, "more than one source tag"},
		{"empty name", reflect.TypeFor[struct {
			A string `query:""`
		}](), nil, "query tag needs a name"},
		{"duplicate", reflect.TypeFor[struct {
			A string `query:"a"`
			B string `query:"a"`
		}](), nil, "fields A and B both bind query:a"},
		{"path slice", reflect.TypeFor[struct {
			A []string `path:"a"`
		}](), []string{"a"}, "path values cannot be slices"},
		{"unsupported type", reflect.TypeFor[struct {
			A map[string]int `query:"a"`
		}](), nil, "unsupported type"},
		{"slice default", reflect.TypeFor[struct {
			A []int `query:"a" default:"1"`
		}](), nil, "default is not supported on slices"},
		{"bad default", reflect.TypeFor[struct {
			A int `query:"a" default:"x"`
		}](), nil, `default "x" must be an integer`},
		{"embedded pointer", reflect.TypeFor[struct{ *Paging }](), nil, "embedded pointer structs"},
		{"missing path field", reflect.TypeFor[struct{}](), []string{"id"}, "path parameter {id} has no field"},
		{"extra path field", reflect.TypeFor[struct {
			ID int `path:"id"`
		}](), nil, `path:"id" is not in the route pattern`},
		{"nested error", reflect.TypeFor[struct {
			Paging
			X string
		}](), nil, "field X has no source tag"},
		{"embedded struct error", reflect.TypeFor[struct {
			Bad
		}](), nil, "field Bad.Name has no source tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.typ, tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

type Bad struct{ Name string }

func TestPathParams(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/pets", nil},
		{"/pets/{id}", []string{"id"}},
		{"/orgs/{org}/pets/{id}", []string{"org", "id"}},
		{"/files/{path...}", []string{"path"}},
		{"/{$}", nil},
		{"/broken/{id", nil},
	}
	for _, tt := range tests {
		if got := PathParams(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("PathParams(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBindBodyEdgeCases(t *testing.T) {
	type when struct {
		At time.Time `json:"at"`
	}
	type required struct {
		N    int `path:"n"`
		Body when
	}
	type optional struct{ Body *when }
	tests := []struct {
		name        string
		plan        *Plan
		dst         any
		path, ctype string
		body        string
		wantStatus  int
		wantFields  []FieldError
	}{
		{"null required", mustPlan[required](t, "n"), &required{}, "1", "", "null", 422, []FieldError{{"body", "is required"}}},
		{"missing body and bad param", mustPlan[required](t, "n"), &required{}, "x", "", "", 400, []FieldError{{"path.n", "must be an integer"}, {"body", "is required"}}},
		{"wrong content type", mustPlan[required](t, "n"), &required{}, "1", "text/plain", `{}`, 415, nil},
		{"json suffix", mustPlan[required](t, "n"), &required{}, "1", "application/problem+json; charset=utf-8", `{"at":"2026-10-11T00:00:00Z"}`, 0, nil},
		{"invalid custom value", mustPlan[required](t, "n"), &required{}, "1", "application/json", `{"at":"yesterday"}`, 400, []FieldError{{"body", "contains an invalid value"}}},
		{"null optional", mustPlan[optional](t), &optional{}, "", "", " null ", 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			r.SetPathValue("n", tt.path)
			if tt.ctype != "" {
				r.Header.Set("Content-Type", tt.ctype)
			}
			err := tt.plan.Bind(r, tt.dst)
			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var be *Error
			if !errors.As(err, &be) || be.Status != tt.wantStatus || !reflect.DeepEqual(be.Fields, tt.wantFields) {
				t.Fatalf("err = %+v, want status %d fields %+v", be, tt.wantStatus, tt.wantFields)
			}
		})
	}
	var o optional
	_ = mustPlan[optional](t).Bind(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("null")), &o)
	if o.Body != nil {
		t.Fatal("null optional body should stay nil")
	}
}

func TestBindHeaderList(t *testing.T) {
	type in struct {
		IDs []int `header:"X-Ids"`
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Add("X-Ids", "1, 2")
	r.Header.Add("X-Ids", "3")
	var got in
	if err := mustPlan[in](t).Bind(r, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.IDs, []int{1, 2, 3}) {
		t.Fatalf("IDs = %v", got.IDs)
	}
}

type formInput struct {
	Caption string                  `form:"caption"`
	Photo   *multipart.FileHeader   `form:"photo"`
	Extras  []*multipart.FileHeader `form:"extras"`
	Tags    []string                `form:"tag"`
	Limit   int                     `form:"limit" default:"3"`
	Page    int                     `query:"page"`
}

func TestFormPlan(t *testing.T) {
	p, err := New(reflect.TypeFor[formInput](), nil)
	if err != nil {
		t.Fatal(err)
	}
	if form, files := p.HasForm(); !form || !files {
		t.Errorf("HasForm = %v, %v", form, files)
	}
	if params := p.Params(); len(params) != 1 || params[0].Name != "page" {
		t.Errorf("Params should hold only the query value: %+v", params)
	}
	fields := p.FormFields()
	if len(fields) != 5 || fields[1].Name != "photo" || !fields[1].File || fields[2].Name != "extras" || !fields[2].File || fields[0].File {
		t.Errorf("FormFields: %+v", fields)
	}
	if p.Locations()["Photo"] != "form.photo" || p.Locations()["Tags"] != "form.tag" {
		t.Errorf("locations: %v", p.Locations())
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("caption", "hi")
	_ = w.WriteField("tag", "a")
	_ = w.WriteField("tag", "b")
	part, _ := w.CreateFormFile("photo", "c.jpg")
	_, _ = part.Write([]byte("x"))
	part, _ = w.CreateFormFile("extras", "1.png")
	_, _ = part.Write([]byte("y"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/?page=2", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	var in formInput
	if err := p.Bind(req, &in); err != nil {
		t.Fatal(err)
	}
	if in.Caption != "hi" || in.Photo == nil || in.Photo.Filename != "c.jpg" || len(in.Extras) != 1 || len(in.Tags) != 2 || in.Limit != 3 || in.Page != 2 {
		t.Errorf("bound %+v", in)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("caption=plain&limit=9"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	in = formInput{}
	if err := p.Bind(req, &in); err != nil || in.Caption != "plain" || in.Limit != 9 || in.Photo != nil {
		t.Errorf("urlencoded: %+v, %v", in, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	err = p.Bind(req, &formInput{})
	var be *Error
	if !errors.As(err, &be) || be.Status != http.StatusUnsupportedMediaType {
		t.Errorf("json to a form: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("limit=abc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err = p.Bind(req, &formInput{})
	if !errors.As(err, &be) || be.Status != http.StatusBadRequest || len(be.Fields) != 1 || be.Fields[0].Location != "form.limit" {
		t.Errorf("bad value: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("caption=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = http.MaxBytesReader(nil, req.Body, 3)
	err = p.Bind(req, &formInput{})
	if !errors.As(err, &be) || be.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %v", err)
	}

	for _, tt := range []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"body and form", reflect.TypeFor[struct {
			Body struct{}
			X    string `form:"x"`
		}](), "has both a Body field and form fields"},
		{"file in header", reflect.TypeFor[struct {
			F *multipart.FileHeader `header:"f"`
		}](), "files come from multipart forms; use a form tag instead of header"},
		{"file default", reflect.TypeFor[struct {
			F []*multipart.FileHeader `form:"f" default:"x"`
		}](), "default is not supported on files"},
		{"form slice of structs", reflect.TypeFor[struct {
			F []struct{} `form:"f"`
		}](), "*multipart.FileHeader in a form"},
	} {
		if _, err := New(tt.typ, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want %q", tt.name, err, tt.want)
		}
	}
}
