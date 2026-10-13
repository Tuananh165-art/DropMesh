# ADR-0007: M2 resumable transfer identity and journal

**Status:** accepted for M2 implementation. M2 milestone acceptance still requires its crash/recovery test matrix and does not imply M1 physical LAN acceptance.

## Context

M1 uses an ephemeral session ID and one-time bearer invite. Reusing that invite as a reconnect credential would weaken its one-receiver admission rule and require secrets or TLS keys to persist at rest. M2 also needs receiver state after a process restart. The initial Windows storage probe showed that `os.Rename` replacement is not documented as atomic across platforms and that directory sync is unavailable on the tested Windows/NTFS host.

## Decision

- Give each transfer a random stable transfer ID distinct from the per-run invite/session ID. The sender prints the transfer ID and accepts it explicitly on resumed runs.
- Create a new one-use invite for each sender process run. The receiver must approve each resume offer. Never persist invite capabilities, TLS private keys, or other reusable authentication secrets.
- Use the versioned v2 invite/control/ALPN identifiers and send a bounded receiver-committed chunk bitmap after consent. The bitmap currently represents a committed prefix because journal writes are append-only and chunks are committed in order.
- Persist only the receiver's descriptor, chunk commit journal, and staging bytes under receiver-owned rooted storage. Revalidate the offer against the saved descriptor and re-hash every journaled chunk during recovery.
- Commit each chunk by syncing its bytes before appending and syncing a CRC32C-protected journal record that includes the commit time. Expiry derives from the last structurally valid committed record, not the journal file modification time, which recovery may change while truncating a torn tail. Do not rely on replacing a mutable manifest for each chunk.
- Lock a transfer's state with an OS file lock so a second process cannot mutate it concurrently; process exit releases the OS lock.
- Expire incomplete state after 24 hours without a valid chunk. Allow explicit deletion by transfer ID. Keep at most one incomplete transfer in an output directory.
- M2 claims recovery after connection/process interruption. It makes no power-loss guarantee.

## Consequences

The receiver must be given a fresh invite after either process restarts, and the sender must retain the non-secret transfer ID and choose the same source file. Resume state can expose basename, size, digest, and partial bytes to users with access to the output directory. Corrupt or incomplete journal entries cause retransmission or transfer restart, never publication. Platform-native fault tests remain mandatory.
