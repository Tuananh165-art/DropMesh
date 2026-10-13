# DropMesh Go engineering

Keep packages small and based on the implemented seams listed in `AGENTS.md`. Pass `context.Context` from CLI to listener, connection, stream, and file loops. Every goroutine needs an owner, cancellation path, bounded communication, and join/termination evidence. Stream files with fixed buffers, propagate short writes/errors, wrap actionable error categories, and avoid hidden globals. Run focused tests, `go vet`, race tests after concurrent changes, then broader package checks. Add a benchmark before changing chunk or concurrency defaults.
