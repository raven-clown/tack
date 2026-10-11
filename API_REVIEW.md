# API review for v0.9

Every exported identifier in the public packages (`tack`, `openapi`, `tsgen`) as of v0.6.3, with a verdict for the v0.9 release candidate. Nothing here changes in a patch release; the maintainer decides before v0.9. The source is `go doc -all` on each package.

Verdicts: **keep** (stays as is), **rename** (same thing, better name), **merge** (fold into another identifier), **hide** (unexport or move to `internal/`).

## Package tack

| Identifier | Verdict | Reason |
| --- | --- | --- |
| `New(c Config) *App` | keep | The one constructor; `Config` keeps it to one argument. |
| `App` | keep | The `http.Handler`; every route is registered on it. |
| `App.ServeHTTP` | keep | Required by `http.Handler`. |
| `App.Use(mw...)` | keep | Mirrors `Group.Use`; plain `func(http.Handler) http.Handler`. |
| `App.Group(prefix, mw...)` | keep | Mirrors `Group.Group`. |
| `App.OpenAPI() *openapi.Document` | keep | Named after what it returns and the `/openapi.json` route. `Document()` was considered and rejected: it would not say which document. |
| `App.WriteError(w, r, err)` | keep | The only way middleware answers in the app's error format. |
| `App.Run(ctx, addr)` | keep | The common case: one call, graceful shutdown. |
| `App.Serve(ctx, srv)` | keep | The escape hatch for a configured `http.Server`. |
| `Config` | keep | One struct instead of functional options; fields are documented and zero values are the defaults. |
| `Config.Title`, `Config.Version`, `Config.Description` | keep | Map one to one onto `openapi.Info`. |
| `Config.Servers` | keep | Maps onto `openapi.Document.Servers`. |
| `Config.Security` | keep | Keyed by the scheme name the `Security` option uses. |
| `Config.SpecPath`, `Config.DocsPath` | keep | The `"-"` convention to disable is documented in DESIGN.md. |
| `Config.MaxBodyBytes` | keep | The one request limit tack applies itself. |
| `Config.Logger` | keep | `*slog.Logger` is the standard choice. |
| `Config.ErrorHandler` | keep | Replaces the error format app-wide; receives the mapped `*Error`. |
| `Config.Dev` | keep | A single switch for development warnings; more checks can hang off it without new fields. |
| `ShutdownTimeout` (const) | keep, revisit at v1.0 | A constant cannot be tuned. If a user asks, it becomes `Config.ShutdownTimeout` with this value as the default and the constant is removed; until then one less field. |
| `Router` | keep | Sealed interface implemented by `*App` and `*Group`, so `Get(app, ...)` and `Get(group, ...)` share one signature. |
| `Get`, `Post`, `Put`, `Patch`, `Delete` | keep | Generic registration; type parameters are inferred. `Head` and `Options` are not offered: they rarely carry typed bodies, and `net/http` serves them when needed. |
| `RouteOption` | keep | Functional options for routes; the parameter type is unexported so the set of options is closed. |
| `Status(code)` | keep | The static success status. |
| `Summary(s)`, `Description(s)`, `Tags(...)`, `OperationID(id)`, `Deprecated()` | keep | One option per OpenAPI operation field, named after the field. |
| `Middleware(mw...)` | keep | Route-level middleware; consistent with `Use`. |
| `Security(name, scopes...)` | keep | Mirrors the OpenAPI security requirement; several options mean alternatives. |
| `Errors(errs...)` | keep | Declares typed errors; the name reads well at the call site (`tack.Errors(ErrPetNotFound)`). |
| `Group` | keep | Prefix, middleware, and shared options. |
| `Group.Group`, `Group.Use` | keep | Same names as on `App`. |
| `Group.With(opts...)` | keep | The only way to share route options; returns a new group so the parent is unchanged. |
| `Error` | keep | RFC 9457 problem details; the type handlers return. |
| `Error.Type`, `Error.Title`, `Error.Status`, `Error.Detail`, `Error.Instance` | keep | The RFC 9457 members. tack never sets `Instance`; users may. |
| `Error.Code` | keep | The discriminator every generated client depends on. |
| `Error.Errors` | keep | Field-level details for 400 and 422. |
| `Error.Error()`, `Error.Is`, `Error.Unwrap` | keep | Standard error interfaces; `Is` matches by code and status so `WithDetail` copies still match. |
| `Error.WithDetail(detail)` | keep | Per-request message without mutating the package-level error. |
| `NewError(status, detail)` | keep | Errors without a code, for one-off cases. |
| `DefineError(status, code, detail)` | keep | Errors with a code, for package-level variables. |
| `ErrUnauthorized` | keep | The 401 that `Security` documents; exported so middleware sends the same code. |
| `ErrorDetail` | keep | `Location` and `Message`; the shape of `Error.Errors` entries. |
| `ContextValue[T](ctx, key)` | keep | Removes the type assertion from handlers; no typed-context API needed beyond it. |
| `Bearer(format)`, `Basic()`, `APIKey(in, name)` | keep | Constructors for the three supported schemes; they return the `openapi` type so users can also build one by hand. |
| `Stream[T]` | keep | A named iterator function type; it is both an `iter.Seq[T]`-shaped value and a type with methods. |
| `Event[T]` | keep | `Name`, `ID`, `Data` map onto the `event`, `id`, and `data` lines. `Name` was preferred to `Event` to avoid `Event.Event`. |

