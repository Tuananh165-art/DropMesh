# Glossary

- **Bearer capability:** random secret; possession grants the corresponding limited authentication attempt.
- **Control plane:** session and transfer negotiation messages.
- **Data plane:** file chunk bytes.
- **Direct:** endpoint-to-endpoint QUIC for the current M1 connection; it does not imply successful WAN traversal.
- **E2E:** data protection between intended endpoints; M1 uses TLS 1.3 with invite certificate pinning/trust and an authenticated capability. No infrastructure participates.
- **Invite:** expiring, one-use endpoint and authentication information.
- **PAKE:** password-authenticated key exchange, reserved for a later short-code design.
- **QUIC:** encrypted UDP transport used by M1. Connection migration is not an M1 guarantee.
- **Resume:** restart a transfer from verified persisted missing chunks; not implemented in M1.
- **Rendezvous / relay:** later services for endpoint exchange / forwarding encrypted traffic.
