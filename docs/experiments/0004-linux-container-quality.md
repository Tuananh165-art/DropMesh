# Experiment 0004: Linux container quality and security checks

**Date:** 2026-10-13  
**Status:** experiment  
**Purpose:** Run the configured Linux quality commands against the current M2 implementation and upgraded module graph.

## Environment and scope

- Docker image: `golang:1.27.2`; analyzer image: `golang:1.26.9`.
- Benchmark container reported an Intel Core i7-11800H CPU at 2.30 GHz.
- Docker Desktop uses its Linux VM. The source was mounted read-only, copied into `/tmp/dropmesh-src`, and tested from the container overlay filesystem. This exercises a Linux Go runtime and Linux kernel APIs, but does not establish bare-metal filesystem durability or hosted GitHub Actions behavior.
- The Go 1.26.9 Staticcheck job lowers the Go directive and removes the toolchain line only in the disposable analysis copy.
- Dependency graph: `quic-go v0.63.0`, `golang.org/x/net v0.60.0`, `golang.org/x/crypto v0.57.0`, and `golang.org/x/sys v0.48.0`.

## Commands and outcomes

All commands passed:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go test -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
go test ./internal/auth -run '^$' -fuzz '^FuzzDecodeInvite$' -fuzztime 10s
go test ./internal/protocol -run '^$' -fuzz '^FuzzReadMessage$' -fuzztime 10s
go test ./internal/filesafe -run '^$' -fuzz '^FuzzValidateName$' -fuzztime 10s
go test ./internal/resume -run '^$' -fuzz '^FuzzReadJournalHeader$' -fuzztime 10s
go test ./internal/resume -run '^$' -fuzz '^FuzzDecodeJournalRecord$' -fuzztime 10s
go test ./internal/transfer -run '^$' -bench '^BenchmarkHashOneMiB$' -benchtime 1x
go test ./internal/resume -run '^$' -bench '^BenchmarkDurableChunkCommit$' -benchtime 1x
```

The latest 10-second fuzz smokes passed with 43,498 auth, 853,418 protocol, 1,140,652 filename, 1,630,286 journal-header, and 1,661,304 journal-record executions. The Linux race run includes the transfer and resume subprocess tests.

`govulncheck` found no vulnerable symbols or imported packages. It reported one uncalled module-level advisory, GO-2026-5932 for `golang.org/x/crypto/openpgp`, with no fixed version; the project does not import OpenPGP. The five module-only `x/net` HTTP/2 advisories from v0.57.0 were removed by upgrading to v0.60.0.

The one-iteration benchmark results were:

- `BenchmarkHashOneMiB`: 1.261634 ms/op, 831.13 MB/s.
- `BenchmarkDurableChunkCommit`: 8.826901 ms/op, 118.79 MB/s.

These are smoke measurements on one container host, not stable performance claims or comparisons with the earlier Windows sample.

The revised Staticcheck command also passed in the Go 1.26.9 container with no findings:

```text
go mod edit -go=1.26.0 -toolchain=none go.mod
go mod tidy
go install honnef.co/go/tools/cmd/staticcheck@v0.8.1
"$(go env GOPATH)/bin/staticcheck" ./...
```

## Acceptance limits

- This experiment does not close hosted Windows/Linux/macOS CI.
- Container process-restart coverage does not prove power-loss, controller-cache, directory-entry durability, or native macOS behavior.
- It does not close the two-device offline LAN requirement or the independent protocol/threat-model review.
