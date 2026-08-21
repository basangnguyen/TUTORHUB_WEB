import { describe, expect, it } from "vitest";
import {
  RuntimeReadinessCoordinator,
  RuntimeReadinessError,
  type RuntimeDependencyProbeResult,
} from "./runtimeReadinessCoordinator.js";

const HEALTHY_DEPENDENCIES: RuntimeDependencyProbeResult = {
  authorityGuard: true,
  controlPlane: true,
  mode: "enabled",
  persistence: true,
};

describe("runtime readiness coordinator", () => {
  it("serializes overlapping dependency and authority probes", async () => {
    const coordinator = activeCoordinator();
    const first = deferred<RuntimeDependencyProbeResult>();
    let authorityStarted = false;
    const dependencyProbe = coordinator.refreshDependencies(async () => {
      return first.promise;
    });
    const authorityProbe = coordinator.refreshAuthority(async () => {
      authorityStarted = true;
      return { mode: "enabled", value: new Set(["lease-a"]) } as const;
    });

    await Promise.resolve();
    expect(authorityStarted).toBe(false);
    first.resolve(HEALTHY_DEPENDENCIES);
    await expect(dependencyProbe).resolves.toMatchObject({ applied: true });
    await expect(authorityProbe).resolves.toMatchObject({ applied: true });
    expect(authorityStarted).toBe(true);
    expect(coordinator.readiness().ready).toBe(true);
  });

  it("does not let an in-flight success reopen readiness after fail-closed", async () => {
    const coordinator = activeCoordinator();
    const pending = deferred<RuntimeDependencyProbeResult>();
    const probe = coordinator.refreshDependencies(() => pending.promise);
    await Promise.resolve();

    coordinator.failClosed();
    pending.resolve(HEALTHY_DEPENDENCIES);

    await expect(probe).resolves.toMatchObject({
      applied: false,
      reason: "stale",
    });
    expect(coordinator.readiness()).toMatchObject({
      ready: false,
      reason: "dependency_unavailable",
    });
  });

  it("invalidates a previously queued success after a dependency failure", async () => {
    const coordinator = activeCoordinator();
    const first = deferred<RuntimeDependencyProbeResult>();
    let staleProbeStarted = false;
    const failed = coordinator.refreshDependencies(() => first.promise);
    const staleSuccess = coordinator.refreshDependencies(async () => {
      staleProbeStarted = true;
      return HEALTHY_DEPENDENCIES;
    });

    first.resolve({
      ...HEALTHY_DEPENDENCIES,
      persistence: false,
    });

    await expect(failed).resolves.toMatchObject({ applied: true });
    await expect(staleSuccess).resolves.toMatchObject({
      applied: false,
      reason: "stale",
    });
    expect(staleProbeStarted).toBe(false);
    expect(coordinator.readiness().ready).toBe(false);
  });

  it("keeps dependency failure closed when an authority probe succeeds", async () => {
    const coordinator = activeCoordinator();
    await coordinator.refreshDependencies(async () => ({
      ...HEALTHY_DEPENDENCIES,
      persistence: false,
    }));
    const outcome = await coordinator.refreshAuthority(async () => ({
      mode: "enabled" as const,
      value: new Set(["lease-a"]),
    }));

    expect(outcome.applied).toBe(true);
    expect(coordinator.readiness().ready).toBe(false);
  });

  it("invalidates queued enabled results after an authoritative off mode", async () => {
    const coordinator = activeCoordinator();
    await coordinator.refreshDependencies(async () => HEALTHY_DEPENDENCIES);
    const first = deferred<{
      mode: "off";
      value: ReadonlySet<string>;
    }>();
    let staleProbeStarted = false;
    const off = coordinator.refreshAuthority(() => first.promise);
    const staleEnabled = coordinator.refreshAuthority(async () => {
      staleProbeStarted = true;
      return { mode: "enabled" as const, value: new Set<string>() };
    });
    first.resolve({ mode: "off", value: new Set<string>() });

    await expect(off).resolves.toMatchObject({ applied: true });
    await expect(staleEnabled).resolves.toMatchObject({
      applied: false,
      reason: "stale",
    });
    expect(staleProbeStarted).toBe(false);
    expect(coordinator.readiness()).toMatchObject({
      authorityMode: "off",
      ready: false,
    });
  });

  it("asserts local authority synchronously at admission and write boundaries", async () => {
    let authorityHeld = true;
    const coordinator = new RuntimeReadinessCoordinator(() => {
      if (!authorityHeld) throw new Error("authority_not_held");
    });
    coordinator.activate();
    await coordinator.refreshDependencies(async () => HEALTHY_DEPENDENCIES);

    expect(coordinator.assertAdmissionReady()).toBe("enabled");
    expect(coordinator.assertAdmissionReady({ write: true })).toBe("enabled");
    authorityHeld = false;
    expect(() => coordinator.assertAdmissionReady()).toThrow(
      "authority_not_held",
    );
    expect(coordinator.readiness().ready).toBe(false);
  });

  it("fails the readiness snapshot closed as soon as local authority is stale", async () => {
    let authorityHeld = true;
    const coordinator = new RuntimeReadinessCoordinator(() => {
      if (!authorityHeld) throw new Error("authority_not_held");
    });
    coordinator.activate();
    await coordinator.refreshDependencies(async () => HEALTHY_DEPENDENCIES);
    const before = coordinator.readiness();
    expect(before.ready).toBe(true);

    authorityHeld = false;
    const after = coordinator.readiness();

    expect(after).toMatchObject({
      ready: false,
      reason: "dependency_unavailable",
    });
    expect(after.epoch).toBeGreaterThan(before.epoch);
    expect(() => coordinator.assertAdmissionReady()).toThrow(
      new RuntimeReadinessError("runtime_not_ready"),
    );
  });

  it("rejects writes in read-only mode and all admission while draining", async () => {
    const coordinator = activeCoordinator();
    await coordinator.refreshDependencies(async () => ({
      ...HEALTHY_DEPENDENCIES,
      mode: "read_only",
    }));

    expect(coordinator.assertAdmissionReady()).toBe("read_only");
    expect(() => coordinator.assertAdmissionReady({ write: true })).toThrow(
      new RuntimeReadinessError("write_disabled"),
    );
    coordinator.beginDrain();
    expect(() => coordinator.assertAdmissionReady()).toThrow(
      new RuntimeReadinessError("runtime_not_ready"),
    );
  });
});

function activeCoordinator(): RuntimeReadinessCoordinator {
  const coordinator = new RuntimeReadinessCoordinator(() => undefined);
  coordinator.activate();
  return coordinator;
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => (resolve = next));
  return { promise, resolve };
}
