# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before v1.0, minor versions may contain breaking changes; they are always listed under **Breaking**.

## [Unreleased]

### Changed

- Routes whose `validate` tags are all simple rules on scalar parameters (`required`, `omitempty`, `min`, `max`, `gte`, `lte`, `gt`, `lt`, `len`, `eq`, `oneof`) run compiled checks instead of the validator; bodies with rules, slices, and other rules keep the validator, and both produce the same errors. The `Content-Type` header is set without canonicalizing or allocating, and inputs with nothing to bind skip the input pool. An empty handler now allocates nothing and costs less than plain `net/http` setting its own header; a route with a path value, a query value, a default, and validation takes about 650 ns and one allocation, down from 1690 ns and ten in v0.6.0. See BENCHMARKS.md.

## [0.6.1] - 2026-10-11

### Changed

- The request path allocates less: the query string is scanned for the bound fields instead of parsed into a map, route inputs are pooled, and responses are encoded by a pooled encoder. A route with a path value, a query value, a default, and validation went from 10 allocations and 504 bytes per request to 4 and 48, about a third faster; see BENCHMARKS.md. Behavior is unchanged, and malformed query pairs are skipped the way `url.ParseQuery` skips them.
- The benchmarks write to a discarding `ResponseWriter`, so their numbers now measure the handlers rather than `httptest.ResponseRecorder`.

### Fixed

- Two tests that failed on Go 1.27 and in CI: one depended on the exact text of a `json` error, and `tack new`'s test disabled the module proxy, which the generated project's `go mod tidy` needs.

## [0.6.0] - 2026-10-11

### Added

- File uploads: a `form:"name"` tag binds a field from a `multipart/form-data` or `application/x-www-form-urlencoded` body, and a `*multipart.FileHeader` or `[]*multipart.FileHeader` field receives uploaded files. Form values use the same converters, defaults, and `validate` rules as query values, with locations such as `form.photo`. The request body is documented as a form with files as binary strings, so the TypeScript client sends `FormData` (`Blob` fields), the Zod schemas use `z.instanceof(Blob)`, and `/docs` shows a file picker. Temporary files are removed when the handler returns. An input cannot have both a `Body` field and form fields.
- Server-sent events: a handler that returns `tack.Stream[T]`, an iterator `func(yield func(T) bool)`, sends one event per value with its JSON on the data line, flushing each. `tack.Event[T]` adds the event name and ID. The request context is done when the client disconnects or the server shuts down, and `yield` returns false once the connection is gone. The response is documented as `text/event-stream` with the schema of `T`; the client returns `AsyncIterable<ServerEvent<T>>` and `tack mock` sends one example event.
- `App.Run(ctx, addr)` and `App.Serve(ctx, srv)` serve until the context is done, then shut down gracefully: no new connections, every request context canceled (which ends streams), and up to `ShutdownTimeout` (10 seconds) for in-flight handlers. `Run` uses a server with a 10 second header timeout and a two minute idle timeout and no body timeouts, so uploads and streams are not cut off.
- A request whose `Content-Length` exceeds `Config.MaxBodyBytes` is refused with 413 before the body is read.
- `tack new <dir> [-module path]` creates a runnable project: one resource with a typed error, groups, a `Location` header, graceful shutdown, a README, and a test that regenerates and checks `client/api.ts` and `client/schemas.ts`.
- `tack version` prints the version. Publishing a GitHub release builds the command for Linux, macOS, and Windows (amd64 and arm64) and attaches the archives and a checksum file.
- `examples/petstore` gains `POST /pets/{id}/photo` and `GET /pets/events`, and uses `Run`.

### Changed

- The detail of the `unsupported_media_type` (415) problem is now "unsupported content type", since bodies can be JSON or forms.

## [0.5.0] - 2026-10-11

### Added

