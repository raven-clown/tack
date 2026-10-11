# tack

**Type-safe HTTP APIs for Go. Declare a struct once — get validation, OpenAPI docs, and a TypeScript client for free.**

[![Go Reference](https://pkg.go.dev/badge/github.com/raven-clown/tack.svg)](https://pkg.go.dev/github.com/raven-clown/tack)
[![CI](https://github.com/raven-clown/tack/actions/workflows/ci.yml/badge.svg)](https://github.com/raven-clown/tack/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> ⚠️ **Early development.** tack is pre-1.0. The API may change between minor versions until v1.0. See the [roadmap](#roadmap).

---

## Why tack?

Writing an API in Go usually means doing the same work three times:

1. Parse and validate the request by hand.
2. Write the OpenAPI spec (or comments that generate it) separately — and watch it drift out of date.
3. Write TypeScript types for the frontend — again by hand.

tack makes **your Go types the single source of truth**. You write a handler that takes a struct and returns a struct. tack does the rest.

```go
package main

import (
	"context"
	"net/http"

	"github.com/raven-clown/tack"
)

type GetPetInput struct {
	ID int `path:"id" validate:"min=1" doc:"Pet ID"`
}

type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name" validate:"required,max=50"`
}

func main() {
	app := tack.New(tack.Config{Title: "Pet API", Version: "1.0.0"})

	tack.Get(app, "/pets/{id}", func(ctx context.Context, in GetPetInput) (Pet, error) {
		if in.ID == 404 {
			return Pet{}, tack.NewError(http.StatusNotFound, "pet not found")
		}
		return Pet{ID: in.ID, Name: "Milo"}, nil
	})

	http.ListenAndServe(":8080", app) // open http://localhost:8080/docs
}
```

That's it. You now have:

- ✅ **Automatic binding** from path, query, header, cookie, and JSON body
- ✅ **Validation** with clear, field-level error messages
- ✅ **Standard errors** in [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) `application/problem+json` format
- ✅ **OpenAPI 3.1** at `/openapi.json` — always in sync with your code
- ✅ **Interactive docs** at `/docs`
- ✅ **TypeScript client** generated from the same types *(v0.4)*

## Installation

```bash
go get github.com/raven-clown/tack
```

Requires one of the two most recent Go releases.

## Features

### Validation errors that make sense

Send a bad request and get every problem at once — not just the first one:

```json
{
  "type": "about:blank",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "validation failed",
  "errors": [
    { "location": "body.name", "message": "is required" },
    { "location": "path.id", "message": "must be at least 1" }
  ]
}
```

### Just `net/http`

A `*tack.App` is an `http.Handler`. Use it with `http.Server`, mount it inside another router, and use any standard middleware (`func(http.Handler) http.Handler`). No lock-in.

### Fast by design

All reflection happens **once**, when routes are registered. At request time tack only runs a precomputed binding plan. See [BENCHMARKS.md](BENCHMARKS.md).

### Mistakes caught at startup

Declare a route with `{id}` in the path but no matching `path:"id"` field? tack panics at registration with a message telling you exactly what to fix — not at 3 a.m. in production.

## How it compares

| | net/http only | Gin / Echo / Fiber | **tack** |
| --- | --- | --- | --- |
| Typed handlers | ❌ | ❌ | ✅ |
| Validation built in | ❌ | Partial | ✅ |
| OpenAPI from code | ❌ | Via plugins / comments | ✅ |
| TypeScript client | ❌ | ❌ | ✅ *(v0.4)* |
| Standard `http.Handler` | ✅ | Varies | ✅ |

tack is inspired by [FastAPI](https://fastapi.tiangolo.com/), and shares goals with excellent Go projects like [Huma](https://github.com/danielgtaylor/huma) and [Fuego](https://github.com/go-fuego/fuego). Check them out too.

## Roadmap

| Version | Focus | Status |
| --- | --- | --- |
| v0.1 | Core: typed handlers, binding, validation, errors | 🚧 In progress |
| v0.2 | OpenAPI 3.1 + `/docs` | Planned |
| v0.3 | Groups, middleware, security schemes | Planned |
| v0.4 | TypeScript client generation | Planned |
| v0.5 | File upload, SSE streaming, CLI | Planned |
| v1.0 | Stable API, docs site | Planned |

## Documentation

- [Architecture](ARCHITECTURE.md) — how tack works inside
- [Examples](examples/) — runnable example projects
- [API reference](https://pkg.go.dev/github.com/raven-clown/tack)

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) and our [Code of Conduct](CODE_OF_CONDUCT.md) first. Looking for a place to start? Try an issue labeled [`good first issue`](https://github.com/raven-clown/tack/labels/good%20first%20issue).

Found a security issue? Please follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

## License

[MIT](LICENSE)
