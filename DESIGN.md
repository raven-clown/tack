# Design

This file records the design decisions behind tack's public API: the options considered, the trade-offs, and what was chosen. Each decision is **Open** until the maintainer marks it **Decided**.

## Summary

| # | Topic | Decision | Status |
| --- | --- | --- | --- |
| D1 | Route declaration and generics | Package-level generic functions on a sealed `Router` interface | Decided |
| D2 | Where the request body lives | A field named `Body` in the input struct | Decided |
| D3 | Status codes and response headers | `tack.Status(code)` route option; output struct with `Body`, `Status`, and `header` fields for dynamic cases | Decided |
| D4 | Errors | `tack.NewError` returning `*tack.Error`, plus one interface for domain errors | Decided |
| D5 | Tags and defaults | `path`, `query`, `header`, `cookie`, `default`; pointers mean optional | Decided |
| D6 | Groups and middleware | `App.Group(prefix, mw...)` and `Group.With(opts...)`; plain `func(http.Handler) http.Handler` at app, group, and route level | Decided |
| D7 | Typed errors | `DefineError(status, code, detail)` returns an `*Error` with a `code`; `Errors(...)` documents them per route, one schema per code | Decided |
| D8 | TypeScript client | One fetch-based file generated from the OpenAPI document; methods return a `Result` whose error is the union of the declared problems; undocumented responses throw; `tsgen.Check` in a test keeps the file fresh | Decided |
| D9 | Zod schemas and the dev loop | Zod in a separate file generated from the document; `tack dev` polls files, builds before it restarts, and writes only what changed; `tack mock` answers from schemas and takes `Prefer: code=` for errors | Decided |
| D10 | Uploads and streams | `form` tags with `*multipart.FileHeader` fields for uploads; `Stream[T]`, an iterator, for server-sent events; `Run`/`Serve` for graceful shutdown | Decided |

## Prior art

Both libraries were read at the source level: Huma v2.39.1 and Fuego v0.21.0.

| | Huma | Fuego |
| --- | --- | --- |
| Register a route | `huma.Get(api, path, h, opts ...func(*huma.Operation))` or `huma.Register(api, huma.Operation{...}, h)` | `fuego.Get(s, path, h, opts ...fuego.RouteOption)` |
| Handler shape | `func(context.Context, *I) (*O, error)` | `func(fuego.Context[B, P]) (T, error)` |
| Router | `huma.API` over adapters (`humago`, `humachi`, `humagin`, ...) | `*fuego.Server` on `http.ServeMux` |
| Path / query / header / cookie | Tags on the input struct | `c.PathParam("id")`; `query` and `header` tags on `P`, parsed by reflection on each `c.Params()` call |
| Request body | Field named `Body` (or `RawBody`) | Type parameter `B`, read with `c.Body()` |
| Status code | `Operation.DefaultStatus`; `Status int` field on output | `option.DefaultStatusCode(201)`; `c.SetStatus(code)` |
| Response headers | `header`-tagged fields on output | `c.SetHeader(k, v)` |
| Errors | RFC 9457 `ErrorModel`; `huma.Error404NotFound(...)` helpers; customized by reassigning the package variable `huma.NewError` | RFC 9457 `HTTPError`; typed errors such as `fuego.NotFoundError`; `ErrorWithStatus` interface |
| Validation | Own JSON Schema tags (`minimum`, `maxLength`, `required`, ...) | `go-playground/validator/v10` (`validate` tag) |

Takeaways for tack:

- Huma's single input struct keeps every request value in one typed place. Its output struct with `Body`, `Status`, and headers works well, but pointer input and output (`*I`, `*O`) add noise to every handler.
- Fuego's handler is tied to a framework context instead of `context.Context`. Its path parameters are untyped strings, and its params are reflected per request, which tack's principle 5 rules out.
- Customizing errors through a package-level variable is global state. tack should put it in `Config` instead.

## D1. Route declaration and generics

