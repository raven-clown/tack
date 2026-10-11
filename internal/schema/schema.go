// Package schema converts Go types to JSON Schema 2020-12.
package schema

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/raven-clown/tack/openapi"
)

var (
	timeType      = reflect.TypeFor[time.Time]()
	rawType       = reflect.TypeFor[json.RawMessage]()
	providerType  = reflect.TypeFor[openapi.SchemaProvider]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
)

// Registry stores named schemas and hands out $ref links to them.
type Registry struct {
	schemas map[string]*openapi.Schema
	names   map[reflect.Type]string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{schemas: map[string]*openapi.Schema{}, names: map[reflect.Type]string{}}
}

// Schemas returns every named schema, keyed by component name.
func (r *Registry) Schemas() map[string]*openapi.Schema { return r.schemas }

// Schema returns the schema for t. Named structs and providers are
// registered once and returned as $ref.
func (r *Registry) Schema(t reflect.Type) (*openapi.Schema, error) {
	if t.Kind() == reflect.Pointer {
		s, err := r.Schema(t.Elem())
		if err != nil {
			return nil, err
		}
		c := *s
		c.Nullable = true
		return &c, nil
	}

	if t.Kind() != reflect.Interface && implements(t, providerType) {
		return r.named(t, func() (*openapi.Schema, error) {
			s := reflect.New(t).Interface().(openapi.SchemaProvider).JSONSchema()
			if s == nil {
				return nil, fmt.Errorf("%s.JSONSchema returned nil", t)
			}
			return s, nil
		})
	}

	switch {
	case t == timeType:
		return &openapi.Schema{Type: "string", Format: "date-time"}, nil
	case t == rawType:
		return &openapi.Schema{}, nil
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		return &openapi.Schema{Type: "string", ContentEncoding: "base64"}, nil
	case implements(t, jsonMarshaler):
		return &openapi.Schema{}, nil
	case implements(t, textMarshaler):
		return &openapi.Schema{Type: "string"}, nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return &openapi.Schema{Type: "boolean"}, nil
	case reflect.String:
		return &openapi.Schema{Type: "string"}, nil
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return &openapi.Schema{Type: "integer", Format: "int32"}, nil
	case reflect.Int, reflect.Int64:
		return &openapi.Schema{Type: "integer", Format: "int64"}, nil
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &openapi.Schema{Type: "integer", Format: "int32", Minimum: ptr(0.0)}, nil
	case reflect.Uint, reflect.Uint64:
		return &openapi.Schema{Type: "integer", Format: "int64", Minimum: ptr(0.0)}, nil
	case reflect.Float32:
		return &openapi.Schema{Type: "number", Format: "float"}, nil
	case reflect.Float64:
		return &openapi.Schema{Type: "number", Format: "double"}, nil
	case reflect.Interface:
		return &openapi.Schema{}, nil
	case reflect.Slice, reflect.Array:
		items, err := r.Schema(t.Elem())
		if err != nil {
			return nil, err
		}
		s := &openapi.Schema{Type: "array", Items: items}
		if t.Kind() == reflect.Array {
			s.MinItems, s.MaxItems = ptr(t.Len()), ptr(t.Len())
		}
		return s, nil
	case reflect.Map:
		k := t.Key()
		if k.Kind() != reflect.String && !isInt(k.Kind()) && !isUint(k.Kind()) && !k.Implements(textMarshaler) {
			return nil, fmt.Errorf("map key type %s is not supported; use string or integer keys", k)
		}
		values, err := r.Schema(t.Elem())
		if err != nil {
			return nil, err
		}
		return &openapi.Schema{Type: "object", AdditionalProperties: values}, nil
	case reflect.Struct:
		if t.Name() == "" {
			return r.object(t)
		}
		return r.named(t, func() (*openapi.Schema, error) { return r.object(t) })
	}
	return nil, fmt.Errorf("type %s is not supported", t)
}

// SchemaAs is like Schema but registers struct type t under name.
func (r *Registry) SchemaAs(t reflect.Type, name string) (*openapi.Schema, error) {
	if _, ok := r.names[t]; !ok {
		if _, taken := r.schemas[name]; !taken {
			r.names[t] = name
			r.schemas[name] = &openapi.Schema{}
			s, err := r.object(t)
			if err != nil {
				delete(r.names, t)
				delete(r.schemas, name)
				return nil, err
			}
			r.schemas[name] = s
		}
	}
	return r.Schema(t)
}

// Define registers a hand-built schema under name and returns a $ref to it.
// Defining the same name again with an equal schema is fine; a different
// schema is an error.
func (r *Registry) Define(name string, s *openapi.Schema) (*openapi.Schema, error) {
	if old, taken := r.schemas[name]; taken {
		if !reflect.DeepEqual(old, s) {
			return nil, fmt.Errorf("schema %q is already defined differently", name)
		}
		return &openapi.Schema{Ref: ref(name)}, nil
	}
	r.schemas[name] = s
	return &openapi.Schema{Ref: ref(name)}, nil
}

func (r *Registry) named(t reflect.Type, build func() (*openapi.Schema, error)) (*openapi.Schema, error) {
	if name, ok := r.names[t]; ok {
		return &openapi.Schema{Ref: ref(name)}, nil
	}
	name := r.name(t)
	r.names[t] = name
	r.schemas[name] = &openapi.Schema{}
	s, err := build()
	if err != nil {
		delete(r.names, t)
		delete(r.schemas, name)
		return nil, err
	}
	r.schemas[name] = s
	return &openapi.Schema{Ref: ref(name)}, nil
}

