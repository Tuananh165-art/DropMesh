# ADR-0002: QUIC for M1 native transfer

**Status:** accepted for M1 only.

## Context

M1 requires encrypted, bounded direct LAN transfer with a Go-native implementation. QUIC is not a NAT traversal service.

## Decision

Use pinned quic-go v0.63.0 over UDP with TLS 1.3/ALPN. Avoid claiming WAN reachability, migration, or performance without hardware/network experiments.

## Consequences

UDP/firewall policy can block the flow. A later TCP/WebRTC/relay path requires independent implementation and measured path-selection policy. Test supported operating systems.
