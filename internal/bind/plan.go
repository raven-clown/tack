// Package bind builds request binding plans once per route and executes them per request.
package bind

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"
)

type source int

const (
	srcPath source = iota
	srcQuery
	srcHeader
	srcCookie
)

var sources = [...]struct {
	tag    string
	prefix string
}{
	srcPath:   {"path", "path"},
	srcQuery:  {"query", "query"},
	srcHeader: {"header", "header"},
	srcCookie: {"cookie", "cookie"},
}

// FieldError describes one problem with one request value.
type FieldError struct {
	Location string
	Message  string
}

// Error is returned by Plan.Bind. Status is 400, 413, or 422.
type Error struct {
	Status int
	Detail string
	Fields []FieldError
}

func (e *Error) Error() string { return e.Detail }

type step struct {
	src     source
	field   reflect.StructField
	name    string
	display string
	index   []int
	set     setter
	slice   bool
	def     *reflect.Value
}

type body struct {
	field    reflect.StructField
	index    []int
	typ      reflect.Type
	optional bool
}

// Plan binds an HTTP request into a struct of one type.
type Plan struct {
	steps     []step
	body      *body
	hasQuery  bool
	locations map[string]string
}

// New analyzes t and returns its plan. pathParams are the wildcard names in the route pattern.
func New(t reflect.Type, pathParams []string) (*Plan, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input type %s must be a struct; wrap values in a struct with path, query, header, or cookie tags", t)
	}
	p := &Plan{locations: map[string]string{}}
	seen := map[string]string{}
	if err := p.walk(t, nil, "", seen); err != nil {
		return nil, err
	}

	bound := map[string]bool{}
	for _, s := range p.steps {
		if s.src == srcPath {
			bound[s.name] = true
		}
	}
	params := map[string]bool{}
	for _, name := range pathParams {
		params[name] = true
		if !bound[name] {
			return nil, fmt.Errorf("path parameter {%s} has no field; add a field tagged path:%q to %s", name, name, t)
		}
	}
	for _, s := range p.steps {
		if s.src == srcPath && !params[s.name] {
			return nil, fmt.Errorf("field %s: path:%q is not in the route pattern; add {%s} to the path or change the tag", seen["path:"+s.name], s.name, s.name)
		}
	}
	return p, nil
}

