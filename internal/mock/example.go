package mock

import (
	"strings"

	"github.com/raven-clown/tack/openapi"
)

// maxDepth bounds recursion through self-referencing schemas.
const maxDepth = 4

// example builds a value that fits s. hint is the property or header name the
// value is for, used for strings with no better source.
func (m *mock) example(s *openapi.Schema, hint string, depth int) any {
	if s == nil {
		return nil
	}
	if s.Ref != "" {
		if depth > maxDepth || m.doc.Components == nil {
			return nil
		}
		name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		return m.example(m.doc.Components.Schemas[name], hint, depth+1)
	}
	switch {
	case len(s.Examples) > 0:
		return s.Examples[0]
	case s.Default != nil:
		return s.Default
	case len(s.Enum) > 0:
		for _, v := range s.Enum {
			if v != nil {
				return v
			}
		}
		return nil
	case len(s.AllOf) > 0:
		merged := map[string]any{}
		for _, p := range s.AllOf {
			if obj, ok := m.example(p, hint, depth).(map[string]any); ok {
				for k, v := range obj {
					merged[k] = v
				}
			}
		}
		return merged
	case len(s.OneOf) > 0:
		return m.example(s.OneOf[0], hint, depth)
	case len(s.AnyOf) > 0:
		return m.example(s.AnyOf[0], hint, depth)
	}
	switch s.Type {
	case "string":
		return exampleString(s, hint)
	case "integer", "number":
		return exampleNumber(s)
	case "boolean":
		return true
	case "null":
		return nil
	case "array":
		n := 1
		if s.MinItems != nil && *s.MinItems > n {
			n = *s.MinItems
		}
		if s.MaxItems != nil && *s.MaxItems < n {
			n = *s.MaxItems
		}
		if depth > maxDepth {
			n = 0
		}
		items := make([]any, 0, n)
		for range n {
			items = append(items, m.example(s.Items, hint, depth+1))
		}
		return items
	case "object", "":
		if s.Type == "" && len(s.Properties) == 0 && s.AdditionalProperties == nil {
			return nil
		}
		obj := map[string]any{}
		for _, name := range sortedKeys(s.Properties) {
			v := m.example(s.Properties[name], name, depth+1)
			if v == nil && !contains(s.Required, name) {
				continue
			}
			obj[name] = v
		}
		if s.AdditionalProperties != nil && len(s.Properties) == 0 {
			obj["key"] = m.example(s.AdditionalProperties, "key", depth+1)
		}
		return obj
	}
	return nil
}

func exampleString(s *openapi.Schema, hint string) string {
	var v string
	switch {
	case s.Format == "date-time":
		return "2026-01-02T15:04:05Z"
	case s.Format == "date":
		return "2026-01-02"
	case s.Format == "email":
		return "user@example.com"
	case s.Format == "uuid":
		return "123e4567-e89b-12d3-a456-426614174000"
	case s.Format == "uri":
		return "https://example.com/" + strings.ToLower(hint)
	case s.ContentEncoding == "base64":
		return "aGVsbG8="
	case hint != "":
		v = hint
	default:
		v = "string"
	}
	if s.MinLength != nil {
		for len(v) < *s.MinLength {
			v += v
		}
	}
	if s.MaxLength != nil && len(v) > *s.MaxLength {
		v = v[:*s.MaxLength]
	}
	return v
}

func exampleNumber(s *openapi.Schema) any {
	v := 1.0
	switch {
	case s.Minimum != nil:
		v = *s.Minimum
	case s.ExclusiveMinimum != nil:
		v = *s.ExclusiveMinimum + 1
	}
	if s.Maximum != nil && v > *s.Maximum {
		v = *s.Maximum
	}
	if s.ExclusiveMaximum != nil && v >= *s.ExclusiveMaximum {
		v = *s.ExclusiveMaximum - 1
	}
	if s.Type == "integer" {
		return int64(v)
	}
	return v
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
