# Spec 0002: one-time invite authentication

**Status:** M1 authentication behavior retained in M2 protocol v2. Local Windows and hosted Windows/Linux/macOS test suites pass; real two-device offline-LAN acceptance remains open.

The sender generates an ephemeral P-256 self-signed CA/server certificate, 128-bit random session ID, and 256-bit random capability from `crypto/rand`. The invite binds these to the endpoint, version 2, prefix `dropmesh:v2:`, ALPN `dropmesh/2`, and a 15-minute expiry. It is a bearer credential and must be shared through a trusted channel. Version 1 is not silently accepted.

Receiver parses a bounded invite, checks structure/version/expiry, builds a private root pool from its embedded certificate, and connects using TLS 1.3 with expected DNS name `dropmesh.local` and ALPN `dropmesh/2`. No general certificate validation is skipped. The receiver proves capability possession in the first control message; sender compares 32 decoded bytes in constant time before sending filename, size, or hash. The first valid receiver consumes the invite; the listener then stops new admissions while preserving the established QUIC connection. Cap failed attempts at eight; allow at most four concurrent pre-auth sessions. Authentication refusal uses a generic QUIC application error and exposes no file metadata.

Authentication is not short-code PAKE, durable identity, or trusted-device pairing. Threats include invite theft, endpoint compromise, malicious TLS implementation, and resource exhaustion. Tests must cover tampered root/cert, expiry, wrong capability, duplicate admission, concurrent admission, malformed encodings, and secret redaction. An independent crypto review remains required before release.
