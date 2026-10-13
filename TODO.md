# DropMesh TODO

## Current milestone

M2 — resumable single-file transfer. M1's offline two-device LAN and hosted acceptance gates remain independently open.

## Dependency queue and requirement-to-evidence map

| Card | Status | Outcome and acceptance evidence |
|---|---|---|
| DM-001 | Complete | M0 docs/ADRs/specs/skills/research, Go 1.27.2 bootstrap, local build baseline. Fresh archive extraction, format, build, vet, and tests pass. |
| DM-002 | Complete locally | TLS 1.3 invite-root trust + capability authentication, one-time receiver, bounded framing/state, four pending auth connections. Unit/fuzz and loopback checks cover invite bounds/expiry, wrong secret, certificate substitution, concurrent/replayed admission, four-slot saturation, and malformed frames. Linux-container race passes; hosted native Linux race gate remains unrun. |
| DM-003 | Complete locally | 1 MiB chunks, exact sequence/length, per-chunk/file digest, source mutation rejection, rooted temporary file and no-clobber publish. Tests cover binary/empty/Unicode-name transfers, corrupt/short/oversized/out-of-order/trailing chunks, names, collisions, symlink containment, sync failure, and handled-error cleanup. Slow-peer lifecycle coverage passes locally; physical-device disk-full/write-fault behavior remains unverified. |
| DM-004 | Complete locally | CLI and real-binary E2E on Windows loopback. Covers binary invite exchange, acceptance, saved digest, progress/status and exit codes; integration tests cover cancellation cleanup, cancellation while a peer is idle, incomplete-auth peer timeout and slot release, slow-receiver flow control, size-limit rejection, destination write-error classification, partial-chunk disconnect cleanup, and clean final QUIC close ordering. Physical LAN acceptance remains. |
| DM-005 | Partial; external gates open | Fresh Windows tests, race, vet, formatting, five parser fuzz smokes, five-target cross-builds, and transfer/resume benchmarks pass. On 2026-10-13, the configured Linux quality commands passed in Go 1.27.2 Docker: race, govulncheck, all five 10-second parser fuzz smokes, and both benchmark smokes; full tests and vet also passed on the updated graph. Results and host limits are in `docs/experiments/0004-linux-container-quality.md`. The Staticcheck job now lowers the Go directive only in its isolated checkout, tidies there, installs v0.8.1 separately, and runs it; this exact job passed in a Go 1.26.9 Linux container with no findings. Refreshed govulncheck reports no vulnerable symbols or imported packages; the only remaining module advisory is uncalled `golang.org/x/crypto/openpgp` (GO-2026-5932). To remove five module-only HTTP/2 advisories, `x/net` was upgraded to v0.60.0; tidy raised transitive `x/crypto` to v0.57.0 and `x/sys` to v0.48.0. Hosted CI and two-device offline LAN acceptance remain open. |
| DM-006 | In progress | M2 v2 invite/control/ALPN, stable transfer ID, fresh invites, receiver consent, bounded committed-prefix bitmap, rooted append-only journal with commit timestamps, per-output OS lock, per-chunk sync/recovery hashing, accurate 24-hour expiry, deletion CLI, mismatch reset, no-clobber publish, and QUIC plus real CLI process-restart/resume are implemented. Native Windows full/race tests, vet, formatting, five parser fuzz smokes, and five target cross-builds pass; cached Go 1.26.8 full tests pass. Go 1.27.2 Linux-container full tests, race, vet, all five configured 10-second parser fuzz smokes, and resume subprocess tests pass on Docker overlay storage. Durable chunk commit measured 200.05 MB/s in the latest local 3-iteration sample and 118.79 MB/s in a one-iteration Linux-container smoke; both are host-specific. Injected data/journal short writes, sync failures, simulated disk-full, corrupt tails/bytes, stale state, malformed duplicate/descending/missing chunks, and abrupt process-exit cases pass on Windows; the process-exit suite also passes in the Linux container. On 2026-10-13, descriptor validation was moved before terminal display and a QUIC regression test proves a control-character filename is rejected without status output; sender resume-ID validation now precedes full-source hashing. Fresh Windows full tests, race, and vet pass after those changes and the x/net security update. Linux evidence is in `docs/experiments/0003-m2-process-exit-recovery.md` and `docs/experiments/0004-linux-container-quality.md`. Remaining: hosted/native crash/fault runs on macOS and Linux; independent protocol/threat review; and final acceptance matrix review. No power-loss claim. M1 physical/hosted acceptance stays open. |
| DM-007 | Proposal prepared; implementation pending | M3 local discovery and pairing proposal plus current primary-source research are in `specs/0005-local-discovery-proposal.md` and `docs/research/0001-m3-local-discovery-and-identity.md`. mDNS is explicitly candidate discovery, not identity. No wire values, dependency, or identity/key-store choices are accepted. Resolve consent, record privacy, QR flow, DNS-SD library/native gaps, OS key storage/recovery, headless Linux behavior, and any short-code requirement; begin implementation only after M2 predecessor acceptance and accepted design. |

