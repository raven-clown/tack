# Migration

Every breaking change between releases, with the code before and after. Before v1.0 a minor version may break; each break is also listed under **Breaking** in CHANGELOG.md with the DESIGN.md decision behind it.

## v0.6.3 to v0.9.0

One exported identifier changed. Everything else in the public API is the same, and v0.9.0 is the surface v1.0 will freeze.

### `ShutdownTimeout` is now `Config.ShutdownTimeout`

The package-level constant is gone. The wait after `ctx` is done is a `Config` field whose zero value is the old ten seconds, so programs that never read the constant need no change.

Before:

```go
app := tack.New(tack.Config{Title: "Pets"})
log.Printf("waiting up to %s on shutdown", tack.ShutdownTimeout)
log.Fatal(app.Run(ctx, ":8080"))
```

After:

```go
app := tack.New(tack.Config{Title: "Pets", ShutdownTimeout: 10 * time.Second})
log.Printf("waiting up to %s on shutdown", 10*time.Second)
log.Fatal(app.Run(ctx, ":8080"))
```

Deployments with a different budget set it once; a Kubernetes pod with the default 30 second grace period might use `ShutdownTimeout: 25 * time.Second`.

### Visible but not breaking

- Every response tack writes now carries `X-Content-Type-Options: nosniff`, and the docs page `Referrer-Policy: no-referrer`. A test that compares the full header map of a response needs the new entries.

## Earlier releases

- v0.3.0: an output struct with a field named `Body` became a response wrapper. A v0.2 output such as `struct{ Body string; Title string }` must rename `Body` to keep sending both fields as JSON. See CHANGELOG.md.