func ref(name string) string { return "#/components/schemas/" + name }

func (r *Registry) name(t reflect.Type) string {
	base := clean(t.Name())
	if _, taken := r.schemas[base]; !taken {
		return base
	}
	pkg := t.PkgPath()
	if i := strings.LastIndexByte(pkg, '/'); i >= 0 {
		pkg = pkg[i+1:]
	}
	prefixed := clean(pascal(pkg) + base)
	name := prefixed
	for i := 2; ; i++ {
		if _, taken := r.schemas[name]; !taken {
			return name
		}
		name = prefixed + strconv.Itoa(i)
	}
}

// clean turns generic names like "Page[example.com/x.Pet]" into "PagePet".
func clean(name string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '[' || r == ']' || r == ',' }) {
		if i := strings.LastIndexByte(part, '.'); i >= 0 {
			part = part[i+1:]
		}
		for _, c := range part {
			if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' {
				b.WriteRune(c)
			}
		}
	}
	return b.String()
}

func pascal(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Field returns the schema for a struct field, applying doc, example, default,
// and validate tags.
func (r *Registry) Field(f reflect.StructField) (*openapi.Schema, error) {
	s, err := r.Schema(f.Type)
	if err != nil {
		return nil, err
	}
	doc, hasDoc := f.Tag.Lookup("doc")
	ex, hasEx := f.Tag.Lookup("example")
	def, hasDef := f.Tag.Lookup("default")
	rules := f.Tag.Get("validate")
	if !hasDoc && !hasEx && !hasDef && rules == "" {
		return s, nil
	}

	c := *s
	if hasDoc {
		c.Description = doc
	}
	if hasEx {
		c.Examples = []any{literal(f.Type, ex)}
	}
	if hasDef {
		c.Default = literal(f.Type, def)
	}
	applyRules(&c, f.Type, rules)
	return &c, nil
}

func applyRules(s *openapi.Schema, t reflect.Type, rules string) {
	if rules == "" {
		return
	}
	head, tail, dive := splitDive(rules)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for _, rule := range strings.Split(head, ",") {
		name, param, _ := strings.Cut(rule, "=")
		applyRule(s, t, name, param)
	}
	if dive && s.Items != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		items := *s.Items
		applyRules(&items, t.Elem(), tail)
		s.Items = &items
	}
}

func applyRule(s *openapi.Schema, t reflect.Type, name, param string) {
	n, numErr := strconv.ParseFloat(param, 64)
	size := numErr == nil
	switch name {
	case "min", "gte", "max", "lte", "gt", "lt", "len":
		if !size {
			return
		}
	case "eq":
		if !size && t.Kind() != reflect.String {
			return
		}
	}
	switch name {
	case "min", "gte":
		setBound(s, t, n, true, false)
	case "max", "lte":
		setBound(s, t, n, false, false)
	case "gt":
		setBound(s, t, n, true, true)
	case "lt":
		setBound(s, t, n, false, true)
	case "len":
		setBound(s, t, n, true, false)
		setBound(s, t, n, false, false)
	case "eq":
		if t.Kind() == reflect.String {
			s.Enum = []any{param}
			return
		}
		setBound(s, t, n, true, false)
		setBound(s, t, n, false, false)
	case "oneof":
		for _, v := range strings.Fields(param) {
			s.Enum = append(s.Enum, literal(t, v))
		}
	case "email":
		s.Format = "email"
	case "url", "http_url":
		s.Format = "uri"
	case "uuid", "uuid4":
		s.Format = "uuid"
	}
}

func setBound(s *openapi.Schema, t reflect.Type, n float64, lower, exclusive bool) {
	i := int(n)
	switch t.Kind() {
	case reflect.String:
		if exclusive {
			if lower {
				i++
			} else {
				i--
			}
		}
		if lower {
			s.MinLength = &i
		} else {
			s.MaxLength = &i
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		if exclusive {
			if lower {
				i++
			} else {
				i--
			}
		}
		switch {
		case t.Kind() == reflect.Map && lower:
			s.MinProperties = &i
		case t.Kind() == reflect.Map:
			s.MaxProperties = &i
		case lower:
			s.MinItems = &i
		default:
			s.MaxItems = &i
		}
	default:
		switch {
		case lower && exclusive:
			s.ExclusiveMinimum = &n
		case lower:
			s.Minimum = &n
		case exclusive:
			s.ExclusiveMaximum = &n
		default:
			s.Maximum = &n
		}
	}
}

// literal converts a tag value to the JSON value of t, falling back to the string.
func literal(t reflect.Type, v string) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case isInt(t.Kind()):
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case isUint(t.Kind()):
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	case t.Kind() == reflect.Bool:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return v
}

// splitDive splits "min=1,dive,max=5" into the slice rules and the item rules.
func splitDive(rules string) (head, tail string, dive bool) {
	head, tail, dive = strings.Cut(","+rules+",", ",dive,")
	return strings.Trim(head, ","), strings.Trim(tail, ","), dive
}

func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

func hasRule(rules, name string) bool {
	head, _, _ := splitDive(rules)
	for _, r := range strings.Split(head, ",") {
		if r == name {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
