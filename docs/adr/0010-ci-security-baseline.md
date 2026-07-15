# ADR-0010: CI and repository security baseline

- Status: Accepted
- Date: 2026-07-15
- Decision owners: TutorHub maintainers

## Context

TutorHub V2 has a web workspace, a Go API, PostgreSQL integration tests and a production container. The original `Verify` workflow ran quality checks but referenced mutable action tags, repeated only part of the integration suite and had no secret, dependency, SAST or container gate. Deployment resources are not yet isolated for staging, so combining repository hardening with deployment would either expose shared credentials or make the task unverifiable.

## Decision

P1-08 is divided into two deliverables:

- P1-08A establishes pull-request verification, security scanning, dependency automation and repository governance.
- P1-08B adds preview and staging deployment only after P1-10 provisions isolated cloud resources.

P1-08A uses two workflows:

- `Verify` runs deterministic format, contract, lint, type, unit, integration, build, Storybook, client-bundle and Go checks.
- `Security` runs Gitleaks, Dependency Review, CodeQL for JavaScript/TypeScript and Go, plus Trivy filesystem and container scans.

External actions are pinned to immutable full commit SHAs. Workflow permissions are read-only by default; only SARIF/code-scanning jobs receive `security-events: write`. Fork pull requests do not receive that write permission path. Local Node scripts enforce the workflow policy and detect server-only credential identifiers or high-confidence token formats in the browser bundle.

CODEOWNERS protects delivery, infrastructure, contracts, migrations and security files. Dependabot opens grouped weekly updates for pnpm, Go, Actions and Docker. Vulnerabilities are reported privately under `SECURITY.md`.

## Alternatives considered

### Use mutable major-version tags for Actions

This is simpler to maintain but allows upstream tag movement to change executable CI code without a TutorHub commit. Dependabot can maintain pinned SHAs, so the convenience does not justify the supply-chain risk.

### Use only a single scanner

One scanner cannot cover Git history secrets, dependency diffs, source data flow and the final container image with equivalent depth. Separate focused gates provide clearer ownership and failure diagnosis.

### Deploy in the same task

Rejected because staging OIDC, Neon roles, Cloudflare Pages and Hugging Face resources are P1-10 deliverables. A deployment workflow before those boundaries exist would normalize shared or manually copied secrets.

## Consequences

- Pull requests perform more work and consume more CI minutes.
- High/Critical findings and leaked credentials become release blockers.
- Action updates require reviewed SHA changes rather than silent tag movement.
- Branch rules and repository security switches still require one-time GitHub configuration and evidence.
- Preview/staging deployment remains intentionally incomplete until P1-08B.
