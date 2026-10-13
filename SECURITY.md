# Security policy

DropMesh is pre-release. No release or supported-version policy exists yet. Do not use it for sensitive transfers without independently reviewing the code and deployment environment.

Please do not publish an exploitable vulnerability or a real invite credential in a public issue. The maintainers have not configured a private security contact; `SECURITY_CONTACT_REQUIRED` is a release blocker. Once configured, report the affected revision, impact, and a minimal reproduction through that private route.

The M1 trust boundary and known assumptions are documented in `docs/THREAT_MODEL.md`. Dependency updates require review of release notes, supported Go version, vulnerability reports, and protocol compatibility. Never relax peer authentication to make tests pass.
