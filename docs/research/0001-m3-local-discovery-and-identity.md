# M3 local discovery and identity research

**Status:** externally sourced research, updated 2026-10-13. This narrows requirements; it does not select or approve an implementation.

## Repository context

DropMesh pins Go 1.27.2. M3's roadmap outcome is mDNS discovery, persistent identity, approved trusted-device pairing, and a QR experiment. M1's fresh invite remains the current authentication mechanism. The next milestone must not turn discovery metadata into an authentication credential.

## Source-backed facts

- RFC 6762 specifies mDNS over UDP port 5353 and multicast on the local link. RFC 6763 describes DNS-SD service instances using PTR, SRV, and TXT records. [RFC 6762](https://www.rfc-editor.org/info/rfc6762/), [RFC 6763](https://www.rfc-editor.org/info/rfc6763/)
- DNS-SD service advertising includes an SRV record for host/port and a TXT record; RFC 6763 permits the TXT record to be empty when no additional properties are needed. That supports data minimization without inventing a nonstandard service format. [RFC 6763](https://www.rfc-editor.org/info/rfc6763/)
- RFC 8882 describes how DNS-SD can expose offering and requesting identities, hostnames, network parameters, service types, instance names, and service properties. It notes that an on-link observer can identify services from multicast discovery traffic and recommends avoiding static/linkable identifiers for private services. [RFC 8882](https://www.rfc-editor.org/info/rfc8882/)
- Go's `crypto/ed25519` implements Ed25519 compatible with RFC 8032. This provides a candidate signing primitive in the pinned Go toolchain, but does not define private-key storage, backup, rotation, or recovery. [Go `crypto/ed25519`](https://go.dev/pkg/crypto/ed25519/)
- Windows DPAPI protects data using the user's logon context and normally expects decryption by that user on that computer. Apple Keychain Services provide encrypted storage for small user data, including keys and certificates. These platform mechanisms do not by themselves establish a portable Go key-store contract. [Microsoft CryptProtectData](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata), [Apple Keychain Services](https://developer.apple.com/documentation/security/keychain-services)
- RFC 9807 describes OPAQUE, a client-server augmented PAKE, and is an IRTF Informational RFC rather than an Internet Standards Track protocol. It is not a ready-made selection for equal peers or short-code pairing; a concrete library and threat model would still need review. [RFC 9807](https://www.rfc-editor.org/info/rfc9807/)
- Current Go DNS-SD candidates do not yet provide sufficient source evidence to select one: `grandcat/zeroconf` says its implementation has reliability bugs and does not yet meet all requirements; `joshuafuller/beacon` currently marks macOS/Windows integration testing as pending and IPv6 support on its roadmap; `brutella/dnssd` has platform-sensitive network dependencies that require native build and behavior checks. These are upstream self-descriptions/manifests, not independent audits. [grandcat/zeroconf](https://github.com/grandcat/zeroconf), [beacon](https://github.com/joshuafuller/beacon), [brutella/dnssd go.mod](https://github.com/brutella/dnssd/blob/master/go.mod)
- The current `go-keyring/keyring` project describes pure-Go backends for macOS Keychain, Windows Credential Manager, and Linux Secret Service, with no plaintext fallback. Its README says per-item user-presence protection is platform-dependent and ignored on Windows/Linux; it also says headless Linux without a Secret Service is unavailable. `zalando/go-keyring` documents using `/usr/bin/security` on macOS. These upstream statements identify candidates and caveats; they are not independent security audits or a key-storage decision. [go-keyring/keyring](https://github.com/go-keyring/keyring), [zalando/go-keyring](https://github.com/zalando/go-keyring)
- The freedesktop Secret Service API binds its session to the caller's D-Bus session-bus connection. Linux deployments without an active user session/service need explicit unavailable behavior; a silent plaintext-file fallback would contradict the M3 proposal. [Secret Service sessions](https://specifications.freedesktop.org/secret-service/latest/sessions.html)

## Design implications

1. Treat mDNS/DNS-SD results as unauthenticated network hints. Verify peer identity through an authenticated connection and explicit user-approved pairing.
2. Do not publish a stable device ID, account identifier, sender/receiver name, file metadata, invite capability, or reusable secret in discovery records. Prefer a short, user-initiated discovery window and ephemeral, non-semantic service labels. This still exposes service presence, timing, network address, and browsing interest to on-link observers.
3. Ed25519 is a plausible identity-signing candidate because the pinned Go toolchain includes a standard implementation. Keep the identity decision open until key creation, OS storage, recovery, rotation, and revoke/unpair behavior are specified for every supported platform.
4. Keep short-code PAKE as a separate product decision. If QR-based out-of-band key verification satisfies M3 onboarding, a PAKE may not be needed for that flow. Do not use a low-entropy code as a bearer secret or invent a cryptographic exchange.
5. Do not select a discovery/key-store dependency by README claims alone. Require pinned source review, parser/fuzz coverage, native Windows/macOS/Linux integration, cancellation/race tests, and clear behavior when a platform vault is unavailable.

## Unresolved before implementation

- Whether devices advertise only while the user opens a receive/discovery action, and the maximum advertisement lifetime.
- Exact service type, instance and host naming, TXT contents, address-family behavior, and interface-change lifecycle.
- QR direction, displayed identity information, mutual-consent steps, replay protection, trusted-device naming, revocation, and key rotation.
- Cross-platform private-key storage and recovery, especially Linux desktop behavior and later Android/iOS integration.
- Which maintained DNS-SD implementation meets service browsing/registration, IPv4/IPv6, conflict handling, interface selection, cancellation, and native Windows/macOS/Linux behavior under DropMesh's bounded-lifecycle requirements.
- Whether DropMesh should require an OS vault for persistent identity or define a separate explicit encrypted backup/recovery workflow; no plaintext fallback is acceptable.
- Whether the target UX needs short codes in addition to QR; if so, the selected PAKE implementation needs a separate primary-source and library audit.
- Native Windows/macOS/Linux multicast behavior and permission/firewall UX.

## Minimum M3 verification gates

- Packet capture confirms the exact advertised fields and that no stable identity, transfer metadata, or invite secret is exposed.
- Spoofed or malformed discovery records never establish peer identity or bypass explicit consent.
- Pairing binds the same peer key at both endpoints, handles replay and duplicate identities, and supports visible unpair/revocation.
- Discovery stops on cancellation, interface loss, and application shutdown; native LAN tests cover supported operating systems and IPv4/IPv6 behavior.
- Key-store failure is explicit and does not silently replace an established identity.
