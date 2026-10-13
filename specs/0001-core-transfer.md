# Spec 0001: M1 core transfer

**Status:** implemented and verified on Windows loopback; physical LAN acceptance remains open. **Updated:** 2026-10-10.

## Problem and goals

Transfer one regular file between two CLI processes on a LAN with no rendezvous dependency. Authenticate the intended receiver, encrypt content, bound resources, verify integrity, show progress, cancel cleanly, and never overwrite a received path. M1 is an engineering milestone, not general WAN P2P.

## Non-goals

No multiple files/folders, persisted resume, trusted devices, mDNS, short codes, rendezvous, NAT traversal, relay, route scoring, browser, desktop, mobile, or public SDK.

## UX and interfaces

`dropmesh send FILE [--listen ADDRESS] [--advertise IP]`; `dropmesh receive [--out DIRECTORY] [--yes] [--max-bytes BYTES]`; stdin invite requires `--invite-stdin --yes`; `dropmesh version`. The sender writes only the versioned invite to stdout; status, offer details, progress, and measured route facts go to stderr. Secrets are never command-line arguments. Exit 0 success, 1 operational failure, 2 usage, 130 cancellation. Receiver defaults to current directory and 100 GiB cap; it displays filename and size before prompted or unattended acceptance; confirmation defaults to no.

## Wire and states

Direct IPv4 UDP QUIC, TLS 1.3 only, ALPN `dropmesh/1`, 0-RTT disabled. One bidirectional stream carries length-prefixed JSON control messages (four-byte big-endian length, maximum 64 KiB). One unidirectional stream carries fixed chunk headers and bytes. Invite text is `dropmesh:v1:` followed by base64url JSON, at most 16 KiB.

Flow: `listen -> invite -> TLS -> capability auth -> offer -> accept/reject -> data chunks -> verify -> safe publish -> completion ack -> control-stream FIN exchange`. Invalid state/version/frame fails closed. One receiver is admitted. Sessions expire 15 minutes after invite generation; admitted transfers continue beyond expiry. At most four pending authentication connections, eight failed auth attempts, one active transfer, 10-second handshake deadline, 60-second inactivity limit. No automatic retries. QUIC limits each peer to one inbound bidirectional and one inbound unidirectional stream.

Offer descriptor: transfer ID/session ID, portable basename, unsigned size, fixed 1 MiB chunk size, whole-file SHA-256. Each chunk header is index (u64), offset (u64), length (u32), SHA-256 (32 bytes), then exactly `length` bytes. Chunks are sequential and all except the last are 1 MiB. Receiver checks lengths before allocation/write, hashes each chunk and full stream, requires exact byte count, then publishes. Unsupported transfer sizes above receiver cap fail before user acceptance.

## Failures, privacy, and compatibility

Reject wrong, expired, replayed, malformed, oversized, unsupported-version, and unauthorized sessions. Caller-context cancellation closes an established QUIC connection so pending stream reads and writes unblock. Preserve existing output on all failure paths. Temporary output is exclusively created in a rooted destination and deleted on handled error/cancel. Final publication uses rooted hard-link creation and refuses replacement. Unsupported hard-link filesystems fail closed. A lost completion acknowledgment is uncertain. M1 changes the protocol only through a new version; it has no negotiation downgrade.

Control metadata visible to receiver after authentication includes basename, size and digest. LAN observers can infer endpoints, timing and volume. No infrastructure participates. Never claim metadata secrecy from a network peer or protection against endpoint compromise.

## Acceptance

Unit/fuzz framing, invite, state, names, bounds; integration authentication, concurrent admission, integrity and cleanup; real-binary loopback E2E; physical LAN with Internet disabled; per-chunk and whole digest; no-clobber and cleanup; race tests. Local unit, integration, fuzz smoke, and loopback evidence are recorded in TODO. Hosted race and physical LAN checks remain open. Loopback is not LAN proof.
