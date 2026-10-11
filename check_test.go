package tack

import (
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/raven-clown/tack/internal/bind"
)

// checkedInput has one field per rule the compiler supports.
type checkedInput struct {
	ID      int      `path:"id" validate:"min=1"`
	Limit   int      `query:"limit" validate:"min=1,max=100"`
	Page    *int     `query:"page" validate:"omitempty,gte=1,lt=30"`
	Ratio   float64  `query:"ratio" validate:"gt=0,lte=1"`
	Count   uint8    `query:"count" validate:"len=4"`
	Code    int      `query:"code" validate:"oneof=1 2 3"`
	Kind    string   `query:"kind" validate:"omitempty,oneof=cat dog"`
	Name    string   `query:"name" validate:"required,max=5"`
	Tag     string   `query:"tag" validate:"len=2"`
	Exact   string   `query:"exact" validate:"omitempty,eq=yes"`
	Token   string   `header:"X-Token" validate:"required"`
	Flag    bool     `query:"flag" validate:"required"`
	Score   *float64 `query:"score" validate:"min=2"`
	Nothing string   `query:"nothing"`
}

// TestChecksMatchValidator runs the same inputs through the compiled checks
// and the validator and expects identical errors.
func TestChecksMatchValidator(t *testing.T) {
	t.Parallel()
	app := New(Config{})
	typ := reflect.TypeFor[checkedInput]()
	plan, err := bind.New(typ, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	checks, ok := compileChecks(typ, plan)
	if !ok || len(checks) != 13 {
		t.Fatalf("compileChecks: ok %v, %d checks", ok, len(checks))
	}
	one, two, half, low := 1, 2, 0.5, 1.5
	inputs := []checkedInput{
		{ID: 1, Limit: 10, Ratio: 0.5, Count: 4, Code: 2, Name: "milo", Tag: "ab", Token: "t", Flag: true},
		{},
		{ID: 0, Limit: 101, Page: &one, Ratio: 1.5, Count: 3, Code: 9, Kind: "fish", Name: "toolong", Tag: "abc", Exact: "no", Token: "", Flag: false, Score: &low},
		{ID: 1, Limit: 1, Page: &two, Ratio: 1, Count: 4, Code: 3, Kind: "cat", Name: "é", Tag: "éé", Exact: "yes", Token: "t", Flag: true, Score: &half},
		{ID: 5, Limit: 50, Ratio: 0.1, Count: 4, Code: 1, Name: "x", Tag: "xy", Token: "t", Flag: true, Kind: "dog"},
	}
	for i, in := range inputs {
		in := in
		got := runChecks(reflect.ValueOf(&in), checks)
		want := app.validateInput(&in, "checkedInput.", plan.Locations())
		if !reflect.DeepEqual(got, want) {
			t.Errorf("input %d:\ncompiled:  %+v\nvalidator: %+v", i, got, want)
		}
	}
}

func TestCompileChecksFallsBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		typ  reflect.Type
		ok   bool
		n    int
	}{
		{"no rules", reflect.TypeFor[struct {
			A string `query:"a"`
		}](), true, 0},
		{"body with rules", reflect.TypeFor[struct {
			A    string `query:"a" validate:"required"`
			Body struct {
				N string `validate:"required"`
			}
		}](), false, 0},
		{"body without rules", reflect.TypeFor[struct {
			A    string `query:"a" validate:"required"`
			Body struct{ N string }
		}](), true, 1},
		{"slice", reflect.TypeFor[struct {
			A []string `query:"a" validate:"max=3"`
		}](), false, 0},
		{"unknown rule", reflect.TypeFor[struct {
			A string `query:"a" validate:"email"`
		}](), false, 0},
		{"bad param", reflect.TypeFor[struct {
			A int `query:"a" validate:"min=x"`
		}](), false, 0},
		{"quoted oneof", reflect.TypeFor[struct {
			A string `query:"a" validate:"oneof='a b' c"`
		}](), false, 0},
		{"bool with min", reflect.TypeFor[struct {
			A bool `query:"a" validate:"min=1"`
		}](), false, 0},
		{"string gt", reflect.TypeFor[struct {
			A string `query:"a" validate:"gt=1"`
		}](), false, 0},
		{"required file", reflect.TypeFor[struct {
			F *multipart.FileHeader `form:"f" validate:"required"`
		}](), true, 1},
		{"file with other rule", reflect.TypeFor[struct {
			F *multipart.FileHeader `form:"f" validate:"required,min=1"`
		}](), false, 0},
		{"embedded", reflect.TypeFor[struct {
			Paging
			Q string `query:"q" validate:"required"`
		}](), true, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := bind.New(tt.typ, nil)
			if err != nil {
				t.Fatal(err)
			}
			checks, ok := compileChecks(tt.typ, plan)
			if ok != tt.ok || len(checks) != tt.n {
				t.Fatalf("ok %v with %d checks, want %v with %d", ok, len(checks), tt.ok, tt.n)
			}
		})
	}
}

// Paging is embedded by a test input.
type Paging struct {
	Limit int `query:"limit" validate:"min=1"`
}

func TestChecksThroughRoutes(t *testing.T) {
	t.Parallel()
	app := New(Config{})
	Get(app, "/pets/{id}", func(_ context.Context, in struct {
		Paging
		ID   int     `path:"id" validate:"min=1"`
		Kind *string `query:"kind" validate:"omitempty,oneof=cat dog"`
	}) (int, error) {
		return in.ID + in.Limit, nil
	})
	Post(app, "/upload", func(_ context.Context, in struct {
		Photo *multipart.FileHeader `form:"photo" validate:"required"`
	}) (string, error) {
		return in.Photo.Filename, nil
	})
	tests := []struct {
		url, want string
		status    int
	}{
		{"/pets/3?limit=2", "5", 200},
		{"/pets/0?limit=0&kind=fish", `"errors":[{"location":"query.limit","message":"must be at least 1"},{"location":"path.id","message":"must be at least 1"},{"location":"query.kind","message":"must be one of: cat, dog"}]`, 422},
		{"/pets/1", `{"location":"query.limit","message":"must be at least 1"}`, 422},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.url, nil))
		if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: got %d %s, want %d containing %s", tt.url, rec.Code, rec.Body, tt.status, tt.want)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.ServeHTTP(rec, req)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `{"location":"form.photo","message":"is required"}`) {
		t.Errorf("upload without file: %d %s", rec.Code, rec.Body)
	}
}
