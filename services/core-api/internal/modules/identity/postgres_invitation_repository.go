package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const membershipInvitationSelectColumns = `
    id,
    tenant_id,
    email,
    intended_role,
    status,
    expires_at,
    accepted_at,
    revoked_at,
    created_at,
    updated_at,
    invited_by,
    accepted_by,
    revoked_by`

func (repository *PostgresRepository) ListMembershipInvitations(
	ctx context.Context,
	tenantContext tenancy.Context,
	now time.Time,
) ([]MembershipInvitation, error) {
	if err := tenantContext.Validate(); err != nil {
		return nil, ErrTenantNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin membership invitation list: %w", err)
	}
	defer rollback(transaction)

	if err := repository.lockAndAuthorizeInvitationAdmin(
		queryContext,
		transaction,
		tenantContext,
	); err != nil {
		return nil, err
	}
	if err := expireTenantMembershipInvitations(
		queryContext,
		transaction,
		tenantContext.TenantID,
		now,
	); err != nil {
		return nil, err
	}

	rows, err := transaction.Query(
		queryContext,
		`SELECT `+membershipInvitationSelectColumns+`
FROM tutorhub.membership_invitations
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 100`,
		tenantContext.TenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list membership invitations: %w", err)
	}
	defer rows.Close()

	invitations := make([]MembershipInvitation, 0)
	for rows.Next() {
		var invitation MembershipInvitation
		if err := scanMembershipInvitation(rows, &invitation); err != nil {
			return nil, fmt.Errorf("scan membership invitation: %w", err)
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate membership invitations: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit membership invitation list: %w", err)
	}
	return invitations, nil
}

func (repository *PostgresRepository) CreateMembershipInvitation(
	ctx context.Context,
	tenantContext tenancy.Context,
	params CreateMembershipInvitationParams,
) (MembershipInvitation, error) {
	if err := tenantContext.Validate(); err != nil {
		return MembershipInvitation{}, ErrTenantNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MembershipInvitation{}, fmt.Errorf("begin membership invitation creation: %w", err)
	}
	defer rollback(transaction)

	if err := repository.lockAndAuthorizeInvitationAdmin(
		queryContext,
		transaction,
		tenantContext,
	); err != nil {
		return MembershipInvitation{}, err
	}
	if err := lockMembershipInvitationEmail(
		queryContext,
		transaction,
		tenantContext.TenantID,
		params.Email,
	); err != nil {
		return MembershipInvitation{}, err
	}
	if err := expireTenantMembershipInvitationEmail(
		queryContext,
		transaction,
		tenantContext.TenantID,
		params.Email,
		params.CreatedAt,
	); err != nil {
		return MembershipInvitation{}, err
	}

	var membershipExists bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.memberships m
    JOIN tutorhub.users u ON u.id = m.user_id
    WHERE m.tenant_id = $1
      AND (
        u.email = $2
        OR EXISTS (
            SELECT 1
            FROM tutorhub.identities i
            WHERE i.user_id = m.user_id
              AND i.status = 'active'
              AND i.email_verified = true
              AND lower(btrim(COALESCE(i.email_at_provider, ''))) = $2
        )
      )
)`,
		tenantContext.TenantID,
		params.Email,
	).Scan(&membershipExists); err != nil {
		return MembershipInvitation{}, fmt.Errorf("check existing invited membership: %w", err)
	}
	if membershipExists {
		return MembershipInvitation{}, ErrMembershipInvitationConflict
	}

	var pendingExists bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.membership_invitations
    WHERE tenant_id = $1 AND email = $2 AND status = 'pending'
)`,
		tenantContext.TenantID,
		params.Email,
	).Scan(&pendingExists); err != nil {
		return MembershipInvitation{}, fmt.Errorf("check pending membership invitation: %w", err)
	}
	if pendingExists {
		return MembershipInvitation{}, ErrMembershipInvitationConflict
	}

	var invitation MembershipInvitation
	err = scanMembershipInvitation(
		transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.membership_invitations (
    tenant_id,
    email,
    intended_role,
    token_hash,
    status,
    expires_at,
    invited_by,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $7)
RETURNING `+membershipInvitationSelectColumns,
			tenantContext.TenantID,
			params.Email,
			params.IntendedRole,
			params.TokenHash,
			params.ExpiresAt,
			tenantContext.ActorID,
			params.CreatedAt,
		),
		&invitation,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) &&
			(postgresError.ConstraintName == "membership_invitations_pending_email_unique_idx" ||
				postgresError.ConstraintName == "membership_invitations_token_hash_unique") {
			return MembershipInvitation{}, ErrMembershipInvitationConflict
		}
		return MembershipInvitation{}, fmt.Errorf("insert membership invitation: %w", err)
	}
	if err := insertMembershipInvitationEvent(
		queryContext,
		transaction,
		invitation,
		"membership.invitation.created",
		tenantContext.ActorID,
		uuid.Nil,
		params.CreatedAt,
	); err != nil {
		return MembershipInvitation{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MembershipInvitation{}, fmt.Errorf("commit membership invitation creation: %w", err)
	}
	return invitation, nil
}

func (repository *PostgresRepository) RevokeMembershipInvitation(
	ctx context.Context,
	tenantContext tenancy.Context,
	invitationID uuid.UUID,
	now time.Time,
) (MembershipInvitation, error) {
	if err := tenantContext.Validate(); err != nil {
		return MembershipInvitation{}, ErrTenantNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MembershipInvitation{}, fmt.Errorf("begin membership invitation revoke: %w", err)
	}
	defer rollback(transaction)

	if err := repository.lockAndAuthorizeInvitationAdmin(
		queryContext,
		transaction,
		tenantContext,
	); err != nil {
		return MembershipInvitation{}, err
	}
	invitation, err := lockMembershipInvitation(
		queryContext,
		transaction,
		tenantContext.TenantID,
		invitationID,
	)
	if err != nil {
		return MembershipInvitation{}, err
	}
	if invitation.Status == MembershipInvitationPending && !now.Before(invitation.ExpiresAt) {
		if err := expireLockedMembershipInvitation(
			queryContext,
			transaction,
			&invitation,
			now,
		); err != nil {
			return MembershipInvitation{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MembershipInvitation{}, fmt.Errorf("commit expired membership invitation: %w", err)
		}
		return MembershipInvitation{}, ErrMembershipInvitationConflict
	}
	switch invitation.Status {
	case MembershipInvitationRevoked:
		if err := transaction.Commit(queryContext); err != nil {
			return MembershipInvitation{}, fmt.Errorf("commit repeated invitation revoke: %w", err)
		}
		return invitation, nil
	case MembershipInvitationPending:
		// Continue with the only valid transition below.
	default:
		return MembershipInvitation{}, ErrMembershipInvitationConflict
	}

	err = scanMembershipInvitation(
		transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.membership_invitations
SET status = 'revoked',
    revoked_at = $3,
    revoked_by = $4,
    updated_at = $3
WHERE tenant_id = $1 AND id = $2 AND status = 'pending'
RETURNING `+membershipInvitationSelectColumns,
			tenantContext.TenantID,
			invitationID,
			now,
			tenantContext.ActorID,
		),
		&invitation,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MembershipInvitation{}, ErrMembershipInvitationConflict
		}
		return MembershipInvitation{}, fmt.Errorf("revoke membership invitation: %w", err)
	}
	if err := insertMembershipInvitationEvent(
		queryContext,
		transaction,
		invitation,
		"membership.invitation.revoked",
		tenantContext.ActorID,
		uuid.Nil,
		now,
	); err != nil {
		return MembershipInvitation{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MembershipInvitation{}, fmt.Errorf("commit membership invitation revoke: %w", err)
	}
	return invitation, nil
}