func (p *Plan) walk(t reflect.Type, index []int, goPrefix string, seen map[string]string) error {
	for i := range t.NumField() {
		f := t.Field(i)
		idx := append(append([]int(nil), index...), i)
		goPath := goPrefix + f.Name

		src, name, n := sourceTag(f)
		if f.Anonymous && n == 0 {
			if f.Type.Kind() == reflect.Pointer {
				return fmt.Errorf("field %s: embedded pointer structs are not supported; embed the struct by value", goPath)
			}
			if f.Type.Kind() == reflect.Struct {
				if err := p.walk(f.Type, idx, goPath+".", seen); err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if n > 1 {
			return fmt.Errorf("field %s has more than one source tag; keep only one of path, query, header, or cookie", goPath)
		}
		if n == 0 {
			if f.Name == "Body" && goPrefix == "" {
				p.body = &body{field: f, index: idx, typ: f.Type, optional: f.Type.Kind() == reflect.Pointer}
				p.locations[goPath] = "body"
				continue
			}
			if f.Tag.Get("json") == "-" {
				continue
			}
			return fmt.Errorf("field %s has no source tag; add a path, query, header, or cookie tag, name it Body to read the JSON body, or tag it json:\"-\" to ignore it", goPath)
		}

		if name == "" || name == "-" {
			return fmt.Errorf("field %s: %s tag needs a name, e.g. %s:\"id\"", goPath, sources[src].tag, sources[src].tag)
		}
		display := name
		if src == srcHeader {
			name = textproto.CanonicalMIMEHeaderKey(name)
		}
		key := sources[src].tag + ":" + name
		if other, dup := seen[key]; dup {
			return fmt.Errorf("fields %s and %s both bind %s; each request value can bind to one field", other, goPath, key)
		}
		seen[key] = goPath

		ft := f.Type
		isSlice := ft.Kind() == reflect.Slice
		if isSlice {
			if src != srcQuery && src != srcHeader {
				return fmt.Errorf("field %s: %s values cannot be slices; use a query or header tag for repeated values", goPath, sources[src].tag)
			}
			ft = ft.Elem()
		}
		set, err := converter(ft)
		if err != nil {
			return fmt.Errorf("field %s: %w; use a string, bool, number, time.Time, or a type implementing encoding.TextUnmarshaler", goPath, err)
		}

		s := step{src: src, field: f, name: name, display: display, index: idx, set: set, slice: isSlice}
		if d, ok := f.Tag.Lookup("default"); ok {
			if isSlice {
				return fmt.Errorf("field %s: default is not supported on slices; remove the default tag", goPath)
			}
			v := reflect.New(f.Type).Elem()
			if err := set(v, d); err != nil {
				return fmt.Errorf("field %s: default %q %w; fix the default tag", goPath, d, err)
			}
			s.def = &v
		}
		if src == srcQuery {
			p.hasQuery = true
		}
		p.steps = append(p.steps, s)
		p.locations[goPath] = sources[src].prefix + "." + display
	}
	return nil
}

func sourceTag(f reflect.StructField) (source, string, int) {
	var (
		src  source
		name string
		n    int
	)
	for s, info := range sources {
		if v, ok := f.Tag.Lookup(info.tag); ok {
			src, name = source(s), v
			n++
		}
	}
	return src, name, n
}

// HasBody reports whether the input reads a JSON body.
func (p *Plan) HasBody() bool { return p.body != nil }

// Param describes one bound request value.
type Param struct {
	In    string
	Name  string
	Field reflect.StructField
}

// Params returns the plan's non-body values in field order.
func (p *Plan) Params() []Param {
	params := make([]Param, len(p.steps))
	for i, s := range p.steps {
		params[i] = Param{In: sources[s.src].tag, Name: s.display, Field: s.field}
	}
	return params
}

// Body returns the Body field and whether the body is required.
func (p *Plan) Body() (f reflect.StructField, required, ok bool) {
	if p.body == nil {
		return reflect.StructField{}, false, false
	}
	return p.body.field, !p.body.optional, true
}

// Locations maps Go field paths (e.g. "Body", "Filter.Page") to request locations
// (e.g. "body", "query.page").
func (p *Plan) Locations() map[string]string { return p.locations }

// Bind fills dst, which must be a pointer to the plan's struct type.
func (p *Plan) Bind(r *http.Request, dst any) error {
	if len(p.steps) == 0 && p.body == nil {
		return nil
	}
	v := reflect.ValueOf(dst).Elem()
	var fields []FieldError

	if len(p.steps) > 0 {
		var query map[string][]string
		if p.hasQuery {
			query = r.URL.Query()
		}
		for i := range p.steps {
			s := &p.steps[i]
			values := lookup(r, query, s)
			if len(values) == 0 || (len(values) == 1 && values[0] == "") {
				if s.def != nil {
					setDefault(v.FieldByIndex(s.index), *s.def)
				}
				continue
			}
			fv := v.FieldByIndex(s.index)
			loc := sources[s.src].prefix + "." + s.display
			if !s.slice {
				if err := s.set(fv, values[0]); err != nil {
					fields = append(fields, FieldError{loc, err.Error()})
				}
				continue
			}
			sv := reflect.MakeSlice(fv.Type(), len(values), len(values))
			for j, raw := range values {
				if err := s.set(sv.Index(j), raw); err != nil {
					fields = append(fields, FieldError{fmt.Sprintf("%s[%d]", loc, j), err.Error()})
				}
			}
			fv.Set(sv)
		}
	}

	if p.body != nil {
		if err := p.bindBody(r, v); err != nil {
			var be *Error
			if !errors.As(err, &be) || be.Status != http.StatusBadRequest {
				return err
			}
			fields = append(fields, be.Fields...)
		}
	}

	if len(fields) > 0 {
		return &Error{Status: http.StatusBadRequest, Detail: "malformed request", Fields: fields}
	}
	return nil
}

// setDefault copies pointer defaults so requests never share them.
func setDefault(dst, def reflect.Value) {
	if def.Kind() != reflect.Pointer {
		dst.Set(def)
		return
	}
	p := reflect.New(def.Type().Elem())
	p.Elem().Set(def.Elem())
	dst.Set(p)
}

func lookup(r *http.Request, query map[string][]string, s *step) []string {
	switch s.src {
	case srcPath:
		return []string{r.PathValue(s.name)}
	case srcQuery:
		return query[s.name]
	case srcHeader:
		return r.Header[s.name]
	default:
		c, err := r.Cookie(s.name)
		if err != nil {
			return nil
		}
		return []string{c.Value}
	}
}

func (p *Plan) bindBody(r *http.Request, v reflect.Value) error {
	fv := v.FieldByIndex(p.body.index)
	target := fv
	if p.body.optional {
		target = reflect.New(p.body.typ.Elem()).Elem()
	}

	var dec *json.Decoder
	err := io.EOF
	if r.Body != nil && r.Body != http.NoBody {
		dec = json.NewDecoder(r.Body)
		err = dec.Decode(target.Addr().Interface())
	}
	if errors.Is(err, io.EOF) {
		if p.body.optional {
			return nil
		}
		return &Error{Status: http.StatusUnprocessableEntity, Detail: "validation failed", Fields: []FieldError{{"body", "is required"}}}
	}
	if err != nil {
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return bodyError(err)
		}
		return &Error{Status: http.StatusBadRequest, Fields: []FieldError{{"body", "must contain a single JSON value"}}}
	}
	if p.body.optional {
		fv.Set(target.Addr())
	}
	return nil
}

func bodyError(err error) error {
	var (
		mbe *http.MaxBytesError
		ute *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &mbe):
		return &Error{Status: http.StatusRequestEntityTooLarge, Detail: fmt.Sprintf("request body exceeds %d bytes", mbe.Limit)}
	case errors.As(err, &ute):
		loc := "body"
		if ute.Field != "" {
			loc += "." + ute.Field
		}
		return &Error{Status: http.StatusBadRequest, Fields: []FieldError{{loc, "must be " + jsonKind(ute.Type)}}}
	default:
		return &Error{Status: http.StatusBadRequest, Fields: []FieldError{{"body", "is not valid JSON"}}}
	}
}

func jsonKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "an array"
	default:
		return "an object"
	}
}

// PathParams returns the wildcard names in a ServeMux path pattern.
func PathParams(path string) []string {
	var names []string
	for {
		i := strings.IndexByte(path, '{')
		if i < 0 {
			return names
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			return names
		}
		name := strings.TrimSuffix(path[i+1:i+j], "...")
		if name != "$" {
			names = append(names, name)
		}
		path = path[i+j+1:]
	}
}
