package tsgen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raven-clown/tack/openapi"
)

func ref(name string) *openapi.Schema { return &openapi.Schema{Ref: "#/components/schemas/" + name} }

func f(v float64) *float64 { return &v }

// testDoc builds a document with the given operations and schemas.
func testDoc(paths map[string]*openapi.PathItem, schemas map[string]*openapi.Schema) *openapi.Document {
	if schemas == nil {
		schemas = map[string]*openapi.Schema{}
	}
	return &openapi.Document{
		OpenAPI:    openapi.Version,
		Info:       openapi.Info{Title: "T", Version: "1"},
		Paths:      paths,
		Components: &openapi.Components{Schemas: schemas},
	}
}

func jsonOK(s *openapi.Schema) map[string]*openapi.Response {
	return map[string]*openapi.Response{"200": {Description: "OK", Content: map[string]*openapi.MediaType{"application/json": {Schema: s}}}}
}

func TestTypeOf(t *testing.T) {
	g := &generator{names: map[string]string{"Pet": "Pet", "Owner": "Owner"}}
	tests := []struct {
		name string
		in   *openapi.Schema
		want string
	}{
		{"nil", nil, "unknown"},
		{"any", &openapi.Schema{}, "unknown"},
		{"string", &openapi.Schema{Type: "string"}, "string"},
		{"integer", &openapi.Schema{Type: "integer", Format: "int64"}, "number"},
		{"number", &openapi.Schema{Type: "number"}, "number"},
		{"boolean", &openapi.Schema{Type: "boolean"}, "boolean"},
		{"null", &openapi.Schema{Type: "null"}, "null"},
		{"nullable", &openapi.Schema{Type: "integer", Nullable: true}, "number | null"},
		{"ref", ref("Pet"), "Pet"},
		{"nullable ref", &openapi.Schema{Ref: "#/components/schemas/Pet", Nullable: true}, "Pet | null"},
		{"enum", &openapi.Schema{Type: "string", Enum: []any{"cat", "dog"}}, `"cat" | "dog"`},
		{"enum numbers", &openapi.Schema{Type: "integer", Enum: []any{1, 2.5, int64(3), true}}, "1 | 2.5 | 3 | true"},
		{"nullable enum", &openapi.Schema{Type: "string", Enum: []any{"cat", nil}, Nullable: true}, `"cat" | null`},
		{"array", &openapi.Schema{Type: "array", Items: ref("Pet")}, "Pet[]"},
		{"array of nullable", &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Nullable: true}}, "(string | null)[]"},
		{"array of anything", &openapi.Schema{Type: "array"}, "unknown[]"},
		{"map", &openapi.Schema{Type: "object", AdditionalProperties: &openapi.Schema{Type: "integer"}}, "Record<string, number>"},
		{"map of anything", &openapi.Schema{Type: "object", AdditionalProperties: &openapi.Schema{}}, "Record<string, unknown>"},
		{"empty object", &openapi.Schema{Type: "object"}, "Record<string, unknown>"},
		{"inline object", &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{
			"name": {Type: "string"}, "age": {Type: "integer"}, "x-y": {Type: "boolean"},
		}, Required: []string{"name"}}, `{ age?: number; name: string; "x-y"?: boolean }`},
		{"object with extras", &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{"a": {Type: "string"}},
			AdditionalProperties: &openapi.Schema{Type: "string"}}, "{ a?: string } & Record<string, string>"},
		{"allOf", &openapi.Schema{AllOf: []*openapi.Schema{ref("Pet"), {Type: "object", Properties: map[string]*openapi.Schema{"code": {Type: "string", Enum: []any{"x"}}}, Required: []string{"code"}}}},
			`Pet & { code: "x" }`},
		{"allOf of unions", &openapi.Schema{AllOf: []*openapi.Schema{{OneOf: []*openapi.Schema{ref("Pet"), ref("Owner")}}, ref("Pet")}}, "(Pet | Owner) & Pet"},
		{"oneOf", &openapi.Schema{OneOf: []*openapi.Schema{ref("Pet"), ref("Owner")}}, "Pet | Owner"},
		{"anyOf", &openapi.Schema{AnyOf: []*openapi.Schema{{Type: "string"}, {Type: "integer"}}}, "string | number"},
		{"binary", &openapi.Schema{Type: "string", ContentEncoding: "base64"}, "string"},
		{"date", &openapi.Schema{Type: "string", Format: "date-time", Minimum: f(1)}, "string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.typeOf(tt.in); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
			if g.err != nil {
				t.Fatal(g.err)
			}
		})
	}
}