func (repository *PostgresRepository) PreviewMembershipInvitation(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) (StoredMembershipInvitationPreview, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return StoredMembershipInvitationPreview{}, fmt.Errorf("begin membership invitation preview: %w", err)
	}
	defer rollback(transaction)

	tenantID, err := lookupMembershipInvitationTenant(
		queryContext,
		transaction,
		tokenHash,
	)
	if err != nil {
		return StoredMembershipInvitationPreview{}, err
	}
	tenantName, err := lockActiveInvitationTenant(queryContext, transaction, tenantID)
	if err != nil {
		return StoredMembershipInvitationPreview{}, err
	}
	invitation, err := lockMembershipInvitationByToken(
		queryContext,
		transaction,
		tenantID,
		tokenHash,
	)
	if err != nil {
		return StoredMembershipInvitationPreview{}, err
	}
	if invitation.Status == MembershipInvitationPending && !now.Before(invitation.ExpiresAt) {
		if err := expireLockedMembershipInvitation(
			queryContext,
			transaction,
			&invitation,
			now,
		); err != nil {
			return StoredMembershipInvitationPreview{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return StoredMembershipInvitationPreview{}, fmt.Errorf("commit expired invitation preview: %w", err)
		}
		return StoredMembershipInvitationPreview{}, ErrMembershipInvitationUnavailable
	}
	if invitation.Status != MembershipInvitationPending {
		return StoredMembershipInvitationPreview{}, ErrMembershipInvitationUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return StoredMembershipInvitationPreview{}, fmt.Errorf("commit membership invitation preview: %w", err)
	}
	return StoredMembershipInvitationPreview{
		Invitation: invitation,
		TenantName: tenantName,
	}, nil
}

