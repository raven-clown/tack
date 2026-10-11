package schema

import (
	"encoding/json"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/raven-clown/tack/internal/schema/internal/a"
	"github.com/raven-clown/tack/internal/schema/internal/b"
	"github.com/raven-clown/tack/openapi"
)

var update = flag.Bool("update", false, "update golden files")

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s mismatch (run go test ./... -update)\ngot:\n%s", path, got)
	}
}

type Status string

func (Status) JSONSchema() *openapi.Schema {
	return &openapi.Schema{Type: "string", Enum: []any{"active", "archived"}}
}

type Node struct {
	Value    int     `json:"value"`
	Children []*Node `json:"children,omitempty"`
}

type Base struct {
	ID        int       `json:"id" doc:"Unique ID" example:"42"`
	CreatedAt time.Time `json:"createdAt"`
}

type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

type marshaled struct{}

func (marshaled) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }

type Kitchen struct {
	Base
	Name     string            `json:"name" validate:"required,min=1,max=50"`
	Email    string            `json:"email,omitempty" validate:"omitempty,email"`
	Site     string            `json:"site,omitempty" validate:"url"`
	Token    string            `json:"token,omitempty" validate:"uuid"`
	Age      uint8             `json:"age" validate:"gte=1,lt=30"`
	Score    float64           `json:"score" validate:"gt=0,lte=1"`
	Ratio    float32           `json:"ratio"`
	Count    int32             `json:"count"`
	Big      int64             `json:"big"`
	Unsigned uint              `json:"unsigned"`
	Active   bool              `json:"active" example:"true"`
	Kind     string            `json:"kind" validate:"oneof=cat dog"`
	Level    int               `json:"level" validate:"oneof=1 2 3"`
	Nick     *string           `json:"nick"`
	Tags     []string          `json:"tags" validate:"max=5,dive,min=2"`
	Grid     [3]int            `json:"grid"`
	Labels   map[string]string `json:"labels,omitempty"`
	ByID     map[int]bool      `json:"byId,omitempty"`
	Raw      json.RawMessage   `json:"raw,omitempty"`
	Blob     []byte            `json:"blob,omitempty"`
	Any      any               `json:"any,omitempty"`
	Addr     netip.Addr        `json:"addr"`
	Custom   marshaled         `json:"custom"`
	Status   Status            `json:"status"`
	Parent   *Node             `json:"parent,omitempty" doc:"Optional parent"`
	Tree     Node              `json:"tree"`
	Pets     Page[a.Pet]       `json:"pets"`
	Other    b.Pet             `json:"other"`
	Inline   struct {
		X int `json:"x"`
	} `json:"inline"`
	Code      string `json:"code" validate:"len=4"`
	Exact     []int  `json:"exact" validate:"len=2"`
	Ignored   string `json:"-"`
	NoTag     string
	private   string
	Zero      int      `json:"zero,omitzero"`
	Short     string   `json:"short" validate:"gt=1,lt=10"`
	Few       []string `json:"few" validate:"gt=0,lt=3"`
	Positive  int      `json:"positive" validate:"min=x"`
	Pattern   string   `json:"pattern" validate:"dive"`
	AnotherID int      `json:"anotherId" validate:"required"`
}

func TestKitchenSink(t *testing.T) {
	r := NewRegistry()
	root, err := r.Schema(reflect.TypeFor[Kitchen]())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Schema(reflect.TypeFor[Kitchen]()); err != nil {
		t.Fatal(err)
	}
	golden(t, "kitchen", map[string]any{"root": root, "components": r.Schemas()})
	_ = Kitchen{}.private
}

