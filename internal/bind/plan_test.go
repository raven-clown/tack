package bind

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
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
	for k, v := range map[string]string{"ID": "path.id", "Paging.Page": "query.page", "Trace": "header.X-Trace-Id", "Body": "body", "Session": "cookie.session"} {
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
		{"two values", mustPlan[in](t), "/", `{} {}`, 0, 400, []FieldError{{"body", "must contain a single JSON value"}}},
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