Go methods cannot have type parameters (Go 1.26 reports `method must have no type parameters`), so `app.Get[In, Out](...)` is not possible.

**A. Package-level generic functions (chosen)**

```go
tack.Get(app, "/pets/{id}", getPet, tack.Summary("Get a pet"))
tack.Get(v1, "/pets/{id}", getPet) // v1 is a *tack.Group
```

Signature: `func Get[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption)`.

- Pro: `In` and `Out` are inferred from `h`. Users never write type arguments.
- Pro: One function works for `*App` and `*Group` through `Router`.
- Pro: The handler uses the standard `context.Context`.
- Con: `tack.Get(app, ...)` reads less like the `app.Get(...)` style people know from Gin and Echo.

**B. Methods taking a wrapped handler**

```go
app.Get("/pets/{id}", tack.Handle(getPet))
```

- Pro: Familiar method style.
- Con: An extra wrapper on every route. Route options would have to go in two places (wrapper or method).

**C. Framework context with type parameters (Fuego style)**

```go
tack.Get(app, "/pets/{id}", func(c tack.Context[In]) (Pet, error) { ... })
```

- Pro: Request helpers live on `c`.
- Con: Handlers depend on a tack type instead of `context.Context`. That makes them harder to call from tests and other code.

Sub-decisions under A:

- **`Router` is sealed** (it has an unexported method), so only `*App` and `*Group` implement it. That lets us add methods later without breaking anyone.
- **`In` and `Out` are values, not pointers.** `func(ctx, GetPetInput) (Pet, error)` is shorter than Huma's `*I`/`*O`. Handlers with no input use `struct{}`.
- **Route options are functional options** (`tack.Summary`, `tack.Tags`, `tack.Status`, ...), not an `Operation` struct. The common case stays one line, and new options are additive.

## D2. Where the request body lives

**A. Field named `Body` (chosen)**

```go
type CreatePetInput struct {
	OrgID string `path:"org"`
	Body  Pet
}
```

- Pro: No ambiguity about which fields come from the body.
- Pro: The body can be any type: a named struct (reused as a `$ref` in OpenAPI), a slice, or a map.
- Pro: Validation locations map directly: `body.name`, `path.org`.
- Con: One extra level of access (`in.Body.Name`), and `Body` is a magic field name.

**B. The whole struct is the body; tagged fields come from elsewhere**

```go
type CreatePetInput struct {
	OrgID string `path:"org"`
	Name  string `json:"name"`
}
```

- Pro: Shortest to write.
- Con: A named type such as `Pet` cannot be the body as-is, so OpenAPI needs an anonymous schema per route.
- Con: Top-level array bodies are impossible, and untagged fields are ambiguous.

**C. Body as a separate handler parameter**

```go
func(ctx context.Context, in CreatePetParams, body Pet) (Pet, error)
```

- Pro: Very explicit.
- Con: It needs a second set of registration functions or a third type parameter, and GET and POST handlers would have different shapes.

Under A, a field tagged `body:"..."` could later replace the magic name without breaking anything. That isn't needed for v0.1.

## D3. Status codes and response headers

**A. Route option for the default status (chosen for v0.1)**

```go
tack.Post(app, "/pets", createPet, tack.Status(http.StatusCreated))
```

- Pro: Covers most APIs. The status is known at registration, so OpenAPI documents it.
- Con: It can't pick a status at runtime or set headers.

**B. Output struct with `Body`, `Status`, and `header` fields (chosen for v0.3, alongside A)**

```go
type CreatePetOutput struct {
	Location string `header:"Location"`
	Body     Pet
}
```

- Pro: Fully typed, and response headers show up in OpenAPI and the TypeScript client.
- Pro: Mirrors the input side (D2).
- Con: The rule "if `Out` has a field named `Body`, only that field is the body" is implicit. Registration should panic when `Out` has `header` or `Status` fields but no `Body`.

**C. Generic response wrapper**

```go
func(ctx context.Context, in CreatePetInput) (tack.Response[Pet], error)
```

