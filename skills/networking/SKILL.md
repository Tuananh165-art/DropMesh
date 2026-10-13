# DropMesh networking

M1 is one direct IPv4 LAN QUIC session with TLS 1.3 and ALPN `dropmesh/1`. There is no signaling server, NAT traversal, relay, or seamless migration. Bound control frames, streams, pre-auth concurrency, timeouts, and data buffers. Model handshake/offer/accept/data/verify/complete order and fail closed on invalid states. Separate established route/transport facts from inferred NAT or packet statistics. Add deterministic loopback failure tests and separately report physical network tests.
