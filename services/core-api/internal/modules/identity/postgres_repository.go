package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type Database interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     Database
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	controls     featurecontrol.Enforcer
}

func NewPostgresRepository(
	database Database,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	controls ...featurecontrol.Enforcer,
) *PostgresRepository {
	repository := &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer,
	}
	if len(controls) > 0 {
		repository.controls = controls[0]
	}
	return repository
}

func (repository *PostgresRepository) CreateFlow(
	ctx context.Context,
	params CreateFlowParams,
) error {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin authentication flow transaction: %w", err)
	}
	defer rollback(transaction)

	if _, err := transaction.Exec(
		queryContext,
		`DELETE FROM tutorhub.auth_flows WHERE expires_at <= $1`,
		params.CreatedAt,
	); err != nil {
		return fmt.Errorf("delete expired authentication flows: %w", err)
	}

	const insertFlow = `
INSERT INTO tutorhub.auth_flows (
    state_hash,
    browser_binding_hash,
    nonce_hash,
    code_verifier_ciphertext,
    return_to,
    purpose,
    user_id,
    session_id,
    created_at,
    expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	if _, err := transaction.Exec(
		queryContext,
		insertFlow,
		params.StateHash,
		params.BrowserBindingHash,
		params.NonceHash,
		params.CodeVerifierCiphertext,
		params.ReturnTo,
		params.Purpose,
		nullUUID(params.UserID),
		nullUUID(params.SessionID),
		params.CreatedAt,
		params.ExpiresAt,
	); err != nil {
		return fmt.Errorf("insert authentication flow: %w", err)
	}

	if err := transaction.Commit(queryContext); err != nil {
		return fmt.Errorf("commit authentication flow: %w", err)
	}

	return nil
}

func (repository *PostgresRepository) ConsumeFlow(
	ctx context.Context,
	stateHash []byte,
	browserBindingHash []byte,
	consumedAt time.Time,
) (StoredFlow, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return StoredFlow{}, fmt.Errorf("begin consume authentication flow: %w", err)
	}
	defer rollback(transaction)

	const consumeFlow = `
UPDATE tutorhub.auth_flows
SET consumed_at = $3
WHERE state_hash = $1
  AND browser_binding_hash = $2
  AND consumed_at IS NULL
  AND expires_at > $3
RETURNING
    nonce_hash,
    code_verifier_ciphertext,
    return_to,
    purpose,
    user_id,
    session_id`
	var flow StoredFlow
	var (
		userID    uuid.NullUUID
		sessionID uuid.NullUUID
	)
	if err := transaction.QueryRow(
		queryContext,
		consumeFlow,
		stateHash,
		browserBindingHash,
		consumedAt,
	).Scan(
		&flow.NonceHash,
		&flow.CodeVerifierCiphertext,
		&flow.ReturnTo,
		&flow.Purpose,
		&userID,
		&sessionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredFlow{}, ErrInvalidAuthFlow
		}
		return StoredFlow{}, fmt.Errorf("consume authentication flow: %w", err)
	}
	flow.UserID = userID.UUID
	flow.SessionID = sessionID.UUID

	if err := transaction.Commit(queryContext); err != nil {
		return StoredFlow{}, fmt.Errorf("commit consumed authentication flow: %w", err)
	}

	return flow, nil
}

func (repository *PostgresRepository) CreateAuthenticatedSession(
	ctx context.Context,
	claims ProviderClaims,
	metadata SessionMetadata,
) (Principal, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Principal{}, fmt.Errorf("begin authenticated session: %w", err)
	}
	defer rollback(transaction)

	identityID, userID, err := resolveIdentity(
		queryContext,
		transaction,
		claims,
		metadata.CreatedAt,
	)
	if err != nil {
		return Principal{}, err
	}

	activeTenantID, err := selectActiveTenant(queryContext, transaction, userID)
	if err != nil {
		return Principal{}, err
	}

	var ipPrefix any
	if metadata.IPPrefix != "" {
		ipPrefix = metadata.IPPrefix
	}

	const insertSession = `
INSERT INTO tutorhub.sessions (
    user_id,
    active_tenant_id,
    token_hash,
    csrf_token_hash,
    user_agent_hash,
    ip_prefix,
    created_at,
    last_seen_at,
    expires_at,
    identity_id,
    absolute_expires_at,
    auth_time
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8, $9, $10, $11)
RETURNING id`
	var sessionID uuid.UUID
	if err := transaction.QueryRow(
		queryContext,
		insertSession,
		userID,
		nullUUID(activeTenantID),
		metadata.TokenHash,
		metadata.CSRFHash,
		metadata.UserAgentHash,
		ipPrefix,
		metadata.CreatedAt,
		metadata.ExpiresAt,
		identityID,
		metadata.AbsoluteAt,
		claims.AuthTime,
	).Scan(&sessionID); err != nil {
		return Principal{}, fmt.Errorf("insert authenticated session: %w", err)
	}

	principal, err := repository.loadPrincipal(
		queryContext, transaction, sessionID, userID, activeTenantID,
	)
	if err != nil {
		return Principal{}, err
	}

	if err := transaction.Commit(queryContext); err != nil {
		return Principal{}, fmt.Errorf("commit authenticated session: %w", err)
	}

	return principal, nil
}

func (repository *PostgresRepository) GetSession(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
	idleTTL time.Duration,
) (SessionRecord, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("begin session lookup: %w", err)
	}
	defer rollback(transaction)

	idleSeconds := max(int64(idleTTL/time.Second), 1)
	const refreshSession = `
UPDATE tutorhub.sessions
SET
    last_seen_at = $2,
    expires_at = LEAST(
        $2 + make_interval(secs => $3),
        absolute_expires_at
    )
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > $2
  AND absolute_expires_at > $2
