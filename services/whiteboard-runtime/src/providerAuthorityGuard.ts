import { Client } from "pg";

const FREE_PRIVATE_ALPHA_LOCK_NAMESPACE = 0x5455_4842;
const FREE_PRIVATE_ALPHA_LOCK_KEY = 0x5742_5244;
const DEFAULT_ACQUISITION_SAFETY_WINDOW_MS = 2_250;
const DEFAULT_AUTHORITY_FRESHNESS_MS = 2_000;

const ACQUIRE_SQL = `
  SELECT
    pg_backend_pid() AS backend_pid,
    pg_try_advisory_lock($1::integer, $2::integer) AS acquired
`;

const PROBE_SQL = `
  SELECT
    pg_backend_pid() AS backend_pid,
    EXISTS (
      SELECT 1
      FROM pg_catalog.pg_locks
      WHERE locktype = 'advisory'
        AND database = (
          SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database()
        )
        AND pid = pg_backend_pid()
        AND classid = $1::oid
        AND objid = $2::oid
        AND objsubid = 2
        AND mode = 'ExclusiveLock'
        AND granted
    ) AS held
`;

const RELEASE_SQL = `
  SELECT
    pg_backend_pid() AS backend_pid,
    pg_advisory_unlock($1::integer, $2::integer) AS released
`;

export type ProviderAuthorityGuardErrorCode =
  | "provider_authority_duplicate"
  | "provider_authority_not_acquired"
  | "provider_authority_session_lost"
  | "provider_authority_stale"
  | "provider_authority_unavailable";

export class ProviderAuthorityGuardError extends Error {
  constructor(readonly code: ProviderAuthorityGuardErrorCode) {
    super(code);
    this.name = "ProviderAuthorityGuardError";
  }
}

export interface ProviderAuthorityGuard {
  acquire(): Promise<void>;
  assertHeld(): void;
  probe(): Promise<void>;
  release(): Promise<void>;
}

export interface ProviderAuthoritySession {
  connect(): Promise<void>;
  end(): Promise<void>;
  query(
    text: string,
    values: readonly unknown[],
  ): Promise<{ rows: Array<Record<string, unknown>> }>;
}

export type ProviderAuthoritySessionFactory = () => ProviderAuthoritySession;

export interface ProviderAuthorityGuardOptions {
  acquisitionSafetyWindowMs?: number;
  freshnessMs?: number;
  now?: () => number;
  sleep?: (durationMs: number) => Promise<void>;
}

/**
 * Holds the FREE_PRIVATE_ALPHA singleton authority on one dedicated PostgreSQL
 * session. Session advisory locks are intentionally not acquired through a
 * pool: returning the client to a pool would detach authority from this guard.
 */
export class PostgresProviderAuthorityGuard implements ProviderAuthorityGuard {
  private backendPid: number | undefined;
  private lastConfirmedAt: number | undefined;
  private operation: Promise<void> = Promise.resolve();
  private session: ProviderAuthoritySession | undefined;
  private readonly acquisitionSafetyWindowMs: number;
  private readonly freshnessMs: number;
  private readonly now: () => number;
  private readonly sleep: (durationMs: number) => Promise<void>;

  constructor(
    databaseUrl: string,
    private readonly sessionFactory: ProviderAuthoritySessionFactory = () =>
      new PgProviderAuthoritySession(databaseUrl),
    options: ProviderAuthorityGuardOptions = {},
  ) {
    this.acquisitionSafetyWindowMs =
      options.acquisitionSafetyWindowMs ?? DEFAULT_ACQUISITION_SAFETY_WINDOW_MS;
    this.freshnessMs = options.freshnessMs ?? DEFAULT_AUTHORITY_FRESHNESS_MS;
    this.now = options.now ?? Date.now;
    this.sleep = options.sleep ?? sleep;
    assertNonNegativeSafeInteger(
      this.acquisitionSafetyWindowMs,
      "provider_authority_safety_window_invalid",
    );
    assertPositiveSafeInteger(
      this.freshnessMs,
      "provider_authority_freshness_invalid",
    );
  }

  acquire(): Promise<void> {
    return this.serialize(async () => {
      if (this.session) {
        await this.probeAcquiredSession();
        return;
      }

      const session = this.sessionFactory();
      try {
        await session.connect();
        const result = await session.query(ACQUIRE_SQL, lockKeys());
        const row = singleRow(result.rows);
        const backendPid = positiveInteger(row?.backend_pid);
        if (row?.acquired !== true || backendPid === undefined) {
          throw new ProviderAuthorityGuardError(
            row?.acquired === false
              ? "provider_authority_duplicate"
              : "provider_authority_unavailable",
          );
        }
        this.session = session;
        this.backendPid = backendPid;
        this.lastConfirmedAt = undefined;
        if (this.acquisitionSafetyWindowMs > 0) {
          await this.sleep(this.acquisitionSafetyWindowMs);
        }
        await this.probeAcquiredSession(false);
      } catch (error) {
        if (this.session === session) {
          this.clearLocalAuthority();
        }
        await endQuietly(session);
        if (error instanceof ProviderAuthorityGuardError) throw error;
        throw new ProviderAuthorityGuardError("provider_authority_unavailable");
      }
    });
  }

