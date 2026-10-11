# Benchmarks

Overhead of tack compared to plain `net/http` on the request path. Comparisons with other frameworks will live in `/bench` (v0.9).

## Run

```bash
go test -run '^$' -bench . -benchmem -cpu 1 -count 8 .
```

## Results (v0.1)

Go 1.26.4, Windows, Intel Core i7-14700, `-cpu 1`, 8 runs. This CPU mixes performance and efficiency cores, so timings vary between runs. Both the fastest and the median run are shown.

| Benchmark | Fastest ns/op | Median ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| `net/http` handler writing `{}` | 466 | 837 | 1010 | 10 |
| tack, empty input and output | 834 | 869 | 1016 | 10 |
| tack, path + query + default + validation | 1999 | 2056 | 1520 | 19 |

The empty tack handler stays within 2x of plain `net/http` (1.8x fastest, 1.04x median) and adds no allocations. Binding and validating two parameters adds about 1.2 µs and 9 allocations.