func TestCollisions(t *testing.T) {
	r := NewRegistry()
	for _, typ := range []reflect.Type{reflect.TypeFor[a.Pet](), reflect.TypeFor[b.Pet](), reflect.TypeFor[*a.Pet]()} {
		if _, err := r.Schema(typ); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Schema(reflect.TypeFor[collide]()); err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range r.Schemas() {
		names = append(names, n)
	}
	golden(t, "collisions", r.Schemas())
	if len(names) != 2 {
		t.Fatalf("names = %v", names)
	}
}

type collide = struct {
	A a.Pet `json:"a"`
}

type nilProvider struct{}

func (nilProvider) JSONSchema() *openapi.Schema { return nil }

func TestErrors(t *testing.T) {
	tests := []struct {
		typ  reflect.Type
		want string
	}{
		{reflect.TypeFor[func()](), "type func() is not supported"},
		{reflect.TypeFor[chan int](), "type chan int is not supported"},
		{reflect.TypeFor[map[float64]int](), "map key type float64 is not supported"},
		{reflect.TypeFor[struct {
			F func() `json:"f"`
		}](), "field F: type func() is not supported"},
		{reflect.TypeFor[[]func()](), "not supported"},
		{reflect.TypeFor[*func()](), "not supported"},
		{reflect.TypeFor[map[string]func()](), "not supported"},
		{reflect.TypeFor[nilProvider](), "JSONSchema returned nil"},
		{reflect.TypeFor[struct{ Embedded }](), "not supported"},
		{reflect.TypeFor[struct {
			N int `json:"n" default:"3"`
		}](), "default tags only apply to path, query, header, and cookie values"},
	}
	for _, tt := range tests {
		r := NewRegistry()
		_, err := r.Schema(tt.typ)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.typ, err, tt.want)
		}
		if len(r.Schemas()) != 0 {
			t.Errorf("%s: failed schema left in registry: %v", tt.typ, r.Schemas())
		}
	}
}

type Embedded struct {
	F func() `json:"f"`
}

func TestFieldErrorAndRequired(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Field(reflect.StructField{Name: "F", Type: reflect.TypeFor[func()]()}); err == nil {
		t.Fatal("want error")
	}
	for rules, want := range map[string]bool{"required": true, "min=1,required": true, "dive,required": false, "": false} {
		if got := hasRule(rules, "required"); got != want {
			t.Errorf("hasRule(%q) = %v, want %v", rules, got, want)
		}
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"Pet":                            "Pet",
		"Page[github.com/x/y.Pet]":       "PagePet",
		"Pair[example.com/a.K,string]":   "PairKstring",
		"Map[string,github.com/b/c.Val]": "MapstringVal",
	} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
	if pascal("") != "" {
		t.Error(`pascal("") != ""`)
	}
}

func TestSchemaAs(t *testing.T) {
	r := NewRegistry()
	s, err := r.SchemaAs(reflect.TypeFor[a.Pet](), "Animal")
	if err != nil || s.Ref != "#/components/schemas/Animal" {
		t.Fatalf("got %+v, %v", s, err)
	}
	if again, _ := r.Schema(reflect.TypeFor[a.Pet]()); again.Ref != s.Ref {
		t.Fatalf("second lookup = %q", again.Ref)
	}
	if b, _ := r.SchemaAs(reflect.TypeFor[b.Pet](), "Animal"); b.Ref != "#/components/schemas/Pet" {
		t.Fatalf("taken name should fall back, got %q", b.Ref)
	}
	if _, err := r.SchemaAs(reflect.TypeFor[Embedded](), "Bad"); err == nil || len(r.Schemas()) != 2 {
		t.Fatalf("err = %v, schemas = %d", err, len(r.Schemas()))
	}
}

type PtrEmb struct {
	*Base
	X int `json:"x"`
}

type Shadow struct {
	ID string `json:"id"`
	Base
}

type Ambiguous struct {
	A
	B
}

type A struct{ Name string }
type B struct{ Name string }

type Rules struct {
	Code   string                 `json:"code" validate:"eq=abc"`
	Labels map[string]int         `json:"labels" validate:"min=1,max=5"`
	ByUint map[uint]bool          `json:"byUint"`
	Quoted int64                  `json:"quoted,string"`
	Kind   *string                `json:"kind" validate:"omitempty,oneof=a b"`
	Any    openapi.SchemaProvider `json:"any"`
}

func TestFieldRules(t *testing.T) {
	r := NewRegistry()
	for _, typ := range []reflect.Type{reflect.TypeFor[PtrEmb](), reflect.TypeFor[Shadow](), reflect.TypeFor[Ambiguous](), reflect.TypeFor[Rules]()} {
		if _, err := r.Schema(typ); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	golden(t, "rules", r.Schemas())

	if got := r.Schemas()["PtrEmb"].Required; !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("PtrEmb required = %v, want [x]", got)
	}
	sh := r.Schemas()["Shadow"]
	if sh.Properties["id"].Type != "string" || !reflect.DeepEqual(sh.Required, []string{"id", "createdAt"}) {
		t.Errorf("Shadow = %+v required %v", sh.Properties["id"], sh.Required)
	}
	if len(r.Schemas()["Ambiguous"].Properties) != 0 {
		t.Errorf("ambiguous fields should be dropped: %v", r.Schemas()["Ambiguous"].Properties)
	}
}
