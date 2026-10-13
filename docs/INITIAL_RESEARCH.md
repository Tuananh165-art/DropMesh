# Initial research disposition

**Status:** architecture-level answers; each implementation claim requires the referenced versioned spec and test. Competitor observations are summarized in `COMPETITIVE_ANALYSIS.md`.

| # | Question and current disposition | Experiment / decision gate |
|---|---|---|
| 1 | Differentiate from LocalSend through measured large-file recovery/diagnostics; not proven. | User interviews and same-network comparison. |
| 2 | PairDrop already targets browser/WebRTC; do not claim cross-device novelty. | Browser feasibility experiment before M7. |
| 3 | croc already has PAKE, direct/relay, cross-platform/browser, and resume. | Compare actual reliability and operations before positioning. |
| 4 | QUIC supplies TLS 1.3, stream multiplexing, and congestion control; UDP reachability is not guaranteed. | Compare TCP/QUIC setup and throughput on Windows/macOS/Linux LAN. |
| 5 | WebRTC is relevant for browser DataChannel/ICE interop, not required for native LAN CLI. | M7 browser prototype and signaling threat model. |
| 6 | Pion exposes Go WebRTC and broad target support; integration suitability is unproven. | Build a minimal interoperable browser prototype. |
| 7 | Reuse mature ICE/STUN/TURN implementations if M4/M7 needs them; never recreate ICE. | Compare Pion integration and operational maintenance at M4. |
| 8 | TURN suits standardized ICE clients; custom relay may suit a narrow opaque stream protocol. | Measure deployment, quotas, abuse controls, client compatibility before M5. |
| 9 | Symmetric/endpoint-dependent NAT, CGNAT, blocked UDP, and firewall policy can prevent hole punching. | Multi-network carrier/hotspot lab at M4. |
| 10 | M1 uses a one-time 256-bit capability and invite-pinned TLS. Short human codes require vetted PAKE. | Threat model and library audit before short-code spec. |
| 11 | Ed25519 is a plausible future signing identity, but persistence/platform key storage is undecided. | Compare recovery, rotation, Android/iOS secure storage before M3. |
| 12 | M1 server sees peer address and authenticated file metadata; no infrastructure observes it. | Versioned metadata inventory before rendezvous. |
| 13 | M2 should store minimal manifest and verified-chunk state with explicit expiry and user-controlled deletion. | Crash/restart and stale-state tests before M2. |
| 14 | 1 MiB is only the M1 baseline. | Measure chunk sizes, hashing, allocations, disk and varied networks before changing. |
| 15 | Require safe relative paths, rooted writes, collision refusal, and reject links/special files. | Platform case/normalization corpus before folder transfers. |
| 16 | No-clobber is fixed for M1; collision rename/skip choices for batches remain open. | User research before multi-file UX. |
| 17 | Enforce a receiver byte cap, bounded streams/buffers, admission cap, and timeouts. | Disk-full/slow-peer load tests. |
| 18 | M1 pins protocol version and rejects unknown fields/messages; no downgrade negotiation. | Compatibility fixture tests before protocol v2. |
| 19 | Go core can serve CLI/desktop; browser requires WebRTC/JS and mobile may need native integration. | M7/M9 prototypes before framework choice. |
| 20 | quic-go v0.63.0 declares Go 1.26.0 and Go 1.27.2 is pinned for this build. | Build CI on the five requested targets. |
| 21 | QUIC migration is not claimed; path probing, socket rebinding and OS background limits matter. | Physical Wi-Fi-to-cellular tests only after mobile work starts. |
| 22 | TURN and custom relay have different interoperability and abuse/cost tradeoffs. | Security/operations comparison before M5. |
| 23 | Smart selection is premature until direct and relay paths provide reliable comparable metrics. | Gather path setup, throughput, failure, and cost measures in M4/M5. |
| 24 | LocalSend, PairDrop, croc, Syncthing, and Magic Wormhole provide diverse direct/relay precedents. | Primary-source references in competitive analysis; no performance claims. |
| 25 | A useful engine requires at least two real transports, observed outcomes, and deterministic initial policy. | Gate M6 on direct and relay evidence. |

## Additional gating experiments

Before M2, test crash-safe persistence. Before M3, test identity lifecycle and mDNS privacy. Before M4, test ICE behavior and relay-independent LAN operation. Before M5, test abuse controls, bandwidth caps, and privacy metadata. Before M7, test browser memory and writable-stream support. Before M8/M9, test platform lifecycle, background limits, and Go-core packaging. Before release, perform independent security review, real LAN tests, reproducible builds, and name/contact review.
