// Package bind builds request binding plans once per route and executes them per request.
package bind

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"
)

// maxFormMemory is how much of a multipart body stays in memory before parts
// spill to temporary files.
const maxFormMemory = 32 << 20

var (
	fileType  = reflect.TypeFor[*multipart.FileHeader]()
	filesType = reflect.TypeFor[[]*multipart.FileHeader]()
)

type source int

const (
	srcPath source = iota
	srcQuery
	srcHeader
	srcCookie
	srcForm
)

var sources = [...]struct {
	tag    string
	prefix string
}{
	srcPath:   {"path", "path"},
	srcQuery:  {"query", "query"},
	srcHeader: {"header", "header"},
	srcCookie: {"cookie", "cookie"},
	srcForm:   {"form", "form"},
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
	loc     string
	index   []int
	set     setter
	slice   bool
	file    bool // a *multipart.FileHeader or a slice of them
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
	hasForm   bool
	hasFile   bool
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
	if p.hasForm && p.body != nil {
		return nil, fmt.Errorf("input type %s has both a Body field and form fields; a request body is JSON or a form, not both", t)
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
			return fmt.Errorf("field %s has more than one source tag; keep only one of path, query, header, cookie, or form", goPath)
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
			return fmt.Errorf("field %s has no source tag; add a path, query, header, cookie, or form tag, name it Body to read the JSON body, or tag it json:\"-\" to ignore it", goPath)
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
		if src == srcForm {
			p.hasForm = true
		}
		if ft == fileType || ft == filesType {
			if src != srcForm {
				return fmt.Errorf("field %s: files come from multipart forms; use a form tag instead of %s", goPath, sources[src].tag)
			}
			if _, ok := f.Tag.Lookup("default"); ok {
				return fmt.Errorf("field %s: default is not supported on files; remove the default tag", goPath)
			}
			p.hasFile = true
			p.steps = append(p.steps, step{src: src, field: f, name: name, display: display, loc: "form." + display, index: idx, slice: ft == filesType, file: true})
			p.locations[goPath] = "form." + display
			continue
		}
		isSlice := ft.Kind() == reflect.Slice
		if isSlice {
			if src != srcQuery && src != srcHeader && src != srcForm {
				return fmt.Errorf("field %s: %s values cannot be slices; use a query, header, or form tag for repeated values", goPath, sources[src].tag)
			}
			ft = ft.Elem()
		}
		set, err := converter(ft)
		if err != nil {
			return fmt.Errorf("field %s: %w; use a string, bool, number, time.Time, a type implementing encoding.TextUnmarshaler, or *multipart.FileHeader in a form", goPath, err)
		}

		s := step{src: src, field: f, name: name, display: display, loc: sources[src].prefix + "." + display, index: idx, set: set, slice: isSlice}
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

// HasForm reports whether the input reads a form body (form tags), and
// whether any of its fields is a file.
func (p *Plan) HasForm() (form, files bool) { return p.hasForm, p.hasFile }

// Param describes one bound request value.
type Param struct {
	In    string
	Name  string
	Field reflect.StructField
	File  bool // a form field holding one or more uploaded files
}

// Params returns the plan's path, query, header, and cookie values in field
// order.
func (p *Plan) Params() []Param {
	var params []Param
	for _, s := range p.steps {
		if s.src != srcForm {
			params = append(params, Param{In: sources[s.src].tag, Name: s.display, Field: s.field})
		}
	}
	return params
}

// FormFields returns the plan's form values and files in field order.
func (p *Plan) FormFields() []Param {
	var fields []Param
	for _, s := range p.steps {
		if s.src == srcForm {
			fields = append(fields, Param{In: "form", Name: s.display, Field: s.field, File: s.file})
		}
	}
	return fields
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

	if p.hasForm {
		if err := p.parseForm(r); err != nil {
			return err
		}
	}
	if len(p.steps) > 0 {
		for i := range p.steps {
			s := &p.steps[i]
			if s.file {
				bindFiles(r, v.FieldByIndex(s.index), s)
				continue
			}
			first, values, found := lookup(r, s)
			if !found || (!s.slice && first == "") || (s.slice && len(values) == 1 && values[0] == "") {
				if s.def != nil {
					setDefault(v.FieldByIndex(s.index), *s.def)
				}
				continue
			}
			fv := v.FieldByIndex(s.index)
			if !s.slice {
				if err := s.set(fv, first); err != nil {
					fields = append(fields, FieldError{s.loc, err.Error()})
				}
				continue
			}
			if s.src == srcHeader {
				values = splitList(values)
			}
			sv := reflect.MakeSlice(fv.Type(), len(values), len(values))
			for j, raw := range values {
				if err := s.set(sv.Index(j), raw); err != nil {
					fields = append(fields, FieldError{fmt.Sprintf("%s[%d]", s.loc, j), err.Error()})
				}
			}
			fv.Set(sv)
		}
	}

	if p.body != nil {
		if err := p.bindBody(r, v); err != nil {
			var be *Error
			if !errors.As(err, &be) {
				return err
			}
			switch be.Status {
			case http.StatusBadRequest:
				fields = append(fields, be.Fields...)
			case http.StatusUnprocessableEntity:
				if len(fields) == 0 {
					return err
				}
				fields = append(fields, be.Fields...)
			default:
				return err
			}
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

// lookup returns the request's values for s: the first one for single
// fields, all of them for slices. It allocates only for slices.
func lookup(r *http.Request, s *step) (first string, values []string, found bool) {
	switch s.src {
	case srcPath:
		return r.PathValue(s.name), nil, true
	case srcQuery:
		return queryValues(r.URL.RawQuery, s.name, s.slice)
	case srcHeader:
		values = r.Header[s.name]
	case srcForm:
		values = r.PostForm[s.name]
	default:
		c, err := r.Cookie(s.name)
		if err != nil {
			return "", nil, false
		}
		return c.Value, nil, true
	}
	if len(values) == 0 {
		return "", nil, false
	}
	return values[0], values, true
}

// queryValues scans a raw query for name without building the whole map, the
// way url.ParseQuery would read it: pairs split on "&", pairs holding ";"
// and pairs that do not unescape are skipped. With all set it collects every
// value; otherwise it stops at the first.
func queryValues(raw, name string, all bool) (first string, values []string, found bool) {
	for raw != "" {
		var pair string
		pair, raw, _ = strings.Cut(raw, "&")
		if pair == "" || strings.Contains(pair, ";") {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		if strings.ContainsAny(key, "%+") {
			k, err := url.QueryUnescape(key)
			if err != nil {
				continue
			}
			key = k
		}
		if key != name {
			continue
		}
		if strings.ContainsAny(value, "%+") {
			v, err := url.QueryUnescape(value)
			if err != nil {
				continue
			}
			value = v
		}
		if !found {
			first, found = value, true
			if !all {
				return first, nil, true
			}
		}
		values = append(values, value)
	}
	return first, values, found
}

// parseForm reads a multipart or URL-encoded form body. A form with files
// is read with r.ParseMultipartForm, whose temporary files the caller removes
// with r.MultipartForm.RemoveAll once the handler is done.
func (p *Plan) parseForm(r *http.Request) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil && r.Header.Get("Content-Type") != "" {
		return &Error{Status: http.StatusUnsupportedMediaType, Detail: fmt.Sprintf("content type %q is not supported; send multipart/form-data", r.Header.Get("Content-Type"))}
	}
	switch mt {
	case "multipart/form-data":
		err = r.ParseMultipartForm(maxFormMemory)
	case "application/x-www-form-urlencoded", "":
		err = r.ParseForm()
	default:
		return &Error{Status: http.StatusUnsupportedMediaType, Detail: fmt.Sprintf("content type %q is not supported; send multipart/form-data", mt)}
	}
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || limitHit(r.Body) {
			return &Error{Status: http.StatusRequestEntityTooLarge, Detail: "request body is too large"}
		}
		return &Error{Status: http.StatusBadRequest, Detail: "malformed request", Fields: []FieldError{{"form", "is not a valid form: " + err.Error()}}}
	}
	return nil
}

// limitHit reports whether a body wrapped by http.MaxBytesReader has reached
// its limit, which the multipart reader reports as a plain EOF.
func limitHit(body io.Reader) bool {
	var b [1]byte
	_, err := body.Read(b[:])
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func bindFiles(r *http.Request, fv reflect.Value, s *step) {
	if r.MultipartForm == nil {
		return
	}
	files := r.MultipartForm.File[s.name]
	if len(files) == 0 {
		return
	}
	if s.slice {
		fv.Set(reflect.ValueOf(files))
		return
	}
	fv.Set(reflect.ValueOf(files[0]))
}

func (p *Plan) bindBody(r *http.Request, v reflect.Value) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSON(ct) {
		return &Error{Status: http.StatusUnsupportedMediaType, Detail: fmt.Sprintf("content type %q is not supported; send application/json", ct)}
	}
	var data []byte
	if r.Body != nil && r.Body != http.NoBody {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return bodyError(err)
		}
		data = bytes.TrimSpace(b)
	}
	if len(data) == 0 || string(data) == "null" {
		if p.body.optional {
			return nil
		}
		return &Error{Status: http.StatusUnprocessableEntity, Detail: "validation failed", Fields: []FieldError{{"body", "is required"}}}
	}

	fv := v.FieldByIndex(p.body.index)
	target := fv
	if p.body.optional {
		target = reflect.New(p.body.typ.Elem()).Elem()
	}
	if err := json.Unmarshal(data, target.Addr().Interface()); err != nil {
		return bodyError(err)
	}
	if p.body.optional {
		fv.Set(target.Addr())
	}
	return nil
}

func isJSON(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func bodyError(err error) error {
	var (
		mbe *http.MaxBytesError
		ute *json.UnmarshalTypeError
		se  *json.SyntaxError
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
	case errors.As(err, &se):
		return &Error{Status: http.StatusBadRequest, Fields: []FieldError{{"body", "is not valid JSON"}}}
	default:
		return &Error{Status: http.StatusBadRequest, Fields: []FieldError{{"body", "contains an invalid value"}}}
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
