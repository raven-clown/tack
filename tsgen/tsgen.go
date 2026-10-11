// Package tsgen generates a TypeScript client from an OpenAPI document.
//
// The client is one dependency-free file built on fetch. Every operation
// becomes a method on Client that returns a Result: on success it holds the
// typed data and the documented response headers; on a documented error it
// holds the problem, typed as a union of the problem schemas the operation
// declares, so callers can switch on its code. Responses the document does
// not describe are thrown as ApiError.
//
// Generation reads only the document, never Go types, so the client, the
// document, and the docs page always agree. The output is deterministic:
// the same document produces byte-identical TypeScript.
package tsgen

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/raven-clown/tack/openapi"
)

// ErrStale is wrapped by the error Check returns when the client file is
// missing or differs from what the document generates.
var ErrStale = errors.New("TypeScript client is stale")

// Generate returns the TypeScript client for doc. It fails when the document
// holds something the client cannot express, such as two operations with the
// same ID, a request body that is not JSON, or a schema whose name collides
// with a name the client needs; the error says what to rename.
func Generate(doc *openapi.Document) ([]byte, error) {
	g := &generator{doc: doc, names: map[string]string{}, taken: map[string]string{}}
	if err := g.collect(); err != nil {
		return nil, fmt.Errorf("tsgen: %w", err)
	}
	g.emit()
	if g.err != nil {
		return nil, fmt.Errorf("tsgen: %w", g.err)
	}
	return g.buf.Bytes(), nil
}

// Check compares the file at path with the client Generate produces for doc.
// It returns nil when they match and an error wrapping ErrStale when the file
// is missing or differs, naming the first line that does. Line endings are
// ignored, so a checkout that converts them still passes.
//
// Call it from a test so CI fails when the committed client is older than the
// routes:
//
//	func TestClient(t *testing.T) {
//		if err := tsgen.Check(newApp().OpenAPI(), "client/api.ts"); err != nil {
//			t.Fatal(err)
//		}
//	}
func Check(doc *openapi.Document, path string) error {
	want, err := Generate(doc)
	if err != nil {
		return err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s does not exist; generate it with tsgen.Generate or `tack gen ts`", ErrStale, path)
		}
		return fmt.Errorf("tsgen: %w", err)
	}
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if bytes.Equal(got, want) {
		return nil
	}
	return fmt.Errorf("%w: %s differs from the document at line %d; regenerate it with tsgen.Generate or `tack gen ts`", ErrStale, path, firstDiff(got, want))
}

