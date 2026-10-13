# Spec 0005 proposal: local discovery and trusted pairing

**Status:** proposal. No M3 wire values, identity format, or key-storage mechanism are accepted. See [M3 research](../docs/research/0001-m3-local-discovery-and-identity.md).

## Goal and boundary

M3 should let a user discover another DropMesh peer on a shared local network, select it, and explicitly approve a trusted-device pairing. Discovery locates candidate endpoints only. It does not authenticate a peer, authorize a transfer, or replace the M1 one-use invite until a separately reviewed design says so.

## Proposed invariants

- Discovery is available only during a user-visible receive/discovery action and stops on cancellation, interface loss, and shutdown.
- Discovery records contain only fields needed to locate the active service. They must not carry file names, sizes, content hashes, invite capabilities, account identifiers, stable device IDs, or reusable authentication secrets.
- A discovered address or advertised key is untrusted until a cryptographically authenticated pairing flow completes and both users approve the peer identity.
- Pairing and later transfer consent are separate decisions. Pairing must be visible, scoped, revocable, and resistant to replay and duplicate-key confusion.
- Persisted identity keys are never silently regenerated after key-store or data-corruption errors.
- QUIC types stay within `internal/transport/quic`; new wire values and record fields require an accepted spec and bounded parser tests.

## Decisions still open

- DNS-SD service type, instance/host labels, record contents, TTL, multicast interface policy, and IPv4/IPv6 behavior.
- Identity signing algorithm and the relationship between device identity, ephemeral transport keys, and M1 invite capabilities.
- QR pairing direction, user confirmation transcript, key verification display, device-name source, rotation, and revocation semantics.
- Native key storage and recovery for Windows, macOS, and Linux; later mobile integration.
- DNS-SD implementation selection after pinned source review, parser/fuzz review, native OS checks, and cancellation/race checks. Current candidate README claims are not implementation evidence.
- Explicit headless Linux behavior when no Secret Service is available; do not silently store identity keys as plaintext.
- Whether short human codes are a requirement. PAKE selection is out of scope until this is answered.
- Per-OS permission prompts, firewall behavior, and failure UX.

## Acceptance criteria before M3 implementation is complete

- The wire and state model has a reviewed, accepted spec, including privacy, consent, replay, revocation, and key-lifecycle behavior.
- Tests and packet captures prove the discovery data-minimization requirements.
- Native Windows, macOS, and Linux tests prove discovery and shutdown behavior on real LAN interfaces; loopback and cross-builds are insufficient.
- Spoofed service records cannot impersonate a paired peer or trigger file transfer without user action.
- Key creation, storage, recovery, rotation, and explicit failure behavior are tested for each supported desktop OS.