## Verification scenarios required by M1

| Requirement | Automated scenario | Current evidence / gap |
|---|---|---|
| Empty, binary, Unicode file names | Real QUIC E2E + filesystem tests | Binary multi-chunk CLI E2E and empty/Unicode-name QUIC tests pass locally. |
| Wrong/expired credential, replay, cert substitution | Auth and loopback integration tests | Covered locally. |
| Concurrent receivers and four-pending limit | Concurrent loopback clients | Exactly-one admission and fifth-pending rejection tests pass locally. |
| Corruption, truncation, oversized, source mutation | Chunk reader and sender/receiver tests | Covered locally; v2 only advertises synced chunks and retains noncommitted receiver state for retransmission after disconnect. |
| Destination collision, symlink, unsafe names | Rooted filesystem tests | Collision refusal, symlink preservation, unsafe-name, and sync-failure tests pass locally; destination writer errors are classified as filesystem failures; physical disk-full/write fault remains. |
| Slow peers, cancellation, socket/goroutine cleanup | Context-driven loopback scenarios + race detector | Four incomplete-auth peers close at the 10-second deadline and a valid receiver proceeds; an unread QUIC data stream backpressures the sender until caller cancellation unblocks it; cancellation while either peer is idle and partial-chunk cleanup pass. Hosted native Linux race remains. |
| Cross-platform compile/runtime | Native matrix and five cross-builds | Cross-builds pass locally; native hosted matrix pending. |
| Fuzz/benchmark/security tools | CI-pinned fuzz, bench, staticcheck, govulncheck | Five parser fuzz smokes and the benchmark pass locally and in Linux-container checks. Staticcheck v0.8.1 and refreshed govulncheck v1.8.0 pass in Linux containers. The scanner finds no vulnerable symbols/imported packages and one uncalled, unfixable OpenPGP module advisory. |
| Real LAN with Internet disabled | Two physical devices and matching digests | Blocked on external hardware/network availability; no LAN claim. |

## Verification scenarios required by M2

| Requirement | Automated scenario | Current evidence / gap |
|---|---|---|
| Fresh invite and stable transfer identity | Real QUIC/CLI sends with new session and same transfer ID | Windows API-level QUIC and child-process CLI restart tests pass. |
| Resume bitmap and chunk ordering | Bounded bitmap tests plus malformed stream cases | Prefix length, high-bit, duplicate, descending, already-committed, corrupt, and missing-index rejection tests pass. |
| Chunk durability and recovery | Short write, disk-full, sync failure, record corruption, and restart tests | Injected write/sync/disk-full and journal/data corruption cases pass on Windows. |
| Process interruption points | Child exits around write/sync/journal/publication | Windows subprocess matrix and Linux Docker-overlay subprocess tests pass; hosted/native Linux and macOS acceptance remain open. Not power-loss evidence. |
| Expiry, deletion, descriptor binding, no-clobber | Rooted state and publication tests | Expiry, explicit delete, mismatch, symlink, collision, matching destination, and empty-file cases pass locally. |
| Performance and static/security checks | Durable commit benchmark, fuzz, vet, staticcheck, govulncheck | 200.05 MB/s local durable commit sample; Windows/Linux-container fuzz and vet pass. Pinned Staticcheck passes; refreshed govulncheck reports only the uncalled OpenPGP module advisory. |

## Blocked / external gates

- M1 acceptance requires two available devices on a LAN with Internet unavailable, matching digests, cancellation, and bounded-resource evidence.
- Hosted CI evidence requires the configured GitHub workflow to run in a repository host.
- Public release requires choosing a non-conflicting name, configuring private security and conduct-enforcement contacts, and reviewing release readiness.

## Completed decisions

- [x] M0 + M1 scope, English docs, Apache-2.0, provisional DropMesh name, local module path, developer/operator audience, 256-bit invite capability, no public SDK.
- [x] DM-001 through DM-004 code/documentation and local Windows loopback evidence, subject to remaining scenarios above.
