package tsgen

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/raven-clown/tack/openapi"
)

// typeOf returns the inline TypeScript type for s.
func (g *generator) typeOf(s *openapi.Schema) string {
	t := g.baseType(s)
	if s != nil && s.Nullable && t != "unknown" && t != "null" {
		t += " | null"
	}
	return t
}

func (g *generator) baseType(s *openapi.Schema) string {
	switch {
	case s == nil:
		return "unknown"
	case s.Ref != "":
		name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		ts, ok := g.names[name]
		if !ok {
			g.fail(fmt.Errorf("reference %q does not point to a schema in components.schemas", s.Ref))
			return "unknown"
		}
		return ts
	case len(s.AllOf) > 0:
		parts := make([]string, len(s.AllOf))
		for i, p := range s.AllOf {
			parts[i] = parenthesize(g.typeOf(p))
		}
		return strings.Join(parts, " & ")
	case len(s.OneOf) > 0 || len(s.AnyOf) > 0:
		var parts []string
		for _, p := range append(s.OneOf, s.AnyOf...) {
			parts = append(parts, g.typeOf(p))
		}
		return strings.Join(parts, " | ")
	case len(s.Enum) > 0:
		var parts []string
		for _, v := range s.Enum {
			if v != nil {
				parts = append(parts, literal(v))
			}
		}
		if len(parts) == 0 {
			return "null"
		}
		return strings.Join(parts, " | ")
	}
	switch s.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		return parenthesize(g.typeOf(s.Items)) + "[]"
	case "object", "":
		if len(s.Properties) == 0 {
			switch {
			case s.AdditionalProperties != nil:
				return "Record<string, " + g.typeOf(s.AdditionalProperties) + ">"
			case s.Type == "object":
				return "Record<string, unknown>"
			default:
				return "unknown"
			}
		}
		var props []string
		for _, name := range sortedKeys(s.Properties) {
			props = append(props, g.property(name, s.Properties[name], contains(s.Required, name)))
		}
		t := "{ " + strings.Join(props, "; ") + " }"
		if s.AdditionalProperties != nil {
			t += " & Record<string, " + g.typeOf(s.AdditionalProperties) + ">"
		}
		return t
	}
	g.fail(fmt.Errorf("unsupported schema type %q", s.Type))
	return "unknown"
}

// property returns `name?: type` for one object property.
func (g *generator) property(name string, s *openapi.Schema, required bool) string {
	opt := "?"
	if required {
		opt = ""
	}
	return propertyKey(name) + opt + ": " + g.typeOf(s)
}

// emitInterface writes a multi-line interface for an object schema.
func (g *generator) emitInterface(name string, s *openapi.Schema) {
	g.docComment("", s.Description, "")
	fmt.Fprintf(&g.buf, "export interface %s {\n", name)
	for _, p := range sortedKeys(s.Properties) {
		ps := s.Properties[p]
		g.docComment("  ", ps.Description, defaultTag(ps))
		fmt.Fprintf(&g.buf, "  %s;\n", g.property(p, ps, contains(s.Required, p)))
	}
	g.buf.WriteString("}\n\n")
}

// isPlainObject reports whether s can be written as an interface rather than
// a type alias.
func isPlainObject(s *openapi.Schema) bool {
	return s != nil && s.Ref == "" && !s.Nullable && len(s.AllOf) == 0 && len(s.OneOf) == 0 && len(s.AnyOf) == 0 &&
		len(s.Enum) == 0 && s.AdditionalProperties == nil && (s.Type == "object" || (s.Type == "" && len(s.Properties) > 0))
}

// docComment writes a JSDoc block when there is something to say.
func (g *generator) docComment(indent string, lines ...string) {
	var kept []string
	for _, l := range lines {
		if l != "" {
			kept = append(kept, strings.ReplaceAll(l, "*/", "*\\/"))
		}
	}
	if len(kept) == 0 {
		return
	}
	if len(kept) == 1 && !strings.Contains(kept[0], "\n") {
		fmt.Fprintf(&g.buf, "%s/** %s */\n", indent, kept[0])
		return
	}
	fmt.Fprintf(&g.buf, "%s/**\n", indent)
	for i, l := range kept {
		if i > 0 {
			fmt.Fprintf(&g.buf, "%s *\n", indent)
		}
		for _, line := range strings.Split(l, "\n") {
			fmt.Fprintf(&g.buf, "%s * %s\n", indent, strings.TrimRight(line, " "))
		}
	}
	fmt.Fprintf(&g.buf, "%s */\n", indent)
}

func defaultTag(s *openapi.Schema) string {
	if s == nil || s.Default == nil {
		return ""
	}
	return "@default " + literal(s.Default)
}

// literal writes a JSON value as a TypeScript literal.
func literal(v any) string {
	switch x := v.(type) {
	case string:
		return tsString(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "unknown"
	}
	return string(b)
}

// tsString quotes s as a TypeScript string literal.
func tsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// propertyKey quotes name when it is not a plain identifier.
func propertyKey(name string) string {
	if identifier.MatchString(name) {
		return name
	}
	return tsString(name)
}

// parenthesize wraps a union or intersection so it binds correctly inside
// another type.
func parenthesize(t string) string {
	if strings.Contains(t, " | ") || strings.Contains(t, " & ") {
		return "(" + t + ")"
	}
	return t
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (g *generator) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}
