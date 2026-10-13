# Product

**Status: proposal.** DropMesh aims to move data directly between devices, keeping transfer data off infrastructure when direct connectivity works and using a disclosed, encrypted relay only if later evidence supports it.

## First user and job

The first audience is developers and operators who need to move a large file between machines without uploading it to cloud storage. M1 tests the low-level transfer core through a CLI. The user supplies a one-time invite over a trusted channel; both endpoints see progress and integrity outcome.

## Differentiation hypothesis

Direct transfer, relaying, resume, and browser clients already exist in croc and other tools. DropMesh will test whether explicit route diagnostics, understandable failure outcomes, resumability designed into protocol identifiers, and measured path selection form a useful differentiator. Do not present this as proven market advantage.

## Non-goals for M1

No folders, overwrite, cloud storage, accounts, directory discovery, device trust, short codes, NAT traversal, relay, browser, UI, clipboard, or resume. The working name conflicts with existing products and is not cleared for publication.

## Success

M1 succeeds when two LAN processes authenticate through the invite, accept/reject the offered single file, transfer with bounded memory, verify the whole-file digest, publish without replacing a path, report completion accurately, and clean up on handled cancellation. Real-device LAN acceptance remains a separate gate.
