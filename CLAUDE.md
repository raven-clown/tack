# Working on tack

Read [ARCHITECTURE.md](ARCHITECTURE.md) and [DESIGN.md](DESIGN.md) before changing anything non-trivial. The rules below are the ones that are easy to miss.

## Rules that are not negotiable

- **No reflection at request time.** Everything that touches `reflect` runs at route registration and is cached. Request handling runs precomputed plans only.
- **Small public API.** Every exported identifier at the root needs a reason, a doc comment, and usually an `Example` test. Prefer a tag, an option, or an existing type over a new one.
- **Fail at registration, with the fix.** A misconfigured route panics at startup naming the route, the field, and what to change. Generators (`tsgen`) fail the same way: one error with the fix, never silent renaming or broken output.
- **No new core dependencies.** The root module depends on `go-playground/validator` only. Test-only and example-only dependencies are fine; examples with extra dependencies are their own module.
- **The OpenAPI document is the contract.** `/docs`, `tsgen`, and the CLI all read the document, never Go types again, so they cannot disagree. Generated output must be deterministic: sort everything.

## Before pushing

```bash
go build ./... && go vet ./... && go test -race ./...
golangci-lint run
(cd examples/auth && go vet ./... && go test -race ./...)
```

- Coverage must stay at or above 85% (CI enforces it).
- Golden files live in `testdata/` and `examples/petstore/client/`. When output changes on purpose, regenerate with `go test ./... -update`, then read the diff before committing.
- The client end-to-end test needs `tsc` and `node` on PATH; it skips otherwise, so run it locally when touching `tsgen`.
- Run `/code-review` on the branch before handing it over, and `/security-review` when a change touches request parsing, errors, or security schemes.

## When a change ships a feature

Update all of these in the same change:

- `CHANGELOG.md`: under **Unreleased**, or under a dated `## [x.y.z] - YYYY-MM-DD` heading when the change is the release. Add the compare link at the bottom of the file. Breaking changes go under **Breaking** with a migration note.
- `DESIGN.md`: a numbered decision (options considered, why one was chosen) for anything a future contributor might question, plus a row in the summary table and the decisions table.
- `ARCHITECTURE.md`: the package layout and the component description.
- `README.md`: the feature section and the roadmap row.

Write all of it in plain prose: no emoji, no em dashes, short sentences.

## Git and releases

- Never commit on `main`. Branch as `feat/...`, `fix/...`, `docs/...`, or `ci/...`, push, and the maintainer merges with a fast-forward.
- Conventional Commits (`feat:`, `fix:`, `docs:`, `ci:`, `refactor(bind):`). Commit messages describe the change and why; they carry no trailers, sign-offs, or tool attribution.
- Author and committer are the maintainer's identity (`raven-clown <160258096+raven-clown@users.noreply.github.com>`). Set `git config user.name` and `user.email` before the first commit.
- Nothing in the repository mentions which tools or assistants produced it: not commit messages, branch names, code comments, or docs.
- Release: finish the CHANGELOG heading and links, tag the merge commit with `git tag -a vX.Y.Z -m "tack vX.Y.Z: <one line of what shipped>"`, push the tag, then `gh release create vX.Y.Z --title vX.Y.Z --notes-file <the CHANGELOG section>`. Publishing the release triggers `.github/workflows/release.yml`, which builds the `tack` command for every platform and attaches the archives.

## Map

| Path | What |
| --- | --- |
| `app.go`, `route.go`, `group.go` | Registration: `New`, `Get`/`Post`/..., groups, middleware, route options |
| `errors.go`, `security.go`, `context.go` | `Error`, `DefineError`, `ErrorHandler`, security schemes, `ContextValue` |
| `openapi.go`, `output.go`, `validate.go` | Document building, output wrappers, validation messages |
| `internal/bind` | Request to struct plans |
| `internal/schema` | Go types to JSON Schema, including `validate` tag mapping |
| `openapi/` | Document types (public) |
| `tsgen/` | TypeScript client and Zod schema generators with `Check`/`CheckZod` |
| `internal/mock` | Example responses for `tack mock` |
| `stream.go`, `server.go` | `Stream`/`Event` for server-sent events; `Run`/`Serve` with graceful shutdown |
| `cmd/tack` | CLI: `gen ts`, `gen zod`, `check`, `dev`, `mock`, `new`, `version`; project template under `template/` |
| `examples/` | Runnable apps; `petstore` commits its generated client |
| `research/` | Measured comparison with other frameworks; do not edit casually |
