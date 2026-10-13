# ADR-0005: sequential 1 MiB chunks and SHA-256

**Status:** accepted as a measurable M1 baseline.

## Context

Large files must stream with bounded memory and detect per-record and whole-file corruption. Resume is a later milestone.

## Decision

Prehash the opened file, stream sequential 1 MiB chunks with index, offset, length and SHA-256, then compare whole-file SHA-256 before completion. The receiver repeats both checks.

## Consequences

Send performs a source hash pass and transfer pass; mutation is detected at the receiver and sender. Chunk size is not an optimized claim. M2 persists manifest/verified chunk state under a separate spec.
