# Open questions

- Is developer/operator transfer reliability and diagnostics a differentiated user need? Validate with target users and comparisons.
- What replacement name and domain are available and legally appropriate? Resolve before publication.
- If short human codes are required, which vetted PAKE and code format meet usability and abuse constraints? QR/out-of-band verification may avoid a PAKE for M3; see `docs/research/0001-m3-local-discovery-and-identity.md`.
- Ed25519 is a candidate signing primitive, but which identity lifecycle and cross-platform key-storage/recovery model is appropriate remains undecided.
- Which DNS-SD/mDNS fields, advertisement windows, and UI consent flow meet the M3 privacy requirements? Discovery records are not authentication.
- Which DNS-SD implementation passes native IPv4/IPv6, service conflict, interface-change, cancellation, parser/fuzz, and race checks on supported desktop OSes? Current Go candidates have documented gaps; see the M3 research packet.
- How should M3 fail when a platform key vault is unavailable, especially on headless Linux? No plaintext fallback is allowed by the proposal.
- Which rendezvous and ICE strategy offers reliable consent and metadata minimization for later WAN work?
- Is standard TURN, Pion TURN, an existing relay, or a custom relay appropriate? Compare operational/security costs before M5.
- What chunk size, parallelism, and hashing strategy work across slow disks and LAN/WAN? Use benchmark and network experiments.
- What limits, TTLs, identity proofs, and quotas make public signaling/relay safe?
- Which folder path/collision rules are safe across Windows/macOS/Linux and case-insensitive filesystems?
- Does the current process-exit and injected-fault matrix justify the documented restart-recovery claim on all supported OSes? Hosted Windows/Linux/macOS test suites pass; independent review should assess coverage. No result establishes power-loss durability.
- Which browser streaming/write APIs provide useful large-file receive support?
- What native mobile framework can share Go behavior without brittle toolchain or lifecycle coupling?
- Which quic-go migration/probing APIs are stable in the selected release, and what succeeds on physical devices?