func (repository *PostgresRepository) AcceptMembershipInvitation(
	ctx context.Context,
	params AcceptMembershipInvitationParams,
) (AcceptMembershipInvitationResult, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AcceptMembershipInvitationResult{}, fmt.Errorf("begin membership invitation acceptance: %w", err)
	}
	defer rollback(transaction)

	tenantID, err := lookupMembershipInvitationTenant(
		queryContext,
		transaction,
		params.TokenHash,
	)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	requestmeta.SetAuditTenant(queryContext, tenantID)
	if _, err := lockActiveInvitationTenant(queryContext, transaction, tenantID); err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	// Preserve the shared lock order: tenant -> session -> identity-user advisory
	// lock -> membership -> invitation. Admin revoke also locks its actor membership
	// before the invitation, so accepting as that same user cannot form a lock cycle.
	activeTenantID, err := lockInvitationAcceptanceSession(
		queryContext,
		transaction,
		params.SessionID,
		params.UserID,
		params.AcceptedAt,
	)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	if err := lockIdentityUser(queryContext, transaction, params.UserID); err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	membershipID, role, status, err := lockInvitationMembership(
		queryContext,
		transaction,
		tenantID,
		params.UserID,
	)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	invitation, err := lockMembershipInvitationByToken(
		queryContext,
		transaction,
		tenantID,
		params.TokenHash,
	)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	if invitation.Status == MembershipInvitationPending &&
		!params.AcceptedAt.Before(invitation.ExpiresAt) {
		if err := expireLockedMembershipInvitation(
			queryContext,
			transaction,
			&invitation,
			params.AcceptedAt,
		); err != nil {
			return AcceptMembershipInvitationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return AcceptMembershipInvitationResult{}, fmt.Errorf("commit expired invitation acceptance: %w", err)
		}
		return AcceptMembershipInvitationResult{}, ErrMembershipInvitationUnavailable
	}

	if invitation.Status == MembershipInvitationAccepted {
		if invitation.AcceptedBy == nil || *invitation.AcceptedBy != params.UserID {
			return AcceptMembershipInvitationResult{}, ErrMembershipInvitationUnavailable
		}
		if membershipID == uuid.Nil || status != "active" ||
			role != invitation.IntendedRole {
			return AcceptMembershipInvitationResult{}, ErrMembershipInvitationConflict
		}
		principal, err := repository.loadPrincipal(
			queryContext,
			transaction,
			params.SessionID,
			params.UserID,
			activeTenantID,
		)
		if err != nil {
			return AcceptMembershipInvitationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return AcceptMembershipInvitationResult{}, fmt.Errorf("commit repeated invitation acceptance: %w", err)
		}
		return AcceptMembershipInvitationResult{Invitation: invitation, Principal: principal}, nil
	}
	if invitation.Status != MembershipInvitationPending {
		return AcceptMembershipInvitationResult{}, ErrMembershipInvitationUnavailable
	}

	identityMatches, err := verifiedIdentityMatchesInvitation(
		queryContext,
		transaction,
		params.UserID,
		invitation.Email,
	)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	if !identityMatches {
		return AcceptMembershipInvitationResult{}, ErrMembershipInvitationIdentityMismatch
	}

	if membershipID == uuid.Nil {
		if err := transaction.QueryRow(
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
VALUES ($1, $2, $3, 'active', $4, $4, $4)
RETURNING id`,
			tenantID,
			params.UserID,
			invitation.IntendedRole,
			params.AcceptedAt,
		).Scan(&membershipID); err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) &&
				postgresError.ConstraintName == "memberships_tenant_user_unique" {
				return AcceptMembershipInvitationResult{}, ErrMembershipInvitationConflict
			}
			return AcceptMembershipInvitationResult{}, fmt.Errorf("insert invited membership: %w", err)
		}
	} else if status != "active" || role != invitation.IntendedRole {
		return AcceptMembershipInvitationResult{}, ErrMembershipInvitationConflict
	}

	err = scanMembershipInvitation(
		transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.membership_invitations
SET status = 'accepted',
    accepted_at = $3,
    accepted_by = $4,
    updated_at = $3
WHERE tenant_id = $1 AND id = $2 AND status = 'pending'
RETURNING `+membershipInvitationSelectColumns,
			tenantID,
			invitation.ID,
			params.AcceptedAt,
			params.UserID,
		),
		&invitation,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AcceptMembershipInvitationResult{}, ErrMembershipInvitationConflict
		}
		return AcceptMembershipInvitationResult{}, fmt.Errorf("accept membership invitation: %w", err)
	}
	if err := insertMembershipInvitationEvent(
		queryContext,
		transaction,
		invitation,
		"membership.invitation.accepted",
		params.UserID,
		membershipID,
		params.AcceptedAt,
	); err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	principal, err := repository.loadPrincipal(
		queryContext,
		transaction,
		params.SessionID,
		params.UserID,
		activeTenantID,
	)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AcceptMembershipInvitationResult{}, fmt.Errorf("commit membership invitation acceptance: %w", err)
	}
	return AcceptMembershipInvitationResult{Invitation: invitation, Principal: principal}, nil
}

