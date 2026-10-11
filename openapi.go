package tack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/raven-clown/tack/internal/bind"
	"github.com/raven-clown/tack/internal/schema"
	"github.com/raven-clown/tack/openapi"
)

const (
	defaultSpecPath = "/openapi.json"
	defaultDocsPath = "/docs"
	scalarURL       = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.69.2/dist/browser/standalone.js"
	scalarSRI       = "sha384-WIChsUVC1uJ+G1lFA6lPYzOUgXAe8dVxUFIZ7lcENNtV34Esuo81NIxsj8EAQeHB"
)

// OpenAPI returns the app's OpenAPI document so it can be extended. Make
// changes before the app serves requests; registering a route afterwards may
// overwrite them.
func (a *App) OpenAPI() *openapi.Document {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.spec = nil
	return a.doc
}

func (a *App) initOpenAPI(c Config) {
	title, version := c.Title, c.Version
	if title == "" {
		title = "API"
	}
	if version == "" {
		version = "0.0.0"
	}
	a.registry = schema.NewRegistry()
	a.opIDs = map[string]string{}
	a.codes = map[string]int{}
	a.doc = &openapi.Document{
		OpenAPI:    openapi.Version,
		Info:       openapi.Info{Title: title, Version: version, Description: c.Description},
		Servers:    c.Servers,
		Paths:      map[string]*openapi.PathItem{},
		Components: &openapi.Components{Schemas: a.registry.Schemas()},
	}
	for name, s := range c.Security {
		if err := checkScheme(name, s); err != nil {
			panic(fmt.Sprintf("tack: Config.Security: %v", err))
		}
		if a.doc.Components.SecuritySchemes == nil {
			a.doc.Components.SecuritySchemes = map[string]*openapi.SecurityScheme{}
		}
		a.doc.Components.SecuritySchemes[name] = &s
	}

	specPath := pathOr(c.SpecPath, defaultSpecPath)
	if specPath != "" {
		a.mux.HandleFunc("GET "+specPath, a.serveSpec)
	}
	docsPath := pathOr(c.DocsPath, defaultDocsPath)
	if docsPath != "" && specPath != "" {
		var buf bytes.Buffer
		err := docsTemplate.Execute(&buf, struct{ Title, SpecURL, ScalarURL, ScalarSRI string }{title, relURL(docsPath, specPath), scalarURL, scalarSRI})
		if err != nil {
			panic(fmt.Sprintf("tack: rendering docs page: %v", err))
		}
		page := buf.Bytes()
		a.mux.HandleFunc("GET "+docsPath, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		})
	}
}

func pathOr(p, def string) string {
	switch p {
	case "":
		return def
	case "-":
		return ""
	}
	return p
}

// relURL returns the spec URL relative to the docs page, so both keep working
// when the app is mounted under a prefix.
func relURL(docsPath, specPath string) string {
	depth := strings.Count(path.Dir(docsPath), "/")
	if path.Dir(docsPath) == "/" {
		depth = 0
	}
	return strings.Repeat("../", depth) + strings.TrimPrefix(specPath, "/")
}

