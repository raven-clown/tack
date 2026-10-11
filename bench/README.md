# bench

The same pet API on net/http, Gin, Echo, Huma, and tack, measured by one harness on identical requests. This is its own module so the frameworks' dependencies never reach `github.com/raven-clown/tack`; it builds tack from the checkout through a `replace` directive.

## Routes and rules

| Route | Rules |
| --- | --- |
| `GET /pets` | `kind` query, one of cat/dog/bird; `limit` query, 1-100, default 20 |
| `GET /pets/{id}` | `id` path, integer >= 1; 404 when missing |
| `POST /pets` | JSON body: `name` required, at most 50 characters; `kind` one of cat/dog/bird; 201 |
| `PUT /pets/{id}` | same body as POST; 404 when missing |

Each variant uses its framework's own binding and validation the way its documentation shows, with no logging or recovery middleware: net/http with `strconv` and `encoding/json` by hand, Gin's `ShouldBind*` with `binding` tags in release mode, Echo's `Bind` with a `go-playground/validator` behind `echo.Validator`, Huma through `humago` with `huma.DefaultConfig`, and tack with its defaults. The store is fixed and read-only.

## Fairness

`TestEquivalence` sends twelve requests to every variant and checks the status codes and that error bodies name the offending fields, so each framework is proven to enforce the same rules before any is timed. The only accepted differences are conventions: Gin answers 400 to every validation error where the others answer 422, and a non-integer path value is 400 everywhere except Huma, which answers 422.

```bash
go test ./...
```

## Run the benchmarks

```bash
bash bench/run.sh        # 8 runs; raw output lands in bench/results/
```

The script records the Go version, the operating system, and the CPU, and prints medians when `benchstat` is installed. The timed cases are `get-by-id` (a path parameter), `list-query` (a query with validation and a default), `create-json` (a JSON body in and out, 201), and `validation-error` (a body failing two rules). Each writes to a discarding `ResponseWriter`, so the numbers measure the frameworks rather than `httptest.ResponseRecorder`. Results and the cases where tack is slower are in [BENCHMARKS.md](../BENCHMARKS.md).
