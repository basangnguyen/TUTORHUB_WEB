import { describe, expect, it } from "vitest";
import {
  EXCALIDRAW_LOAD_BUDGETS,
  EXCALIDRAW_LOAD_PROFILES,
  runExcalidrawLoadProfile,
} from "./excalidrawLoadHarness";

describe.sequential("P5-COLLAB-14 Excalidraw 2/10/50 profiles", () => {
  for (const profile of EXCALIDRAW_LOAD_PROFILES) {
    it(`${profile.name} meets the published convergence and cleanup budgets`, async () => {
      const result = await runExcalidrawLoadProfile(profile);
      const budget = EXCALIDRAW_LOAD_BUDGETS[profile.name];

      process.stdout.write(`P5_COLLAB_14_PROFILE ${JSON.stringify(result)}\n`);
      expect(result.activeConnectionsAtPeak).toBe(profile.clients);
      expect(result.cleanupActiveConnections).toBe(0);
      expect(result.joinP95Ms).toBeLessThanOrEqual(budget.joinP95Ms);
      expect(result.convergenceP95Ms).toBeLessThanOrEqual(
        budget.convergenceP95Ms,
      );
      expect(result.cpuMs).toBeLessThanOrEqual(budget.cpuMs);
      expect(result.heapDeltaBytes).toBeLessThanOrEqual(budget.heapDeltaBytes);
      expect(result.inputLatencyP95Ms).toBeLessThanOrEqual(
        budget.inputLatencyP95Ms,
      );
      expect(result.receivedBytes).toBeLessThanOrEqual(budget.receivedBytes);
      expect(result.recoveryJoinMs).toBeLessThanOrEqual(budget.recoveryJoinMs);
      expect(result.snapshotEncodeMs).toBeLessThanOrEqual(
        budget.snapshotEncodeMs,
      );
      expect(result.cleanupMs).toBeLessThanOrEqual(budget.cleanupMs);
      expect(result.encodedStateBytes).toBeGreaterThan(0);
      expect(result.semanticHash).toMatch(/^fnv1a64:[a-f0-9]{16}$/);
    }, 90_000);
  }
});
