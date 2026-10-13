# Experiment 0003: M2 process-exit recovery on Windows

**Status:** experiment; local Windows/NTFS evidence only. This does not establish power-loss durability or cross-platform behavior.

## Question

After abrupt endpoint-process exit at representative receiver journal boundaries, does M2 recover only state whose bytes validate against the saved descriptor, and can it publish the exact original bytes without replacing an existing path?

## Hypothesis

Unjournaled bytes are retransmitted. A complete valid journal record may be recovered after process exit even if the process did not call journal `Sync`; every recovered chunk is re-read and hashed. A published file with retained state is recognized idempotently and state cleanup completes on the next process run. Expiry follows the record's commit time even if later recovery truncates the journal.

## Environment

- Windows/amd64, NTFS, Go 1.27.2.
- The repository-local Go cache and temporary directory were used because the user-profile build cache is inaccessible in this environment.
- Journal format version 2 includes the commit timestamp in each CRC32C-protected record.

## Method

`TestRecoveryAfterAbruptProcessExit` starts a child copy of the test executable. The child opens the real rooted store and exits with `os.Exit(0)` without running deferred cleanup at each point below. The parent then reopens the same output directory, recovers the state, verifies the file, and publishes it.

Command:

```powershell
$env:GOCACHE = (Resolve-Path .cache/gocache).Path
$env:GOTMPDIR = (Resolve-Path .cache/gotmp).Path
go test ./internal/resume -run TestRecoveryAfterAbruptProcessExit -count=1 -v
```

## Results

| Process exit point | Recovered committed chunks | Outcome |
|---|---:|---|
| After data write, before data sync | 0 | Parent retransmitted, verified, and published. |
| After data sync, before journal record | 0 | Parent retransmitted, verified, and published. |
| After full journal record write, before journal sync | 1 | Parent re-hashed, verified, and published. This reflects process-exit/kernel behavior only. |
| After journal sync | 1 | Parent re-hashed, verified, and published. |
| After final hard link, before state cleanup/acknowledgment | 1 | Parent verified the matching destination, removed state, and completed successfully. |

All five subtests passed. Separate injected-fault tests pass for short data/journal writes, data/journal sync errors, simulated no-space-on-device errors, corrupt journal tails, and altered committed bytes. Journal commit timestamps also preserve the 24-hour expiry boundary when recovery truncates a torn tail. The latest durable chunk commit benchmark measured 5.24 ms/op (200.05 MB/s) for three iterations on this host.

## Interpretation and decision

The process-restart ordering works for these Windows/NTFS cases. Recovery trusts only fixed-size CRC32C-valid records whose referenced data re-hashes correctly; CRC32C is a torn-write check, not authentication. An unsynced but complete record remained visible after ordinary process exit, which must not be generalized to power loss or storage-controller failure.

M2 remains unaccepted until hosted/native macOS and Linux crash/fault acceptance, physical device fault behavior, and independent protocol/threat review pass. The resume-stream rejection matrix passes in Windows and Linux-container regression runs (`TestReceiveResumeChunksRejectsMissingDuplicateDescendingAndCorruptChunks`); the Linux container evidence and its limits are recorded in `docs/experiments/0004-linux-container-quality.md`. Process termination and cross-compilation are not substitutes for power-loss or native macOS evidence.
