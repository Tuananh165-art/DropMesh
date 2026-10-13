# Contributing

DropMesh is in its foundation and LAN CLI milestones. Read `AGENTS.md`, `docs/INDEX.md`, current state, the active spec and ADR, and `TODO.md` before proposing changes. Select a dependency-safe task and keep the change scoped to its acceptance criteria.

Install the local Go toolchain with `./scripts/bootstrap.ps1`, then dot-source `./scripts/env.ps1`. If PowerShell blocks scripts, use `Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass` in the current shell; this policy ends with that shell. Before requesting review, run `gofmt`, `go vet ./...`, `go test ./...`, and `go test -race ./...` for concurrency changes. Include platform build results and classify physical LAN checks separately from loopback.

Update the relevant spec or ADR, the TODO card, and current-state evidence. Never publish security, performance, privacy, migration, or platform claims that the current evidence does not establish. Do not include real invite strings, credentials, private keys, or user file paths in issues and logs.
