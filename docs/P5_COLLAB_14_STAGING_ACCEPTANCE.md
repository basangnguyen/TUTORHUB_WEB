# P5-COLLAB-14 performance acceptance

Status: **DONE**

Date: 2026-08-23

## Candidate scope

- Exercise the real authorized Hocuspocus WebSocket path and canonical Y.Doc authority with exact
  `2 x 500`, `10 x 500` and `50 x 2,000` profiles.
- Record join, input/update convergence, recovery join, snapshot encode, CPU, heap delta, received
  network bytes and cleanup-zero evidence against published local private-alpha budgets.
- Exercise the production runtime policies at the exact 50-connection/2,000-shape cap: checkpoint
  compaction, noisy-tenant operation quota, per-socket ingress backpressure and session cleanup.
- Build the production web bundle and fail if the Excalidraw engine enters the initial static import
  closure or exceeds the published raw/gzip budgets.
- This is a local deterministic performance gate. It does not add a migration, load any `.env*.local`
  file, connect Neon/B2/Render, write shared staging or deploy. Production whiteboard remains force-off.

## Published budgets

| Profile | Join p95 | Convergence p95 | Input p95 | Recovery | Snapshot encode | CPU | Heap delta | Received | Cleanup |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 x 500 | 3,000 ms | 1,500 ms | 750 ms | 3,000 ms | 1,500 ms | 3,000 ms | 128 MiB | 4 MiB | 2,000 ms |
| 10 x 500 | 7,500 ms | 2,500 ms | 1,000 ms | 7,500 ms | 2,500 ms | 8,000 ms | 320 MiB | 16 MiB | 3,000 ms |
| 50 x 2,000 | 20,000 ms | 5,000 ms | 2,000 ms | 20,000 ms | 5,000 ms | 75,000 ms | 1,024 MiB | 128 MiB | 5,000 ms |

Bundle budgets are: initial entry raw `<= 768 KiB`, lazy whiteboard root raw `<= 800 KiB`, static
whiteboard closure raw `<= 6 MiB` and the same closure gzip `<= 2 MiB`.

## Observed exact profile

Run on the local candidate with Node.js 24.15.0:

| Profile | Join p95 | Convergence p95 | Input p95 | Recovery | Snapshot bytes / encode | CPU | Heap delta | Received | Cleanup / final |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 x 500 | 39.9 ms | 140.4 ms | 21.9 ms | 55.5 ms | 307,625 / 2.1 ms | 782 ms | 8,317,232 B | 616,619 B | 23.4 ms / 0 |
| 10 x 500 | 62.9 ms | 149.7 ms | 23.2 ms | 46.9 ms | 307,625 / 1.7 ms | 1,109 ms | 29,987,936 B | 3,080,684 B | 22.6 ms / 0 |
| 50 x 2,000 | 1,074.8 ms | 897.5 ms | 77.9 ms | 123.8 ms | 1,579,667 / 8.7 ms | 28,687 ms | 255,498,024 B | 62,197,063 B | 25.0 ms / 0 |

All three profiles converged to their expected canonical semantic hash. The recovery measurement destroys
one authorized client, waits for server cleanup, exchanges a fresh one-time grant and requires the rejoined
authority to recover the exact final semantic hash.

## Runtime and bundle evidence

- Ten consecutive compaction passes over the production 2,000-shape authority retained the exact semantic
  hash. Encoded and compacted state were both `519,185` bytes; compaction p95 was `16.0 ms`.
- Fifty production runtime reservations reached exactly one active document scope and 50 active sessions;
  releasing all reservations left `0` active sessions and `0` active document scopes.
- The noisy tenant exhausted its operation quota while the quiet tenant remained accepted. A noisy socket
  exhausted its ingress message budget while a quiet socket remained accepted.
- Production build transformed `4,466` modules. The initial entry was `485,826` raw bytes. The lazy
  whiteboard root was `673,672` raw bytes; its 11-chunk static closure was `2,253,775` raw bytes and
  `660,631` gzip bytes. The initial entry did not statically import that closure.

## Commands and current result

```text
pnpm test:collaboration:p514
pnpm --filter @tutorhub/whiteboard-spike typecheck
pnpm --filter @tutorhub/whiteboard-runtime typecheck
```

Current result:

- Real WebSocket profiles: **3/3 PASS**.
- Production runtime compaction/backpressure/cleanup soak: **1/1 PASS**.
- Bundle guard regression: **2/2 PASS**; production bundle budgets: **PASS**.
- Focused TypeScript, ESLint and Prettier checks: **PASS**.
- Full repository `pnpm verify`: **PASS**, including format, generated OpenAPI check, local/E2E
  infrastructure, security, lint, typecheck, all package tests/builds, Storybook, client-bundle security,
  Go tests and `go vet`.

## Current decision

The exact private-alpha ceiling of **50 concurrent collaboration connections and 2,000 shapes per
document remains accepted**; current evidence does not require publishing a lower cap. This is a bounded
single-instance private-alpha profile, not a public production SLA or an HA/multi-region claim.

Full repository verification and final diff/no-secret review pass. Exact candidate `e1b1201` was pushed
to `origin/main`; GitHub Verify `32621951917` and Security `32621951910` both passed. P5-COLLAB-14 is
therefore `DONE`; P5-COLLAB-15 is the next runnable task.