RETURNING id, user_id, active_tenant_id, csrf_token_hash, expires_at`
	var (
		sessionID     uuid.UUID
		userID        uuid.UUID
		activeTenant  uuid.NullUUID
		csrfTokenHash []byte
		expiresAt     time.Time
	)
	if err := transaction.QueryRow(
		queryContext,
		refreshSession,
		tokenHash,
		now,
		idleSeconds,
	).Scan(&sessionID, &userID, &activeTenant, &csrfTokenHash, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionRecord{}, ErrSessionNotFound
		}
		return SessionRecord{}, fmt.Errorf("refresh session: %w", err)
	}

	principal, err := repository.loadPrincipal(
		queryContext, transaction, sessionID, userID, activeTenant.UUID,
	)
	if err != nil {
		return SessionRecord{}, err
	}

	if err := transaction.Commit(queryContext); err != nil {
		return SessionRecord{}, fmt.Errorf("commit session lookup: %w", err)
	}

	return SessionRecord{
		Principal: principal,
		CSRFHash:  csrfTokenHash,
		ExpiresAt: expiresAt,
	}, nil
}

func (repository *PostgresRepository) RotateCSRF(
	ctx context.Context,
	sessionID uuid.UUID,
	csrfHash []byte,
	now time.Time,
) error {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin CSRF rotation: %w", err)
	}
	defer rollback(transaction)

	command, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.sessions
SET csrf_token_hash = $2
WHERE id = $1
  AND revoked_at IS NULL
  AND expires_at > $3
  AND absolute_expires_at > $3`,
		sessionID,
		csrfHash,
		now,
	)
	if err != nil {
		return fmt.Errorf("rotate CSRF token: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrSessionNotFound
	}

	if err := transaction.Commit(queryContext); err != nil {
		return fmt.Errorf("commit CSRF rotation: %w", err)
	}

	return nil
}

func (repository *PostgresRepository) RevokeSession(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
	reason string,
) error {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin session revocation: %w", err)
	}
	defer rollback(transaction)

	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.sessions
SET revoked_at = COALESCE(revoked_at, $2),
    revoked_reason = COALESCE(revoked_reason, $3)
WHERE token_hash = $1`,
		tokenHash,
		now,
		reason,
	); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	if err := transaction.Commit(queryContext); err != nil {
		return fmt.Errorf("commit session revocation: %w", err)
	}

	return nil
}

func (repository *PostgresRepository) CreateTenant(
	ctx context.Context,
	sessionID uuid.UUID,
	userID uuid.UUID,
	authorizedSourceTenantID uuid.UUID,
	input CreateTenantInput,
	rotation SessionRotation,
) (TenantMutationResult, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return TenantMutationResult{}, fmt.Errorf("begin tenant creation: %w", err)
	}
	defer rollback(transaction)

	var lockedSession lockedTenantSession
	if authorizedSourceTenantID == uuid.Nil {
		lockedSession, err = lockTenantMutationSession(
			queryContext,
			transaction,
			sessionID,
			userID,
			rotation,
		)
		if err != nil {
			return TenantMutationResult{}, err
		}
		if lockedSession.ActiveTenantID != uuid.Nil {
			return TenantMutationResult{}, ErrTenantCreationDenied
		}
		if err := lockActiveTenantUser(queryContext, transaction, userID); err != nil {
			return TenantMutationResult{}, err
		}
		eligible, err := hasNoActiveTenantMembership(queryContext, transaction, userID)
		if err != nil {
			return TenantMutationResult{}, err
		}
		if !eligible {
			return TenantMutationResult{}, ErrTenantCreationDenied
		}
	} else {
		if err := repository.lockAndAuthorizeTenantCreationSource(
			queryContext,
			transaction,
			userID,
			authorizedSourceTenantID,
		); err != nil {
			return TenantMutationResult{}, err
		}
		lockedSession, err = lockTenantMutationSession(
			queryContext,
			transaction,
			sessionID,
			userID,
			rotation,
		)
		if err != nil {
			return TenantMutationResult{}, err
		}
		if lockedSession.ActiveTenantID != authorizedSourceTenantID {
			return TenantMutationResult{}, ErrSessionContextConflict
		}
		if err := lockActiveTenantUser(queryContext, transaction, userID); err != nil {
			return TenantMutationResult{}, err
		}
	}

	var tenantID uuid.UUID
	var tenantVersion int64
	if err := transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.tenants (slug, name)
VALUES ($1, $2)
RETURNING id, version`,
		input.Slug,
		input.Name,
	).Scan(&tenantID, &tenantVersion); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) &&
			postgresError.ConstraintName == "tenants_slug_unique" {
			return TenantMutationResult{}, ErrTenantSlugTaken
		}
		return TenantMutationResult{}, fmt.Errorf("insert tenant: %w", err)
	}

	if _, err := transaction.Exec(
		queryContext,
		`INSERT INTO tutorhub.memberships (
    tenant_id,
    user_id,
    role,
    status,
    joined_at,
    created_at,
    updated_at
)
VALUES ($1, $2, 'org_admin', 'active', $3, $3, $3)`,
		tenantID,
		userID,
		rotation.RotatedAt,
	); err != nil {
		return TenantMutationResult{}, fmt.Errorf("insert tenant owner membership: %w", err)
	}

	if err := updateTenantSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		tenantID,
		rotation,
	); err != nil {
		return TenantMutationResult{}, err
	}

	if err := insertTenantEvent(
		queryContext,
		transaction,
		tenantID,
		userID,
		"tenant.created",
		uuid.Nil,
		uuid.Nil,
		tenantVersion,
		rotation.RotatedAt,
	); err != nil {
		return TenantMutationResult{}, err
	}

	principal, err := repository.loadPrincipal(
		queryContext,
		transaction,
		sessionID,
		userID,
		tenantID,
	)
	if err != nil {
		return TenantMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return TenantMutationResult{}, fmt.Errorf("commit tenant creation: %w", err)
	}

	return TenantMutationResult{Principal: principal, ExpiresAt: lockedSession.ExpiresAt}, nil
}