- Pro: Explicit and short.
- Con: Headers are an untyped `http.Header`, so OpenAPI cannot document them.
- Con: It adds an exported generic type that the B design does not need.

## D4. Errors

**A. One error type and a constructor (chosen)**

```go
return Pet{}, tack.NewError(http.StatusNotFound, "pet not found")
```

`*tack.Error` holds the RFC 9457 fields plus `Errors []ErrorDetail` (`location`, `message`). Domain errors map without wrapping by implementing:

```go
interface{ HTTPStatus() int }
```

- Pro: Small API: one type, one constructor, one interface.
- Pro: Works with `errors.As` through wrapped errors.
- Con: `NewError(404, ...)` takes a bare status, with no per-status helper.

**B. One helper per status (Huma style: `Error404NotFound`, ...)**

- Pro: Reads well at call sites.
- Con: Adds about 20 exported functions. That conflicts with principle 3.

**C. One typed error per status (Fuego style: `NotFoundError{}`)**

- Pro: Callers can match on type.
- Con: It has the same API growth as B, and status matching already works with `errors.As` on `*tack.Error`.

In every option, unknown errors become a 500 with no details sent to the client and the real error logged through `slog`. The global error format is replaced through `Config.ErrorHandler` (v0.3), never a package variable.

## D5. Tags and defaults

| Tag | Source | Notes |
| --- | --- | --- |
| `path:"id"` | `r.PathValue("id")` | Always required. Must match a `{id}` in the route, checked at registration |
| `query:"page"` | URL query | Repeated keys bind to slices |
| `header:"X-Request-ID"` | Request header | Canonicalized |
| `cookie:"session"` | Cookie | |
| `Body` field | JSON body | See D2 |
| `validate:"..."` | — | `go-playground/validator/v10` rules |
| `default:"10"` | — | Applied when the value is absent |
| `doc:"..."`, `example:"..."` | — | OpenAPI only (v0.2) |

**Defaults**

- **A. `default` tag, parsed once at registration (chosen).** A value that can't be parsed panics at startup. It also feeds the OpenAPI `default` keyword.
- **B. A `Defaults()` method on the input type.** Flexible, but OpenAPI cannot see the values.
- **C. No defaults; use pointers and handle `nil` in the handler.** Less code in tack, more in every handler.

**Optional vs required**

- Pointer fields (`*int`) are optional and keep "absent" distinct from the zero value.
- Non-path values are required only when `validate:"required"` says so. A single rule, the validator, drives both the 422 responses and the OpenAPI `required` list.

**Untagged exported fields** in an input struct panic at registration, naming the route, the field, and the fix ("add a `query`, `path`, `header`, or `cookie` tag, or name it `Body`"). Unexported fields and fields tagged `json:"-"` are ignored.

## D6. Groups and middleware

**A. Groups take middleware; options come through `With` (chosen)**

```go
v1 := app.Group("/v1", logging)
admin := v1.Group("/admin", requireAdmin).With(tack.Security("bearer"))
tack.Get(admin, "/users", listUsers, tack.Middleware(audit))
```

- Yes: a group is just a prefix, a middleware list, and a parent. `With` returns a child with no extra prefix, so nothing is copied and `Group.Use` on the parent still reaches it.
- Yes: middleware stays `func(http.Handler) http.Handler` everywhere, so chi's `Logger`, `Recoverer`, and `RequestID` work unchanged (there is a test).
- No: a route path of `"/"` under a prefix would silently become a `ServeMux` subtree match, so it panics; `"/{$}"` or `""` say what is meant.

**B. Groups take route options, middleware through `Use` only (Fuego style)**

- Yes: one variadic parameter.
- No: `Use` after creating sub-groups is the common case for middleware, and mixing the two kinds of argument in one list reads badly.

**C. chi-style `Route(prefix, func(r Router))` callbacks**

- No: handlers register with generic package functions, so a callback adds nesting without adding anything.

