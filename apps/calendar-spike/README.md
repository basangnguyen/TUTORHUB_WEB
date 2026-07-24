# TutorHub Calendar spike

This is an isolated P3-CAL-01 technical spike. It is intentionally not wired to
`apps/web`, the Core API, authentication, or production routes.

The spike proves the renderer boundary, civil-time/DST behavior, optimistic
drag/resize revert, keyboard Agenda alternative, Warm Academic tokens and
dependency/license guard. It is not a production calendar or recurrence API.

From the repository root (after the root lockfile has been regenerated):

```powershell
corepack pnpm --filter @tutorhub/calendar-spike security:dependencies
corepack pnpm --filter @tutorhub/calendar-spike test:guard
corepack pnpm --filter @tutorhub/calendar-spike typecheck
corepack pnpm --filter @tutorhub/calendar-spike test
corepack pnpm --filter @tutorhub/calendar-spike e2e
```

For a performance fixture, append `?events=500`, `?events=1000` or
`?events=2000`. The result must be recorded in
`docs/calendar/P3_CAL_01_SPIKE_EVIDENCE.md` with commit, browser and machine
metadata before ADR-0019 can be accepted.
