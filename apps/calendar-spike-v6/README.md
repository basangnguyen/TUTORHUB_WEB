# FullCalendar v6 fallback comparator

This private package exists only to reproduce the P3-CAL-01 performance fallback
baseline with FullCalendar Standard `6.1.21`, React `19.2.7` and the same
500/1,000/2,000 fixture counts as the v7 spike. The Month density options are
kept at parity with v7: six visible rows/stacks, popover overflow, block event
display and deterministic `start,title` ordering.

It is not a production app, is not imported by `apps/web`, and must not be deployed.
The accepted renderer remains isolated behind the TutorHub calendar adapter.

From the repository root:

```powershell
corepack pnpm --filter @tutorhub/calendar-spike-v6 typecheck
corepack pnpm --filter @tutorhub/calendar-spike-v6 lint
corepack pnpm --filter @tutorhub/calendar-spike-v6 test
corepack pnpm --filter @tutorhub/calendar-spike-v6 build
corepack pnpm --filter @tutorhub/calendar-spike-v6 e2e
```

License notices used by the decision are recorded in
`apps/calendar-spike/THIRD_PARTY_NOTICES.md`.
