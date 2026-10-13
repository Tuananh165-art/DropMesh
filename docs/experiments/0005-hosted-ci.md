# Experiment 0005: First hosted CI run

**Status: experiment.** Run on 2026-10-13 against commit `c26c424ca7f9579b5409afff87691ccc9996e74e` in the public `Tuananh165-art/DropMesh` repository.

## Question

Does the configured native-platform and Linux quality workflow pass on hosted runners?

## Method

Triggered by the initial push to `main`; inspected GitHub Actions run [38344069584](https://github.com/Tuananh165-art/DropMesh/actions/runs/38344069584) and its job summaries.

## Results

- Native `ubuntu-latest`: format, vet, and full tests passed.
- Native `macos-latest`: format, vet, and full tests passed, including subprocess process-exit recovery cases.
- `windows-latest`: setup passed; the format check failed before vet and tests. The check uses `gofmt -l`; local reproduction with a CRLF Go file made `gofmt -l` report the file.
- All five configured cross-build targets passed.
- Staticcheck job passed.
- Linux race detector and govulncheck passed; all five 10-second fuzz smokes and both benchmark smokes completed successfully.

## Interpretation and action

Windows Git checkout line-ending conversion can make otherwise formatted Go sources fail the workflow's format gate. Added `.gitattributes` with `*.go text eol=lf`; a follow-up hosted run is needed to verify Windows CI and its native test suite.

These hosted process-exit tests do not establish power-loss durability. Physical offline-LAN acceptance and independent protocol/threat-model review remain open.
