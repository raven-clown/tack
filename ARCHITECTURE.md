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
/tsgen              Generates TypeScript from the OpenAPI document (v0.4)
/cmd/tack           CLI: gen, check, dev, mock, new, spec, diff, build (v0.4+)
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

### Errors

All errors leaving tack are RFC 9457 `application/problem+json`.

| Situation | Status |
| --- | --- |
| Malformed input (bad JSON, wrong type) | 400 |
| Body too large | 413 |
| Validation failed | 422 |
| `tack.NewError(status, detail)` from handler | that status |
| Any other error from handler | 500 — details hidden from client, logged via `slog` |

`Config.ErrorHandler` lets an app replace the error format globally (v0.3).

### Schema registry (`internal/schema`)

Converts `reflect.Type` to JSON Schema 2020-12. Named types are stored once in a registry and referenced by `$ref`, which also handles recursive types. Name collisions across packages are resolved by prefixing the package name. Struct tags (`json`, `validate`, `doc`, `example`) map onto schema keywords such as `minimum`, `maxLength`, `enum`, `format`, and `description`. Types can override their schema by implementing `openapi.SchemaProvider`. An object property is required when encoding/json always writes it (no `omitempty` or `omitzero`, not a pointer) or when it has `validate:"required"`.

### OpenAPI (`openapi`)

The `openapi` package only defines the document types. Each route adds its operation to the app's document at registration, converting its types through the schema registry, so unsupported types panic at startup rather than on the first spec request. The spec is encoded to JSON once and cached; registering another route or calling `app.OpenAPI()` clears the cache. `/docs` serves a small HTML page that loads a pinned Scalar version from a CDN, with its hosted extras (assistant, MCP, developer tools, external client, telemetry) turned off.

### TypeScript generation (`tsgen`, v0.4)

Generates TypeScript **from the OpenAPI document**, not by reflecting on Go types again. This guarantees the client, the spec, and `/docs` always agree. Output is a single dependency-free file using `fetch`, and must be deterministic (byte-identical across runs).

## Dependencies

- **Core:** `github.com/go-playground/validator/v10` only. Adding any other core dependency requires maintainer approval.
- **Tests:** test-only dependencies are fine (e.g. `kin-openapi` to validate generated specs, `goleak` for goroutine leaks).
- **Benchmarks and examples** live in separate modules so their dependencies never reach users.

## Testing strategy

- Table-driven unit tests for every converter, tag, and error path.
- **Golden files** in `testdata/` for generated JSON Schema, OpenAPI, and TypeScript. Update with `go test ./... -update`.
- Generated specs are validated against the OpenAPI 3.1 spec in tests.
- Fuzz tests for binding and parsing (v0.9+).
- `go test -race` always runs in CI.

## Decision log

Significant design decisions are recorded in `DESIGN.md` with the options considered and why one was chosen. Add an entry whenever you make a decision a future contributor might question.
