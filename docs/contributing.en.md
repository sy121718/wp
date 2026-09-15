# Contributing

This is the practical English entry point for repository contributions. Project-specific rules are defined in [AGENTS.md](../AGENTS.md). The current open-source readiness notes are in [11-foundation-and-open-source.md](11-foundation-and-open-source.md).

## Before Editing

1. Read `AGENTS.md` and any closer `CLAUDE.md` or `AGENTS.md` in the directory you will change.
2. Read the implementation, configuration, and existing tests for the affected path.
3. Check `git status` and preserve unrelated work in a dirty tree.
4. Trace the active assembly or runtime path before relying on comments, plans, or static scans.

Keep changes narrow. Do not combine behavior changes with unrelated cleanup, dependency upgrades, or broad formatting. Business modules collaborate through narrow contracts; they do not import another module's `service` or `model` package.

## Verification

During development, run tests only for directly affected packages:

```bash
go test ./internal/module/example/...
```

Before submitting a Go change, format changed Go files and run the repository-wide compile and vet gates:

```bash
gofmt -w path/to/changed.go
go build ./...
go vet ./...
```

Report skipped tests and environment-dependent gaps. A zero exit code does not prove a database-backed test ran; inspect skip messages. Generated contracts must be checked with their documented generator when the corresponding source changes.

## Documentation and Contracts

- Explain intent, invariants, failure modes, and extension boundaries rather than paraphrasing code.
- Keep exported plugin contracts discoverable with concise English or bilingual Godoc summaries.
- Preserve detailed Chinese design documents. Add a curated English entry when a document becomes a prerequisite for contribution instead of mechanically translating the whole repository.
- Update documentation in the same change when a public contract or architectural invariant changes.

## Review Scope

A review should be able to identify the affected contract, the evidence for current behavior, the focused tests, and the repository-wide build/vet result. If a prerequisite in the issue differs from the implementation, stop and document the mismatch rather than manufacturing a compatible-looking change.

The repository is still defining its license and public security-reporting process. Do not infer terms from implementation files; follow the project maintainers' published repository metadata when those processes are added.