`App.Use` wraps the whole app, docs and 404s included, because logging and recovery are expected to see everything; group and route middleware wrap only matched routes, so they can read path values.

## D7. Typed errors

**A. `DefineError` returns an `*Error` with a code (chosen)**

```go
var ErrPetNotFound = tack.DefineError(404, "pet_not_found", "pet not found")

return Pet{}, ErrPetNotFound.WithDetail(fmt.Sprintf("pet %d not found", id))
```

- Yes: no new type. The value is an error, `errors.Is` works on copies made with `WithDetail` because `Is` compares code and status, and `Errors(ErrPetNotFound)` documents it.
- Yes: `code` is the discriminator the TypeScript client (D8) needs, independent of the human-readable detail.
- No: the variable is shared, so handlers must not mutate it (the same rule as any sentinel error).

**B. A separate `ErrorDef` type with `New()`**

- No: an extra exported type, and the definition would not itself be returnable.

**C. Status-only declarations (`Errors(404, 409)`)**

- No: two 404s with different meanings could not be told apart by a client.

One schema per code (`PetNotFoundProblem` = `allOf` of `ProblemDetails` and `{code: enum [pet_not_found]}`) rather than an inline schema per response, so generators get a named type and a status with several codes is a `oneOf` of named types. The code is the identity: declaring the same code with a different status or default detail panics at registration.

## D8. TypeScript client

**A. Methods return a `Result` union; undocumented responses throw (chosen)**

```ts
const res = await api.getPetsById({ id: 7 });
if (!res.ok) {
  switch (res.error.code) {
    case "pet_not_found": ...   // GetPetsByIdError = PetNotFoundProblem | ValidationFailedProblem | InternalProblem
  }
}
res.data.name;
```

- Yes: `catch (e)` gives `unknown`, so a thrown error cannot carry the union the route declares; a returned value can. The discriminant is `code`, which D7 made the identity of an error, and `allOf` maps to a TypeScript intersection, so `PetNotFoundProblem` is `ProblemDetails & { code: "pet_not_found" }` with no generator-specific trick.
- Yes: a 502 from a proxy, or a 401 from middleware on a route without `Security`, is not in the document. Returning it would make the union lie, so it is thrown as `ApiError`; network failures were always exceptions. The rule is one line: documented responses are values, everything else throws. `unwrap(res)` throws the same class for code that prefers exceptions.
- No: two ways to fail. Accepted, because the two are different kinds of failure: one the API designed, one it did not.

**B. Throw a typed error class (Encore, tRPC style)**

- No: the type is lost at `catch`, and every caller writes `instanceof` and a cast before it can switch on the code.

**C. Return the data and throw on any non-2xx**

- No: the same loss, and callers forget to catch.

Sub-decisions under A:

- **Generated from the OpenAPI document, not from Go types.** The client cannot disagree with `/docs`, and `tack gen ts` works from a served or exported document. Reading the document back from JSON must produce byte-identical output to generating in process; a test checks it.
- **Input is one flat object:** the parameters by their wire names (`id`, `limit`, `"X-API-Key"`) plus `body`, mirroring the Go input struct. Two parameters with one name in different locations fail generation rather than being renamed. Cookie parameters are left out, with a note on the method: browsers send them.
- **Name collisions fail generation** with the fix, as registration panics do: a schema named `Client` or `Result`, or a schema `GetPetsError` next to an operation `getPets`. Silent renaming was rejected because a name that changes on the next regeneration breaks every consumer.
- **Documented response headers are parsed by their schema** (`number`, `boolean`, comma-separated arrays) into a typed `headers` object; everything else stays on `response.headers`.
- **The output is deterministic**: schemas, operations, and properties are sorted, so the file changes only when the API does, diffs are small, and `tsgen.Check` is a byte comparison with CRLF normalized.
- **Freshness is a test first and a command second.** `tsgen.Check(app.OpenAPI(), "client/api.ts")` in a Go test needs no running server and no exported file, and runs wherever the code's tests run; `tack check` covers CI setups that already export the document.

