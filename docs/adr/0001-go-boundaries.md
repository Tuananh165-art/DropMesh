# ADR-0001: Go module and package boundaries

**Status:** accepted for M1.

## Context

The product is explicitly Go-first. Empty repository; one CLI and one native transport are currently in scope.

## Decision

Use one module named `dropmesh`, one command under `cmd/dropmesh`, and narrow internal packages for auth, protocol, transfer, QUIC adaptation and safe receiving. No public SDK or server binaries. Pin Go 1.27.2 locally and quic-go v0.63.0.

## Consequences

The module path must be changed before publication. QUIC types stay internal. The pinned toolchain requires a workspace-local bootstrap/cache.
