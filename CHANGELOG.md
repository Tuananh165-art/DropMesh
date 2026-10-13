# Changelog

## Unreleased

- Establish M0 documentation and implement the M1 direct LAN CLI.
- Implement invite-authenticated QUIC sessions, bounded framing, chunk/file integrity, and rooted no-clobber receiver publication.
- Verify CLI transfer scenarios on Windows loopback, including binary, empty, Unicode-named, concurrent receiver, and source-mutation cases.
- Enforce and test the four-pending-auth limit; test size-limit refusal and cancellation cleanup.
- Upgrade `golang.org/x/crypto` to v0.56.0 after vulnerability scan findings.
- Start M2 protocol v2 with fresh one-use invites, stable transfer IDs, receiver consent on resume, bounded committed-chunk bitmaps, rooted append-only journaled state, explicit deletion, and process-restart recovery tests.
- Add Windows/Unix output-state locking, per-chunk data and journal sync, recovery re-hashing, and reset of unpublishable whole-file-mismatch data for safe retransmission.
- Verify the process-exit/fault test matrix on hosted Windows, Linux, and macOS; make no power-loss durability claim. Independent protocol/threat review remains required.
- No release artifacts or compatibility guarantees have been published.
