# Future fit

Whether the add-ons planned after v1.0 can be built on the v0.9 public API without changing an existing exported identifier. Each section shows the code a user would write, built in `internal/futurefit` from a few lines of stand-in code that use only exported identifiers; the tests there compile and run against the checkout, so a change to the public API that breaks an add-on fails the build.

Summary: all six fit. No exported identifier needs to change before v1.0. Four extension points would make some of them nicer, and every one is additive (a new function, method, or `Config` field), so they can wait for evidence from real use.

| Add-on | Addable without changing existing identifiers | What it needs today | Additive improvement later |
| --- | --- | --- | --- |
| API versioning (`App.Version`) | Yes | `App.Group` with `Tags` and `Deprecated`; the document from `App.OpenAPI` | `App.Version(v)` as a group constructor; a per-version document method |
| Typed pagination (`tack.Page[T]`) | Yes | A generic struct and an embedded input struct | The two types themselves |
| WebSocket | Yes | Nothing in the root yet; a raw handler mounts beside the app | `tack.Socket[In, Out]` detected like `Stream`; `App.Handle` for raw handlers |
| tackdb | Yes | `DefineError`, `Errors`, group middleware, `ContextValue`, `App.WriteError` | A `Config` error mapper that runs before the 500 fallback |
| tackcache | Yes | `Middleware` as the route option | A registration hook so the option can document its header |
| tackauth | Yes | Group middleware, `ContextValue`, `Security`, `Errors`, `App.WriteError` | An option combinator so `Require` can document its own 403 |

## API versioning

```go
v1 := app.Group("/v1").With(tack.Tags("v1"), tack.Deprecated())
v2 := app.Group("/v2").With(tack.Tags("v2"))
tack.Get(v1, "/pets/{id}", getPet, tack.Errors(ErrPetNotFound))
tack.Get(v2, "/pets/{id}", getPetV2)
```

A version is a group: a prefix, a tag, and `Deprecated` on the old one. `App.Version("v2")` would return exactly that group, which is a new method. A document per version is a filter over `app.OpenAPI().Paths` by prefix, which the sketch does in ten lines from the value the API already returns; a method on `App` for it would be additive. Operation IDs already carry the version (`getV1PetsById`, `getV2PetsById`), so the generated client holds both.

Verdict: addable. See `versioning_test.go`.

## Typed pagination

```go
type listPetsInput struct {
	tack.PageInput              // cursor and limit query parameters
	Kind string `query:"kind"`
}

func listPets(ctx context.Context, in listPetsInput) (tack.Page[Pet], error)
```

`Page[T]` is a plain generic struct (`items`, `next`), and `PageInput` an embedded struct with `query` tags. Both already work: embedded structs bind and validate (`?limit=0` is a 422 at `query.limit`), the schema registry names the instantiation `PagePet`, and the client exports `interface PagePet`. Nothing in the root needs to change to add the two types; they could even live in a separate package.

Verdict: addable. See `pagination_test.go`.

## WebSocket

```go
tack.Get(app, "/chat", func(ctx context.Context, in ChatInput) (tack.Socket[ClientMessage, ServerMessage], error) {
	return func(ctx context.Context, in <-chan ClientMessage, out chan<- ServerMessage) { ... }, nil
})
```

`Socket[In, Out]` would be a named function type in the root package, like `Stream`: every instantiation implements an unexported interface, so the route finds it with a type assertion at registration and never reflects on function types. The sketch proves the mechanism for a two-parameter type. The route would upgrade the connection with a library of the user's choice, decode `In` and encode `Out` as JSON, and document the handshake; `Status(code)` keeps its 2xx or 3xx rule, since the 101 is written by the upgrade, not by the route option.

Today a socket is a raw handler mounted beside the app on an outer `http.ServeMux`, which the sketch does. An `App.Handle(pattern, h)` method for raw handlers would keep app middleware in front of such a handler, and would be additive.

Verdict: addable. See `websocket_test.go`.

## tackdb

```go
pets := app.Group("/pets", tackdb.Tx(app, db)).With(tack.Errors(tackdb.ErrUnavailable))

tack.Post(pets, "", func(ctx context.Context, in createPetInput) (Pet, error) {
	tx, _ := tackdb.From(ctx)
	if err := insert(ctx, tx, in.Body); err != nil {
		return Pet{}, tackdb.Err(err) // 23505 becomes ErrConflict, no rows becomes ErrNotFound
	}
	...
}, tack.Status(http.StatusCreated), tack.Errors(tackdb.ErrConflict))

tack.Get(app, "/health", tackdb.Health(db), tack.Errors(tackdb.ErrUnavailable))
```

