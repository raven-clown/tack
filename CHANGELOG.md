# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before v1.0, minor versions may contain breaking changes; they are always listed under **Breaking**.

## [Unreleased]

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

[Unreleased]: https://github.com/raven-clown/tack/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/raven-clown/tack/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/raven-clown/tack/releases/tag/v0.1.0
