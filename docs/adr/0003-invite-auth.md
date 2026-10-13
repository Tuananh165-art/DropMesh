# ADR-0003: invite certificate and bearer capability

**Status:** accepted for M1.

## Context

Short human codes need a proven PAKE; M1 is local CLI and can share a high-entropy invite.

## Decision

Use a short-lived invite with an ephemeral TLS certificate, random session ID, and 256-bit one-use capability. Trust the certificate from the bounded invite via a private root pool; authenticate receiver capability after TLS. Compare capability bytes in constant time. Require TLS 1.3 and no 0-RTT.

## Consequences

Anyone obtaining invite text can receive one transfer. Secret redaction and expiry are essential. This decision does not choose persistent device identity or a short-code PAKE.
