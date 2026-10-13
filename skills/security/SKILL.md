# DropMesh security

Treat network bytes, invite text, sender names, and destination state as untrusted. Keep invite secrets and private keys out of args/logs. M1 uses a high-entropy one-time bearer invite plus TLS certificate trust and capability authentication; do not replace it with a short code or SHA-derived password. Preserve TLS verification, enforce version/frame/size limits, validate each chunk and the full file, and use rooted, exclusive, no-clobber publication. Never add overwrite fallback. Update the threat model when a trust boundary changes.