var docsTemplate = template.Must(template.New("docs").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Title}}</title>
<style>
html { color-scheme: light dark; }
body { margin: 0; }
body.dark-mode { color-scheme: dark; }
body.light-mode { color-scheme: light; }
</style>
</head>
<body>
<div id="app"></div>
<script src="{{.ScalarURL}}" integrity="{{.ScalarSRI}}" crossorigin="anonymous"></script>
<script>
Scalar.createApiReference("#app", {
  url: {{.SpecURL}},
  showDeveloperTools: "never",
  hideClientButton: true,
  telemetry: false,
  agent: {disabled: true},
  mcp: {disabled: true}
})
</script>
</body>
</html>
`))

func (a *App) serveSpec(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.spec == nil {
		b, err := json.Marshal(a.doc)
		if err != nil {
			a.mu.Unlock()
			a.writeError(w, r, fmt.Errorf("encoding OpenAPI document: %w", err), nil)
			return
		}
		a.spec = b
	}
	b := a.spec
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// addOperation records the route in the document and returns the error codes
// it documents.
func (a *App) addOperation(method, route string, plan *bind.Plan, out *output, cfg routeConfig) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	op := &openapi.Operation{
		OperationID: cfg.operationID,
		Summary:     cfg.summary,
		Description: cfg.description,
		Tags:        cfg.tags,
		Deprecated:  cfg.deprecated,
		Responses:   map[string]*openapi.Response{},
		Security:    cfg.security,
	}
	if op.OperationID == "" {
		op.OperationID = operationID(method, route)
	}
	if other, dup := a.opIDs[op.OperationID]; dup {
		return nil, fmt.Errorf("operation ID %q is already used by %s; set a unique one with tack.OperationID", op.OperationID, other)
	}

	for _, p := range plan.Params() {
		s, err := a.registry.Field(p.Field)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", p.Field.Name, err)
		}
		desc := s.Description
		s.Description = ""
		s.Nullable = false
		_, hasDefault := p.Field.Tag.Lookup("default")
		op.Parameters = append(op.Parameters, &openapi.Parameter{
			Name:        p.Name,
			In:          p.In,
			Description: desc,
			Required:    p.In == "path" || (!hasDefault && a.zeroFails(p.Field)),
			Schema:      s,
		})
	}

	f, required, hasBody := plan.Body()
	if hasBody {
		if f.Type.Kind() == reflect.Pointer {
			f.Type = f.Type.Elem()
		}
		s, err := a.registry.Field(f)
		if err != nil {
			return nil, fmt.Errorf("field Body: %w", err)
		}
		desc := s.Description
		s.Description = ""
		op.RequestBody = &openapi.RequestBody{
			Description: desc,
			Required:    required,
			Content:     map[string]*openapi.MediaType{"application/json": {Schema: s}},
		}
	}

	ok := &openapi.Response{Description: http.StatusText(cfg.status)}
	if cfg.status != http.StatusNoContent && cfg.status != http.StatusNotModified {
		s, err := a.registry.Schema(out.typ)
		if err != nil {
			return nil, fmt.Errorf("output type %s: %w", out.typ, err)
		}
		ok.Content = map[string]*openapi.MediaType{"application/json": {Schema: s}}
	}
	for _, h := range out.headers {
		s, err := a.registry.Field(h.field)
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", h.name, err)
		}
		desc := s.Description
		s.Description = ""
		s.Nullable = false
		if ok.Headers == nil {
			ok.Headers = map[string]*openapi.Header{}
		}
		ok.Headers[h.name] = &openapi.Header{Description: desc, Required: h.required, Schema: s}
	}
	op.Responses[strconv.Itoa(cfg.status)] = ok

	errs := []*Error{{Status: http.StatusInternalServerError, Code: codeInternal, Detail: "internal error"}}
	if hasBody || len(op.Parameters) > 0 {
		errs = append(errs,
			&Error{Status: http.StatusBadRequest, Code: codeMalformed, Detail: "malformed request"},
			&Error{Status: http.StatusUnprocessableEntity, Code: codeValidation, Detail: "validation failed"})
	}
	if hasBody {
		errs = append(errs,
			&Error{Status: http.StatusRequestEntityTooLarge, Code: codeTooLarge, Detail: "request body too large"},
			&Error{Status: http.StatusUnsupportedMediaType, Code: codeMediaType, Detail: "content type is not JSON"})
	}
	if len(cfg.security) > 0 {
		errs = append(errs, ErrUnauthorized)
	}
	errs = append(errs, cfg.errs...)
	codes, err := a.addErrors(op, errs)
	if err != nil {
		return nil, err
	}

	p := specPath(route)
	item := a.doc.Paths[p]
	if item == nil {
		item = &openapi.PathItem{}
		a.doc.Paths[p] = item
	}
	switch method {
	case http.MethodGet:
		item.Get = op
	case http.MethodPost:
		item.Post = op
	case http.MethodPut:
		item.Put = op
	case http.MethodPatch:
		item.Patch = op
	case http.MethodDelete:
		item.Delete = op
	}
	a.opIDs[op.OperationID] = method + " " + route
	a.spec = nil
	return codes, nil
}

// addErrors documents errs on op, one problem schema per code, and returns
// the codes in order.
func (a *App) addErrors(op *openapi.Operation, errs []*Error) ([]string, error) {
	base, err := a.registry.SchemaAs(reflect.TypeFor[Error](), "ProblemDetails")
	if err != nil {
		return nil, err
	}
	var (
		codes    []string
		byStatus = map[int][]*openapi.Schema{}
		statuses []int
	)
	for _, e := range errs {
		if status, ok := a.codes[e.Code]; ok && status != e.Status {
			return nil, fmt.Errorf("error code %q is already defined with status %d; one code has one status", e.Code, status)
		}
		if slices.Contains(codes, e.Code) {
			continue
		}
		s := &openapi.Schema{
			Description: e.Detail,
			AllOf: []*openapi.Schema{base, {
				Type:       "object",
				Properties: map[string]*openapi.Schema{"code": {Type: "string", Enum: []any{e.Code}}},
				Required:   []string{"code"},
			}},
		}
		ref, err := a.registry.Define(problemName(e.Code), s)
		if err != nil {
			return nil, fmt.Errorf("error code %q: %w", e.Code, err)
		}
		a.codes[e.Code] = e.Status
		if _, seen := byStatus[e.Status]; !seen {
			statuses = append(statuses, e.Status)
		}
		byStatus[e.Status] = append(byStatus[e.Status], ref)
		codes = append(codes, e.Code)
	}
	for _, status := range statuses {
		s := byStatus[status][0]
		if len(byStatus[status]) > 1 {
			s = &openapi.Schema{OneOf: byStatus[status]}
		}
		op.Responses[strconv.Itoa(status)] = &openapi.Response{
			Description: http.StatusText(status),
			Content:     map[string]*openapi.MediaType{"application/problem+json": {Schema: s}},
		}
	}
	return codes, nil
}

// problemName turns "pet_not_found" into "PetNotFoundProblem".
func problemName(code string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(code, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		r := []rune(part)
		b.WriteString(string(unicode.ToUpper(r[0])) + string(r[1:]))
	}
	b.WriteString("Problem")
	return b.String()
}

// zeroFails reports whether omitting the value fails validation, which makes
// the parameter required in practice.
func (a *App) zeroFails(f reflect.StructField) bool {
	rules := f.Tag.Get("validate")
	if rules == "" {
		return false
	}
	return a.validate.Var(reflect.Zero(f.Type).Interface(), rules) != nil
}

// specPath converts a ServeMux path to an OpenAPI path.
func specPath(p string) string {
	p = strings.ReplaceAll(p, "{$}", "")
	return strings.ReplaceAll(p, "...}", "}")
}

// operationID derives an ID such as "getPetsById" from GET /pets/{id}.
func operationID(method, route string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(route, "/") {
		if seg == "" || seg == "{$}" {
			continue
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			b.WriteString("By")
			seg = strings.TrimSuffix(seg[1:len(seg)-1], "...")
		}
		for _, part := range strings.FieldsFunc(seg, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			r := []rune(part)
			b.WriteString(string(unicode.ToUpper(r[0])) + string(r[1:]))
		}
	}
	return b.String()
}