func (repository *PostgresRepository) SwitchActiveTenant(
	ctx context.Context,
	sessionID uuid.UUID,
	userID uuid.UUID,
	tenantID uuid.UUID,
	rotation SessionRotation,
) (TenantMutationResult, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return TenantMutationResult{}, fmt.Errorf("begin tenant switch: %w", err)
	}
	defer rollback(transaction)

	var targetTenantVersion int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT t.version
    FROM tutorhub.memberships m
    JOIN tutorhub.tenants t ON t.id = m.tenant_id
    WHERE m.user_id = $1
      AND m.tenant_id = $2
      AND m.status = 'active'
	  AND t.status = 'active'
FOR SHARE OF t, m`,
		userID,
		tenantID,
	).Scan(&targetTenantVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TenantMutationResult{}, ErrTenantAccessDenied
		}
		return TenantMutationResult{}, fmt.Errorf("check tenant membership: %w", err)
	}
	lockedSession, err := lockTenantMutationSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		rotation,
	)
	if err != nil {
		return TenantMutationResult{}, err
	}

	if err := updateTenantSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		tenantID,
		rotation,
	); err != nil {
		return TenantMutationResult{}, err
	}
	if err := insertTenantEvent(
		queryContext,
		transaction,
		tenantID,
		userID,
		"tenant.switched",
		lockedSession.ActiveTenantID,
		tenantID,
		targetTenantVersion,
		rotation.RotatedAt,
	); err != nil {
		return TenantMutationResult{}, err
	}
	principal, err := repository.loadPrincipal(
		queryContext,
		transaction,
		sessionID,
		userID,
		tenantID,
	)
	if err != nil {
		return TenantMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return TenantMutationResult{}, fmt.Errorf("commit tenant switch: %w", err)
	}

	return TenantMutationResult{Principal: principal, ExpiresAt: lockedSession.ExpiresAt}, nil
}

type lockedTenantSession struct {
	ExpiresAt      time.Time
	ActiveTenantID uuid.UUID
}

func lockTenantMutationSession(
	ctx context.Context,
	transaction pgx.Tx,
	sessionID uuid.UUID,
	userID uuid.UUID,
	rotation SessionRotation,
) (lockedTenantSession, error) {
	var result lockedTenantSession
	var activeTenantID uuid.NullUUID
	var contextVersion int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT expires_at, active_tenant_id, context_version
FROM tutorhub.sessions
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > $3
  AND absolute_expires_at > $3
FOR UPDATE`,
		sessionID,
		userID,
		rotation.RotatedAt,
	).Scan(&result.ExpiresAt, &activeTenantID, &contextVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedTenantSession{}, ErrSessionNotFound
		}
		return lockedTenantSession{}, fmt.Errorf("lock tenant session: %w", err)
	}
	if contextVersion != rotation.ExpectedContextVersion {
		return lockedTenantSession{}, ErrSessionContextConflict
	}
	result.ActiveTenantID = activeTenantID.UUID

	return result, nil
}

func lockActiveTenantUser(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
) error {
	var lockedUserID uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT id FROM tutorhub.users WHERE id = $1 AND status = 'active' FOR UPDATE`,
		userID,
	).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("lock tenant owner: %w", err)
	}
	return nil
}

func hasNoActiveTenantMembership(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
) (bool, error) {
	var hasActiveMembership bool
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.memberships m
    JOIN tutorhub.tenants t ON t.id = m.tenant_id
    WHERE m.user_id = $1
      AND m.status = 'active'
      AND t.status = 'active'
)`,
		userID,
	).Scan(&hasActiveMembership); err != nil {
		return false, fmt.Errorf("check bootstrap tenant eligibility: %w", err)
	}
	return !hasActiveMembership, nil
}

func (repository *PostgresRepository) lockAndAuthorizeTenantCreationSource(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
	tenantID uuid.UUID,
) error {
	var role string
	if err := transaction.QueryRow(
		ctx,
		`SELECT m.role
FROM tutorhub.tenants t
JOIN tutorhub.memberships m
  ON m.tenant_id = t.id AND m.user_id = $2 AND m.status = 'active'
WHERE t.id = $1 AND t.status = 'active'
FOR SHARE OF t, m`,
		tenantID,
		userID,
	).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTenantCreationDenied
		}
		return fmt.Errorf("lock tenant creation source: %w", err)
	}

	tenantContext, err := tenancy.New(tenantID, userID)
	if err != nil {
		return ErrTenantCreationDenied
	}
	if err := repository.authorizeLockedTenantMembership(
		tenantContext,
		role,
		policy.ActionTenantManage,
	); err != nil {
		return ErrTenantCreationDenied
	}
	return nil
}

