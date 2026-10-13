# DropMesh agent guide

DropMesh is a provisional, cross-platform, direct-first peer-to-peer transfer project. M1 is a one-file LAN CLI; M2 resumable transfer is implemented in progress and awaits native/platform acceptance. It is not a cloud service, trusted-device system, browser app, or release-ready product.

## Priority and required workflow

Prioritize correctness, security, reliability, simplicity, maintainability, observability, performance, developer experience, then feature speed. Read in this order: this file; `docs/context/PRODUCT.md`; `docs/context/CURRENT_STATE.md`; `docs/context/ARCHITECTURE.md`; `TODO.md`; relevant specs; relevant ADRs; relevant skills. Inspect the code and current diff before editing. Work the first unblocked TODO. Update tests, documentation, TODO status, current state, and known issues with every completed task.

Use `docs/INDEX.md` to find the canonical documents. Mark evidence as one of: implemented and verified, implemented but unverified, proposal, experiment, or externally sourced. Do not turn an unverified cross-platform build or loopback run into LAN acceptance.

## Go and lifecycle rules

Use idiomatic Go, explicit ownership, propagated contexts, bounded goroutines and queues, backpressure, graceful shutdown, wrapped actionable errors, and structured logs with secrets removed. Add interfaces only at demonstrated boundaries. Do not create generic utility packages, unneeded services, or speculative abstractions. Public APIs and wire formats require a spec. Never claim resume, seamless migration, measured loss, or performance without evidence.

Keep QUIC types in `internal/transport/quic`. Never accept a peer path as a filesystem path. Never replace an existing destination file. Avoid logging invite text, capability secrets, private keys, file contents, and private paths. Do not invent cryptography. The invite secret is a bearer credential; the person carrying it is in the trust boundary.

## Completion

For behavior changes, run relevant unit, integration, E2E, and security checks; run the race detector for concurrent code. Run format, vet, static analysis, vulnerability checks, and platform builds when available. Fuzz protocol and filesystem parsers. Record the exact commands and outcomes. Before completion, update `TODO.md`, `docs/context/CURRENT_STATE.md`, and, if necessary, `docs/context/KNOWN_ISSUES.md` and `docs/context/OPEN_QUESTIONS.md`. Record durable engineering lessons in `docs/MEMORY.md`; do not copy session logs.
