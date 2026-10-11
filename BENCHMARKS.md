# Benchmarks

Overhead of tack compared to plain `net/http` on the request path. Comparisons with other frameworks will live in `/bench` (v0.9).

## Run

```bash
go test -run '^$' -bench . -benchmem -cpu 1 -count 8 .
```

The benchmarks write to a `ResponseWriter` that discards its output, so they measure the handlers rather than `httptest.ResponseRecorder`, which clones the header map on every request and used to account for most of the time and allocations.

## Results (v0.6.1)

Go 1.26, Linux, one core, 8 runs; the median run is shown. The v0.6.0 column is the same benchmark on the previous release.

| Benchmark | v0.6.0 ns/op | v0.6.1 ns/op | v0.6.0 B/op, allocs | v0.6.1 B/op, allocs |
| --- | --- | --- | --- | --- |
| `net/http` handler writing `{}` | 285 | 285 | 18, 2 | 18, 2 |
| tack, empty input and output | 425 | 450 | 24, 2 | 16, 1 |
| tack, path + query + default + validation | 1690 | 1130 | 504, 10 | 48, 4 |

The empty tack handler costs about 160 ns over plain `net/http` (the pooled encoder, the input pool, and the JSON encoding of an empty struct) and one allocation, the `Content-Type` header, which `net/http` makes too. Binding and validating two parameters costs about 700 ns and three allocations: the path values `ServeMux` extracts, one inside the validator, and the header.

## What changed in v0.6.1

- The query string is scanned for the fields a route binds instead of being parsed into a map with `r.URL.Query()`, and single values are read without a slice. That removed six allocations per request with query parameters.
- Each route pools its input struct, so binding and validation, which need a pointer, no longer move every request's input to the heap. The handler receives a copy, so nothing is shared.
- Responses are encoded into a pooled buffer by a pooled `json.Encoder` instead of `json.Marshal`.
