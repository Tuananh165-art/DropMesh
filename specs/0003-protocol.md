# Spec 0003: M1 version 1 control and chunk protocol

**Status:** historical v1 wire contract. Active M2 wire behavior is version 2 in spec 0004; a v2 implementation rejects v1 without downgrade.

## Control envelope

Each control record has a four-byte big-endian payload length followed by UTF-8 JSON. Maximum encoded JSON is 64 KiB; zero, oversized, truncated, trailing, or unknown-field messages are rejected. Each message carries protocol version 1 and a typed message name. Messages: `authenticate`, `offer`, `accept`, `reject`, `complete`. Session and descriptor values are checked against the active invite. Unknown types and invalid order close the session. Cancellation and operational errors terminate the active stream/connection with a QUIC cancellation or application error and are surfaced in a classified CLI error; they are not separate JSON records in v1.

## Stream and transfer rules

One bidirectional stream carries control. Sender opens one unidirectional stream only after receiver accepts. Each chunk has a 52-byte binary header: 8-byte index, 8-byte offset, 4-byte length, 32-byte SHA-256; all integers are unsigned big-endian. Payload bytes follow immediately. Index/offset start at zero; fixed chunk size is 1 MiB; last length is the exact remainder. Empty files have no chunk bytes but still require final SHA-256 of the empty input.

Receiver rejects sequence gaps/duplicates, arithmetic overflow, out-of-range or zero chunk lengths, incorrect digest, premature EOF, extra stream data, byte-count mismatch, and final digest mismatch. It creates a random same-directory temporary file before accepting and never writes sender-provided path components. After `Sync`, no-clobber publish, and temp removal, receiver sends `complete` and closes its control write side. Sender reports success only after receiving the acknowledgment and control-stream completion. If a remote QUIC application close with code zero overtakes delivery of the final stream FIN after the acknowledgment, it is treated as clean completion; nonzero application codes remain failures. Authentication refusal uses QUIC application code 1, consumed-invite refusal code 3, and invalid-session refusal code 2.

M2 can build durable resume on transfer/chunk identifiers, but changes to manifest, persistence, chunk parallelism, or reconnect semantics require a new spec/version. M1 has no resume and must restart after failure.