func (repository *PostgresRepository) lockAndAuthorizeInvitationAdmin(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
) error {
	var tenantStatus string
	if err := transaction.QueryRow(
		ctx,
		`SELECT status FROM tutorhub.tenants WHERE id = $1 FOR SHARE`,
		tenantContext.TenantID,
	).Scan(&tenantStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTenantNotFound
		}
		return fmt.Errorf("lock invitation tenant: %w", err)
	}
	if tenantStatus != "active" {
		return ErrTenantNotFound
	}

	var role string
	if err := transaction.QueryRow(
		ctx,
		`SELECT role
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'
FOR SHARE`,
		tenantContext.TenantID,
		tenantContext.ActorID,
	).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTenantAccessDenied
		}
		return fmt.Errorf("lock invitation administrator membership: %w", err)
	}
	return repository.authorizeLockedTenantMembership(
		tenantContext,
		role,
		policy.ActionTenantManageMembers,
	)
}

func lockMembershipInvitationEmail(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	email string,
) error {
	key := "membership-invitation:" + tenantID.String() + ":" + email
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		key,
	); err != nil {
		return fmt.Errorf("lock membership invitation email: %w", err)
	}
	return nil
}

func expireTenantMembershipInvitations(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	now time.Time,
) error {
	return expireMembershipInvitations(
		ctx,
		transaction,
		`UPDATE tutorhub.membership_invitations
SET status = 'expired', updated_at = $2
WHERE tenant_id = $1 AND status = 'pending' AND expires_at <= $2
RETURNING `+membershipInvitationSelectColumns,
		now,
		tenantID,
		now,
	)
}

func expireTenantMembershipInvitationEmail(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	email string,
	now time.Time,
) error {
	return expireMembershipInvitations(
		ctx,
		transaction,
		`UPDATE tutorhub.membership_invitations
SET status = 'expired', updated_at = $3
WHERE tenant_id = $1 AND email = $2 AND status = 'pending' AND expires_at <= $3
RETURNING `+membershipInvitationSelectColumns,
		now,
		tenantID,
		email,
		now,
	)
}