func TestTypeOfErrors(t *testing.T) {
	g := &generator{names: map[string]string{}}
	g.typeOf(ref("Missing"))
	if g.err == nil || !strings.Contains(g.err.Error(), "Missing") {
		t.Fatalf("want error about missing schema, got %v", g.err)
	}
	g = &generator{names: map[string]string{}}
	g.typeOf(&openapi.Schema{Type: "file"})
	if g.err == nil || !strings.Contains(g.err.Error(), `"file"`) {
		t.Fatalf("want error about unsupported type, got %v", g.err)
	}
	doc := testDoc(map[string]*openapi.PathItem{"/": {Get: &openapi.Operation{Responses: jsonOK(ref("Missing"))}}}, nil)
	if _, err := Generate(doc); err == nil || !strings.Contains(err.Error(), "Missing") {
		t.Fatalf("Generate: want error about missing schema, got %v", err)
	}
}

func TestGenerateShapes(t *testing.T) {
	doc := testDoc(map[string]*openapi.PathItem{
		"/": {Get: &openapi.Operation{Responses: map[string]*openapi.Response{"204": {Description: "No Content"}}}},
		"/pets/{id}": {
			Get: &openapi.Operation{
				Summary:     "Get a pet",
				Description: "Returns one pet.",
				Deprecated:  true,
				Parameters: []*openapi.Parameter{
					{Name: "id", In: "path", Required: true, Schema: &openapi.Schema{Type: "integer"}},
					{Name: "session", In: "cookie", Schema: &openapi.Schema{Type: "string"}},
					{Name: "lang", In: "cookie", Schema: &openapi.Schema{Type: "string"}},
				},
				Responses: map[string]*openapi.Response{
					"200": {Description: "OK", Content: map[string]*openapi.MediaType{"application/json": {Schema: ref("Pet")}}, Headers: map[string]*openapi.Header{
						"X-Count": {Required: true, Schema: &openapi.Schema{Type: "integer"}},
						"X-Flag":  {Schema: &openapi.Schema{Type: "boolean"}},
						"X-IDs":   {Schema: &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "integer"}}},
						"X-Tags":  {Schema: &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string"}}},
						"X-Plain": {Schema: nil},
					}},
					"404":     {Description: "Not Found", Content: map[string]*openapi.MediaType{"application/problem+json": {Schema: ref("NotFound")}}},
					"410":     {Description: "Gone", Content: map[string]*openapi.MediaType{"application/json": {Schema: ref("NotFound")}}},
					"418":     {Description: "Teapot"},
					"default": {Description: "ignored"},
				},
			},
		},
		"/search": {Post: &openapi.Operation{
			OperationID: "search",
			Parameters: []*openapi.Parameter{
				{Name: "q", In: "query", Description: "Query", Schema: &openapi.Schema{Type: "string", Default: "all"}},
				{Name: "X-Trace", In: "header", Schema: &openapi.Schema{Type: "string"}},
			},
			RequestBody: &openapi.RequestBody{Description: "Filters", Content: map[string]*openapi.MediaType{"application/json": {Schema: &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{"kind": {Type: "string"}}}}}},
			Responses:   jsonOK(&openapi.Schema{Type: "array", Items: ref("Pet")}),
		}},
	}, map[string]*openapi.Schema{
		"Pet":      {Type: "object", Description: "A pet.", Properties: map[string]*openapi.Schema{"name": {Type: "string", Description: "Display */ name"}}, Required: []string{"name"}},
		"NotFound": {AllOf: []*openapi.Schema{ref("Pet"), {Type: "object", Properties: map[string]*openapi.Schema{"code": {Type: "string", Enum: []any{"not_found"}}}}}},
		"Kind":     {Type: "string", Enum: []any{"cat", "dog"}},
		"9lives":   {Type: "integer"},
	})
	out, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(out)
	for _, want := range []string{
		"// Code generated by tack from T 1. DO NOT EDIT.",
		"export type SecurityName = never;",
		"const securitySchemes: Record<string, { type: string; scheme?: string; in?: string; name?: string }> = {};",
		"  get(init?: RequestOptions): Promise<Result<void, GetError>> {\n    return this.send<void, GetError>({ method: \"GET\", path: \"/\", ok: 204 }, {}, init);",
		"export type GetError = never;",
		"   * Get a pet\n   *\n   * Returns one pet.\n   *\n   * Cookies \"lang\", \"session\": not part of the input; the browser sends it when the request has `credentials: \"include\"`.\n   *\n   * @deprecated\n   */\n  getPetsById(input: GetPetsByIdInput, init?: RequestOptions): Promise<Result<Pet, GetPetsByIdError, GetPetsByIdHeaders>> {",
		`ok: 200, json: true, errors: [404, 410, 418], resHeaders: [["X-Count", "number"], ["X-Flag", "boolean"], ["X-IDs", "numbers"], ["X-Plain", "string"], ["X-Tags", "array"]] }, input, init);`,
		"export type GetPetsByIdError = NotFound | unknown;",
		"export interface GetPetsByIdHeaders {\n  \"X-Count\": number;\n  \"X-Flag\"?: boolean;\n  \"X-IDs\"?: number[];\n  \"X-Plain\"?: unknown;\n  \"X-Tags\"?: string[];\n}",
		"  search(input?: SearchInput, init?: RequestOptions): Promise<Result<Pet[], SearchError>> {\n    return this.send<Pet[], SearchError>({ method: \"POST\", path: \"/search\", query: [\"q\"], headers: [\"X-Trace\"], body: true, ok: 200, json: true }, input ?? {}, init);",
		"export interface SearchInput {\n  /**\n   * Query\n   *\n   * @default \"all\"\n   */\n  q?: string;\n  \"X-Trace\"?: string;\n  /** Filters */\n  body?: { kind?: string };\n}",
		"/** A pet. */\nexport interface Pet {\n  /** Display *\\/ name */\n  name: string;\n}",
		"export type NotFound = Pet & { code?: \"not_found\" };",
		"export type Kind = \"cat\" | \"dog\";",
		"export type _9lives = number;",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("output lacks:\n%s\n\nfull output:\n%s", want, ts)
		}
	}
	if !strings.HasSuffix(ts, "}\n") || strings.HasSuffix(ts, "\n\n") {
		t.Errorf("output should end with exactly one newline, got %q", ts[len(ts)-5:])
	}
}

