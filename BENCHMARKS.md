# Benchmarks

Overhead of tack compared to plain `net/http` on the request path. Comparisons with other frameworks will live in `/bench` (v0.9).

## Run

```bash
go test -run '^$' -bench . -benchmem -cpu 1 -count 8 .
```

The benchmarks write to a `ResponseWriter` that discards its output, so they measure the handlers rather than `httptest.ResponseRecorder`, which clones the header map on every request and used to account for most of the time and allocations.

## Results

Go 1.26, Linux, one core, 8 runs; the median run is shown. Each column is the same benchmark on that release.

| Benchmark | v0.6.0 | v0.6.1 | v0.6.2 |
| --- | --- | --- | --- |
| `net/http` handler writing `{}` | 285 ns, 18 B, 2 allocs | 285 ns, 18 B, 2 allocs | 285 ns, 18 B, 2 allocs |
| tack, empty input and output | 425 ns, 24 B, 2 allocs | 450 ns, 16 B, 1 alloc | 270 ns, 0 B, 0 allocs |
| tack, path + query + default + validation | 1690 ns, 504 B, 10 allocs | 1130 ns, 48 B, 4 allocs | 650 ns, 16 B, 1 alloc |

The empty tack handler is now cheaper than the plain `net/http` handler because it writes the `Content-Type` header without canonicalizing the key or allocating its value, which `Header().Set` does. The remaining allocation on the bound route is the path values `ServeMux` extracts for `{id}`.

## What changed

v0.6.1:

- The query string is scanned for the fields a route binds instead of being parsed into a map with `r.URL.Query()`, and single values are read without a slice.
- Each route pools its input struct, so binding and validation, which need a pointer, no longer move every request's input to the heap. The handler receives a copy, so nothing is shared.
- Responses are encoded into a pooled buffer by a pooled `json.Encoder` instead of `json.Marshal`.

v0.6.2:

- A route whose `validate` tags are all simple rules on scalar parameters runs compiled checks, one comparison per rule, instead of the validator's reflective walk. Bodies with rules, slices, and rules the compiler does not know keep the validator, and a test holds both to the same errors.
- The `Content-Type` header is a shared slice whose length equals its capacity, so a later `Header.Add` copies instead of writing into it.
- An input with nothing to bind skips the pool and the bind plan entirely.
