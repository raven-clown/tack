# Architecture

This document explains how tack works inside. Read it before making non-trivial changes. If your change alters anything described here, update this file in the same PR.

## Goals and principles

1. **Built on `net/http`.** `App` implements `http.Handler`. Middleware is `func(http.Handler) http.Handler`. We never invent our own request/response abstraction.
2. **Types are the single source of truth.** One Go struct drives binding, validation, OpenAPI, and TypeScript generation. These must never disagree.
3. **Types reach the frontend.** Success responses, every error a route declares, and the validation rules all arrive in the generated client. This is the main difference from similar projects.
4. **Small public API.** Every exported identifier must justify its existence.
5. **Helpful failures.** Misconfigured routes panic at registration with a message naming the route, the field, and the fix.
6. **Reflection once.** All reflection runs at registration time and is cached. The request path runs only precomputed plans.

Add-ons (`tackdb`, `tackcache`, ...) exist only where tack knowing the types adds something, such as database errors appearing in `/docs` and the client. Anything an existing library already does well is left to that library.

## The two phases

Everything in tack happens in one of two phases. Keeping them separate is the most important rule in the codebase.

```text
REGISTRATION TIME (once, at startup)          REQUEST TIME (every request)
──────────────────────────────────────        ──────────────────────────────
tack.Get(app, "/pets/{id}", handler)         HTTP request
        │                                             │
        ▼                                             ▼
reflect on In and Out types                   http.ServeMux matches route
        │                                             │
        ├─► build bind plan (internal/bind)           ▼
        ├─► validate route ↔ struct match     middleware chain
        ├─► register schemas (internal/schema)        │
        └─► record operation metadata                 ▼
        │                                     execute bind plan → In
        ▼                                             │
register http.HandlerFunc on ServeMux                 ▼
                                              validate In
                                                      │
                                                      ▼
                                              call user handler(ctx, In)
                                                      │
                                              ┌───────┴────────┐
                                              ▼                ▼
                                         (Out, nil)       (_, error)
                                              │                │
                                              ▼                ▼
                                         encode JSON      problem+json
```

**Rule:** if code under `REQUEST TIME` calls `reflect.TypeOf`, walks struct fields, or parses struct tags, it is a bug.

The one accepted exception is `go-playground/validator`, which walks the input value on each request (with its own cached struct metadata). tack skips it entirely for inputs without `validate` tags.

## Package layout

```text
/                   Public API: App, Config, Get/Post/Put/Patch/Delete,
                    Group, route options, errors, context helpers
/internal/bind      Builds and executes bind plans (request → struct)
/internal/schema    Converts Go types to JSON Schema 2020-12 + schema registry
/openapi            OpenAPI 3.1 document types
/tsgen              Generates the TypeScript client and Zod schemas from the OpenAPI document
/internal/mock      Serves example responses for a document (tack mock)
/cmd/tack           CLI: gen ts, gen zod, check, dev, mock (new, spec, diff, build planned)
/tacktest           Typed test client (v1.3)
/examples           Runnable example projects (each its own module if it has extra deps)
/bench              Benchmarks against other frameworks (separate module)
```

Packages under `internal/` may change freely. Anything at the root is public API and follows semver.

## Core components

### App and routing

`App` wraps an `http.ServeMux` and uses Go's native pattern syntax (`GET /pets/{id}`). We do not ship our own router. Route registration functions are generic:

```go
func Get[In, Out any](r Router, path string, h func(context.Context, In) (Out, error), opts ...RouteOption)
```

Type parameters are inferred from the handler, so users never write them explicitly.

`Router` is sealed and implemented by `*App` and `*Group`. A group adds a path prefix, middleware, and route options (through `Group.With`) to every route registered on it, and groups nest. The full path, the middleware chain, and the options are resolved at registration by walking up to the app, so `Group.Use` after nesting still reaches routes registered later.

### Middleware

Middleware is plain `func(http.Handler) http.Handler` at three levels. `App.Use` wraps the whole mux, including the docs and unmatched routes; it is stored in an atomic pointer so `Use` after the server has started is safe. Group middleware and the `Middleware` route option wrap the route's handler at registration, so they run after `ServeMux` has matched the route and path values are available. The order is app, outer group, inner group, route, handler.

### Bind plan (`internal/bind`)

At registration, the `In` type is analyzed once to produce a **plan**: a flat list of steps, one per field.

| Field tag | Source | Example |
| --- | --- | --- |
| `path:"id"` | `r.PathValue("id")` | `/pets/{id}` |
| `query:"page"` | URL query (repeated keys → slices) | `?page=2` |
| `header:"X-Request-ID"` | Request header | |
| `cookie:"session"` | Cookie | |
| `json:"name"` | JSON request body | |

