# Benchmarks

Two sets of numbers: tack against the other frameworks on the same API (`bench/`, since v0.9), and tack's own overhead over plain `net/http` on the request path (`bench_test.go`, since v0.1).

## tack against net/http, Gin, Echo, and Huma

`bench/` is its own module. It serves the same four pet routes on net/http (by hand), Gin, Echo, Huma, and tack, with the same rules: a `kind` query that must be cat, dog, or bird; a `limit` query between 1 and 100 with a default of 20; an `id` path value of at least 1; a JSON body whose `name` is required and at most 50 characters. `TestEquivalence` sends twelve requests to every variant and checks the status codes and that error bodies name the failing fields, so all five are proven to enforce the same rules before any is timed. Each variant is configured the way its documentation shows, without logging or recovery middleware. See [bench/README.md](bench/README.md).

### Run

```bash
bash bench/run.sh        # 8 runs, raw output in bench/results/
```

The script builds the test binary once and pins it to one core, because a single-threaded benchmark on a CPU with performance and efficiency cores is bimodal otherwise (the first unpinned run here spread by up to 63%). The equivalent command, without the pinning, is:

```bash
cd bench && go test -run '^$' -bench . -benchmem -cpu 1 -count 8 .
```

### Results

Go 1.27.2 (windows/amd64), Intel Core i7-14700, one pinned core, 2026-10-11, median of 8 runs from `bench/results/2026-10-11-windows-amd64.txt`; the spread between runs was 9% or less. Allocations are per request.

| Request | net/http | Gin v1.12.0 | Echo v4.15.4 | Huma v2.39.1 | tack v0.9.0 |
| --- | --- | --- | --- | --- | --- |
| `GET /pets/7` (path value, 200) | 491 ns, 128 B, 4 | 1093 ns, 600 B, 10 | 828 ns, 192 B, 7 | 1232 ns, 432 B, 11 | **442 ns, 16 B, 1** |
| `GET /pets?kind=cat&limit=10` (query, validation, 200) | 1410 ns, 912 B, 8 | 2215 ns, 1144 B, 13 | 2106 ns, 960 B, 11 | 1675 ns, 712 B, 10 | **1257 ns, 416 B, 1** |
| `POST /pets` valid body (JSON in and out, 201) | **1100 ns, 648 B, 9** | 1418 ns, 664 B, 11 | 1495 ns, 664 B, 11 | 2928 ns, 920 B, 21 | 1308 ns, 176 B, 5 |
| `POST /pets` empty name, bad kind (validation error) | **1499 ns, 744 B, 11** | 2870 ns, 2296 B, 36 | 2999 ns, 2248 B, 39 | 4372 ns, 1208 B, 31 | 3193 ns, 1424 B, 35 |

Where tack is slower:

- `POST /pets` valid: 1308 ns against 1100 ns for the hand-written net/http handler, 19% slower, with fewer bytes and allocations. It is faster than Gin, Echo, and Huma.
- `POST /pets` validation error: 3193 ns against 1499 ns for net/http (2.1 times slower), 2870 ns for Gin (11% slower), and 2999 ns for Echo (6% slower); only Huma is slower. tack also allocates more bytes than Huma here (1424 B against 1208 B). A body with `validate` rules runs the validator's reflective walk and builds its error list; the compiled checks that make parameter validation cheap do not cover bodies yet. This is the known slow path.
- `GET /pets/7`: tack's 442 ns and net/http's 491 ns are within the run-to-run spread; call them equal.

Everywhere else tack is the fastest of the five and allocates the least. Note what the comparison does and does not say: every variant was written by the same person for this harness, each framework offers knobs (a faster JSON library for Gin or Echo, a leaner config for Huma) that were left at their documented defaults, and the hand-written net/http handler is the floor only because it checks exactly these rules and nothing else.

## tack against plain net/http

Overhead of tack compared to plain `net/http` on the request path.

### Run

