# Security Policy

## Supported versions

| Version | Supported |
| --- | --- |
| Latest minor release | Yes |
| Older releases | No, please upgrade |

Before v1.0, only the latest minor version receives security fixes. After v1.0, the latest two minor versions will be supported.

## Defaults

What a `tack.New(tack.Config{})` app does before you configure anything, reviewed for the v0.9 release candidate. Each row names the knob that changes it.

| Area | Default | Change it with |
| --- | --- | --- |
| Request body size | 1 MiB for JSON and form bodies. A `Content-Length` above the limit is refused with 413 before the body is read; a chunked body is cut off by `http.MaxBytesReader` and also answered with 413. Routes without a `Body` or `form` fields never read the body. | `Config.MaxBodyBytes` |
| Form memory | Multipart forms keep 32 MiB in memory and spill the rest to temporary files, which are removed when the handler returns. | Lower `Config.MaxBodyBytes` to bound the whole body |
| Body content type | A JSON `Body` accepts `application/json` and a missing `Content-Type`; anything else is 415. A form accepts `multipart/form-data`, and `application/x-www-form-urlencoded` when it has no file fields. This keeps cross-site `text/plain` posts away from JSON handlers. | Not configurable |
| Validation output | Every failing field is reported, up to 50; then "and N more errors". | Not configurable |
| Internal errors | Any error that is not a `*tack.Error`, does not implement `HTTPStatus()`, or has a 5xx status reaches the client as `{"status": 500, "code": "internal"}` with no detail. The real error is logged through `Config.Logger` with the route pattern. | `Config.ErrorHandler` receives the mapped error; the original stays in `Unwrap` |
| Domain errors | For an error that implements `HTTPStatus()` with a 4xx status, only that error's own message is sent; wrapped context such as hostnames stays in the logs. | Return a `*tack.Error` with the detail you want |
| Panics | tack adds no recovery middleware. `net/http` recovers a panicking handler, logs it, and closes the connection, so nothing leaks, but the client sees no response. | `app.Use` with a recoverer that calls `app.WriteError(w, r, err)`, such as chi's `middleware.Recoverer` |
| Response headers | Every response tack writes carries an explicit `Content-Type` and `X-Content-Type-Options: nosniff`. The docs page also sends `Referrer-Policy: no-referrer`. | Add more in `app.Use` middleware |
| Server timeouts (`Run`) | `ReadHeaderTimeout` 10 seconds and `IdleTimeout` two minutes; no `ReadTimeout` or `WriteTimeout`, so uploads and streams are not cut off. `MaxHeaderBytes` is the standard library's 1 MiB. A client that sends a body slowly can hold a connection for as long as it keeps sending. | `Serve` with your own `*http.Server`; set `ReadTimeout` when the API has no uploads or streams, or wrap routes in `http.TimeoutHandler` |
| Shutdown | `Run` and `Serve` stop accepting, cancel every request context, and wait up to ten seconds for handlers. | `Config.ShutdownTimeout` |
| Docs and spec | `/openapi.json` and `/docs` are public. The docs page loads Scalar from jsDelivr at a pinned version with a Subresource Integrity hash and its hosted extras turned off; it makes no other third-party request. | `Config.DocsPath` or `SpecPath` set to `"-"`, or auth middleware in `app.Use`, which wraps both |
| Authentication | tack documents the schemes in `Config.Security` and the 401 on secured routes. It never checks credentials; middleware does. | Your middleware, answering with `app.WriteError(w, r, tack.ErrUnauthorized)` |
| Logging | 5xx errors are logged at error level with the route pattern and the error. Request bodies, headers, and query values are never logged. | `Config.Logger` |
| Development warnings | Off. | `Config.Dev` logs undeclared errors and undocumented statuses |

What runs in CI on every push and pull request: the test suite with the race detector on two Go versions, `goleak` on the stream, server, and `tack dev` tests, six fuzz targets for 20 seconds each, govulncheck on the root module, `examples/auth`, and `bench`, gosec (separately for the library and the command), Trivy, CodeQL, and the OpenSSF Scorecard. Findings land in the Security tab. As of v0.9.0 the tab holds only Scorecard policy items: branch protection, code review, a CII badge, the project's age, and unpinned `npm` and `go run` commands in the workflows. GO-2026-5932 (`golang.org/x/crypto/openpgp`, unmaintained) appears through the validator's dependency on `x/crypto`; nothing in tack or its dependencies imports that package, and govulncheck reports no reachable vulnerability.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Instead, use one of these private channels:

1. **GitHub private vulnerability reporting** (preferred): go to the [Security tab](https://github.com/raven-clown/tack/security) and click **Report a vulnerability**.
2. **Email:** ekdanai.kk@gmail.com

Please include:

- A description of the issue and its impact.
- Steps to reproduce, ideally a minimal `main.go`.
- The tack version and Go version affected.
- Any suggested fix, if you have one.

## What to expect

| Step | Target time |
| --- | --- |
| Acknowledge your report | within 3 days |
| Initial assessment | within 7 days |
| Fix released (for confirmed issues) | as soon as possible, typically within 30 days |

We will keep you informed throughout, and credit you in the release notes unless you prefer to remain anonymous.

## Scope

In scope: request binding and parsing, validation bypasses, information leaks in error responses, denial of service through unbounded input, and generated code (OpenAPI, TypeScript) that is unsafe.

Out of scope: vulnerabilities in your own handlers or middleware, and issues in third-party dependencies (please report those upstream; we will update promptly).
