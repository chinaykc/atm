# Contributing

[中文](CONTRIBUTING.zh-CN.md)

ATM is deliberately small. Contributions should keep the ATM file format stable, preserve the zero-dependency runtime, and avoid adding workflow-engine features.

## Local Checks

Run these before opening a change:

```sh
go test ./...
go vet ./...
go build ./...
```

The test suite uses fake tool executables, so it does not require the real Codex or Claude Code CLI.

## Change Guidelines

- Keep task semantics explicit in the ATM file.
- Prefer standard-library APIs.
- Preserve cross-platform behavior on Linux, macOS, and Windows.
- Add focused tests for parser, marker, storage, and orchestration changes.
- Do not add a new tool adapter until the `toolRunner` contract is enough for it without changing the todo language.

## Documentation Checks

- When CLI flags or subcommands change, compare the docs with `go run ./cmd/atm -h` and the relevant `go run ./cmd/atm <command> -h` output.
- When DSL commands change, update `README.md`, `README.zh-CN.md`, `docs/commands.md`, `docs/commands.zh-CN.md`, and `docs/user/reference/commands.md` in the same change.
- Validate runnable examples with `go run ./cmd/atm check examples/*.todo.md`.
