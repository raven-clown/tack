# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before v1.0, minor versions may contain breaking changes; they are always listed under **Breaking**.

## [Unreleased]

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

[Unreleased]: https://github.com/raven-clown/tack/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/raven-clown/tack/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/raven-clown/tack/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/raven-clown/tack/releases/tag/v0.1.0