func TestGenerateFormsAndStreams(t *testing.T) {
	form := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{
		"avatar": {Type: "string", Format: "binary"},
		"photos": {Type: "array", Items: &openapi.Schema{Type: "string", Format: "binary"}},
		"name":   {Type: "string"},
	}, Required: []string{"avatar"}}
	doc := testDoc(map[string]*openapi.PathItem{
		"/upload": {Post: &openapi.Operation{
			RequestBody: &openapi.RequestBody{Required: true, Content: map[string]*openapi.MediaType{"multipart/form-data": {Schema: form}}},
			Responses:   map[string]*openapi.Response{"204": {}},
		}},
		"/login": {Post: &openapi.Operation{
			RequestBody: &openapi.RequestBody{Content: map[string]*openapi.MediaType{
				"application/x-www-form-urlencoded": {Schema: &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{"user": {Type: "string"}}}},
				"multipart/form-data":               {Schema: &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{"user": {Type: "string"}}}},
			}},
			Responses: map[string]*openapi.Response{"204": {}},
		}},
		"/ticks": {Get: &openapi.Operation{
			Responses: map[string]*openapi.Response{"200": {Content: map[string]*openapi.MediaType{"text/event-stream": {Schema: ref("Tick")}}}},
		}},
	}, map[string]*openapi.Schema{"Tick": {Type: "object", Properties: map[string]*openapi.Schema{"n": {Type: "integer"}}, Required: []string{"n"}}})
	out, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(out)
	for _, want := range []string{
		`{ method: "POST", path: "/upload", body: "multipart", ok: 204 }`,
		"export interface PostUploadInput {\n  body: { avatar: Blob; name?: string; photos?: Blob[] };\n}",
		`{ method: "POST", path: "/login", body: "urlencoded", ok: 204 }`,
		"getTicks(init?: RequestOptions): Promise<Result<AsyncIterable<ServerEvent<Tick>>, GetTicksError>> {\n    return this.send<AsyncIterable<ServerEvent<Tick>>, GetTicksError>({ method: \"GET\", path: \"/ticks\", ok: 200, stream: true }, {}, init);",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("output lacks:\n%s\n\nfull output:\n%s", want, ts)
		}
	}
	zod, err := GenerateZod(doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "body: z.object({ avatar: z.instanceof(Blob), name: z.string().optional(), photos: z.array(z.instanceof(Blob)).optional() }),"; !strings.Contains(string(zod), want) {
		t.Errorf("zod output lacks:\n%s\n\nfull output:\n%s", want, zod)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	doc := testDoc(map[string]*openapi.PathItem{
		"/b": {Get: &openapi.Operation{Responses: jsonOK(ref("B"))}, Post: &openapi.Operation{Responses: jsonOK(ref("A"))}},
		"/a": {Delete: &openapi.Operation{Responses: map[string]*openapi.Response{"204": {}}}},
	}, map[string]*openapi.Schema{
		"B": {Type: "object", Properties: map[string]*openapi.Schema{"z": {Type: "string"}, "a": {Type: "string"}}},
		"A": {Type: "string"},
	})
	first, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := Generate(doc)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatal("output differs between runs")
		}
	}
	ts := string(first)
	if strings.Index(ts, "deleteA(") > strings.Index(ts, "getB(") || strings.Index(ts, "getB(") > strings.Index(ts, "postB(") {
		t.Errorf("operations should be ordered by path, then method:\n%s", ts)
	}
	if strings.Index(ts, "export type A ") > strings.Index(ts, "export interface B ") {
		t.Errorf("schemas should be ordered by name:\n%s", ts)
	}
	if strings.Index(ts, "  a?: string;") > strings.Index(ts, "  z?: string;") {
		t.Errorf("properties should be ordered by name:\n%s", ts)
	}
}

