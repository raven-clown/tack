// Package mock serves fake responses for every operation in an OpenAPI
// document, so a frontend can be built against an API before it exists or
// without running it.
//
// Each operation answers with its success status and a body made from the
// response schema: examples and defaults when the document has them, otherwise
// values derived from the type, format, bounds, and enum. A request with a
// Prefer: code=NNN header gets the documented response for that status
// instead, so the frontend's handling of each declared error can be tried.
// Every response allows cross-origin requests, and the document itself is
// served at /openapi.json.
package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/raven-clown/tack/openapi"
)

// Handler returns the mock for doc. It fails when the document has no paths
// or an operation has no success response.
func Handler(doc *openapi.Document) (http.Handler, error) {
	if len(doc.Paths) == 0 {
		return nil, fmt.Errorf("mock: the document has no paths")
	}
	spec, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("mock: encoding the document: %w", err)
	}
	m := &mock{doc: doc, mux: http.NewServeMux()}
	for _, path := range sortedKeys(doc.Paths) {
		item := doc.Paths[path]
		if item == nil {
			continue
		}
		for _, method := range methods {
			op := method.get(item)
			if op == nil {
				continue
			}
			r, err := m.responder(op)
			if err != nil {
				return nil, fmt.Errorf("mock: %s %s: %w", method.name, path, err)
			}
			m.mux.Handle(method.name+" "+pattern(path), r)
			m.routes = append(m.routes, method.name+" "+path)
		}
	}
	m.mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	})
	return m, nil
}

// Routes lists the operations h serves, as "METHOD /path", when h came from
// Handler.
func Routes(h http.Handler) []string {
	if m, ok := h.(*mock); ok {
		return m.routes
	}
	return nil
}

var methods = []struct {
	name string
	get  func(*openapi.PathItem) *openapi.Operation
}{
	{http.MethodGet, func(i *openapi.PathItem) *openapi.Operation { return i.Get }},
	{http.MethodPost, func(i *openapi.PathItem) *openapi.Operation { return i.Post }},
	{http.MethodPut, func(i *openapi.PathItem) *openapi.Operation { return i.Put }},
	{http.MethodPatch, func(i *openapi.PathItem) *openapi.Operation { return i.Patch }},
	{http.MethodDelete, func(i *openapi.PathItem) *openapi.Operation { return i.Delete }},
}

type mock struct {
	doc    *openapi.Document
	mux    *http.ServeMux
	routes []string
}

func (m *mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Expose-Headers", "*")
	if r.Method == http.MethodOptions {
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
		h.Set("Access-Control-Allow-Headers", "*")
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, pattern := m.mux.Handler(r)
	if pattern == "" {
		writeProblem(w, http.StatusNotFound, "not_found", fmt.Sprintf("no operation for %s %s in the document", r.Method, r.URL.Path))
		return
	}
	m.mux.ServeHTTP(w, r)
}

// pattern turns an OpenAPI path into a ServeMux pattern. A trailing slash
// would match a whole subtree, while the document means that one path.
func pattern(path string) string {
	if strings.HasSuffix(path, "/") {
		return path + "{$}"
	}
	return path
}

// responder prepares every documented response for op once.
func (m *mock) responder(op *openapi.Operation) (*responder, error) {
	r := &responder{responses: map[int]*prepared{}}
	for key, res := range op.Responses {
		status, err := strconv.Atoi(key)
		if err != nil || res == nil {
			continue
		}
		p := &prepared{status: status, headers: map[string]string{}}
		if types := sortedKeys(res.Content); len(types) > 0 {
			ct := types[0]
			mt := res.Content[ct]
			var schema *openapi.Schema
			if mt != nil {
				schema = mt.Schema
			}
			value := m.example(schema, "", 0)
			if obj, ok := value.(map[string]any); ok && status >= 400 {
				// A problem carries the real status and title, and the
				// detail the error was defined with.
				obj["status"], obj["title"] = status, http.StatusText(status)
				if d := m.description(schema); d != "" {
					obj["detail"] = d
				}
			}
			body, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("response %d: %w", status, err)
			}
			if ct == eventStream {
				body = append(append([]byte("data: "), body...), "\n\n"...)
			}
			p.contentType, p.body = ct, body
		}
		for name, h := range res.Headers {
			if h == nil {
				continue
			}
			p.headers[name] = headerValue(m.example(h.Schema, name, 0))
		}
		if r.ok == 0 && status >= 200 && status < 400 || status < r.ok && status >= 200 {
			r.ok = status
		}
		r.responses[status] = p
	}
	if r.ok == 0 {
		return nil, fmt.Errorf("no 2xx or 3xx response")
	}
	return r, nil
}

// description returns the description of s, following the first alternative
// of a oneOf or anyOf (the one example picks) and one reference.
func (m *mock) description(s *openapi.Schema) string {
	if s == nil {
		return ""
	}
	if s.Description == "" && len(s.OneOf) > 0 {
		return m.description(s.OneOf[0])
	}
	if s.Description == "" && len(s.AnyOf) > 0 {
		return m.description(s.AnyOf[0])
	}
	if s.Ref != "" && m.doc.Components != nil {
		if target := m.doc.Components.Schemas[strings.TrimPrefix(s.Ref, "#/components/schemas/")]; target != nil && s.Description == "" {
			return target.Description
		}
	}
	return s.Description
}

type responder struct {
	ok        int
	responses map[int]*prepared
}

type prepared struct {
	status      int
	contentType string
	body        []byte
	headers     map[string]string
}

// eventStream is the content type whose example is sent as one event.
const eventStream = "text/event-stream"

func (r *responder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	status := r.ok
	if code := preferredCode(req.Header.Get("Prefer")); code != 0 {
		if _, ok := r.responses[code]; !ok {
			var known []string
			for _, s := range sortedInts(r.responses) {
				known = append(known, strconv.Itoa(s))
			}
			writeProblem(w, http.StatusBadRequest, "undocumented_status", fmt.Sprintf("status %d is not documented for this operation; documented: %s", code, strings.Join(known, ", ")))
			return
		}
		status = code
	}
	p := r.responses[status]
	for name, v := range p.headers {
		w.Header().Set(name, v)
	}
	if p.body != nil {
		w.Header().Set("Content-Type", p.contentType)
	}
	w.WriteHeader(p.status)
	_, _ = w.Write(p.body)
}

// preferredCode reads the status from a Prefer header such as "code=404".
func preferredCode(prefer string) int {
	for _, part := range strings.Split(prefer, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(k) != "code" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 100 && n < 600 {
			return n
		}
	}
	return 0
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail,
	})
}

func headerValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = headerValue(e)
		}
		return strings.Join(parts, ", ")
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedInts[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
