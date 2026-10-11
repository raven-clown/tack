package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		in   *Schema
		want string
	}{
		{"plain", &Schema{Type: "string"}, `{"type":"string"}`},
		{"nullable type", &Schema{Type: "integer", Format: "int64", Nullable: true}, `{"type":["integer","null"],"format":"int64"}`},
		{"nullable ref", &Schema{Ref: "#/components/schemas/Pet", Description: "Owner", Nullable: true},
			`{"description":"Owner","anyOf":[{"$ref":"#/components/schemas/Pet"},{"type":"null"}]}`},
		{"nullable any", &Schema{Nullable: true}, `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSchemaRoundTrip(t *testing.T) {
	in := map[string]*Schema{
		"a": {Type: "string", Enum: []any{"x", "y"}, Nullable: true},
		"b": {Ref: "#/components/schemas/Pet", Nullable: true},
		"c": {Type: "integer"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enum":["x","y",null]`) {
		t.Fatalf("nullable enum should include null: %s", b)
	}
	var out map[string]*Schema
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	for k, want := range in {
		got := out[k]
		if got.Type != want.Type || got.Ref != want.Ref || got.Nullable != want.Nullable || len(got.Enum) != len(want.Enum) {
			t.Errorf("%s: got %+v, want %+v", k, got, want)
		}
	}
	if err := json.Unmarshal([]byte(`{"type":3}`), &Schema{}); err == nil {
		t.Error("want error for invalid type")
	}
	if err := json.Unmarshal([]byte(`[]`), &Schema{}); err == nil {
		t.Error("want error for non-object schema")
	}
}