## D9. Zod schemas and the dev loop

**A. Zod in its own file, generated from the document (chosen)**

```ts
import { PostPetsInput } from "./schemas";
const result = PostPetsInput.safeParse({ body: form });
```

- Yes: the client stays dependency-free (D8) and projects that do not use Zod never see it.
- Yes: the `validate` rules already live in the document as JSON Schema keywords, so Zod is one more projection of the same contract, not a second reading of struct tags. Anything the schema registry cannot express (custom validators) is simply not in the Zod file, which is honest.
- Yes: the same name planning as the client, so `PostPetsInput` is the same shape in both files.
- No: a schema in a reference cycle cannot use `z.infer`, so its type is written out and the schema is `z.lazy`. Accepted; it only affects recursive types.

**B. Zod inside the client file**

- No: every consumer of the client would install Zod.

**C. Generate Zod from Go struct tags directly (zen style)**

- No: a second reader of the tags can disagree with the schema registry, which is exactly the drift tack exists to remove.

**The dev loop polls files and builds before it restarts.** A file watcher would be the first dependency outside the standard library, and a 500 ms poll of a project's Go files costs nothing noticeable. `go build` runs first and the old app keeps serving until the new binary exists, so a typo never leaves the frontend without an API. Files are written only when their content changed, so editors and type checkers are not woken for an identical client.

## D10. Uploads and streams

**A. Uploads: `form` tags and `*multipart.FileHeader` fields (chosen)**

```go
type photoInput struct {
	ID      int                   `path:"id"`
	Photo   *multipart.FileHeader `form:"photo" validate:"required"`
	Caption string                `form:"caption" validate:"max=100"`
}
```

- Yes: one more source tag next to `path`, `query`, `header`, and `cookie`, with the same converters, defaults, validation, and `form.photo` locations. Nothing new to learn.
- Yes: `*multipart.FileHeader` is the standard library's own type, so handlers open files the way they always have, and no tack type wraps it.
- Yes: the document gets a `multipart/form-data` body with `binary` strings, which is what makes the client send `FormData` and `/docs` show a file picker.
- No: a form and a JSON `Body` in one input, since a request has one body.

**B. A `tack.File` type or an `Upload` wrapper**

- No: it would only hide `*multipart.FileHeader`, and every handler would convert back to it.

**C. Raw `io.Reader` bodies**

- No: a raw body has no fields to document or validate; it is a different feature (downloads and raw uploads) left for later.

**A. Streams: `Stream[T]` is an iterator the handler returns (chosen)**

```go
func(ctx context.Context, in In) (tack.Stream[Tick], error) {
	return func(yield func(Tick) bool) { ... }, nil
}
```

- Yes: the handler keeps the `(Out, error)` shape, so groups, middleware, errors, and docs work unchanged, and an error before the first event is still a problem response.
- Yes: a Go iterator is the idiomatic way to produce values on demand; `yield` returning false is the disconnect signal, and `ctx` covers waiting inside the loop.
- Yes: `Stream` is a named function type with methods, so every instantiation implements the serving interface and the route sends it with a type assertion, keeping reflection out of request time.
- No: errors after the first event cannot change the status; they are sent as an `error` event. That is a property of SSE, not of this design.

**B. A channel (`<-chan T`)**

- No: the producer goroutine's lifetime is the handler's problem, and a closed channel cannot report why it closed.

**C. A `w http.ResponseWriter` escape hatch**

- No: nothing would be documented, and the client could not type the events.

**Shutdown goes through the server's base context.** `Run` and `Serve` register a shutdown hook that cancels the context every request derives from, so streams end on their own and `Shutdown` does not wait for them; handlers that ignore their context still get `ShutdownTimeout`. The timeouts `Run` picks guard headers and idle connections only, because body timeouts would cut uploads and streams.

