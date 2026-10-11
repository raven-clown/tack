## What does this PR do?

<!-- A short summary of the change and why it's needed. -->

Closes #

## Type of change

- [ ] Bug fix (non-breaking)
- [ ] New feature (non-breaking)
- [ ] Breaking change (existing code will need changes)
- [ ] Documentation / examples
- [ ] Refactor / tests / tooling

## Public API changes

<!--
List any exported identifiers added, renamed, or removed, and explain why.
Write "None" if the public API is unchanged.
-->

None

## How was this tested?

<!-- Describe the tests you added or ran. Bug fixes need a test that fails without the fix. -->

## Checklist

- [ ] I read [CONTRIBUTING.md](../CONTRIBUTING.md) and [ARCHITECTURE.md](../ARCHITECTURE.md).
- [ ] No reflection or struct-tag parsing happens at request time.
- [ ] `go vet ./...`, `go test -race ./...`, and `golangci-lint run` pass locally.
- [ ] New exported identifiers have doc comments (and an `Example` test where it makes sense).
- [ ] Golden files are updated and the diff was reviewed (if output changed).
- [ ] `CHANGELOG.md` is updated under **Unreleased** (if behavior changed).
- [ ] Docs and examples are updated (if behavior changed).
