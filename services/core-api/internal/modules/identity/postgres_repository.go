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
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type Database interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     Database
	queryTimeout time.Duration
	authorizer   policy.Authorizer
}

func NewPostgresRepository(
	database Database,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
) *PostgresRepository {
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer,
	}
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
    created_at,
    expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if _, err := transaction.Exec(
		queryContext,
		insertFlow,
		params.StateHash,
		params.BrowserBindingHash,
		params.NonceHash,
		params.CodeVerifierCiphertext,
		params.ReturnTo,
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
RETURNING nonce_hash, code_verifier_ciphertext, return_to`
	var flow StoredFlow
	if err := transaction.QueryRow(
		queryContext,
		consumeFlow,
		stateHash,
		browserBindingHash,
		consumedAt,
	).Scan(&flow.NonceHash, &flow.CodeVerifierCiphertext, &flow.ReturnTo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredFlow{}, ErrInvalidAuthFlow
		}
		return StoredFlow{}, fmt.Errorf("consume authentication flow: %w", err)
	}

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

	expiresAt, err := lockTenantMutationSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		rotation.RotatedAt,
	)
	if err != nil {
		return TenantMutationResult{}, err
	}
	if err := ensureFirstTenantCreation(
		queryContext,
		transaction,
		userID,
	); err != nil {
		return TenantMutationResult{}, err
	}

	var tenantID uuid.UUID
	if err := transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.tenants (slug, name)
VALUES ($1, $2)
RETURNING id`,
		input.Slug,
		input.Name,
	).Scan(&tenantID); err != nil {
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

	if _, err := transaction.Exec(
		queryContext,
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
    'tenant.created',
    jsonb_build_object(
        'actor_user_id', $2::uuid,
        'name', $3::text,
        'slug', $4::text
    ),
    $5,
    $5
)`,
		tenantID,
		userID,
		input.Name,
		input.Slug,
		rotation.RotatedAt,
	); err != nil {
		return TenantMutationResult{}, fmt.Errorf("insert tenant creation event: %w", err)
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

	return TenantMutationResult{Principal: principal, ExpiresAt: expiresAt}, nil
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

	expiresAt, err := lockTenantMutationSession(
		queryContext,
		transaction,
		sessionID,
		userID,
		rotation.RotatedAt,
	)
	if err != nil {
		return TenantMutationResult{}, err
	}

	var membershipExists bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.memberships m
    JOIN tutorhub.tenants t ON t.id = m.tenant_id
    WHERE m.user_id = $1
      AND m.tenant_id = $2
      AND m.status = 'active'
      AND t.status = 'active'
)`,
		userID,
		tenantID,
	).Scan(&membershipExists); err != nil {
		return TenantMutationResult{}, fmt.Errorf("check tenant membership: %w", err)
	}
	if !membershipExists {
		return TenantMutationResult{}, ErrTenantAccessDenied
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

	return TenantMutationResult{Principal: principal, ExpiresAt: expiresAt}, nil
}

func lockTenantMutationSession(
	ctx context.Context,
	transaction pgx.Tx,
	sessionID uuid.UUID,
	userID uuid.UUID,
	now time.Time,
) (time.Time, error) {
	var expiresAt time.Time
	if err := transaction.QueryRow(
		ctx,
		`SELECT expires_at
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
	).Scan(&expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, ErrSessionNotFound
		}
		return time.Time{}, fmt.Errorf("lock tenant session: %w", err)
	}

	return expiresAt, nil
}

func ensureFirstTenantCreation(
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

	var activeMembershipExists bool
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.memberships
    WHERE user_id = $1 AND status = 'active'
)`,
		userID,
	).Scan(&activeMembershipExists); err != nil {
		return fmt.Errorf("check existing tenant memberships: %w", err)
	}
	if activeMembershipExists {
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
    last_seen_at = $6
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > $6
  AND absolute_expires_at > $6`,
		sessionID,
		userID,
		tenantID,
		rotation.TokenHash,
		rotation.CSRFHash,
		rotation.RotatedAt,
	)
	if err != nil {
		return fmt.Errorf("rotate active tenant session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrSessionNotFound
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
SELECT id, user_id
FROM tutorhub.identities
WHERE provider = $1 AND subject = $2
FOR UPDATE`
	var identityID uuid.UUID
	var userID uuid.UUID
	err := transaction.QueryRow(ctx, findIdentity, claims.Issuer, claims.Subject).Scan(
		&identityID,
		&userID,
	)
	if err == nil {
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
		if err := updateUserProfile(ctx, transaction, userID, claims, authenticatedAt); err != nil {
			return uuid.Nil, uuid.Nil, err
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
	} else if err := updateUserProfile(ctx, transaction, userID, claims, authenticatedAt); err != nil {
		return uuid.Nil, uuid.Nil, err
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

func updateUserProfile(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
	claims ProviderClaims,
	authenticatedAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.users
SET display_name = $2,
    locale = $3,
    updated_at = $4
WHERE id = $1`,
		userID,
		claims.DisplayName,
		claims.Locale,
		authenticatedAt,
	); err != nil {
		return fmt.Errorf("update identity user profile: %w", err)
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
		`SELECT tenant_id
FROM tutorhub.memberships
WHERE user_id = $1 AND status = 'active'
ORDER BY joined_at ASC NULLS LAST, created_at ASC, tenant_id ASC
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
	if err := transaction.QueryRow(
		ctx,
		`SELECT id, email, display_name, locale, timezone
FROM tutorhub.users
WHERE id = $1 AND status = 'active'`,
		userID,
	).Scan(
		&principal.User.ID,
		&principal.User.Email,
		&principal.User.DisplayName,
		&principal.User.Locale,
		&principal.User.Timezone,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, ErrSessionNotFound
		}
		return Principal{}, fmt.Errorf("load session user: %w", err)
	}

	rows, err := transaction.Query(
		ctx,
		`SELECT t.id, t.slug, t.name, m.role
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
		if err := rows.Scan(&membership.ID, &membership.Slug, &membership.Name, &membership.Role); err != nil {
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