func TestGenerateErrors(t *testing.T) {
	ok := jsonOK(&openapi.Schema{Type: "string"})
	param := func(name, in string) *openapi.Parameter {
		return &openapi.Parameter{Name: name, In: in, Schema: &openapi.Schema{Type: "string"}}
	}
	tests := []struct {
		name    string
		paths   map[string]*openapi.PathItem
		schemas map[string]*openapi.Schema
		schemes map[string]*openapi.SecurityScheme
		want    string
	}{
		{"duplicate id", map[string]*openapi.PathItem{
			"/a": {Get: &openapi.Operation{OperationID: "x", Responses: ok}},
			"/b": {Get: &openapi.Operation{OperationID: "x", Responses: ok}},
		}, nil, nil, `operation ID "x" is already used by GET /a`},
		{"invalid id", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{OperationID: "get-a", Responses: ok}}}, nil, nil, "not a TypeScript identifier"},
		{"member id", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{OperationID: "send", Responses: ok}}}, nil, nil, "member of the generated Client"},
		{"reserved schema", map[string]*openapi.PathItem{}, map[string]*openapi.Schema{"Client": {Type: "string"}}, nil, `schema "Client" needs the TypeScript name Client, which the generated client already uses`},
		{"global schema", map[string]*openapi.PathItem{}, map[string]*openapi.Schema{"Response": {Type: "string"}}, nil, `schema "Response" needs the TypeScript name Response, which the generated client already uses`},
		{"schema vs operation", map[string]*openapi.PathItem{"/pets": {Get: &openapi.Operation{Responses: ok}}}, map[string]*openapi.Schema{"GetPetsError": {Type: "string"}}, nil,
			`operation getPets needs the TypeScript name GetPetsError, which schema "GetPetsError" already uses`},
		{"schema name clash", map[string]*openapi.PathItem{}, map[string]*openapi.Schema{"A-B": {Type: "string"}, "AB": {Type: "string"}}, nil, "needs the TypeScript name AB"},
		{"unusable schema name", map[string]*openapi.PathItem{}, map[string]*openapi.Schema{"-": {Type: "string"}}, nil, "no characters usable"},
		{"duplicate param", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{Parameters: []*openapi.Parameter{param("id", "query"), param("id", "header")}, Responses: ok}}}, nil, nil,
			`parameters "id" in query and in header would share one input property`},
		{"body param", map[string]*openapi.PathItem{"/a": {Post: &openapi.Operation{Parameters: []*openapi.Parameter{param("body", "query")},
			RequestBody: &openapi.RequestBody{Content: map[string]*openapi.MediaType{"application/json": {}}}, Responses: ok}}}, nil, nil, `parameter "body" would share`},
		{"param location", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{Parameters: []*openapi.Parameter{param("x", "form")}, Responses: ok}}}, nil, nil, `unsupported location "form"`},
		{"text body", map[string]*openapi.PathItem{"/a": {Post: &openapi.Operation{RequestBody: &openapi.RequestBody{Content: map[string]*openapi.MediaType{"text/plain": {}}}, Responses: ok}}}, nil, nil,
			"request body has no application/json, multipart/form-data, or application/x-www-form-urlencoded content"},
		{"no success", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{Responses: map[string]*openapi.Response{"404": {}}}}}, nil, nil, "no 2xx or 3xx response"},
		{"two successes", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{Responses: map[string]*openapi.Response{"200": {}, "202": {}}}}}, nil, nil, "several success responses ([200 202])"},
		{"html response", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{Responses: map[string]*openapi.Response{"200": {Content: map[string]*openapi.MediaType{"text/html": {}}}}}}}, nil, nil,
			"response 200 has no application/json or text/event-stream content"},
		{"unknown scheme", map[string]*openapi.PathItem{"/a": {Get: &openapi.Operation{Security: []openapi.SecurityRequirement{{"bearer": nil}}, Responses: ok}}}, nil, nil,
			`security scheme "bearer" is not in components.securitySchemes`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := testDoc(tt.paths, tt.schemas)
			doc.Components.SecuritySchemes = tt.schemes
			_, err := Generate(doc)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestGenerateNoComponents(t *testing.T) {
	doc := &openapi.Document{Info: openapi.Info{Title: "Empty"}, Paths: map[string]*openapi.PathItem{"/": nil}}
	out, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "export class Client {") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestOperationID(t *testing.T) {
	tests := map[string]string{
		"GET /":                    "get",
		"GET /pets":                "getPets",
		"GET /pets/{id}":           "getPetsById",
		"POST /v1/pet-owners/{id}": "postV1PetOwnersById",
		"PUT /files/{path}":        "putFilesByPath",
	}
	for in, want := range tests {
		method, route, _ := strings.Cut(in, " ")
		if got := operationID(method, route); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestCheck(t *testing.T) {
	doc := testDoc(map[string]*openapi.PathItem{"/": {Get: &openapi.Operation{Responses: jsonOK(&openapi.Schema{Type: "string"})}}}, nil)
	want, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "api.ts")

	if err := Check(doc, path); !errors.Is(err, ErrStale) || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing file: got %v", err)
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(doc, path); err != nil {
		t.Fatalf("fresh file: got %v", err)
	}
	crlf := strings.ReplaceAll(string(want), "\n", "\r\n")
	if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(doc, path); err != nil {
		t.Fatalf("CRLF file: got %v", err)
	}
	stale := strings.Replace(string(want), "export class Client {", "export class Client {\n  extra = 1;", 1)
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	err = Check(doc, path)
	if !errors.Is(err, ErrStale) || !strings.Contains(err.Error(), "line 5") {
		t.Fatalf("stale file: got %v", err)
	}
	if err := os.WriteFile(path, want[:len(want)-10], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(doc, path); !errors.Is(err, ErrStale) {
		t.Fatalf("truncated file: got %v", err)
	}
	if err := Check(doc, dir); err == nil || errors.Is(err, ErrStale) {
		t.Fatalf("directory: got %v", err)
	}
	bad := testDoc(map[string]*openapi.PathItem{"/": {Get: &openapi.Operation{OperationID: "a b", Responses: jsonOK(nil)}}}, nil)
	if err := Check(bad, path); err == nil || errors.Is(err, ErrStale) {
		t.Fatalf("invalid document: got %v", err)
	}
}

func TestGenerateSecurity(t *testing.T) {
	doc := testDoc(map[string]*openapi.PathItem{
		"/me": {Get: &openapi.Operation{
			Security:  []openapi.SecurityRequirement{{"bearer": []string{"admin"}, "apiKey": nil}, {"basic": nil}},
			Responses: jsonOK(&openapi.Schema{Type: "string"}),
		}},
	}, nil)
	doc.Components.SecuritySchemes = map[string]*openapi.SecurityScheme{
		"bearer": {Type: "http", Scheme: "Bearer", BearerFormat: "JWT"},
		"basic":  {Type: "http", Scheme: "basic"},
		"apiKey": {Type: "apiKey", In: "query", Name: "key"},
	}
	out, err := Generate(doc)
	if err != nil {
		t.Fatal(err)
	}
	ts := string(out)
	for _, want := range []string{
		`export type SecurityName = "apiKey" | "basic" | "bearer";`,
		"const securitySchemes: Record<string, { type: string; scheme?: string; in?: string; name?: string }> = {\n" +
			"  apiKey: { type: \"apiKey\", in: \"query\", name: \"key\" },\n" +
			"  basic: { type: \"http\", scheme: \"basic\" },\n" +
			"  bearer: { type: \"http\", scheme: \"bearer\" }\n};",
		`security: [["apiKey", "bearer"], ["basic"]] }`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("output lacks:\n%s\n\nfull output:\n%s", want, ts)
		}
	}
}