**The mock answers from schemas and takes `Prefer: code=NNN` for errors.** The success response is the default; a frontend asks for a declared error by status, which is the header Prism uses, so nothing new to learn. `examples` and `default` win over derived values, so an API that documents examples gets a realistic mock for free. The mock does not validate requests: that is the real server's job, and a mock that rejects what the server would accept is worse than one that accepts everything.

## Decisions

Decided on 2026-10-11. The options marked "chosen" above were accepted as written; the remaining questions were settled as follows.

| Question | Decision | Why |
| --- | --- | --- |
| Unknown JSON fields in the body | Ignored, as `encoding/json` does | Clients can roll out new fields before servers, and this matches what Go developers expect |
| Missing required header or cookie | 422, like any other validation failure | One rule for every source; 400 stays reserved for malformed input |
| Lead selling point | API docs that never drift from the code | Available from v0.2; the TypeScript client (v0.4) builds on the same promise |
| Required object properties (v0.2) | Required when encoding/json always writes the field (no `omitempty`/`omitzero`, not a pointer), or when it has `validate:"required"` | Response schemas match real output, so generated clients get non-optional fields |
| Pointer parameters (v0.2) | Optional, never `null` in the schema | Query, header, and cookie values are absent or present; they can't be null |
| Turning off spec or docs (v0.2) | `SpecPath` or `DocsPath` set to `"-"` | An empty string keeps the useful default |
| When the spec is built (v0.2) | At route registration, then encoded once and cached | Unsupported types fail at startup, not on the first spec request |
| Docs page (v0.2) | Scalar at a pinned version with a Subresource Integrity hash, and its hosted extras turned off | A predictable page with no third-party features beyond rendering the spec |
| Request body content type (v0.2) | A `Content-Type` that is not JSON gets 415; a missing one is accepted | Stops cross-site `text/plain` posts from reaching JSON handlers without breaking simple clients |
| `null` body (v0.2) | Treated as a missing body: 422 when required, nil when the Body field is a pointer | `null` should not slip past "body is required" as a zero value |
| Slice elements with rules (v0.2) | Registration panics unless the field has `validate:"dive"` | validator skips element rules without `dive`, while the spec would still advertise them |
| Error list size (v0.2) | At most 50 details, then "and N more errors" | Bounds the response for large invalid inputs |
| Repeated header values (v0.2) | Slice headers accept repeated lines and comma-separated values | Matches the default OpenAPI header style |
| Domain error detail (v0.2) | Only the message of the error that implements `HTTPStatus()` reaches the client | Wrapped context such as hostnames stays in logs |
| Nil slices and maps (known limitation) | Encoded as `null` by encoding/json, while the schema says array or object | Return empty values from handlers; revisit when encoding/json/v2 encodes nil slices as `[]` |
| Error codes on built-in errors (v0.3) | 400, 413, 415, 422, and 500 carry `malformed_request`, `body_too_large`, `unsupported_media_type`, `validation_failed`, and `internal` | One discriminator for every error a client can receive, not only the declared ones |
| `ErrorHandler` input (v0.3) | The mapped `*Error`, with the original error reachable through `Unwrap` | Replacing the format should not mean reimplementing the status mapping and 5xx hiding |
| 401 on secured routes (v0.3) | Documented automatically with `ErrUnauthorized`; middleware sends it with `App.WriteError` | A route that declares a scheme always has a 401; the exported value keeps docs and middleware in agreement |
| Several `Security` options (v0.3) | Alternatives: any one scheme is enough | Matches the OpenAPI list semantics; combined requirements can come later without breaking this |
| Output `Status` detection (v0.3) | In a wrapper (a struct with `Body`), `Status` must be an untagged `int`; without `Body`, a `Status` with a `json` tag or another type stays a JSON field and only an untagged `int` panics | Health-style responses keep working, and a tag states intent |
| `ContextValue[T]` (v0.3) | One generic helper instead of a typed context API | Middleware already uses `context.WithValue`; the helper only removes the type assertion and its `ok` boilerplate from handlers, so nothing else needs to exist |
| Runtime `Status` (v0.3) | Must be 2xx or 3xx, like the `Status` option; anything else is a 500 | Failures go through errors so they get problem+json and appear in the spec |
| Empty response headers (v0.3) | Nil pointers, empty slices, and empty strings are not sent and are optional in the spec | `Location: ` with no value is never wanted |
| `time.Time` headers (v0.3) | RFC 3339 | Mirrors the input side; use a string field for HTTP-date headers such as `Last-Modified` |
| Client runtime size (v0.4) | One shared `send` with a small literal per operation, about 150 lines in all | Readable methods, one place to fix, and no dependency to install or update |
| Credentials (v0.4) | `ClientOptions.credentials` keyed by scheme name, a string or an async function; the first security alternative with every credential set is sent | Mirrors the document's alternatives; a function covers tokens that refresh |
| Numbers in the client (v0.4) | Every integer and number is `number`, whatever its format | `int64` beyond 2^53 loses precision in JavaScript either way; a `string` field is the fix on the Go side |
| Empty success body (v0.4) | `data` is `undefined` for 204 and for a `Content-Length: 0` response; otherwise the body must be JSON | Matches what tack sends; a 200 with an empty body is a server bug, and `response.json()` reports it |
| Zod version (v0.5) | Only calls shared by Zod 3.23 and Zod 4 (`.email()`, `.datetime()`, `z.record(key, value)`, `.and`) | One output for both major versions; the end-to-end test runs it with Zod 4 |
| Defaults in Zod (v0.5) | A documented `default` becomes `.default(v)` rather than `.optional()` | The parsed value then matches what the server would have bound |
| Mock `oneOf` (v0.5) | The first alternative, in document order | One deterministic answer; the other codes are reachable by status with `Prefer` only when they have their own status |
| Dev loop restart (v0.5) | Kill and restart the binary; no graceful shutdown | It is a development server; in-flight requests on save are not worth waiting for |
| Form content types (v0.6) | `multipart/form-data` always; `application/x-www-form-urlencoded` too when the form has no file fields; anything else is 415 | A file needs multipart; a plain form should work from an HTML form without JavaScript |
| Form memory (v0.6) | 32 MiB in memory, the rest in temporary files removed after the handler | The standard library's default, and uploads should not be bounded by RAM |
| Known oversized bodies (v0.6) | Refused with 413 from `Content-Length` before reading | No point reading what will be rejected, and the multipart reader hides the limit error otherwise |
| Event encoding (v0.6) | `Event.Name` and `Event.ID` lines are flattened to one line each | SSE fields cannot span lines; data is JSON so it never does |
| `tack new` and versions (v0.6) | The template's `go.mod` names no tack version; `go mod tidy` picks the latest | The command cannot know which version the project should pin, and a stale pin would be worse than none |
| Release binaries (v0.6) | Built when a release is published, from plain `go build` in a workflow, and attached with checksums | No extra tooling to configure, and the maintainer keeps writing the release notes |
| Query scanning (v0.7) | The raw query is scanned per bound field, skipping what `url.ParseQuery` skips, instead of building the map | A route binds a few names; a map of every pair is work and allocations thrown away each request |
| Pooled inputs and encoders (v0.7) | One `sync.Pool` of the input struct per route and one of JSON encoders app-wide; the handler gets a copy of the input | Binding needs a pointer, which would otherwise heap-allocate every request; a copy keeps handlers free of aliasing rules |

## Prototype

A throwaway prototype (not committed) confirmed D1-A and D2-A on Go 1.26:

- `In` and `Out` are inferred for func literals, named functions, and method values.
- One `Get` serves both `*App` and `*Group` through a sealed `Router`.
- `struct{}` works as an input with no fields.
- A `Body` field decodes JSON, and `Status(201)` sets the response code.
- A type parameter on a method is rejected by the compiler. Passing `nil` as a handler fails with `cannot infer In`, which is an acceptable compile-time error.