- `tsgen.GenerateZod(doc)` writes Zod schemas: one per component schema and one per operation input, with the bounds, lengths, patterns, enums, formats, and defaults the server enforces, so a form can validate before it sends. Each schema exports a value and a type of the same name; schemas in a reference cycle use `z.lazy` with an explicit type. The file imports `zod` and works with Zod 3.23 and later, Zod 4 included. `tsgen.CheckZod` keeps it fresh like `Check` does for the client.
- `tack gen zod -spec <file|url|-> [-o schemas.ts]` and `tack check -spec <file|url|-> -zod schemas.ts`.
- `tack dev [-spec URL] [-o api.ts] [-zod schemas.ts] [-dir .] [-- command...]` builds the directory with `go build`, runs the binary, waits for the document URL to answer, and regenerates the client and schemas, writing only when they changed. A change to a Go file, `go.mod`, or `go.sum` does it again; a failed build keeps the running app and reports the error. A command after `--` replaces the build and run step.
- `tack mock -spec <file|url|-> [-addr 127.0.0.1:8081]` serves every operation with its success status and a body built from the response schema (examples and defaults first, then values derived from the type, format, bounds, and enum). A `Prefer: code=404` header returns that documented error with its `code` and detail. Responses allow cross-origin requests and the document is served at `/openapi.json`.
- `examples/petstore` commits `client/schemas.ts` next to its client; CI checks both with `tack check` and type-checks them.

## [0.4.0] - 2026-10-11

### Added

- `tsgen`: `Generate(doc)` turns the OpenAPI document into one dependency-free TypeScript file built on `fetch`. Every operation is a method on `Client` that returns a `Result`: `{ ok: true, data, headers, response }` with the response type and the documented headers parsed, or `{ ok: false, error, response }` where `error` is the union of the problem schemas the route declares, so `switch (res.error.code)` is checked by the compiler. Responses the document does not describe are thrown as `ApiError`, and `unwrap(res)` throws it for error results. `ClientOptions` takes `baseUrl`, `fetch`, default `headers`, and `credentials` by security scheme name, which the client sends as `Authorization: Bearer`, `Authorization: Basic`, or the API key header or query parameter the scheme names. Output is deterministic and sorted, so the file changes only when the API does.
- `tsgen.Check(doc, path)` returns an error wrapping `tsgen.ErrStale` when the file is missing or differs from the document, for a test that fails CI when the committed client is older than the routes.
- `cmd/tack`: `tack gen ts -spec <file|url|-> [-o api.ts]` writes the client and `tack check -spec <file|url|-> api.ts` exits 1 when it is stale. The document can come from a file, a URL such as `http://localhost:8080/openapi.json`, or standard input.
- Generation fails with the fix when the document holds something the client cannot express: two operations with one ID, an ID that is not a TypeScript identifier, a schema named like something the client defines (`Client`, `Result`, `GetPetsError` for operation `getPets`), two parameters with one name in different locations, a request body that is not JSON, or several success responses.
- `examples/petstore` commits its generated `client/api.ts` and a test that fails when it is stale (`go test . -update` regenerates it). CI checks the same file with `tack check` from the served document and runs the client end to end with node.

## [0.3.0] - 2026-10-11

### Breaking

- An output struct with a field named `Body` is now a response wrapper (DESIGN.md D3): only `Body` is encoded, an untagged `Status int` sets the status, and `header`-tagged fields become headers; any other exported field panics at registration. A v0.2 output such as `struct{ Body string; Title string }` must rename `Body` to keep sending both fields as JSON.

### Added

- Route groups: `App.Group(prefix, mw...)`, nestable with `Group.Group`, plus `Group.Use` and `Group.With(opts...)` to share route options. A group prefix may hold wildcards such as `/orgs/{org}`.
- Middleware at three levels with plain `func(http.Handler) http.Handler`: `App.Use`, group middleware, and the `Middleware` route option. Order: app, outer group, inner group, route, handler. chi's `RequestID`, `Logger`, and `Recoverer` work unchanged.
- `ContextValue[T](ctx, key)` reads typed values middleware stored in the context.
- Typed errors: `DefineError(status, code, detail)` returns an `*Error` with a `Code`; `Error.WithDetail` makes a per-request copy, `Error.Is` matches by code and status, and `Error.Unwrap` returns the handler's original error. The `Errors(...)` route option lists them in the OpenAPI document, one `<Code>Problem` schema per code, so clients can build a typed union per route. With `Config.Dev`, returning an undeclared typed error logs a warning.
- `Config.ErrorHandler` replaces the error format for the whole app, and `App.WriteError` lets middleware answer in that format.
- Security schemes: `Config.Security` with `Bearer`, `Basic`, and `APIKey`, bound to routes or groups with the `Security(name, scopes...)` option. They appear in `components.securitySchemes` and each operation's `security`, so `/docs` offers a token field and sends it. Secured routes document a 401 and `ErrUnauthorized` is the matching error.
- Responses with headers: an output struct with a `Body` field, an optional `Status int`, and `header`-tagged fields. Headers are documented in the spec. Registration panics when a struct has `header` fields or an untagged `Status int` but no `Body`.
- `openapi` gains `SecurityScheme`, `SecurityRequirement`, `Header`, `Components.SecuritySchemes`, `Operation.Security`, `Response.Headers`, and `Schema.AllOf`/`OneOf`.
- `examples/auth`: a login that signs a JWT and a route that requires it, as its own module.