func updateTenantSession(
	ctx context.Context,
	transaction pgx.Tx,
	sessionID uuid.UUID,
	userID uuid.UUID,
	tenantID uuid.UUID,
	rotation SessionRotation,
) error {
	command, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.sessions
SET active_tenant_id = $3,
    token_hash = $4,
    csrf_token_hash = $5,
	    last_seen_at = $6,
	    context_version = context_version + 1
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > $6
	  AND absolute_expires_at > $6
	  AND context_version = $7`,
		sessionID,
		userID,
		nullUUID(tenantID),
		rotation.TokenHash,
		rotation.CSRFHash,
		rotation.RotatedAt,
		rotation.ExpectedContextVersion,
	)
	if err != nil {
		return fmt.Errorf("rotate active tenant session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrSessionContextConflict
	}

	return nil
}

func (repository *PostgresRepository) ListTenants(
	ctx context.Context,
	userID uuid.UUID,
) ([]Tenant, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin tenant list: %w", err)
	}
	defer rollback(transaction)

	rows, err := transaction.Query(
		queryContext,
		`SELECT t.id, t.slug, t.name, t.locale, t.timezone, t.status, t.version,
       m.role, t.created_at, t.updated_at, t.archived_at
FROM tutorhub.memberships m
JOIN tutorhub.tenants t ON t.id = m.tenant_id
WHERE m.user_id = $1
  AND m.status = 'active'
  AND t.status = 'active'
ORDER BY t.name ASC, t.id ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}

	tenants := make([]Tenant, 0)
	for rows.Next() {
		var tenant Tenant
		if err := scanTenant(rows, &tenant); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan tenant list: %w", err)
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate tenant list: %w", err)
	}
	rows.Close()

	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit tenant list: %w", err)
	}
	return tenants, nil
}

func (repository *PostgresRepository) GetTenant(
	ctx context.Context,
	tenantContext tenancy.Context,
) (Tenant, error) {
	if err := tenantContext.Validate(); err != nil {
		return Tenant{}, ErrTenantNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Tenant{}, fmt.Errorf("begin tenant lookup: %w", err)
	}
	defer rollback(transaction)

	var tenant Tenant
	err = scanTenant(
		transaction.QueryRow(
			queryContext,
			`SELECT t.id, t.slug, t.name, t.locale, t.timezone, t.status, t.version,
       m.role, t.created_at, t.updated_at, t.archived_at
FROM tutorhub.tenants t
JOIN tutorhub.memberships m
  ON m.tenant_id = t.id AND m.user_id = $2 AND m.status = 'active'
WHERE t.id = $1 AND t.status = 'active'`,
			tenantContext.TenantID,
			tenantContext.ActorID,
		),
		&tenant,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrTenantNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	tenant.IsActive = true

	if err := transaction.Commit(queryContext); err != nil {
		return Tenant{}, fmt.Errorf("commit tenant lookup: %w", err)
	}
	return tenant, nil
}

func (repository *PostgresRepository) UpdateTenant(
	ctx context.Context,
	tenantContext tenancy.Context,
	input UpdateTenantInput,
	updatedAt time.Time,
) (Tenant, error) {
	if err := tenantContext.Validate(); err != nil {
		return Tenant{}, ErrTenantNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Tenant{}, fmt.Errorf("begin tenant update: %w", err)
	}
	defer rollback(transaction)

	var currentVersion int64
	var role string
	if err := transaction.QueryRow(
		queryContext,
		`SELECT t.version, m.role
FROM tutorhub.tenants t
JOIN tutorhub.memberships m
  ON m.tenant_id = t.id AND m.user_id = $2 AND m.status = 'active'
WHERE t.id = $1 AND t.status = 'active'
FOR UPDATE OF t, m`,
		tenantContext.TenantID,
		tenantContext.ActorID,
	).Scan(&currentVersion, &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		return Tenant{}, fmt.Errorf("lock tenant update: %w", err)
	}
	if err := repository.authorizeLockedTenantMembership(
		tenantContext,
		role,
		policy.ActionTenantManage,
	); err != nil {
		return Tenant{}, err
	}
	if currentVersion != input.ExpectedVersion {
		return Tenant{}, ErrTenantVersionConflict
	}

	var tenant Tenant
	err = transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.tenants
SET name = COALESCE($2, name),
    slug = COALESCE($3, slug),
    locale = COALESCE($4, locale),
    timezone = COALESCE($5, timezone),
    version = version + 1,
    updated_at = $6
WHERE id = $1 AND status = 'active' AND version = $7
RETURNING id, slug, name, locale, timezone, status, version,
          created_at, updated_at, archived_at`,
		tenantContext.TenantID,
		nullableString(input.Name),
		nullableString(input.Slug),
		nullableString(input.Locale),
		nullableString(input.Timezone),
		updatedAt,
		input.ExpectedVersion,
	).Scan(
		&tenant.ID,
		&tenant.Slug,
		&tenant.Name,
		&tenant.Locale,
		&tenant.Timezone,
		&tenant.Status,
		&tenant.Version,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.ArchivedAt,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) &&
			postgresError.ConstraintName == "tenants_slug_unique" {
			return Tenant{}, ErrTenantSlugTaken
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, ErrTenantVersionConflict
		}
		return Tenant{}, fmt.Errorf("update tenant: %w", err)
	}
	tenant.Role = role
	tenant.IsActive = true

	if err := insertTenantEvent(
		queryContext,
		transaction,
		tenant.ID,
		tenantContext.ActorID,
		"tenant.updated",
		uuid.Nil,
		uuid.Nil,
		tenant.Version,
		updatedAt,
	); err != nil {
		return Tenant{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Tenant{}, fmt.Errorf("commit tenant update: %w", err)
	}
	return tenant, nil
}

func (repository *PostgresRepository) ArchiveTenant(
	ctx context.Context,
	tenantContext tenancy.Context,
	sessionID uuid.UUID,
	expectedVersion int64,
	rotation SessionRotation,
) (TenantArchiveMutationResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return TenantArchiveMutationResult{}, ErrTenantNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return TenantArchiveMutationResult{}, fmt.Errorf("begin tenant archive: %w", err)
	}
	defer rollback(transaction)

	var currentVersion int64
	var role string
	if err := transaction.QueryRow(
		queryContext,
		`SELECT t.version, m.role
FROM tutorhub.tenants t
JOIN tutorhub.memberships m
  ON m.tenant_id = t.id AND m.user_id = $2 AND m.status = 'active'
WHERE t.id = $1 AND t.status = 'active'
FOR UPDATE OF t, m`,
		tenantContext.TenantID,
		tenantContext.ActorID,
	).Scan(&currentVersion, &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TenantArchiveMutationResult{}, ErrTenantNotFound
		}
		return TenantArchiveMutationResult{}, fmt.Errorf("lock tenant archive: %w", err)
	}
	if err := repository.authorizeLockedTenantMembership(
		tenantContext,
		role,
		policy.ActionTenantManage,
	); err != nil {
		return TenantArchiveMutationResult{}, err
	}
	if currentVersion != expectedVersion {
		return TenantArchiveMutationResult{}, ErrTenantVersionConflict
	}
	lockedSession, err := lockTenantMutationSession(
		queryContext,
		transaction,
		sessionID,
		tenantContext.ActorID,
		rotation,
	)
	if err != nil {
		return TenantArchiveMutationResult{}, err
	}
	if lockedSession.ActiveTenantID != tenantContext.TenantID {
		return TenantArchiveMutationResult{}, ErrSessionContextConflict
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.sessions
SET active_tenant_id = NULL,
    context_version = context_version + 1
WHERE active_tenant_id = $1 AND id <> $2`,
		tenantContext.TenantID,
		sessionID,
	); err != nil {
		return TenantArchiveMutationResult{}, fmt.Errorf("clear archived tenant sessions: %w", err)
	}
	if err := lockActiveTenantUser(
		queryContext, transaction, tenantContext.ActorID,
	); err != nil {
		return TenantArchiveMutationResult{}, err
	}

	var hasOtherManagedTenant bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.memberships m
    JOIN tutorhub.tenants t ON t.id = m.tenant_id
    WHERE m.user_id = $1
      AND m.tenant_id <> $2
      AND m.role = 'org_admin'
      AND m.status = 'active'
      AND t.status = 'active'
)`,
		tenantContext.ActorID,
		tenantContext.TenantID,
	).Scan(&hasOtherManagedTenant); err != nil {
		return TenantArchiveMutationResult{}, fmt.Errorf("check managed tenant fallback: %w", err)
	}
	if !hasOtherManagedTenant {
		return TenantArchiveMutationResult{}, ErrLastManagedTenant
	}

	command, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.tenants
SET status = 'archived',
    archived_at = $3,
    updated_at = $3,
    version = version + 1
WHERE id = $1 AND status = 'active' AND version = $2`,
		tenantContext.TenantID,
		expectedVersion,
		rotation.RotatedAt,
	)
	if err != nil {
		return TenantArchiveMutationResult{}, fmt.Errorf("archive tenant: %w", err)
	}
	if command.RowsAffected() != 1 {
		return TenantArchiveMutationResult{}, ErrTenantVersionConflict
	}

	if err := updateTenantSession(
		queryContext,
		transaction,
		sessionID,
		tenantContext.ActorID,
		uuid.Nil,
		rotation,
	); err != nil {
		return TenantArchiveMutationResult{}, err
	}

	archivedVersion := expectedVersion + 1
	if err := insertTenantEvent(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
		"tenant.archived",
		uuid.Nil,
		uuid.Nil,
		archivedVersion,
		rotation.RotatedAt,
	); err != nil {
		return TenantArchiveMutationResult{}, err
	}
	principal, err := repository.loadPrincipal(
		queryContext,
		transaction,
		sessionID,
		tenantContext.ActorID,
		uuid.Nil,
	)
	if err != nil {
		return TenantArchiveMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return TenantArchiveMutationResult{}, fmt.Errorf("commit tenant archive: %w", err)
	}
	return TenantArchiveMutationResult{
		Principal: principal,
		ExpiresAt: lockedSession.ExpiresAt,
	}, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTenant(row scanner, tenant *Tenant) error {
	return row.Scan(
		&tenant.ID,
		&tenant.Slug,
		&tenant.Name,
		&tenant.Locale,
		&tenant.Timezone,
		&tenant.Status,
		&tenant.Version,
		&tenant.Role,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.ArchivedAt,
	)
}

