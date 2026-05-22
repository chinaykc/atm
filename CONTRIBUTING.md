# Contributing

[中文](CONTRIBUTING.zh-CN.md)

ATM is deliberately small. Contributions should keep the todo file format stable, preserve the zero-dependency runtime, and avoid adding workflow-engine features.

## Local Checks

Run these before opening a change:

```sh
go test ./...
go vet ./...
go build -buildvcs=false ./...
```

The test suite uses fake tool executables, so it does not require the real Codex or Claude Code CLI.

`-buildvcs=false` keeps local checks working from source snapshots or directories without valid VCS metadata. Release builds from a clean git checkout can omit it if VCS stamping is desired.

## Change Guidelines

- Keep task semantics explicit in the todo file.
- Prefer standard-library APIs.
- Preserve cross-platform behavior on Linux, macOS, and Windows.
- Add focused tests for parser, marker, storage, and orchestration changes.
- Do not add a new tool adapter until the `toolRunner` contract is enough for it without changing the todo language.

## Release Checklist

1. Run `go test ./...`.
2. Run `go vet ./...`.
3. Run `go build -buildvcs=false ./...`.
4. Review `README.md`, `docs/commands.md`, and `docs/design.md` for behavior drift.
5. Verify examples under `examples/` still describe current behavior.
