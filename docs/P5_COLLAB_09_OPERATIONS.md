# P5-COLLAB-09 whiteboard operations runbook

Status: private-alpha candidate; production whiteboard remains force-off until P5-COLLAB-17.

## 1. Owners and accepted profile

- Primary on-call, security incident owner and cost owner: Bá Sáng.
- Backup on-call: Duy Mạnh.
- Profile: one Render instance, no HA/multi-region, cold start and short interruption accepted.
- Hard cost cap: `0 USD`. Never auto-upgrade or auto-scale to a paid plan. If an included quota is at
  risk, keep whiteboard force-off or move it to read-only.
- Accepted recovery objective: RPO no worse than the last verified durable artifact; operator-driven
  RTO target at most five minutes. Object Lock remains disabled for private alpha.

## 2. Safe states

| Mode        | Read/list | Export    | Edit/lifecycle                  | Snapshot/restore         | Public behavior               |
| ----------- | --------- | --------- | ------------------------------- | ------------------------ | ----------------------------- |
| `enabled`   | allowed   | allowed   | feature/capability gated        | feature/capability gated | normal                        |
| `read_only` | allowed   | allowed   | rejected; grant clamped to view | rejected                 | explicit unavailable mutation |
| `off`       | concealed | concealed | concealed                       | concealed                | HTTP 404                      |

The deployment feature guard remains off unless the P5-COLLAB-17 acceptance explicitly changes it.

## 3. Signals and alerts

Observe only bounded signals:

- `/livez`, `/readyz`, drain state and dependency readiness;
- connection outcomes by fixed capability/outcome;
- checkpoint outcomes and dirty-document count;
- policy rejection counts for fixed reasons, especially `connection_quota` and `operation_quota`;
- Render free instance hours, bandwidth, pipeline minutes, included services and unbilled charges;
- Neon compute/storage/branch quota and migration ledger health;
- B2 storage, download and Class B/C transaction caps.

Alert the primary owner when readiness fails twice consecutively, checkpoint persistence fails, quota
rejections rise continuously, any provider reaches 75% of an accepted free cap, or Render unbilled
charges become greater than zero. Do not put tenant/document/user/provider identifiers or content in an
alert label or message.

## 4. Emergency kill switch

1. Set `COLLABORATION_RUNTIME_MODE=read_only` on Core API and redeploy.
2. Verify document projection, snapshot listing and export work; verify lifecycle, snapshot creation,
   restore and edit grants fail closed.
3. If containment is still insufficient, set mode to `off`; verify public routes conceal with 404 and the
   runtime does not admit new connections.
4. Preserve PostgreSQL rows, current checkpoint and all verified B2 object versions. Do not run a down
   migration during the incident.
5. Rotate a compromised credential at the provider, update the secret consumer, redeploy, prove the old
   credential fails and the new one succeeds without printing either value.
6. Return to `enabled` only after Core API, control authority, PostgreSQL and B2 probes are healthy and a
   verified export can be read back. The feature deployment guard may still remain force-off.

## 5. Drill checklist

- Health/readiness: `/livez` remains process-only; `/readyz` fails on authority or persistence loss.
- Drain: start drain, reject authentication completing after drain begins, persist dirty checkpoints,
  close sockets and prove cleanup zero before replacement.
- Backup/restore: verify immutable artifact version/checksum/scope, stage next-generation checkpoint and
  atomically swap generation/revoke fence. Record RPO and RTO, never a credential.
- Secret rotation: create scoped replacement, update consumer, redeploy, verify boolean PASS/FAIL, then
  revoke old credential.
- Cost: capture plan, included usage/caps and unbilled-charge evidence; stop the candidate if hard cap
  could be exceeded.
- Outage: new rooms fail closed; an existing room follows the published reconnect/read-only behavior;
  recovery must not cross the accepted RPO.

Evidence already retained from P5-COLLAB-01/07/08 covers the provider 600-second outage, B2 rotation,
exact immutable artifact recovery and measured `RTO_MS=3096`. P5-COLLAB-09 adds focused feature/quota,
bounded telemetry and kill-switch tests. Its forward-only disposable Neon migration/quota drill passed at
final ledger `41 false`; the branch remains retained until candidate CI closes the task.