func (repository *PostgresRepository) authorizeLockedTenantMembership(
	tenantContext tenancy.Context,
	role string,
	action policy.Action,
) error {
	if repository.authorizer == nil {
		return ErrTenantAccessDenied
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID:          tenantContext.ActorID,
			ActiveTenantID:   tenantContext.TenantID,
			MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{
				policy.OrganizationRole(role),
			},
		},
		Action: action,
		Resource: policy.Resource{
			TenantID: tenantContext.TenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return ErrTenantAccessDenied
	}
	return nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func insertTenantEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	eventType string,
	fromTenantID uuid.UUID,
	toTenantID uuid.UUID,
	version int64,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    occurred_at,
    available_at
)
VALUES (
    $1,
    'tenant',
    $1,
    $3,
    jsonb_strip_nulls(jsonb_build_object(
        'actor_user_id', $2::uuid,
        'from_tenant_id', $4::uuid,
        'to_tenant_id', $5::uuid,
        'version', $6::bigint
    )),
    $7,
    $7
)`,
		tenantID,
		actorID,
		eventType,
		nullUUID(fromTenantID),
		nullUUID(toTenantID),
		version,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		"effect":  strings.TrimPrefix(eventType, "tenant."),
		"version": fmt.Sprintf("%d", version),
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      tenantID,
		ActorID:       actorID,
		EventType:     eventType,
		AggregateType: "tenant",
		AggregateID:   tenantID,
		Metadata:      metadata,
		OccurredAt:    occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func resolveIdentity(
	ctx context.Context,
	transaction pgx.Tx,
	claims ProviderClaims,
	authenticatedAt time.Time,
) (uuid.UUID, uuid.UUID, error) {
	lockKey := fmt.Sprintf(
		"issuer:%d:%s:subject:%s",
		len(claims.Issuer),
		strings.ToLower(claims.Issuer),
		claims.Subject,
	)
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		lockKey,
	); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("lock identity mapping: %w", err)
	}

	const findIdentity = `
