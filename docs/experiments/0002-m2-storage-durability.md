# M2 storage durability probe

**Status:** experiment, preliminary. This probe checks file and directory operations on one Windows volume; it is not a power-loss or multi-platform durability result.

## Question

Can M2 rely on syncing a manifest file and replacing it with `os.Rename` as a cross-platform atomic commit for verified chunk state?

## Hypothesis

File sync may persist file contents, but replacing a manifest and syncing its parent directory may have platform-specific limits. M2 must not infer crash safety from a process restart alone.

## Environment

- Windows/amd64, Go 1.27.2
- `F:\DropMesh` on NTFS
- Local disposable files under `.cache`

## Method

Run `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/experiments/m2-storage-probe.ps1`. It writes and syncs an old and replacement file, replaces the old file with `os.Rename`, reads the new contents, and attempts to sync the parent directory. The probe removes only its generated source file and uniquely named empty test directory.

## Results

- Replacing an existing manifest with `os.Rename` succeeded and the new contents were readable.
- Opening the directory succeeded, but `File.Sync` on the directory returned `Access is denied` on this Windows/NTFS host.
- Go's `os.Rename` documentation says replacement is not atomic on non-Unix platforms.

## Interpretation and limits

The result does not establish what survives abrupt process termination, power loss, storage-controller caches, or other Windows/macOS/Linux filesystems. A replaceable-manifest design cannot be called cross-platform crash-safe from this probe. M2 needs a journal or platform-specific durable replacement strategy, with fault injection at every data/manifest boundary and native filesystem tests.

## Decision

Keep M2 storage unimplemented. Before implementation, compare append-only journal and platform-specific replacement designs, define resume authorization/lifetime and state deletion, then run process-crash and filesystem fault-injection tests. Revalidate every claimed chunk after restart regardless of manifest state.