func expireMembershipInvitations(
	ctx context.Context,
	transaction pgx.Tx,
	query string,
	now time.Time,
	arguments ...any,
) error {
	rows, err := transaction.Query(ctx, query, arguments...)
	if err != nil {
		return fmt.Errorf("expire membership invitations: %w", err)
	}
	expired := make([]MembershipInvitation, 0)
	for rows.Next() {
		var invitation MembershipInvitation
		if err := scanMembershipInvitation(rows, &invitation); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired membership invitation: %w", err)
		}
		expired = append(expired, invitation)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired membership invitations: %w", err)
	}
	rows.Close()

	for _, invitation := range expired {
		if err := insertMembershipInvitationEvent(
			ctx,
			transaction,
			invitation,
			"membership.invitation.expired",
			uuid.Nil,
			uuid.Nil,
			now,
		); err != nil {
			return err
		}
	}
	return nil
}

func lookupMembershipInvitationTenant(
	ctx context.Context,
	transaction pgx.Tx,
	tokenHash []byte,
) (uuid.UUID, error) {
	var tenantID uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT tenant_id FROM tutorhub.membership_invitations WHERE token_hash = $1`,
		tokenHash,
	).Scan(&tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrMembershipInvitationUnavailable
		}
		return uuid.Nil, fmt.Errorf("locate membership invitation tenant: %w", err)
	}
	return tenantID, nil
}

func lockActiveInvitationTenant(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) (string, error) {
	var name string
	var status string
	if err := transaction.QueryRow(
		ctx,
		`SELECT name, status FROM tutorhub.tenants WHERE id = $1 FOR SHARE`,
		tenantID,
	).Scan(&name, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrMembershipInvitationUnavailable
		}
		return "", fmt.Errorf("lock invitation tenant: %w", err)
	}
	if status != "active" {
		return "", ErrMembershipInvitationUnavailable
	}
	return name, nil
}

func lockInvitationAcceptanceSession(
	ctx context.Context,
	transaction pgx.Tx,
	sessionID uuid.UUID,
	userID uuid.UUID,
	now time.Time,
) (uuid.UUID, error) {
	var activeTenant uuid.NullUUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id
FROM tutorhub.sessions
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > $3
  AND absolute_expires_at > $3
FOR SHARE`,
		sessionID,
		userID,
		now,
	).Scan(&activeTenant); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrSessionNotFound
		}
		return uuid.Nil, fmt.Errorf("lock invitation acceptance session: %w", err)
	}
	return activeTenant.UUID, nil
}

func lockMembershipInvitation(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	invitationID uuid.UUID,
) (MembershipInvitation, error) {
	var invitation MembershipInvitation
	err := scanMembershipInvitation(
		transaction.QueryRow(
			ctx,
			`SELECT `+membershipInvitationSelectColumns+`
FROM tutorhub.membership_invitations
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`,
			tenantID,
			invitationID,
		),
		&invitation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MembershipInvitation{}, ErrMembershipInvitationUnavailable
	}
	if err != nil {
		return MembershipInvitation{}, fmt.Errorf("lock membership invitation: %w", err)
	}
	return invitation, nil
}

func lockMembershipInvitationByToken(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	tokenHash []byte,
) (MembershipInvitation, error) {
	var invitation MembershipInvitation
	err := scanMembershipInvitation(
		transaction.QueryRow(
			ctx,
			`SELECT `+membershipInvitationSelectColumns+`
FROM tutorhub.membership_invitations
WHERE tenant_id = $1 AND token_hash = $2
FOR UPDATE`,
			tenantID,
			tokenHash,
		),
		&invitation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MembershipInvitation{}, ErrMembershipInvitationUnavailable
	}
	if err != nil {
		return MembershipInvitation{}, fmt.Errorf("lock membership invitation token: %w", err)
	}
	return invitation, nil
}