SELECT id, user_id, status
FROM tutorhub.identities
WHERE provider = $1 AND subject = $2
FOR UPDATE`
	var identityID uuid.UUID
	var userID uuid.UUID
	var identityStatus string
	err := transaction.QueryRow(ctx, findIdentity, claims.Issuer, claims.Subject).Scan(
		&identityID,
		&userID,
		&identityStatus,
	)
	if err == nil {
		if identityStatus != "active" {
			return uuid.Nil, uuid.Nil, ErrIdentityInactive
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.identities
SET email_at_provider = $2,
    email_verified = $3,
    last_authenticated_at = $4,
    updated_at = $4
WHERE id = $1`,
			identityID,
			claims.Email,
			claims.EmailVerified,
			authenticatedAt,
		); err != nil {
			return uuid.Nil, uuid.Nil, fmt.Errorf("update identity mapping: %w", err)
		}
		return identityID, userID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, fmt.Errorf("find identity mapping: %w", err)
	}

	emailLockKey := "email:" + strings.ToLower(claims.Email)
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		emailLockKey,
	); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("lock identity email: %w", err)
	}

	if err := transaction.QueryRow(
		ctx,
		`SELECT id FROM tutorhub.users WHERE email = $1 FOR UPDATE`,
		strings.ToLower(claims.Email),
	).Scan(&userID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, fmt.Errorf("find identity user: %w", err)
		}
		if err := transaction.QueryRow(
			ctx,
			`INSERT INTO tutorhub.users (email, display_name, locale)
VALUES ($1, $2, $3)
RETURNING id`,
			strings.ToLower(claims.Email),
			claims.DisplayName,
			claims.Locale,
		).Scan(&userID); err != nil {
			return uuid.Nil, uuid.Nil, fmt.Errorf("insert identity user: %w", err)
		}
	} else {
		return uuid.Nil, uuid.Nil, ErrIdentityLinkRequired
	}

	if err := transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.identities (
    user_id,
    provider,
    subject,
    email_at_provider,
    email_verified,
    last_authenticated_at
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id`,
		userID,
		claims.Issuer,
		claims.Subject,
		claims.Email,
		claims.EmailVerified,
		authenticatedAt,
	).Scan(&identityID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert identity mapping: %w", err)
	}

	return identityID, userID, nil
}

func (repository *PostgresRepository) GetProfile(
	ctx context.Context,
	userID uuid.UUID,
) (User, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return User{}, fmt.Errorf("begin get profile: %w", err)
	}
	defer rollback(transaction)

	profile, err := getProfile(queryContext, transaction, userID)
	if err != nil {
		return User{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return User{}, fmt.Errorf("commit get profile: %w", err)
	}

	return profile, nil
}

func (repository *PostgresRepository) UpdateProfile(
	ctx context.Context,
	sessionID uuid.UUID,
	userID uuid.UUID,
	patch ProfilePatch,
	updatedAt time.Time,
) (User, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return User{}, fmt.Errorf("begin update profile: %w", err)
	}
	defer rollback(transaction)

	tenantID, err := lockOwnedSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		updatedAt,
	)
	if err != nil {
		return User{}, err
	}

	avatarValue := any(nil)
	if patch.AvatarObjectKey != nil && *patch.AvatarObjectKey != "" {
		avatarValue = *patch.AvatarObjectKey
	}
	var profile User
	if err := transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.users
SET display_name = COALESCE($2, display_name),
    locale = COALESCE($3, locale),
    timezone = COALESCE($4, timezone),
    avatar_object_key = CASE WHEN $5 THEN $6 ELSE avatar_object_key END,
    updated_at = $7
WHERE id = $1 AND status = 'active'
RETURNING id, email, display_name, locale, timezone, COALESCE(avatar_object_key, '')`,
		userID,
		patch.DisplayName,
		patch.Locale,
		patch.Timezone,
		patch.AvatarObjectKey != nil,
		avatarValue,
		updatedAt,
	).Scan(
		&profile.ID,
		&profile.Email,
		&profile.DisplayName,
		&profile.Locale,
		&profile.Timezone,
		&profile.AvatarObjectKey,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrSessionNotFound
		}
		return User{}, fmt.Errorf("update profile: %w", err)
	}

	if err := insertIdentityEvent(
		queryContext,
		transaction,
		tenantID,
		userID,
		"identity.profile.updated",
		sessionID,
		updatedAt,
	); err != nil {
		return User{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return User{}, fmt.Errorf("commit update profile: %w", err)
	}

	return profile, nil
}

