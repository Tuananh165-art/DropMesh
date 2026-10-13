# Domain model

**Status: M2 implementation in progress and future proposals.**

- **Peer:** process participating in this one-time session.
- **Session:** random, ephemeral invite-scoped exchange; one receiver may authenticate. It is fresh on every send, including resume.
- **Invite:** endpoint, certificate, session ID, capability, version, expiry. It is a bearer secret.
- **Transfer:** one file offer and its verified receive operation; its stable random 128-bit ID differs from the ephemeral session ID.
- **Descriptor:** transfer ID, safe basename, size, fixed initial chunk size, SHA-256.
- **Chunk:** sequential offset/index, bounded length, content hash, bytes. M2 syncs receiver bytes and an append-only journal record before advertising a committed prefix.
- **Resume state:** receiver-owned descriptor, journal, and partial bytes rooted under the selected output directory; it expires after 24 hours idle and can be explicitly deleted.
- **Transport:** QUIC implementation hidden behind the internal adapter.
- **Route:** direct LAN in M1. Other route kinds are proposals only.
- **Device, identity, trust, candidate, relay, rendezvous:** future concepts; no durable representation in M1.