- Error mapping: `tackdb.ErrNotFound`, `ErrConflict`, and `ErrUnavailable` are `DefineError` values, so `tack.Errors` documents them and the client gets `ConflictProblem` and `UnavailableProblem` in its unions. `tackdb.Err(err)` maps driver errors to them and returns anything else as is, which stays a hidden 500.
- Transaction per request: group middleware begins a transaction, stores it in the context, and commits or rolls back by the status the handler produced, through a `ResponseWriter` wrapper with `Unwrap` so streams still flush. Handlers read it back typed with `tack.ContextValue`.
- Health: a handler for `tack.Get` that returns a 503 error when the ping fails.

The one thing the sketch cannot do is map errors app-wide without a call in every handler. `Config.ErrorHandler` runs after tack has turned an unknown error into a logged 500, so it is the wrong place. A `Config` field that runs before that mapping, `func(error) error`, would be additive.

Verdict: addable as a separate module today. See `tackdb_test.go`.

## tackcache

```go
cache := tackcache.New(redisClient)
tack.Get(pets, "/{id}", getPet, cache.TTL(time.Minute), tack.Errors(ErrPetNotFound))
tack.Put(pets, "/{id}", func(ctx context.Context, in updateInput) (Pet, error) {
	cache.Invalidate("/pets/" + strconv.Itoa(in.ID))
	...
})
```

`TTL` is a `tack.RouteOption` built from `tack.Middleware`, so it goes wherever options go and composes with groups through `With`. The middleware stores 200 responses by request URI, serves them with an `Age` header until they expire, sends `Cache-Control: max-age` on misses, and lets errors through. `Invalidate` drops entries by prefix.

What it cannot do today is show the cache in `/docs`: a `RouteOption` does not learn which operation it was applied to, so the option cannot add the `Cache-Control` header to that operation's responses. A registration hook, for example an option that receives the `*openapi.Operation` at registration, would close that gap and is additive. The test asserts the gap so FUTURE_FIT.md is updated when the hook lands.

Verdict: addable; documenting the cache waits for an additive hook. See `tackcache_test.go`.

## tackauth

```go
app := tack.New(tack.Config{Security: map[string]openapi.SecurityScheme{"bearer": tack.Bearer("JWT")}})
admin := app.Group("/admin", tackauth.JWT[Claims](app, keys)).
	With(tack.Security("bearer"), tackauth.Require(app, "admin"), tack.Errors(tackauth.ErrForbidden))

tack.Get(admin, "/me", func(ctx context.Context, _ struct{}) (Me, error) {
	claims, _ := tackauth.Claims[Claims](ctx)
	return Me{Subject: claims.Subject}, nil
})
```

The middleware checks the bearer token, answers 401 through `app.WriteError(w, r, tack.ErrUnauthorized)` so the format matches, and stores the user's own claims type in the context; `tack.ContextValue` returns it typed. `Require(roles...)` is a route option built from `tack.Middleware` that answers 403 with a `DefineError` value. `tack.Security` documents the 401 and the scheme, and `tack.Errors(tackauth.ErrForbidden)` documents the 403, so the client's union for the route holds `UnauthorizedProblem` and `ForbiddenProblem`.

The seam: `Require` cannot bundle `Middleware` and `Errors` into one option, because a `RouteOption` cannot be composed from outside the package. Users write the two options side by side. A combinator, `tack.Options(opts ...RouteOption) RouteOption`, would let `Require` document its own 403 and is additive.

Verdict: addable as a separate module today. See `tackauth_test.go`.

## Extension points deferred, all additive

None of these change an existing identifier, so none is needed before v1.0. They are listed so the decision is on record (DESIGN.md D12).

| Extension point | Shape | Unblocks |
| --- | --- | --- |
| Option combinator | `func Options(opts ...RouteOption) RouteOption` | An add-on option that does two things, such as `Require` with its 403 |
| Registration hook | An option that receives the `*openapi.Operation` at registration | `tackcache` documenting its header; any add-on that annotates operations |
| Raw handlers | `App.Handle(pattern string, h http.Handler)` | WebSocket and other non-JSON handlers behind app middleware |
| Error mapper | A `Config` field `func(error) error` run before the 500 fallback | `tackdb` mapping driver errors without a call per handler |
