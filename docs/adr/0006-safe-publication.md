# ADR-0006: rooted staging and no-clobber publication

**Status:** accepted for M1.

## Context

The sender is untrusted and the receiver must prevent traversal, symlink escape, truncation, and overwrite.

## Decision

Require a portable basename. Stage inside `os.Root` using exclusive creation. After integrity verification and `Sync`, publish with `Root.Link` to the final basename and remove the temporary entry. Never replace an existing path or fall back when hard links fail.

## Consequences

Existing files are preserved. Some destination filesystems will be unsupported. A crash can leave a hidden partial file. Race and platform tests are required.