```bash
go test -run '^$' -bench . -benchmem -cpu 1 -count 8 .
```

The benchmarks write to a `ResponseWriter` that discards its output, so they measure the handlers rather than `httptest.ResponseRecorder`, which clones the header map on every request and used to account for most of the time and allocations.

### Results

Go 1.26, Linux, one core, 8 runs; the median run is shown. Each column is the same benchmark on that release.

| Benchmark | v0.6.0 | v0.6.1 | v0.6.2 |
| --- | --- | --- | --- |
| `net/http` handler writing `{}` | 285 ns, 18 B, 2 allocs | 285 ns, 18 B, 2 allocs | 285 ns, 18 B, 2 allocs |
| tack, empty input and output | 425 ns, 24 B, 2 allocs | 450 ns, 16 B, 1 alloc | 270 ns, 0 B, 0 allocs |
| tack, path + query + default + validation | 1690 ns, 504 B, 10 allocs | 1130 ns, 48 B, 4 allocs | 650 ns, 16 B, 1 alloc |

The empty tack handler is now cheaper than the plain `net/http` handler because it writes the `Content-Type` header without canonicalizing the key or allocating its value, which `Header().Set` does. The remaining allocation on the bound route is the path values `ServeMux` extracts for `{id}`.

Go 1.27, Windows, one core, 8 runs, median shown. v0.6.2 is `main` before the release, measured with the same benchmark file; the two new rows did not exist before.

| Benchmark | v0.6.2 | v0.6.3 |
| --- | --- | --- |
| `net/http` handler writing `{}` | 117 ns, 18 B, 2 allocs | 117 ns, 18 B, 2 allocs |
| tack, empty input and output | 177 ns, 0 B, 0 allocs | 176 ns, 0 B, 0 allocs |
| tack, path + query + default + validation | 411 ns, 24 B, 2 allocs | 364 ns, 16 B, 1 alloc |
| tack, JSON body in, JSON body out, 201 | 987 ns, 672 B, 5 allocs | 833 ns, 112 B, 2 allocs |
| tack, validation error (422 problem) | 1257 ns, 696 B, 10 allocs | 1122 ns, 392 B, 8 allocs |

On Go 1.27 `encoding/json` copies a non-addressable value before encoding it, which is the second allocation the bound route had on this machine and not on Go 1.26. Encoding through a pointer removes it on both.

`TestAllocations` in `bench_test.go` holds each shape to its allocation budget, so a regression fails the build. CI runs it in a step of its own without `-race`, since the race detector allocates on its own, and on the current Go only: Go 1.26's `encoding/json` decoder allocates seven times on the JSON body route where 1.27's allocates twice.

### What changed

v0.6.1:

- The query string is scanned for the fields a route binds instead of being parsed into a map with `r.URL.Query()`, and single values are read without a slice.
- Each route pools its input struct, so binding and validation, which need a pointer, no longer move every request's input to the heap. The handler receives a copy, so nothing is shared.
- Responses are encoded into a pooled buffer by a pooled `json.Encoder` instead of `json.Marshal`.

v0.6.2:

- A route whose `validate` tags are all simple rules on scalar parameters runs compiled checks, one comparison per rule, instead of the validator's reflective walk. Bodies with rules, slices, and rules the compiler does not know keep the validator, and a test holds both to the same errors.
- The `Content-Type` header is a shared slice whose length equals its capacity, so a later `Header.Add` copies instead of writing into it.
- An input with nothing to bind skips the pool and the bind plan entirely.

v0.6.3:

- Results are copied into a pooled value and encoded through its pointer, so the encoder has an addressable value and copies nothing. Wrapped outputs hand the encoder a pointer to their `Body` field the same way.
- JSON bodies are read into a pooled buffer sized from `Content-Length` instead of `io.ReadAll`'s growing slice; `encoding/json` copies what it keeps, so the buffer is reused as soon as `Unmarshal` returns.
- Problem responses use the same encoder pool as success responses and a shared `Content-Type` value.
