# Contributing to tack

Thanks for your interest in tack! Every contribution helps — bug reports, docs fixes, examples, and code.

By participating, you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to contribute

- **Report a bug** — open an issue with a minimal reproduction (a short `main.go` is ideal), what you expected, and what happened.
- **Suggest a feature** — open an issue describing the problem first, before the solution. Features that grow the public API need discussion before a PR.
- **Improve docs or examples** — always welcome, no issue needed.
- **Fix an issue** — look for [`good first issue`](https://github.com/raven-clown/tack/labels/good%20first%20issue) or [`help wanted`](https://github.com/raven-clown/tack/labels/help%20wanted). Comment on the issue so others know you're working on it.
- **Security issues** — do **not** open a public issue. See [SECURITY.md](SECURITY.md).

## Development setup

You need one of the two most recent Go releases and, for linting, [golangci-lint](https://golangci-lint.run/).

```bash
git clone https://github.com/raven-clown/tack.git
cd tack
go mod download
go test ./...
```

Before opening a PR, run the same checks as CI:

```bash
go build ./...
go vet ./...
go test -race ./...
golangci-lint run
```

### Golden files

Generated JSON Schema, OpenAPI, and TypeScript are tested against files in `testdata/`. If you intentionally change output, regenerate and review the diff carefully:

```bash
go test ./... -update
git diff testdata/
```

## Pull request process

1. Fork the repo and create a branch from `main` (e.g. `fix/query-slice-binding`).
2. Make focused changes — one logical change per PR.
3. Add or update tests. Bug fixes must include a test that fails without the fix.
4. Update docs, examples, and `CHANGELOG.md` (under **Unreleased**) if behavior changes.
5. Make sure all checks above pass.
6. Open the PR and fill in the template. Link the related issue.

A maintainer will review as soon as possible. We may ask for changes — that's a normal part of the process, not a rejection.

## Code guidelines

- Read [ARCHITECTURE.md](ARCHITECTURE.md) first. The most important rule: **no reflection at request time.**
- Format with `gofmt`; follow [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- Every exported identifier needs a doc comment; public functions should have an `Example` test.
- Prefer table-driven tests.
- Wrap errors with context: `fmt.Errorf("binding query %q: %w", name, err)`.
- No global mutable state.
- Do not add dependencies to the core module without discussing it in an issue first.

## Public API changes

Anything exported from the root package is public API.

- **Before v1.0:** breaking changes are allowed in minor versions but must be called out in `CHANGELOG.md` with a migration note.
- **After v1.0:** no breaking changes until v2. New features must be additive.

If your PR adds, renames, or removes an exported identifier, explain why in the PR description.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```text
feat: support time.Time in query parameters
fix: report all validation errors instead of the first
docs: add file upload example
test: cover nested embedded structs in schema
refactor(bind): precompute converters per field
```

Use `feat!:` or a `BREAKING CHANGE:` footer for breaking changes.

## Questions?

Open a [discussion](https://github.com/raven-clown/tack/discussions) or comment on a relevant issue.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
