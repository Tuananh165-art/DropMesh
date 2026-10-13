# Threat model

**Status:** M2 implementation with local Windows and hosted Windows/Linux/macOS test evidence; physical LAN and independent security review remain open. An internal code-path review was completed on 2026-10-13 and found no additional blocking finding; it does not satisfy the independent review gate. Scope is one sender and one receiver on a potentially hostile local network.

## Assets and adversaries

Assets: file bytes, basename/size/hash metadata, ephemeral certificate/private key, invite capability, endpoint IPs, receiver resume metadata and partial bytes, destination data, and relay capacity (no relay exists yet). Adversaries: passive/active LAN attacker, invite guesser, invite thief, malicious receiver, malicious sender, malformed-protocol peer, and local filesystem race. A compromised endpoint or OS is outside the protection claim.

## Security boundaries

The invite is a bearer credential. Anyone receiving its unexpired text can attempt to connect. It contains a certificate trust anchor, endpoint, one-use random 256-bit capability, and expiry. TLS 1.3 authenticates the sender against the invite-provided CA certificate; an authenticated control message proves capability possession before metadata is sent. The capability is not password-derived. This is transport authentication plus channel confidentiality; it is not a formally analyzed PAKE, persistent identity, or account system.

## Controls to verify

- TLS 1.3 and ALPN pinning; no 0-RTT; bounded pre-auth connections; one successful receiver; bounded failed attempts.
- Versioned bounded frames, deadlines, explicit states, typed error termination, and no unbounded queues.
- Validate the receiver descriptor and every chunk before writing; compare per-chunk and final digests.
- Restrict receiver names to portable basenames; use `os.Root`; stage with exclusive creation; publish by no-clobber rooted hard link; remove owned temporary output on handled failure.
- M2 binds receiver-owned resume state to the authenticated transfer descriptor, serializes local mutation with an OS file lock, syncs data before append-only journal commits, and re-hashes each committed chunk on recovery. CRC32C detects torn journal writes; it is not an authenticity mechanism.
- Bound size, streams, buffering, invites, control frames, and idle connections. Never log invite, keys, file contents, or full source paths.

## Residual risks and limitations

Invite theft gives one transfer attempt before expiry. LAN peers can observe IPs, timing, size, and filename after authentication. TLS does not protect against endpoint compromise or recipient copying. SHA-256 detects corruption; it does not prove sender intent or defend malicious content. Disk exhaustion is bounded by declared size but free disk space may be insufficient. A crash may leave receiver-owned resume data until it expires or is explicitly deleted. Resume journal recovery is not a power-loss guarantee; local users with access to the output directory can inspect or alter saved metadata and partial bytes. Hard-link publication may be unsupported on some filesystems and must fail closed. Losing the final acknowledgement leaves outcome uncertain. No relay, public-service abuse controls, forward secrecy claim beyond TLS properties, or formal crypto review exists. Invite certificate/profile parsing, root trust semantics, race behavior, limits, and clean-up are covered by local/hosted tests but still require independent review before release.
