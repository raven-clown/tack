// Package openapi defines the OpenAPI 3.1 document that tack generates.
package openapi

import "encoding/json"

// Version is the OpenAPI version tack generates.
const Version = "3.1.0"

// Document is an OpenAPI document.
type Document struct {
	OpenAPI    string               `json:"openapi"`
	Info       Info                 `json:"info"`
	Servers    []Server             `json:"servers,omitempty"`
	Paths      map[string]*PathItem `json:"paths"`
	Components *Components          `json:"components,omitempty"`
}

// Info describes the API.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Server is a base URL the API is served from.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// PathItem holds the operations on one path.
type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
	Patch  *Operation `json:"patch,omitempty"`
}

// Operation describes one route.
type Operation struct {
	OperationID string               `json:"operationId,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Deprecated  bool                 `json:"deprecated,omitempty"`
	Parameters  []*Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty"`
	Responses   map[string]*Response `json:"responses"`
}

// Parameter is a path, query, header, or cookie parameter.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema"`
}

// RequestBody describes a request body.
type RequestBody struct {
	Description string                `json:"description,omitempty"`
	Required    bool                  `json:"required,omitempty"`
	Content     map[string]*MediaType `json:"content"`
}

// Response describes one response status.
type Response struct {
	Description string                `json:"description"`
	Content     map[string]*MediaType `json:"content,omitempty"`
}

// MediaType holds the schema for one content type.
type MediaType struct {
	Schema *Schema `json:"schema"`
}

// Components holds reusable schemas.
type Components struct {
	Schemas map[string]*Schema `json:"schemas,omitempty"`
}

// Schema is a JSON Schema 2020-12 object.
//
// Nullable adds "null" to Type, or wraps Ref in anyOf with a null schema.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Nullable             bool               `json:"-"`
	Format               string             `json:"format,omitempty"`
	ContentEncoding      string             `json:"contentEncoding,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Default              any                `json:"default,omitempty"`
	Examples             []any              `json:"examples,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	ExclusiveMinimum     *float64           `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum     *float64           `json:"exclusiveMaximum,omitempty"`
	MinLength            *int               `json:"minLength,omitempty"`
	MaxLength            *int               `json:"maxLength,omitempty"`
	MinItems             *int               `json:"minItems,omitempty"`
	MaxItems             *int               `json:"maxItems,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
}

// SchemaProvider is implemented by types that define their own schema.
type SchemaProvider interface {
	JSONSchema() *Schema
}

// MarshalJSON encodes Nullable as a "null" type or an anyOf.
func (s *Schema) MarshalJSON() ([]byte, error) {
	type plain Schema
	if !s.Nullable {
		return json.Marshal((*plain)(s))
	}
	c := *s
	c.Nullable = false
	if c.Ref != "" {
		c.AnyOf = []*Schema{{Ref: c.Ref}, {Type: "null"}}
		c.Ref = ""
		return json.Marshal((*plain)(&c))
	}
	if c.Type == "" {
		return json.Marshal((*plain)(&c))
	}
	return json.Marshal(struct {
		Type []string `json:"type"`
		*plain
	}{[]string{c.Type, "null"}, (*plain)(&c)})
}