Each step holds the field index path, the source, the name, and a **converter** chosen up front (string → int, string → `time.Time`, etc.). At request time, executing the plan is a loop with no reflection on types — only `reflect.Value` field sets at known indices.

### Validation

Validation is delegated to `go-playground/validator/v10`, wrapped so that:

- all field errors are collected into one response,
- locations are reported as `body.name`, `query.page`, `path.id`,
- messages are human-readable.

### Output plan

The `Out` type is analyzed once as well. A struct with a field named `Body` is an output wrapper: `Body` is encoded as JSON, an untagged `int` field named `Status` overrides the status code when it is not zero, and `header`-tagged fields become response headers, each with a formatter chosen at registration (string, bool, numbers, `time.Time` as RFC 3339, `encoding.TextMarshaler`, and pointers or slices of those). Nil pointers, empty slices, and empty strings send no header. A struct without `Body` is the body itself, and registration panics if it has `header` fields or an untagged `int` `Status`, since that is almost always a forgotten `Body`.

### Errors

All errors leaving tack are RFC 9457 `application/problem+json` with a `code` member that names the kind of problem.

| Situation | Status | Code |
| --- | --- | --- |
| Malformed input (bad JSON, wrong type) | 400 | `malformed_request` |
| Body too large | 413 | `body_too_large` |
| Body with a non-JSON content type | 415 | `unsupported_media_type` |
| Validation failed | 422 | `validation_failed` |
| `tack.DefineError(status, code, detail)` from handler | that status | that code |
| `tack.NewError(status, detail)` from handler | that status | none |
| Error implementing `HTTPStatus() int` | that status | none |
| Any other error from handler | 500, details hidden, logged via `slog` | `internal` |

Every error goes through one path: `problem` maps the handler's error to an `*Error` (keeping the original as its cause), 5xx errors are logged, and then either `Config.ErrorHandler` or the default problem+json writer sends it. `App.WriteError` exposes that path to middleware. With `Config.Dev`, a handler returning a typed error the route did not declare logs a warning.

In the OpenAPI document each declared error code becomes a component named after it, such as `PetNotFoundProblem`: `allOf` of `ProblemDetails` and an object whose `code` is an enum with that one value. A response status lists its codes with `oneOf`. The code to status mapping is tracked per app so one code cannot be documented with two statuses.

### Security

`Config.Security` declares named schemes (`Bearer`, `Basic`, `APIKey`) that land in `components.securitySchemes`. The `Security` route or group option attaches a requirement to the operation and documents a 401 with code `unauthorized`. tack never checks credentials; middleware does, typically answering with `App.WriteError(w, r, tack.ErrUnauthorized)`.

### Schema registry (`internal/schema`)

Converts `reflect.Type` to JSON Schema 2020-12. Named types are stored once in a registry and referenced by `$ref`, which also handles recursive types. Name collisions across packages are resolved by prefixing the package name. Struct tags (`json`, `validate`, `doc`, `example`) map onto schema keywords such as `minimum`, `maxLength`, `enum`, `format`, and `description`. Types can override their schema by implementing `openapi.SchemaProvider`. An object property is required when encoding/json always writes it (no `omitempty` or `omitzero`, not a pointer) or when it has `validate:"required"`.

### OpenAPI (`openapi`)

The `openapi` package only defines the document types. Each route adds its operation to the app's document at registration, converting its types through the schema registry, so unsupported types panic at startup rather than on the first spec request. The spec is encoded to JSON once and cached; registering another route or calling `app.OpenAPI()` clears the cache. `/docs` serves a small HTML page that loads a pinned Scalar version from a CDN, with its hosted extras (assistant, MCP, developer tools, external client, telemetry) turned off.

### TypeScript generation (`tsgen`)

Generates TypeScript **from the OpenAPI document**, not by reflecting on Go types again. This guarantees the client, the spec, and `/docs` always agree, and it is why `tack gen ts` reading the JSON produces the same bytes as `tsgen.Generate` in process (there is a test). Output is a single dependency-free file using `fetch`, and it is deterministic: schemas, operations, and properties are sorted, so the file changes only when the API does and `tsgen.Check` can compare bytes (after normalizing CRLF).

Generation has two passes. `collect` validates the document and plans every TypeScript name first, so a problem (duplicate operation IDs, an ID that is not an identifier, a schema that needs a name the runtime or another operation uses, two parameters with one name, a non-JSON body, several success responses) is reported once with a fix instead of as broken output. `emit` then writes, in order: the `Client` class with one method per operation, the shared runtime (`ClientOptions`, `Result`, `ApiError`, `unwrap`, the request sender, and the credential logic), one `Input`, `Error`, and `Headers` type per operation, and one type per component schema.

