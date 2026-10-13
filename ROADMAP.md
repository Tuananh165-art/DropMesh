# Roadmap

Milestones are capability-based; dates are intentionally omitted. A milestone is complete only when its exit criteria and test gates pass.

| Milestone | Outcome and exit criteria | Exclusions |
|---|---|---|
| M0 Foundation | **Implemented.** Specs, threat model, ADRs, contributor workflow, research, TODO, local build, and CI baseline. | Transfer feature claims. |
| M1 LAN Core Transfer | **Implemented; acceptance open.** Authenticated direct QUIC; one file; streaming; consent; integrity; cancellation; safe no-overwrite output; loopback evidence. Physical LAN, hosted CI, and remaining lifecycle/security gates must pass. | Resume, discovery, WAN. |
| M2 Resumable Transfer | **In progress.** Durable append-only journal, fresh-invite resume negotiation, restart after interruption, and full revalidation. Exit only after native crash/fault evidence and the full resume acceptance matrix. | Folder materialization. |
| M3 Local Discovery | Proposal prepared from current standards/privacy research; implementation pending accepted identity, key-storage, consent, and wire decisions. Target: mDNS, persistent identity, approved trusted-device pairing, QR experiment. | Internet routing. |
| M4 Internet P2P | Rendezvous, STUN/ICE research, candidate exchange, real multi-network direct tests. | Relay guarantee. |
| M5 Relay Fallback | Authenticated, bounded, encrypted-through relay with abuse controls and telemetry. | Route scoring. |
| M6 Smart Transport | Measured deterministic route policy; explain route changes; direct preference and fallback evidence. | Unsupported performance claims. |
| M7 Browser Receiver | Browser interoperability and storage constraints proven in supported browsers. | Native browser send assumption. |
| M8 Desktop | Cross-platform UI selected after product evidence. | Framework commitment before evaluation. |
| M9 Mobile | Native platform and Go-core reuse evaluated on Android/iOS. | Framework commitment before evaluation. |
| M10 Production Hardening | Abuse, privacy, scale, observability, release and compatibility controls. | Kubernetes/services without need. |
| M11 Public Release | Name conflict resolved, contacts configured, signed artifacts, checksums, SBOM, release process. | Unverified guarantees. |

M2 implementation may proceed against the locally verified M1 core while M1's physical and hosted acceptance gates remain open. M2 milestone acceptance still follows M1 and requires the decisions/evidence in `TODO.md` DM-006. Resume credentials are never reused across process runs.
