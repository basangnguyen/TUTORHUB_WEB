package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (service *NativeAuthService) validateSecuritySession(ctx context.Context, tx pgx.Tx, principal Principal) error {
	if err := validatePrincipal(principal); err != nil {
		return err
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorhub.sessions s
        JOIN tutorhub.users u ON u.id=s.user_id WHERE s.id=$1 AND s.user_id=$2
        AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>$3
        AND s.absolute_expires_at>$3 AND s.context_version=$4)`,
		principal.SessionID, principal.User.ID, service.now().UTC(), principal.ContextVersion).Scan(&valid)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if !valid {
		return ErrSessionNotFound
	}
	return nil
}

func (service *NativeAuthService) ListMyLoginMethods(ctx context.Context, principal Principal) (MyLoginMethods, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return MyLoginMethods{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err := service.validateSecuritySession(ctx, tx, principal); err != nil {
		return MyLoginMethods{}, err
	}
	result := MyLoginMethods{Identities: []LoginMethodIdentity{}}
	err = tx.QueryRow(ctx, `SELECT (SELECT updated_at FROM tutorhub.user_password_credentials WHERE user_id=$1 AND status='active'),
        (SELECT count(*) FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active')`, principal.User.ID).Scan(&result.Password.UpdatedAt, &result.PasskeyCount)
	if err != nil {
		return MyLoginMethods{}, ErrAuthFoundationStorage
	}
	result.Password.HasPassword = result.Password.UpdatedAt != nil
	rows, err := tx.Query(ctx, `SELECT id,kind,provider_slug,email_at_provider,email_verified,last_authenticated_at,created_at
        FROM tutorhub.identities WHERE user_id=$1 AND status='active' ORDER BY created_at,id`, principal.User.ID)
	if err != nil {
		return MyLoginMethods{}, ErrAuthFoundationStorage
	}
	defer rows.Close()
	for rows.Next() {
		var item LoginMethodIdentity
		if err := rows.Scan(&item.ID, &item.Kind, &item.ProviderSlug, &item.EmailAtProvider, &item.EmailVerified, &item.LastAuthenticatedAt, &item.CreatedAt); err != nil {
			return MyLoginMethods{}, ErrAuthFoundationStorage
		}
		result.Identities = append(result.Identities, item)
	}
	if rows.Err() != nil {
		return MyLoginMethods{}, ErrAuthFoundationStorage
	}
	return result, nil
}

func (service *NativeAuthService) ListMySessions(ctx context.Context, principal Principal) ([]MySession, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err := service.validateSecuritySession(ctx, tx, principal); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `WITH owned AS (
        SELECT s.*,EXISTS (SELECT 1 FROM tutorhub.memberships m
          WHERE m.user_id=$1 AND m.role='org_admin' AND m.status='active') AS is_admin
        FROM tutorhub.sessions s WHERE s.user_id=$1 AND s.revoked_at IS NULL
      ), effective AS (
        SELECT *,LEAST(expires_at,absolute_expires_at,
          CASE WHEN is_admin THEN created_at+($4*interval '1 second') ELSE absolute_expires_at END) AS effective_expiry
        FROM owned
      ) SELECT id,device_label,auth_methods,created_at,last_seen_at,
        effective_expiry,persistent AND NOT is_admin,id=$2
        FROM effective WHERE effective_expiry>$3 ORDER BY last_seen_at DESC,id`, principal.User.ID, principal.SessionID, service.now().UTC(), min(service.config.SessionAbsoluteTTL, 24*time.Hour).Seconds())
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rows.Close()
	result := []MySession{}
	for rows.Next() {
		var item MySession
		if err := rows.Scan(&item.ID, &item.DeviceLabel, &item.AuthMethods, &item.CreatedAt, &item.LastSeenAt, &item.ExpiresAt, &item.Persistent, &item.IsCurrent); err != nil {
			return nil, ErrAuthFoundationStorage
		}
		if item.AuthMethods == nil {
			item.AuthMethods = []string{}
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, ErrAuthFoundationStorage
	}
	return result, nil
}

func (service *NativeAuthService) RevokeMySession(ctx context.Context, principal Principal, sessionID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if _, err := service.lockNativeSecuritySession(ctx, tx, principal); err != nil {
		return err
	}
	if sessionID == principal.SessionID {
		return ErrCannotRevokeCurrentSession
	}
	command, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason='user_revoked'
        WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, sessionID, principal.User.ID, service.now().UTC())
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if command.RowsAffected() != 1 {
		return ErrManagedSessionNotFound
	}
	if tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (service *NativeAuthService) RevokeOtherSessions(ctx context.Context, principal Principal) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return 0, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if _, err := service.lockNativeSecuritySession(ctx, tx, principal); err != nil {
		return 0, err
	}
	command, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason='user_revoked_others'
        WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, principal.User.ID, principal.SessionID, service.now().UTC())
	if err != nil {
		return 0, ErrAuthFoundationStorage
	}
	if tx.Commit(ctx) != nil {
		return 0, ErrAuthFoundationStorage
	}
	return int(command.RowsAffected()), nil
}