func expireLockedMembershipInvitation(
	ctx context.Context,
	transaction pgx.Tx,
	invitation *MembershipInvitation,
	now time.Time,
) error {
	if invitation == nil || invitation.Status != MembershipInvitationPending {
		return nil
	}
	if err := scanMembershipInvitation(
		transaction.QueryRow(
			ctx,
			`UPDATE tutorhub.membership_invitations
SET status = 'expired', updated_at = $2
WHERE id = $1 AND status = 'pending'
RETURNING `+membershipInvitationSelectColumns,
			invitation.ID,
			now,
		),
		invitation,
	); err != nil {
		return fmt.Errorf("expire locked membership invitation: %w", err)
	}
	return insertMembershipInvitationEvent(
		ctx,
		transaction,
		*invitation,
		"membership.invitation.expired",
		uuid.Nil,
		uuid.Nil,
		now,
	)
}

func verifiedIdentityMatchesInvitation(
	ctx context.Context,
	transaction pgx.Tx,
	userID uuid.UUID,
	email string,
) (bool, error) {
	var matches bool
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.identities
    WHERE user_id = $1
      AND status = 'active'
      AND email_verified = true
      AND lower(btrim(COALESCE(email_at_provider, ''))) = $2
)`,
		userID,
		email,
	).Scan(&matches); err != nil {
		return false, fmt.Errorf("match verified invitation identity: %w", err)
	}
	return matches, nil
}

func lockInvitationMembership(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	userID uuid.UUID,
) (uuid.UUID, string, string, error) {
	var membershipID uuid.UUID
	var role string
	var status string
	err := transaction.QueryRow(
		ctx,
		`SELECT id, role, status
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = $2
FOR UPDATE`,
		tenantID,
		userID,
	).Scan(&membershipID, &role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", "", nil
	}
	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("lock invited membership: %w", err)
	}
	return membershipID, role, status, nil
}

func insertMembershipInvitationEvent(
	ctx context.Context,
	transaction pgx.Tx,
	invitation MembershipInvitation,
	eventType string,
	actorID uuid.UUID,
	membershipID uuid.UUID,
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
    'membership_invitation',
    $2,
    $3,
    jsonb_strip_nulls(jsonb_build_object(
        'actor_user_id', $4::uuid,
        'status', $5::text,
        'intended_role', $6::text,
        'expires_at', $7::timestamptz,
        'membership_id', $8::uuid
    )),
    $9,
    $9
)`,
		invitation.TenantID,
		invitation.ID,
		eventType,
		nullUUID(actorID),
		string(invitation.Status),
		invitation.IntendedRole,
		invitation.ExpiresAt,
		nullUUID(membershipID),
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		"effect":        invitationEventEffect(eventType),
		"status":        string(invitation.Status),
		"intended_role": invitation.IntendedRole,
	}
	if membershipID != uuid.Nil {
		metadata["membership_id"] = membershipID.String()
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      invitation.TenantID,
		ActorID:       actorID,
		EventType:     eventType,
		AggregateType: "membership_invitation",
		AggregateID:   invitation.ID,
		Metadata:      metadata,
		OccurredAt:    occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func invitationEventEffect(eventType string) string {
	switch eventType {
	case "membership.invitation.created":
		return "created"
	case "membership.invitation.revoked":
		return "revoked"
	case "membership.invitation.accepted":
		return "accepted"
	case "membership.invitation.expired":
		return "expired"
	default:
		return "updated"
	}
}

func scanMembershipInvitation(row scanner, invitation *MembershipInvitation) error {
	var acceptedBy uuid.NullUUID
	var revokedBy uuid.NullUUID
	err := row.Scan(
		&invitation.ID,
		&invitation.TenantID,
		&invitation.Email,
		&invitation.IntendedRole,
		&invitation.Status,
		&invitation.ExpiresAt,
		&invitation.AcceptedAt,
		&invitation.RevokedAt,
		&invitation.CreatedAt,
		&invitation.UpdatedAt,
		&invitation.InvitedBy,
		&acceptedBy,
		&revokedBy,
	)
	if err != nil {
		return err
	}
	invitation.AcceptedBy = nil
	if acceptedBy.Valid {
		value := acceptedBy.UUID
		invitation.AcceptedBy = &value
	}
	invitation.RevokedBy = nil
	if revokedBy.Valid {
		value := revokedBy.UUID
		invitation.RevokedBy = &value
	}
	return nil
}