func (repository *PostgresRepository) ListIdentities(
	ctx context.Context,
	userID uuid.UUID,
) ([]ExternalIdentity, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin list identities: %w", err)
	}
	defer rollback(transaction)

	rows, err := transaction.Query(
		queryContext,
		`SELECT id,
       provider,
       COALESCE(email_at_provider, ''),
       email_verified,
       created_at,
       COALESCE(last_authenticated_at, created_at)
FROM tutorhub.identities
WHERE user_id = $1 AND status = 'active'
ORDER BY created_at ASC, id ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	identities := make([]ExternalIdentity, 0)
	for rows.Next() {
		var externalIdentity ExternalIdentity
		if err := rows.Scan(
			&externalIdentity.ID,
			&externalIdentity.Provider,
			&externalIdentity.Email,
			&externalIdentity.EmailVerified,
			&externalIdentity.CreatedAt,
			&externalIdentity.LastAuthenticatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		identities = append(identities, externalIdentity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identities: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit list identities: %w", err)
	}

	return identities, nil
}

func (repository *PostgresRepository) LinkIdentity(
	ctx context.Context,
	userID uuid.UUID,
	sessionID uuid.UUID,
	claims ProviderClaims,
	linkedAt time.Time,
) (ExternalIdentity, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ExternalIdentity{}, fmt.Errorf("begin link identity: %w", err)
	}
	defer rollback(transaction)

	tenantID, err := lockOwnedSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		linkedAt,
	)
	if err != nil {
		return ExternalIdentity{}, err
	}
	if err := lockIdentityUser(queryContext, transaction, userID); err != nil {
		return ExternalIdentity{}, err
	}
	if err := lockProviderSubject(queryContext, transaction, claims); err != nil {
		return ExternalIdentity{}, err
	}

	var existing ExternalIdentity
	var existingUserID uuid.UUID
	var status string
	err = transaction.QueryRow(
		queryContext,
		`SELECT id,
       user_id,
       provider,
       COALESCE(email_at_provider, ''),
       email_verified,
       created_at,
       COALESCE(last_authenticated_at, created_at),
       status
FROM tutorhub.identities
WHERE provider = $1 AND subject = $2
FOR UPDATE`,
		claims.Issuer,
		claims.Subject,
	).Scan(
		&existing.ID,
		&existingUserID,
		&existing.Provider,
		&existing.Email,
		&existing.EmailVerified,
		&existing.CreatedAt,
		&existing.LastAuthenticatedAt,
		&status,
	)
	if err == nil {
		if existingUserID != userID {
			return ExternalIdentity{}, ErrIdentityConflict
		}
		if _, err := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.identities
SET email_at_provider = $2,
    email_verified = $3,
    last_authenticated_at = $4,
    status = 'active',
    unlinked_at = NULL,
    updated_at = $4
WHERE id = $1`,
			existing.ID,
			claims.Email,
			claims.EmailVerified,
			linkedAt,
		); err != nil {
			return ExternalIdentity{}, fmt.Errorf("reactivate identity: %w", err)
		}
		existing.Email = claims.Email
		existing.EmailVerified = claims.EmailVerified
		existing.LastAuthenticatedAt = linkedAt
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return ExternalIdentity{}, fmt.Errorf("find linked identity: %w", err)
		}
		if err := transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.identities (
    user_id,
    provider,
    subject,
    email_at_provider,
    email_verified,
    last_authenticated_at,
    status
)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
RETURNING id, provider, COALESCE(email_at_provider, ''), email_verified, created_at,
          COALESCE(last_authenticated_at, created_at)`,
			userID,
			claims.Issuer,
			claims.Subject,
			claims.Email,
			claims.EmailVerified,
			linkedAt,
		).Scan(
			&existing.ID,
			&existing.Provider,
			&existing.Email,
			&existing.EmailVerified,
			&existing.CreatedAt,
			&existing.LastAuthenticatedAt,
		); err != nil {
			return ExternalIdentity{}, fmt.Errorf("insert linked identity: %w", err)
		}
	}

	if err := insertIdentityEvent(
		queryContext,
		transaction,
		tenantID,
		userID,
		"identity.linked",
		existing.ID,
		linkedAt,
	); err != nil {
		return ExternalIdentity{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ExternalIdentity{}, fmt.Errorf("commit link identity: %w", err)
	}

	return existing, nil
}

func (repository *PostgresRepository) UnlinkIdentity(
	ctx context.Context,
	userID uuid.UUID,
	sessionID uuid.UUID,
	identityID uuid.UUID,
	unlinkedAt time.Time,
) error {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin unlink identity: %w", err)
	}
	defer rollback(transaction)

	tenantID, err := lockOwnedSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		unlinkedAt,
	)
	if err != nil {
		return err
	}
	if err := lockIdentityUser(queryContext, transaction, userID); err != nil {
		return err
	}

	var identityOwner uuid.UUID
	var identityStatus string
	if err := transaction.QueryRow(
		queryContext,
		`SELECT user_id, status
FROM tutorhub.identities
WHERE id = $1
FOR UPDATE`,
		identityID,
	).Scan(&identityOwner, &identityStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIdentityNotFound
		}
		return fmt.Errorf("lock identity to unlink: %w", err)
	}
	if identityOwner != userID || identityStatus != "active" {
		return ErrIdentityNotFound
	}

	var activeIdentityCount int
	if err := transaction.QueryRow(
		queryContext,
		`SELECT count(*)
FROM tutorhub.identities
WHERE user_id = $1 AND status = 'active'`,
		userID,
	).Scan(&activeIdentityCount); err != nil {
		return fmt.Errorf("count active identities: %w", err)
	}
	if activeIdentityCount <= 1 {
		return ErrLastIdentity
	}

	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.identities
SET status = 'unlinked', unlinked_at = $2, updated_at = $2
WHERE id = $1`,
		identityID,
		unlinkedAt,
	); err != nil {
		return fmt.Errorf("unlink identity: %w", err)
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.sessions
SET revoked_at = $3, revoked_reason = 'identity_unlinked'
WHERE user_id = $1
  AND identity_id = $2
  AND id <> $4
  AND revoked_at IS NULL`,
		userID,
		identityID,
		unlinkedAt,
		sessionID,
	); err != nil {
		return fmt.Errorf("revoke unlinked identity sessions: %w", err)
	}

	if err := insertIdentityEvent(
		queryContext,
		transaction,
		tenantID,
		userID,
		"identity.unlinked",
		identityID,
		unlinkedAt,
	); err != nil {
		return err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return fmt.Errorf("commit unlink identity: %w", err)
	}

	return nil
}

func getProfile(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
) (User, error) {
	var profile User
	if err := transaction.QueryRow(
		ctx,
		`SELECT id, email, display_name, locale, timezone, COALESCE(avatar_object_key, '')
FROM tutorhub.users
WHERE id = $1 AND status = 'active'`,
		userID,
	).Scan(
		&profile.ID,
		&profile.Email,
		&profile.DisplayName,
		&profile.Locale,
		&profile.Timezone,
		&profile.AvatarObjectKey,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrSessionNotFound
		}
		return User{}, fmt.Errorf("get profile: %w", err)
	}
	return profile, nil
}

func lockOwnedSession(
	ctx context.Context,
	transaction pgx.Tx,
	sessionID uuid.UUID,
	userID uuid.UUID,
	now time.Time,
) (uuid.UUID, error) {
	var activeTenantID uuid.NullUUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id
FROM tutorhub.sessions
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > $3
  AND absolute_expires_at > $3
FOR UPDATE`,
		sessionID,
		userID,
		now,
	).Scan(&activeTenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrSessionNotFound
		}
		return uuid.Nil, fmt.Errorf("lock profile session: %w", err)
	}
	return activeTenantID.UUID, nil
}

