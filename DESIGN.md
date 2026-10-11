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

## Prototype

A throwaway prototype (not committed) confirmed D1-A and D2-A on Go 1.26:

- `In` and `Out` are inferred for func literals, named functions, and method values.
- One `Get` serves both `*App` and `*Group` through a sealed `Router`.
- `struct{}` works as an input with no fields.
- A `Body` field decodes JSON, and `Status(201)` sets the response code.
- A type parameter on a method is rejected by the compiler. Passing `nil` as a handler fails with `cannot infer In`, which is an acceptable compile-time error.