## Package openapi

| Identifier | Verdict | Reason |
| --- | --- | --- |
| `Version` (const) | keep | The `openapi` field value tack writes. |
| `Document` | keep | The root object; `App.OpenAPI` returns it for extension. |
| `Info`, `Server`, `PathItem`, `Operation`, `Parameter`, `RequestBody`, `MediaType`, `Response`, `Header`, `Components` | keep | One struct per OpenAPI object tack emits, with the spec's field names. Objects tack never writes (callbacks, links, examples objects) are left out on purpose. |
| `Schema` | keep | JSON Schema 2020-12 subset; one struct keeps `$ref` and inline schemas in one type as the spec does. |
| `Schema.Nullable` | keep | Encoded as a `null` type or `anyOf`; a field is simpler than asking users to build the union. |
| `Schema.MarshalJSON`, `Schema.UnmarshalJSON` | keep | Needed for `Nullable`; they must be exported to satisfy `json.Marshaler`. |
| `SchemaProvider` | keep | The one hook for custom schemas; a single-method interface. |
| `SecurityScheme` | keep | The scheme object; `Bearer`, `Basic`, and `APIKey` in `tack` build it. |
| `SecurityRequirement` | keep | `map[string][]string`, as in the spec. |

## Package tsgen

| Identifier | Verdict | Reason |
| --- | --- | --- |
| `Generate(doc)` | keep | The TypeScript client as bytes; the CLI and tests both call it. |
| `GenerateZod(doc)` | keep | A separate file so the client stays dependency-free. |
| `Check(doc, path)` | keep | The test-time staleness check; the documented way to keep a committed client fresh. |
| `CheckZod(doc, path)` | keep | Same for the schemas. Merging the two into one `Check` with a target argument was considered; two names read better in a test table and match the two `Generate` functions. |
| `ErrStale` | keep | Lets callers and the CLI tell a stale file from an I/O error with `errors.Is`. |

## Not public

`internal/bind`, `internal/schema`, `internal/mock`, and the CLI are not part of the API and may change in any release. The `cmd/tack` command is the user-facing surface for them; its flags are documented in its package comment and checked by its tests.

## Open questions for v0.9

1. `ShutdownTimeout` as a constant or a `Config` field (see above).
2. Whether `Error` should gain a constructor for RFC 9457 `Type` URIs, or whether users keep setting the field.
3. Whether `Stream[T]` should be declared as `iter.Seq[T]` once the standard library's iterator types are used widely enough that users expect it. Today the named function type is needed for the methods that route detection relies on.
