import { describe, expect, it } from "vitest";
import {
  PostgresProviderAuthorityGuard,
  ProviderAuthorityGuardError,
  type ProviderAuthoritySession,
} from "./providerAuthorityGuard.js";

describe("FREE_PRIVATE_ALPHA PostgreSQL provider authority guard", () => {
  it("acquires, probes, and releases one session lock idempotently", async () => {
    const database = new FakeAdvisoryDatabase();
    const guard = createGuard(database);

    await expect(guard.release()).resolves.toBeUndefined();
    await Promise.all([guard.acquire(), guard.acquire()]);
    expect(() => guard.assertHeld()).not.toThrow();
    await expect(guard.probe()).resolves.toBeUndefined();
    await Promise.all([guard.release(), guard.release()]);

    expect(database.sessions).toHaveLength(1);
    expect(database.acquireCount).toBe(1);
    expect(database.probeCount).toBe(3);
    expect(database.releaseCount).toBe(1);
    expect(database.sessions[0]?.endCount).toBe(1);
    expect(database.owner).toBeUndefined();
    expect(() => guard.assertHeld()).toThrow(
      new ProviderAuthorityGuardError("provider_authority_not_acquired"),
    );
  });

  it("fails closed when a duplicate process already owns authority", async () => {
    const database = new FakeAdvisoryDatabase();
    const first = createGuard(database);
    const duplicate = createGuard(database);
    await first.acquire();

    await expect(duplicate.acquire()).rejects.toEqual(
      new ProviderAuthorityGuardError("provider_authority_duplicate"),
    );
    expect(database.sessions[1]?.endCount).toBe(1);
    await expect(first.probe()).resolves.toBeUndefined();

    await first.release();
    await expect(duplicate.acquire()).resolves.toBeUndefined();
    await duplicate.release();
  });

  it("fails a probe after the owning PostgreSQL session is lost", async () => {
    const database = new FakeAdvisoryDatabase();
    const guard = createGuard(database);
    await guard.acquire();
    database.loseOwnerSession();

    await expect(guard.probe()).rejects.toEqual(
      new ProviderAuthorityGuardError("provider_authority_session_lost"),
    );
    expect(() => guard.assertHeld()).toThrow(
      new ProviderAuthorityGuardError("provider_authority_not_acquired"),
    );
    await expect(guard.probe()).rejects.toEqual(
      new ProviderAuthorityGuardError("provider_authority_not_acquired"),
    );

    await expect(guard.acquire()).resolves.toBeUndefined();
    expect(database.sessions).toHaveLength(2);
    await guard.release();
  });

  it("does not retain a session when initial acquisition is unavailable", async () => {
    const database = new FakeAdvisoryDatabase();
    database.failNextAcquire = true;
    const guard = createGuard(database);

    await expect(guard.acquire()).rejects.toEqual(
      new ProviderAuthorityGuardError("provider_authority_unavailable"),
    );
    expect(database.sessions[0]?.endCount).toBe(1);
    await expect(guard.probe()).rejects.toEqual(
      new ProviderAuthorityGuardError("provider_authority_not_acquired"),
    );
  });

  it("requires a fresh successful probe at synchronous admission boundaries", async () => {
    const database = new FakeAdvisoryDatabase();
    let now = 1_000;
    const guard = createGuard(database, {
      acquisitionSafetyWindowMs: 0,
      freshnessMs: 100,
      now: () => now,
    });
    await guard.acquire();
    expect(() => guard.assertHeld()).not.toThrow();

    now += 101;
    expect(() => guard.assertHeld()).toThrow(
      new ProviderAuthorityGuardError("provider_authority_stale"),
    );
    await guard.probe();
    expect(() => guard.assertHeld()).not.toThrow();
    await guard.release();
  });

  it("re-probes the same lock after the acquisition safety window", async () => {
    const database = new FakeAdvisoryDatabase();
    const sleepDurations: number[] = [];
    const guard = createGuard(database, {
      acquisitionSafetyWindowMs: 75,
      freshnessMs: 1_000,
      sleep: async (durationMs) => {
        sleepDurations.push(durationMs);
        database.loseOwnerSession();
      },
    });

    await expect(guard.acquire()).rejects.toEqual(
      new ProviderAuthorityGuardError("provider_authority_session_lost"),
    );
    expect(sleepDurations).toEqual([75]);
    expect(() => guard.assertHeld()).toThrow(
      new ProviderAuthorityGuardError("provider_authority_not_acquired"),
    );
    expect(database.sessions[0]?.endCount).toBe(1);
  });

  it("does not resolve a replacement acquire before the old freshness window expires", async () => {
    const database = new FakeAdvisoryDatabase();
    let now = 0;
    const oldGuard = createGuard(database, {
      acquisitionSafetyWindowMs: 0,
      freshnessMs: 2_000,
      now: () => now,
    });
    await oldGuard.acquire();
    database.loseOwnerSession();
    expect(() => oldGuard.assertHeld()).not.toThrow();

    const safetyGate = deferred<void>();
    const safetyStarted = deferred<number>();
    const replacement = createGuard(database, {
      now: () => now,
      sleep: async (durationMs) => {
        safetyStarted.resolve(durationMs);
        await safetyGate.promise;
      },
    });
    let replacementResolved = false;
    const acquiring = replacement.acquire().then(() => {
      replacementResolved = true;
    });

    await expect(safetyStarted.promise).resolves.toBe(2_250);
    now = 1_999;
    expect(() => oldGuard.assertHeld()).not.toThrow();
    expect(replacementResolved).toBe(false);

    now = 2_001;
    expect(() => oldGuard.assertHeld()).toThrow(
      new ProviderAuthorityGuardError("provider_authority_stale"),
    );
    expect(replacementResolved).toBe(false);

    now = 2_250;
    safetyGate.resolve();
    await acquiring;
    expect(replacementResolved).toBe(true);
    expect(() => replacement.assertHeld()).not.toThrow();
    await replacement.release();
  });
});

