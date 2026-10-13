# Spec 0004: resumable one-file transfer

**Status:** accepted for M2 implementation. Hosted M1 CI acceptance passed on Windows/Linux/macOS in run 38344545090; physical offline-LAN acceptance remains open.

## Version 2 wire flow

M2 uses invite prefix `dropmesh:v2:`, invite version 2, control version 2, and ALPN `dropmesh/2`. Version 1 is not negotiated or silently accepted by a v2 implementation.

1. `authenticate` proves the fresh invite capability and session.
2. `offer` contains a stable transfer ID and the complete descriptor. The sender creates a random 128-bit ID initially or supplies the saved ID on resume.
3. After consent and state validation, receiver sends `accept` with a `resume_bitmap`. Bit `i` set means chunk `i` is durably journaled and revalidated. Bitmap length is exactly `ceil(chunk_count/8)`; unused high bits must be zero. At 100 GiB and 1 MiB chunks, the base64 JSON field remains below the 64 KiB control-frame limit.
4. Sender opens one unidirectional data stream and sends each missing chunk once, in ascending index order, using the v1 52-byte chunk header and bounded payload. An all-committed bitmap permits an empty data stream.
5. Receiver rejects chunks marked committed, duplicate or descending indexes, bad offsets/lengths, and missing indexes at stream EOF. Every validated chunk is data-synced before a CRC32C-protected journal record is appended and synced.
6. When all chunks are committed, receiver re-hashes the full file, verifies the descriptor digest, and publishes with the existing no-clobber hard-link operation. It removes state only after successful publication, then sends `complete`.

The receiver journal currently commits chunks in order, so the advertised bitmap must be one contiguous prefix followed by zero bits. Sender rejects absent/oversized bitmaps, nonzero unused high bits, and any set bit after the first missing chunk. A whole-file mismatch clears the commit journal and staging bytes so a subsequent authenticated run retransmits all chunks.

If the destination already exists and its complete digest equals the authenticated offer, receiver may complete idempotently without changing that file. A differing digest remains a no-overwrite collision.

## Receiver state

State is rooted at `<output>/.dropmesh-state/<transfer-id>/`, with fixed `data.partial` and `journal` names. The receiver-owned `.dropmesh-state/.active.lock` serializes state operations for that output directory, which is limited to one incomplete transfer. Transfer IDs must be exactly 32 lowercase hexadecimal characters. Sender names and paths never contribute to internal paths. An OS file lock prevents two receiver processes from mutating state concurrently; a process crash releases the lock.

Journal records are bounded, fixed-size records containing chunk index, offset, length, SHA-256, commit timestamp, and CRC32C. The local journal format is versioned independently from the wire protocol. Recovery scans the header and records in order, stops at the first malformed/torn record, re-hashes every recorded chunk, truncates the journal at the first invalid record, and advertises only the valid prefix's bitmap. Data bytes without a valid record are retransmitted. State older than 24 hours since the last valid chunk is removed when a receive begins. `dropmesh resume delete TRANSFER_ID --out DIRECTORY` removes a specific incomplete state under that output root.

## Goals

- Resume a single-file transfer after an interrupted connection.
- Preserve the M1 integrity requirement: verify every accepted chunk and the complete file before safe no-clobber publication.
- Recover receiver staging state after either endpoint process restarts, using a fresh one-use invite and the stable transfer ID.
- Keep metadata minimal, bounded, receiver-owned, expiring, and explicitly deletable.

## Required invariants

- Bind resume state to a random transfer ID and the complete descriptor (portable basename, size, chunk size, and whole-file digest).
- Treat every reconnect and chunk as untrusted input. A manifest or resume credential does not replace transport authentication, per-chunk hashing, or final full-file verification.
- Record a chunk as committed only after its bytes are written and synced. On recovery, re-read and hash every claimed chunk; discard or retransmit any uncertain or corrupt chunk.
- Never accept sender-controlled filesystem paths. Keep staging and state under receiver-owned rooted storage; preserve no-clobber publication and delete owned state on completion, explicit user deletion, or expiry.
- Version any reconnect messages or changed retry semantics. Reject unknown versions and do not silently downgrade.

## Acceptance review still open

- An independent protocol/threat reviewer must assess resume credential replay, theft, expiry, deletion, and whether the recorded tests support the stated restart-recovery claim.
- The internal matrix is recorded in `docs/acceptance/M2.md`; final maintainer signoff must preserve its distinction between process-exit recovery, power-loss durability, and M1's physical-LAN gate.

## Accepted M2 direction

- Separate the stable transfer ID from the ephemeral invite session ID. Generate a random transfer ID for the initial offer and display it to the sender; the sender supplies it explicitly on a resumed run.
- Generate a fresh one-use invite for every sender process run, including resume. The new authenticated offer must match the saved transfer ID, file size, chunk size, and full-file digest. Do not persist TLS private keys, invite capabilities, or reusable reconnect secrets.
- Require receiver consent again on a resumed process. If a saved state exists, validate its descriptor against the new authenticated offer before using it.
- This allows either process to restart while keeping the original M1 bearer-invite model. It requires resending the fresh invite over the trusted channel and explicitly preserving the transfer ID.
- Receiver state lives under a receiver-owned hidden directory inside the selected output directory. No sender path is stored or used to construct a path.
- Incomplete state expires 24 hours after the last valid chunk. Explicit deletion is available through `dropmesh resume delete TRANSFER_ID`. Limit each output directory to one resumable transfer so stale partial files cannot accumulate without bound.
- A chunk is committed only after data sync and an append-only, checksummed journal record is synced. Recovery validates every record and re-hashes each referenced chunk. A torn/corrupt journal tail is discarded; the expected full-file digest remains authoritative.
- M2 promises recovery after process or connection interruption, not survival of power loss or controller-cache failure. Native crash/fault tests must pass before claiming restart reliability.

## Required evidence before M2 acceptance

- Crash-consistency experiment with process termination between chunk write, data sync, state commit, recovery scan, and final publication.
- Fault injection for short writes, sync failure, state corruption, stale state, duplicate chunks, and disk exhaustion.
- Native hosted-OS test evidence on Windows, macOS, and Linux. Cross-compilation is not runtime evidence, and process termination is not power-loss evidence.
- Real CLI sender and receiver processes must be stopped and restarted with the same source, output directory, and transfer ID, using a newly generated invite; verify retransmission only of uncommitted chunks and exact final bytes.
- Threat-model and protocol review for resume credential replay, theft, expiry, and deletion.

The preliminary Windows storage probe is recorded in `docs/experiments/0002-m2-storage-durability.md`; subprocess restart outcomes are in `docs/experiments/0003-m2-process-exit-recovery.md`; hosted CI outcomes are in `docs/experiments/0005-hosted-ci.md`. The implementation uses an append-only journal rather than relying on replaceable-manifest atomicity. Local Windows and hosted Windows/Linux/macOS process-exit suites and injected storage-failure tests pass; they do not prove power-loss durability.