  /**
   * Synchronous, local fence used at admission and mutation boundaries.
   *
   * PostgreSQL remains the source of truth and `probe()` performs the network
   * verification. This assertion makes an already-observed release/session
   * loss fail closed immediately instead of waiting for another asynchronous
   * probe to be scheduled.
   */
  assertHeld(): void {
    if (
      !this.session ||
      this.backendPid === undefined ||
      this.lastConfirmedAt === undefined
    ) {
      throw new ProviderAuthorityGuardError("provider_authority_not_acquired");
    }
    const age = this.now() - this.lastConfirmedAt;
    if (age < 0 || age > this.freshnessMs) {
      throw new ProviderAuthorityGuardError("provider_authority_stale");
    }
  }

  probe(): Promise<void> {
    return this.serialize(() => this.probeAcquiredSession());
  }

  release(): Promise<void> {
    return this.serialize(async () => {
      const session = this.session;
      const backendPid = this.backendPid;
      if (!session || backendPid === undefined) return;

      this.clearLocalAuthority();
      let failure: ProviderAuthorityGuardError | undefined;
      try {
        const result = await session.query(RELEASE_SQL, lockKeys());
        const row = singleRow(result.rows);
        if (
          row?.released !== true ||
          positiveInteger(row.backend_pid) !== backendPid
        ) {
          failure = new ProviderAuthorityGuardError(
            "provider_authority_session_lost",
          );
        }
      } catch {
        failure = new ProviderAuthorityGuardError(
          "provider_authority_session_lost",
        );
      } finally {
        await endQuietly(session);
      }
      if (failure) throw failure;
    });
  }

  private async probeAcquiredSession(endOnFailure = true): Promise<void> {
    const session = this.session;
    const backendPid = this.backendPid;
    if (!session || backendPid === undefined) {
      throw new ProviderAuthorityGuardError("provider_authority_not_acquired");
    }

    try {
      const result = await session.query(PROBE_SQL, lockKeys());
      const row = singleRow(result.rows);
      if (
        row?.held !== true ||
        positiveInteger(row.backend_pid) !== backendPid
      ) {
        throw new ProviderAuthorityGuardError(
          "provider_authority_session_lost",
        );
      }
      this.lastConfirmedAt = this.now();
    } catch (error) {
      this.clearLocalAuthority();
      if (endOnFailure) await endQuietly(session);
      if (
        error instanceof ProviderAuthorityGuardError &&
        error.code === "provider_authority_session_lost"
      ) {
        throw error;
      }
      throw new ProviderAuthorityGuardError("provider_authority_session_lost");
    }
  }

  private serialize(operation: () => Promise<void>): Promise<void> {
    const result = this.operation.then(operation, operation);
    this.operation = result.catch(() => undefined);
    return result;
  }

  private clearLocalAuthority(): void {
    this.session = undefined;
    this.backendPid = undefined;
    this.lastConfirmedAt = undefined;
  }
}

class PgProviderAuthoritySession implements ProviderAuthoritySession {
  private readonly client: Client;

  constructor(databaseUrl: string) {
    this.client = new Client({
      application_name: "tutorhub-whiteboard-authority",
      connectionString: databaseUrl,
      connectionTimeoutMillis: 2_000,
      keepAlive: true,
      query_timeout: 5_000,
      statement_timeout: 5_000,
    });
    // pg emits idle-session failures through EventEmitter. The next freshness
    // assertion/probe fails closed; this listener prevents an uncaught driver
    // error from terminating the process before controlled cleanup runs.
    this.client.on("error", () => undefined);
  }

  async connect(): Promise<void> {
    await this.client.connect();
  }

  async end(): Promise<void> {
    await this.client.end();
  }

  async query(
    text: string,
    values: readonly unknown[],
  ): Promise<{ rows: Array<Record<string, unknown>> }> {
    const result = await this.client.query<Record<string, unknown>>(text, [
      ...values,
    ]);
    return { rows: result.rows };
  }
}

function lockKeys(): readonly [number, number] {
  return [FREE_PRIVATE_ALPHA_LOCK_NAMESPACE, FREE_PRIVATE_ALPHA_LOCK_KEY];
}

function singleRow(
  rows: Array<Record<string, unknown>>,
): Record<string, unknown> | undefined {
  return rows.length === 1 ? rows[0] : undefined;
}

function positiveInteger(value: unknown): number | undefined {
  return Number.isSafeInteger(value) && (value as number) > 0
    ? (value as number)
    : undefined;
}

async function endQuietly(session: ProviderAuthoritySession): Promise<void> {
  try {
    await session.end();
  } catch {
    // Authority is already fail-closed locally; never expose raw driver errors.
  }
}

function assertNonNegativeSafeInteger(value: number, code: string): void {
  if (!Number.isSafeInteger(value) || value < 0) throw new Error(code);
}

function assertPositiveSafeInteger(value: number, code: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(code);
}

function sleep(durationMs: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, durationMs));
}
