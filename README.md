# DropMesh (working name)

Direct, authenticated file transfer between devices. DropMesh is an early Go project; its current milestone is a single-file LAN CLI. The name is provisional: another file-transfer app and a separate product already use DropMesh. Recheck naming and trademark concerns before publication.

## Status

The M0 foundation and M1 direct QUIC transfer are implemented. Hosted Windows/Linux/macOS CI passes; M1 still needs offline two-device LAN acceptance. M2 resume is in progress: protocol v2, receiver-owned journaled state, explicit state deletion, and process-restart recovery are implemented and tested locally and on hosted Windows/Linux/macOS. Independent protocol/threat review and maintainer signoff remain open; see [`docs/acceptance/M2.md`](docs/acceptance/M2.md). Tests do not establish power-loss durability. Discovery, rendezvous, relay, browser, desktop, and mobile support are future roadmap work.

## Resume a transfer

Every sender run creates a fresh one-use invite. Save the non-secret `Transfer ID` printed by the sender if a transfer may need to resume. If a connection stops, rerun the sender with the same source file and ID, share the new invite, then approve the offer again on the receiver:

```text
dropmesh send FILE --resume TRANSFER_ID
dropmesh receive --out DIRECTORY
```

Incomplete receiver state is kept under `DIRECTORY/.dropmesh-state/` for 24 hours after its last committed chunk. Remove it explicitly with `dropmesh resume delete TRANSFER_ID --out DIRECTORY`. Version 2 does not accept version 1 invites or silently downgrade.

## Development

Use PowerShell. If execution policy blocks scripts, set a process-only policy for the current shell; this does not persist after the shell exits:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
./scripts/bootstrap.ps1
. ./scripts/env.ps1
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
```

The bootstrap verifies the upstream archive SHA-256 and installs the build/test toolchain under `.tools`; module and build caches stay under `.cache`. To reduce local extraction limits, the toolchain omits Go source test fixtures and vendored command sources. Use a complete upstream Go installation for standard-library/toolchain development. See `CONTRIBUTING.md` for checks and evidence boundaries.

## Security

The LAN invite is a one-time bearer secret. Share each newly generated invite through a trusted channel. Transfer content is protected by TLS 1.3; the receiver validates per-chunk and whole-file SHA-256 digests and revalidates journaled chunks on restart. Resume state contains file metadata and partial bytes readable by users with access to the output directory. The threat model and remaining assumptions are in `docs/THREAT_MODEL.md`. Report security issues privately using `SECURITY.md`; the security contact is not configured yet.

## Roadmap and decisions

Start with `docs/INDEX.md`, then `TODO.md`, `ROADMAP.md`, and `docs/context/CURRENT_STATE.md`. See the Apache-2.0 license in `LICENSE`.
