# P5-COLLAB-15 accessibility and browser-matrix acceptance

Status: **DONE**

Date: 2026-08-23

## Scope

- Test the production `Drawer` plus `CanonicalExcalidrawCanvas`, not the earlier isolated engine spike.
- Keep the harness local-only (`DEV` and loopback host); it does not request a grant, read an
  `.env*.local` file, connect Neon/B2/Render, write shared staging, or deploy.
- Verify keyboard open/close, named tools, focus handoff/recovery, live reconnect/failure status,
  semantic fallback pagination, constrained reflow, forced colors and reduced motion.
- Publish unsupported pilot targets as `UNAVAILABLE`; do not infer support from Excalidraw docs.
- Keep physical NVDA speech and physical browser zoom separate from Axe/Playwright automation.

## Candidate changes

- The production semantic fallback is bounded at 50 elements per page and exposes explicit navigation,
  a visible “read as text” focus handoff, and a return-to-canvas action.
- The visual canvas is now a named, focusable region associated with the semantic description.
- The production whiteboard portal has explicit visible-focus, forced-colors and reduced-motion rules.
- The pinned Excalidraw `0.18.1` adapter normalizes two upstream DOM semantics: the unnamed main-menu
  trigger receives an accessible name, and its nested footer becomes a named group rather than an
  invalid nested `contentinfo` landmark.
- The exact production component has a loopback-only physical harness and an installed Chrome/Edge
  runner. No harness route is included in the production Vite entry.

## Automated evidence

Commands:

```text
pnpm --filter @tutorhub/web test -- src/features/collaboration/LazyWhiteboardEngine.test.tsx
pnpm --filter @tutorhub/web typecheck
pnpm --filter @tutorhub/web lint
set P5_COLLAB_15_HEADLESS=1 && pnpm test:collaboration:p515:browser
pnpm test:collaboration:p515:browser
```

Current result:

- Web unit regression: **73 files / 457 tests PASS**.
- Web TypeScript and ESLint: **PASS**.
- Full repository `pnpm verify`: **PASS** on the final local candidate tree, including format,
  OpenAPI/generated client, local/e2e infrastructure checks, security, lint, typecheck, builds,
  Storybook, Go tests and `go vet`.
- Google Chrome `152.0.7977.54` on Windows, installed headful run: **PASS**, Axe `0` default and
  `0` constrained violations.
- Microsoft Edge `151.0.4129.101` on Windows, installed headful run: **PASS**, Axe `0` default and
  `0` constrained violations.
- Both browsers: keyboard Drawer open, named Selection/Rectangle/Arrow/Text tools, 50-item semantic
  page, second page with 5 items, semantic-to-canvas focus, reconnect/failure/restore live status,
  640 px reflow, forced colors, reduced motion, close and trigger focus recovery: **PASS**.
- Physical owner matrix at browser zoom **200%** with NVDA Speech Viewer: Chrome and Edge both
  **PASS**. NVDA announced the Drawer/dialog, named shape/draw/text/image/eraser tools, semantic
  fallback, reconnect/failure/restore states and close-to-trigger focus recovery.
- Supplemental installed-browser headless run: **PASS**.
- Local evidence: `test-results/p5-collab-15-browser-matrix/evidence.json` and browser screenshots.

## Pilot matrix

| Target                   | Status                             | Evidence boundary                                                            |
| ------------------------ | ---------------------------------- | ---------------------------------------------------------------------------- |
| Google Chrome / Windows  | PASS (automated + physical NVDA)   | Exact Drawer/canvas, Axe, keyboard, focus, 200% zoom, speech, forced colors and reduced motion |
| Microsoft Edge / Windows | PASS (automated + physical NVDA)   | Exact Drawer/canvas, Axe, keyboard, focus, 200% zoom, speech, forced colors and reduced motion |
| Firefox / Windows        | UNAVAILABLE                        | Outside the declared private-alpha pilot matrix                              |
| Safari / macOS           | UNAVAILABLE                        | No physical macOS/Safari device in this pilot                                |
| Mobile browsers          | UNAVAILABLE                        | Desktop classroom scope                                                      |

## Physical owner gate — PASS

Run both installed Chrome and Edge with NVDA on the loopback harness:

```text
pnpm --dir apps/web dev --host 127.0.0.1 --port 5195
http://127.0.0.1:5195/p5-15-physical.html
```

For each browser:

1. Set browser zoom to **200%** and enable NVDA.
2. Keyboard-open “Open classroom whiteboard”; confirm NVDA reads the dialog title and named drawing
   tools, and visible focus remains clear without horizontal page overflow.
3. Activate “Read whiteboard as text”; confirm the heading, `Page 1/2 · 55 elements`, element labels,
   Next/Previous controls and “Move focus to drawing canvas” are spoken correctly.
4. Activate reconnect, failure and restore controls; confirm each status is announced once and remains
   understandable.
5. Enable Windows forced colors and reduced motion, repeat the focus path, then close the Drawer and
   confirm focus returns to “Open classroom whiteboard”.

Owner result on 2026-08-23:

- Google Chrome + NVDA + browser zoom 200%: **PASS**.
- Microsoft Edge + NVDA + browser zoom 200%: **PASS**.
- Named toolbar controls, semantic text pages, reconnect/failure/restore announcements, forced-colors,
  reduced-motion focus path and close-to-trigger focus recovery: **PASS**.

The owner confirmation is the required speech/200%-zoom evidence; Axe and browser automation remain
supporting evidence only.

## Current decision

Automated pre-staging gates and the owner physical Chrome/Edge + NVDA matrix are green. Exact
candidate `09449fc` is published on `origin/main`; GitHub Verify `32638928557` and Security
`32638928501` both passed. P5-COLLAB-15 is **DONE**. Production whiteboard remains force-off until
the later rollout gates.