// firstDiff returns the 1-based number of the first line where a and b differ.
func firstDiff(a, b []byte) int {
	al, bl := bytes.Split(a, []byte("\n")), bytes.Split(b, []byte("\n"))
	for i := range min(len(al), len(bl)) {
		if !bytes.Equal(al[i], bl[i]) {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}

// reserved are the identifiers the generated runtime defines. A schema or
// operation that needs one of them fails generation.
var reserved = []string{
	"Client", "ClientOptions", "RequestOptions", "Credential", "SecurityName",
	"Result", "Ok", "Err", "ApiError", "unwrap", "Op", "HeaderKind",
	"securitySchemes", "appendQuery", "readHeaders", "problemMessage", "isJSON",
}

// reservedMembers are the members of Client an operation ID may not shadow.
var reservedMembers = []string{"constructor", "options", "send", "applySecurity"}

var identifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

type generator struct {
	doc   *openapi.Document
	names map[string]string // component schema name -> TypeScript type name
	taken map[string]string // TypeScript identifier -> what owns it
	ops   []*operation
	buf   bytes.Buffer
	err   error // the first problem found while emitting
}

type operation struct {
	id, method, path     string
	typeName             string // the ID with its first letter upper-cased
	summary, description string
	deprecated           bool
	params               []*openapi.Parameter // path, then query, then header, each sorted by name
	cookies              []string
	body                 *openapi.RequestBody
	bodySchema           *openapi.Schema
	okStatus             int
	okSchema             *openapi.Schema // nil when the response has no body
	headers              []*header       // sorted by name
	errors               []*errResponse  // sorted by status
	security             []openapi.SecurityRequirement
}

type header struct {
	name     string
	required bool
	schema   *openapi.Schema
	kind     string // string, number, boolean, array, or numbers
}

type errResponse struct {
	status int
	schema *openapi.Schema // nil when the response has no JSON body
}

func (g *generator) reserve(name, owner string) error {
	if other, ok := g.taken[name]; ok {
		return fmt.Errorf("%s needs the TypeScript name %s, which %s already uses; rename one of them", owner, name, other)
	}
	g.taken[name] = owner
	return nil
}

// collect validates the document and plans every name before anything is
// written, so a problem is reported once with a fix instead of as broken
// output.
func (g *generator) collect() error {
	for _, r := range reserved {
		g.taken[r] = "the generated client"
	}
	var schemas map[string]*openapi.Schema
	if g.doc.Components != nil {
		schemas = g.doc.Components.Schemas
	}
	for _, name := range sortedKeys(schemas) {
		ts := cleanIdentifier(name)
		if ts == "" {
			return fmt.Errorf("schema %q has no characters usable in a TypeScript name", name)
		}
		if err := g.reserve(ts, "schema "+strconv.Quote(name)); err != nil {
			return err
		}
		g.names[name] = ts
	}

	ids := map[string]string{}
	for _, p := range sortedKeys(g.doc.Paths) {
		item := g.doc.Paths[p]
		if item == nil {
			continue
		}
		for _, m := range methods {
			op := m.get(item)
			if op == nil {
				continue
			}
			o, err := g.operation(m.name, p, op)
			if err != nil {
				return fmt.Errorf("%s %s: %w", m.name, p, err)
			}
			if other, dup := ids[o.id]; dup {
				return fmt.Errorf("%s %s: operation ID %q is already used by %s; set a unique one", m.name, p, o.id, other)
			}
			ids[o.id] = m.name + " " + p
			for _, suffix := range []string{"Input", "Error", "Headers"} {
				if err := g.reserve(o.typeName+suffix, "operation "+o.id); err != nil {
					return err
				}
			}
			g.ops = append(g.ops, o)
		}
	}
	return nil
}

var methods = []struct {
	name string
	get  func(*openapi.PathItem) *openapi.Operation
}{
	{"GET", func(i *openapi.PathItem) *openapi.Operation { return i.Get }},
	{"POST", func(i *openapi.PathItem) *openapi.Operation { return i.Post }},
	{"PUT", func(i *openapi.PathItem) *openapi.Operation { return i.Put }},
	{"PATCH", func(i *openapi.PathItem) *openapi.Operation { return i.Patch }},
	{"DELETE", func(i *openapi.PathItem) *openapi.Operation { return i.Delete }},
}

func (g *generator) operation(method, path string, op *openapi.Operation) (*operation, error) {
	o := &operation{
		id:          op.OperationID,
		method:      method,
		path:        path,
		summary:     op.Summary,
		description: op.Description,
		deprecated:  op.Deprecated,
		body:        op.RequestBody,
		security:    op.Security,
	}
	if o.id == "" {
		o.id = operationID(method, path)
	}
	if !identifier.MatchString(o.id) {
		return nil, fmt.Errorf("operation ID %q is not a TypeScript identifier; set one with tack.OperationID", o.id)
	}
	if slices.Contains(reservedMembers, o.id) {
		return nil, fmt.Errorf("operation ID %q is a member of the generated Client; set another with tack.OperationID", o.id)
	}
	o.typeName = strings.ToUpper(o.id[:1]) + o.id[1:]

	seen := map[string]string{}
	for _, in := range []string{"path", "query", "header"} {
		var group []*openapi.Parameter
		for _, p := range op.Parameters {
			if p.In == in {
				group = append(group, p)
			}
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].Name < group[j].Name })
		o.params = append(o.params, group...)
	}
	for _, p := range op.Parameters {
		switch p.In {
		case "cookie":
			o.cookies = append(o.cookies, p.Name)
		case "path", "query", "header":
			if other, dup := seen[p.Name]; dup {
				return nil, fmt.Errorf("parameters %q in %s and in %s would share one input property; rename one", p.Name, other, p.In)
			}
			seen[p.Name] = p.In
		default:
			return nil, fmt.Errorf("parameter %q: unsupported location %q", p.Name, p.In)
		}
	}
	sort.Strings(o.cookies)

	if o.body != nil {
		if _, dup := seen["body"]; dup {
			return nil, fmt.Errorf("parameter %q would share the input property the request body uses; rename it", "body")
		}
		mt := o.body.Content["application/json"]
		if mt == nil {
			return nil, fmt.Errorf("request body has no application/json content; the client sends JSON only")
		}
		o.bodySchema = mt.Schema
	}

	var ok []int
	for key, res := range op.Responses {
		status, err := strconv.Atoi(key)
		if err != nil {
			continue // "default" and ranges such as 2XX are not documented statuses
		}
		switch {
		case status >= 200 && status < 400:
			ok = append(ok, status)
		case status >= 400:
			e := &errResponse{status: status}
			if res != nil {
				if mt := res.Content["application/problem+json"]; mt != nil {
					e.schema = mt.Schema
				} else if mt := res.Content["application/json"]; mt != nil {
					e.schema = mt.Schema
				}
			}
			o.errors = append(o.errors, e)
		}
	}
	sort.Ints(ok)
	sort.Slice(o.errors, func(i, j int) bool { return o.errors[i].status < o.errors[j].status })
	switch len(ok) {
	case 0:
		return nil, fmt.Errorf("no 2xx or 3xx response")
	case 1:
	default:
		return nil, fmt.Errorf("several success responses (%v); the client expects one", ok)
	}
	o.okStatus = ok[0]
	res := op.Responses[strconv.Itoa(o.okStatus)]
	if res != nil {
		if len(res.Content) > 0 {
			mt := res.Content["application/json"]
			if mt == nil {
				return nil, fmt.Errorf("response %d has no application/json content; the client reads JSON only", o.okStatus)
			}
			o.okSchema = mt.Schema
			if o.okSchema == nil {
				o.okSchema = &openapi.Schema{}
			}
		}
		for _, name := range sortedKeys(res.Headers) {
			h := res.Headers[name]
			o.headers = append(o.headers, &header{name: name, required: h.Required, schema: h.Schema, kind: headerKind(h.Schema)})
		}
	}

	for _, req := range o.security {
		for name := range req {
			if g.doc.Components == nil || g.doc.Components.SecuritySchemes[name] == nil {
				return nil, fmt.Errorf("security scheme %q is not in components.securitySchemes", name)
			}
		}
	}
	return o, nil
}

// headerKind picks how the runtime parses a response header value.
func headerKind(s *openapi.Schema) string {
	if s == nil {
		return "string"
	}
	switch s.Type {
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if s.Items != nil && (s.Items.Type == "integer" || s.Items.Type == "number") {
			return "numbers"
		}
		return "array"
	}
	return "string"
}

// operationID derives an ID such as "getPetsById" from GET /pets/{id}, the
// same way tack does, for documents that leave operationId out.
func operationID(method, route string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(route, "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			b.WriteString("By")
			seg = seg[1 : len(seg)-1]
		}
		for _, part := range strings.FieldsFunc(seg, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			r := []rune(part)
			b.WriteString(string(unicode.ToUpper(r[0])) + string(r[1:]))
		}
	}
	return b.String()
}

// cleanIdentifier keeps the letters, digits, and underscores of name, with a
// leading underscore when it would otherwise start with a digit.
func cleanIdentifier(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s != "" && unicode.IsDigit([]rune(s)[0]) {
		s = "_" + s
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
