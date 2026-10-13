# ADR-0004: versioned control protocol

**Status:** accepted for M1; M2 supersedes the active wire version with v2.

## Context

Network input is untrusted; ad-hoc unbounded JSON and implicit goroutine state are unsafe.

## Decision

Use versioned typed JSON control messages in a four-byte length-prefixed stream with a 64 KiB bound, plus a distinct unidirectional chunk stream. Reject unknown fields/types and invalid state order.

## Consequences

Wire behavior is a compatibility contract. Changes require a reviewed spec and explicit version decision. Binary chunks avoid base64 expansion. M2 uses invite prefix/version 2, control version 2, and ALPN `dropmesh/2`; the implementation rejects v1 rather than downgrading. See spec 0004.
