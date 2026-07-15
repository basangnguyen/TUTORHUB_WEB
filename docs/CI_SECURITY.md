# CI/CD and Security Runbook

## 1. Scope

P1-08A establishes deterministic pull-request verification and the repository security baseline. It does not deploy the web application or Core API. Preview and staging deployment remain P1-08B and require the isolated P1-10 cloud resources first.

## 2. Required workflows

### Verify

`Quality and integration` runs on pull requests to `main`, pushes to `main` and manual dispatch. It provisions PostgreSQL 17 and executes:

1. installation from the committed pnpm lockfile;
2. local GitHub Actions policy validation;
3. classroom and identity integration tests against real PostgreSQL;
4. format, generated OpenAPI client, lint, typecheck, unit test, production build, Storybook build, client-bundle secret check, Go test and Go vet.

### Security

The workflow runs on pull requests, pushes to `main`, manual dispatch and every Monday:

| Check | Purpose | Blocking threshold |
| --- | --- | --- |
| `Secret scan` | Scan complete Git history with Gitleaks | Any verified secret finding |
| `Dependency review` | Inspect dependencies newly introduced by a pull request | New High/Critical advisory |
| `CodeQL (javascript-typescript)` | SAST for browser and TypeScript code | Code scanning policy in GitHub |
| `CodeQL (go)` | SAST for the Core API | Code scanning policy in GitHub |
| `Repository vulnerability scan` | Trivy filesystem dependency, secret and misconfiguration scan | Fixed High/Critical finding |
| `Core API container scan` | Build and scan the production Docker image | Fixed High/Critical finding |

CodeQL and SARIF uploads are not granted write access for untrusted fork pull requests. The workflow never uses `pull_request_target`.

## 3. Local commands

Run the same gates before opening a pull request:

```powershell
pnpm install --frozen-lockfile
pnpm security:test
pnpm security:actions
pnpm test:integration
pnpm verify
```

`pnpm security:bundle` expects `apps/web/dist` and is already executed after the production web build by `pnpm verify:web`.

## 4. GitHub Actions supply-chain policy

- Every external action is referenced by a complete 40-character commit SHA.
- The human-readable release is retained as an adjacent comment for review.
- Every checkout disables persisted Git credentials.
- Every job has a timeout and both workflows cancel superseded runs.
- Workflow-wide permission is `contents: read`; only CodeQL/SARIF jobs receive `security-events: write`.
- An action update must arrive through a reviewed Dependabot pull request or be verified against the publisher's official repository. Run `pnpm security:actions` after editing a workflow.

## 5. Repository settings checklist

These controls are configured in GitHub and cannot be proven by repository files alone:

### Actions

- Allow actions from GitHub and verified publishers required by the workflows.
- Require actions to be pinned to a full-length commit SHA when the repository setting is available.
- Keep workflow permissions at read-only by default and do not allow Actions to approve pull requests.

### Security and analysis

- Enable dependency graph, Dependabot alerts and Dependabot security updates.
- Enable code scanning, secret scanning, push protection and private vulnerability reporting when available for the repository plan.
- Review unresolved CodeQL, Gitleaks and Trivy findings before every release.

### Ruleset for `main`

- Require a pull request before merge and at least one approval.
- Require review from CODEOWNERS for protected paths.
- Dismiss stale approvals and require all conversations to be resolved.
- Require the Verify and Security checks listed above to pass.
- Block force pushes and branch deletion; require linear history.
- Permit bypass only for an explicitly documented emergency, followed by retrospective review.

Record a screenshot or exported ruleset after configuration. Until that evidence exists, branch protection remains a manual P1-08A follow-up rather than a verified automated control.

## 6. Triage and exceptions

1. Reproduce a finding on the exact commit and identify the owning module.
2. Prefer upgrading, removing or isolating the affected component.
3. A suppression must link to an issue, explain why the finding is not exploitable, name an owner and include an expiry date.
4. Never suppress a confirmed credential. Revoke and replace it, then remove it from all reachable history and artifacts.
5. A temporary CI outage may be bypassed only by the repository owner after recording the failed check, risk, approval and follow-up issue.

## 7. P1-08B boundary

P1-08B will add web preview deployment, Core API staging deployment, migration/rollback coordination, post-deploy health checks and deployment concurrency. It must not begin until P1-10 provides separate staging URLs, identities and secrets.
