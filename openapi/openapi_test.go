package openapi

import (
	"encoding/json"
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