Each method passes a small literal to the shared `send`: method, path template, the names of the query and header parameters, whether there is a body, the success status and whether it carries JSON, the documented error statuses, the response headers with how to parse each (`number`, `boolean`, comma-separated `array` or `numbers`), and the security alternatives. At runtime `send` builds the URL (path parameters URL-encoded, arrays as repeated query keys), applies the first security alternative for which every credential is set, sends JSON, and classifies the response: the documented success status is `{ ok: true }` with the parsed body and headers; a documented error status with a JSON body is `{ ok: false }` typed as the union of that operation's problem schemas; anything else is thrown as `ApiError` so the union never lies. Cookie parameters are left out of the input, because browsers send them.

Schema mapping: `$ref` to the component's name, `allOf` to an intersection (which is how `PetNotFoundProblem` becomes `ProblemDetails & { code: "pet_not_found" }`, the discriminant of the union), `oneOf`/`anyOf` to unions, enums to literal unions, nullable to `| null`, maps to `Record<string, T>`, and schemas with no type to `unknown`. Numbers are `number` regardless of format, and dates are `string`.

### Zod generation (`tsgen.GenerateZod`)

The same `collect` pass plans the names, then a second emitter writes a separate file that imports `zod`, so the client stays dependency-free. Component schemas are written in dependency order (a depth-first walk of their `$ref`s); a schema that can reach itself is written as `const X: z.ZodType<X> = z.lazy(() => ...)` with the type spelled out the way the client spells it, since `z.infer` cannot name a lazy schema. Keywords map to Zod calls: `minimum`/`maximum`/exclusive bounds to `.min`/`.max`/`.gt`/`.lt`, lengths and item counts to `.min`/`.max`, `pattern` to `.regex(new RegExp(...))`, `format` to `.email()`, `.uuid()`, `.url()`, `.datetime()`, `enum` to `z.enum` or a union of literals, `allOf` to `.and`, `oneOf`/`anyOf` to `z.union`, nullable to `.nullable()`, a missing `required` entry to `.optional()`, and `default` to `.default(v)`. Every operation with parameters or a body also gets an `XInput` object schema. Only the calls shared by Zod 3.23 and Zod 4 are used; the end-to-end test runs the output with Zod 4.

### Mock server (`internal/mock`)

`mock.Handler(doc)` registers one `ServeMux` route per operation and prepares every documented response once: the body is built from the response schema by `example`, which prefers `examples`, then `default`, then the first `enum` value, and otherwise derives a value from the type, format, bounds, and property name, merging `allOf` and taking the first `oneOf`/`anyOf` alternative; recursion through `$ref` stops after four levels. A problem body gets the real status, title, and the detail the error was defined with. The success status is served by default; a `Prefer: code=NNN` header selects another documented status, and an undocumented one is a 400 with code `undocumented_status`. OpenAPI paths are exact, so a path ending in `/` is registered with `{$}`. The handler adds cross-origin headers to every response, answers preflight requests, and serves the document at `/openapi.json`. Requests are not validated.

### Dev loop (`tack dev`)

`devLoop` has no dependency on a file watcher: every 500 ms it rescans the directory for `.go`, `go.mod`, and `go.sum` files (skipping hidden directories, `node_modules`, `vendor`, and `testdata`) and compares sizes and modification times. On a change it runs `go build -o <tmp>/app .`, and only on success kills the running app and starts the new binary, so a typo never takes the app down. It then polls the document URL until the app answers, generates the client (and schemas with `-zod`), and writes each file only when its content changed, so editors and type checkers are not woken for nothing. With a command after `--` the build step is skipped and the command is restarted instead. The dev tests build and edit a small stdlib-only program, and use the test binary itself as the `--` command.

## Dependencies

- **Core:** `github.com/go-playground/validator/v10` only. Adding any other core dependency requires maintainer approval.
- **Tests:** test-only dependencies are fine (e.g. `kin-openapi` to validate generated specs, `chi` to prove stock middleware works, `goleak` for goroutine leaks).
- **Benchmarks and examples** live in separate modules so their dependencies never reach users.

## Testing strategy

- Table-driven unit tests for every converter, tag, and error path.
- **Golden files** in `testdata/` for generated JSON Schema, OpenAPI, and TypeScript. Update with `go test ./... -update`.
- The generated client is **run end to end**: a test compiles it with `tsc --strict` and runs it with node against an `httptest` server, covering parameters, bodies, typed errors, headers, 204, credentials, and an undocumented response; the Zod schemas are run the same way after `npm install zod`. It is skipped when `tsc` or `node` is not on PATH; CI installs both.
- Generated specs are validated against the OpenAPI 3.1 spec in tests.
- Fuzz tests for binding and parsing (v0.9+).
- `go test -race` always runs in CI.

## Decision log

Significant design decisions are recorded in `DESIGN.md` with the options considered and why one was chosen. Add an entry whenever you make a decision a future contributor might question.