function createGuard(
  database: FakeAdvisoryDatabase,
  options: ConstructorParameters<typeof PostgresProviderAuthorityGuard>[2] = {
    acquisitionSafetyWindowMs: 0,
  },
): PostgresProviderAuthorityGuard {
  return new PostgresProviderAuthorityGuard(
    "postgresql://unused:unused@127.0.0.1/unused",
    () => database.createSession(),
    options,
  );
}

class FakeAdvisoryDatabase {
  acquireCount = 0;
  failNextAcquire = false;
  owner: FakeAuthoritySession | undefined;
  probeCount = 0;
  releaseCount = 0;
  readonly sessions: FakeAuthoritySession[] = [];

  createSession(): FakeAuthoritySession {
    const session = new FakeAuthoritySession(
      this,
      10_000 + this.sessions.length,
    );
    this.sessions.push(session);
    return session;
  }

  loseOwnerSession(): void {
    const owner = this.owner;
    if (!owner) throw new Error("fake_owner_missing");
    owner.connected = false;
    this.owner = undefined;
  }
}

class FakeAuthoritySession implements ProviderAuthoritySession {
  connected = false;
  endCount = 0;

  constructor(
    private readonly database: FakeAdvisoryDatabase,
    private readonly backendPid: number,
  ) {}

  async connect(): Promise<void> {
    this.connected = true;
  }

  async end(): Promise<void> {
    this.endCount += 1;
    this.connected = false;
    if (this.database.owner === this) this.database.owner = undefined;
  }

  async query(
    text: string,
    values: readonly unknown[],
  ): Promise<{ rows: Array<Record<string, unknown>> }> {
    if (!this.connected) throw new Error("fake_session_lost");
    if (values.length !== 2) throw new Error("fake_lock_key_missing");
    if (text.includes("pg_try_advisory_lock")) {
      this.database.acquireCount += 1;
      if (this.database.failNextAcquire) {
        this.database.failNextAcquire = false;
        throw new Error("fake_acquire_unavailable");
      }
      const acquired =
        this.database.owner === undefined || this.database.owner === this;
      if (acquired) this.database.owner = this;
      return { rows: [{ acquired, backend_pid: this.backendPid }] };
    }
    if (text.includes("pg_catalog.pg_locks")) {
      this.database.probeCount += 1;
      return {
        rows: [
          {
            backend_pid: this.backendPid,
            held: this.database.owner === this,
          },
        ],
      };
    }
    if (text.includes("pg_advisory_unlock")) {
      this.database.releaseCount += 1;
      const released = this.database.owner === this;
      if (released) this.database.owner = undefined;
      return { rows: [{ backend_pid: this.backendPid, released }] };
    }
    throw new Error("fake_query_unexpected");
  }
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => (resolve = next));
  return { promise, resolve };
}