### Changed

- Every error tack sends now carries a `code`: `malformed_request` (400), `body_too_large` (413), `unsupported_media_type` (415), `validation_failed` (422), and `internal` (500). The `ProblemDetails` schema gains the optional `code` property.
- `examples/petstore` uses a group, `DefineError`, and a `Location` header.

## [0.2.0] - 2026-10-11

### Added

- OpenAPI 3.1 document generated from route types, served at `/openapi.json`, with interactive docs at `/docs`. `Config` gains `Description`, `Servers`, `SpecPath`, and `DocsPath`; `App.OpenAPI` returns the document.
- Route options `Summary`, `Description`, `Tags`, `OperationID`, and `Deprecated`. Operation IDs are generated from the method and path, such as `getPetsById`.
- JSON Schema 2020-12 for structs, embedded structs, slices, arrays, maps, pointers (nullable), `time.Time`, `[]byte`, `json.RawMessage`, recursive types, and generics, with `doc`, `example`, and `validate` tags mapped to schema keywords. Types can supply their own schema through `openapi.SchemaProvider`.
- Every operation documents its problem+json error responses, using the `ProblemDetails` schema.
- Request bodies with a non-JSON `Content-Type` get 415 Unsupported Media Type.
- CI generates Go and TypeScript clients from the petstore spec with openapi-generator.

### Changed

- A `null` request body counts as missing: 422 when the body is required, nil when `Body` is a pointer.
- Errors implementing `HTTPStatus()` send only their own message to the client, not the wrapped chain.
- Validation responses list at most 50 errors.
- Slice header fields also accept comma-separated values.
- Registration now panics, with the route and a fix, for invalid or duplicate paths, slice fields whose element rules need `validate:"dive"`, `default` tags inside JSON bodies, and success statuses that cannot carry the handler's output.

### Fixed

- Validation error locations for anonymous and generic input types.

## [0.1.0] - 2026-10-11

### Added

- Project setup: CI, linting, license, contributor guides.
- `New`, `Config`, and `App`, an `http.Handler` built on `http.ServeMux`.
- Typed routes: `Get`, `Post`, `Put`, `Patch`, `Delete` with inferred input and output types, and the `Status` route option.
- Binding from `path`, `query`, `header`, and `cookie` tags and a JSON `Body` field, with `default` values. Supports strings, bools, all integer and float types, `time.Time`, `encoding.TextUnmarshaler`, pointers for optional values, and slices for repeated query and header values.
- Validation with `go-playground/validator/v10`, reporting every failing field at once.
- RFC 9457 `application/problem+json` errors: `Error`, `ErrorDetail`, `NewError`, and the `HTTPStatus() int` convention for domain errors. Request bodies are limited to 1 MiB by default.
- Route registration panics with the route, field, and fix when an input struct does not match its path.
- `examples/petstore` and `BENCHMARKS.md`.

<!--
Template for each release:

## [0.1.0] - YYYY-MM-DD

### Breaking
- What changed, and how to migrate (before/after code).

### Added
- New features.

### Changed
- Changes to existing behavior.

### Fixed
- Bug fixes (link the issue: #123).

### Security
- Security fixes.
-->

[Unreleased]: https://github.com/raven-clown/tack/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/raven-clown/tack/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/raven-clown/tack/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/raven-clown/tack/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/raven-clown/tack/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/raven-clown/tack/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/raven-clown/tack/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/raven-clown/tack/releases/tag/v0.1.0
