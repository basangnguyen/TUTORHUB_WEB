# P5-COLLAB-11 credential, revoke and WebSocket abuse acceptance

Status: **VERIFY**

Date: 2026-08-23

## Candidate scope

- One-time collaboration credentials remain digest-only, bound to the exact Origin and opaque provider
  document, and hard-limited to a maximum TTL of 60 seconds.
- The Core API grant broker proves one winner under concurrent consume, consumes failed binding attempts,
  rejects replay/expiry/forged leases and leaves no reusable lease after a consume/revoke race.
- Runtime authentication now revalidates the exact authority lease after grant exchange and before session
  reservation. A revoke or control-plane outage in that window therefore fails closed without emitting an
  accepted connection.
- `view` capability remains enforced in the data plane: a reader mutation does not reach another client or
  a durable checkpoint.
- Raw WebSocket frame/unauthenticated queue, awareness, update, document, connection, reconnect and tenant
  operation limits are exercised together. Deterministic malformed-frame fuzz and cumulative CRDT ingress
  amplification return bounded codes and retain the last accepted budget.
- No schema migration, Neon disposable branch, Backblaze B2 credential, shared-staging write or deployment
  is required for this test slice. Production whiteboard remains force-off.

## Local gate

Run without loading any `.env*.local` file:

```text
pnpm test:collaboration:p511
```

The aggregate runs focused Go broker/service tests and the Hocuspocus runtime authority/ingress suites with
the Go cache kept inside the workspace. It must prove:

1. one-time TTL/origin/document binding, replay/expiry and revoke-generation denial;
2. consume/revoke race and concurrent consume remain single-winner and fail closed;
3. revoke during handshake and control-plane partial outage never reserve a connection;
4. reader direct mutation cannot modify authoritative/durable state;
5. frame, payload, update, document, connection, rate and reconnect limits fail with bounded reasons;
6. malformed awareness fuzz and cumulative CRDT amplification do not panic, echo private input or exceed
   the configured cap.

Local result (2026-08-23): Go credential/revoke broker suite PASS; runtime focused aggregate PASS
`62/62` across five files. Repository-wide `pnpm verify` also PASS with format/OpenAPI/security `52/52`,
lint, typecheck, web/runtime tests and builds, Storybook, bundle security, Go test and vet green. Final
diff and scoped no-secret review also PASS; the only URL-shaped credential match is the intentional
`unused:unused@127.0.0.1` test fixture.

## Closure gates

- [x] Add post-exchange/pre-admission exact lease revalidation.
- [x] Prove revoke-during-handshake, control-plane outage and reader enforcement fail closed.
- [x] Prove broker race/replay/TTL plus WebSocket malformed/fuzz/amplification bounds.
- [x] Run full `pnpm verify`.
- [x] Complete final diff and no-secret review.
- [ ] Stage/commit/push only after explicit authorization and verify GitHub Verify/Security.
- [ ] Mark P5-COLLAB-11 `DONE`; do not migrate shared staging or deploy for this test slice.