func lockProviderSubject(
	ctx context.Context,
	transaction pgx.Tx,
	claims ProviderClaims,
) error {
	lockKey := fmt.Sprintf(
		"issuer:%d:%s:subject:%s",
		len(claims.Issuer),
		strings.ToLower(claims.Issuer),
		claims.Subject,
	)
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		lockKey,
	); err != nil {
		return fmt.Errorf("lock identity mapping: %w", err)
	}
	return nil
}

func lockIdentityUser(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
) error {
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"identity-user:"+userID.String(),
	); err != nil {
		return fmt.Errorf("lock identity user: %w", err)
	}
	return nil
}

func insertIdentityEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	userID uuid.UUID,
	eventType string,
	targetID uuid.UUID,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    occurred_at,
    available_at
)
VALUES (
    $1,
    'user',
    $2,
    $3,
    jsonb_build_object(
        'actor_user_id', $2::uuid,
        'target_id', $4::uuid
    ),
    $5,
    $5
)`,
		nullUUID(tenantID),
		userID,
		eventType,
		targetID,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s event: %w", eventType, err)
	}
	return nil
}

func selectActiveTenant(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT m.tenant_id
FROM tutorhub.memberships m
JOIN tutorhub.tenants t ON t.id = m.tenant_id
WHERE m.user_id = $1
  AND m.status = 'active'
  AND t.status = 'active'
ORDER BY m.joined_at ASC NULLS LAST, m.created_at ASC, m.tenant_id ASC
LIMIT 1`,
		userID,
	).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("select active tenant: %w", err)
	}

	return tenantID, nil
}

func (repository *PostgresRepository) loadPrincipal(
	ctx context.Context,
	transaction pgx.Tx,
	sessionID uuid.UUID,
	userID uuid.UUID,
	activeTenantID uuid.UUID,
) (Principal, error) {
	principal := Principal{SessionID: sessionID}
	var identityID uuid.NullUUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT identity_id, auth_time, context_version
FROM tutorhub.sessions
WHERE id = $1 AND user_id = $2`,
		sessionID,
		userID,
	).Scan(
		&identityID,
		&principal.AuthenticatedAt,
		&principal.ContextVersion,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, ErrSessionNotFound
		}
		return Principal{}, fmt.Errorf("load session authentication: %w", err)
	}
	principal.IdentityID = identityID.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT id, email, display_name, locale, timezone, COALESCE(avatar_object_key, '')
FROM tutorhub.users
WHERE id = $1 AND status = 'active'`,
		userID,
	).Scan(
		&principal.User.ID,
		&principal.User.Email,
		&principal.User.DisplayName,
		&principal.User.Locale,
		&principal.User.Timezone,
		&principal.User.AvatarObjectKey,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, ErrSessionNotFound
		}
		return Principal{}, fmt.Errorf("load session user: %w", err)
	}

	rows, err := transaction.Query(
		ctx,
		`SELECT t.id, t.slug, t.name, t.locale, t.timezone, t.status, t.version,
       m.role, t.created_at, t.updated_at, t.archived_at
FROM tutorhub.memberships m
JOIN tutorhub.tenants t ON t.id = m.tenant_id
WHERE m.user_id = $1
  AND m.status = 'active'
  AND t.status = 'active'
ORDER BY t.name ASC, t.id ASC`,
		userID,
	)
	if err != nil {
		return Principal{}, fmt.Errorf("load session memberships: %w", err)
	}
	defer rows.Close()

	principal.Memberships = make([]Tenant, 0)
	for rows.Next() {
		var membership Tenant
		if err := scanTenant(rows, &membership); err != nil {
			return Principal{}, fmt.Errorf("scan session membership: %w", err)
		}
		membership.IsActive = membership.ID == activeTenantID
		principal.Memberships = append(principal.Memberships, membership)
		if membership.IsActive {
			active := membership
			principal.ActiveTenant = &active
			if repository.authorizer != nil {
				principal.Permissions = policy.PermissionStrings(
					repository.authorizer.EffectivePermissions(policy.Subject{
						ActorID:          principal.User.ID,
						ActiveTenantID:   membership.ID,
						MembershipActive: true,
						OrganizationRoles: []policy.OrganizationRole{
							policy.OrganizationRole(membership.Role),
						},
					}),
				)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Principal{}, fmt.Errorf("iterate session memberships: %w", err)
	}
	if principal.Permissions == nil {
		principal.Permissions = []string{}
	}

	return principal, nil
}

func nullUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func rollback(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}

func (repository *PostgresRepository) contextWithTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if repository.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, repository.queryTimeout)
}
