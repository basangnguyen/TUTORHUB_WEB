package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const nativePasswordRecentAuthTTL = 10 * time.Minute

type passwordSettingsSnapshot struct {
	email, displayName string
	verified           bool
	hash, status       *string
	updatedAt          *time.Time
	lockedUntil        *time.Time
}

func (snapshot passwordSettingsSnapshot) active() bool {
	return snapshot.status != nil && *snapshot.status == "active" && snapshot.hash != nil
}

func (service *NativeAuthService) GetMyPasswordStatus(ctx context.Context, principal Principal) (MyPasswordStatus, error) {
	snapshot, err := service.passwordSettingsSnapshot(ctx, principal)
	if err != nil {
		return MyPasswordStatus{}, err
	}
	result := MyPasswordStatus{HasPassword: snapshot.active()}
	if result.HasPassword {
		result.UpdatedAt = snapshot.updatedAt
	}
	return result, nil
}

func (service *NativeAuthService) passwordSettingsSnapshot(ctx context.Context, principal Principal) (passwordSettingsSnapshot, error) {
	if err := validatePrincipal(principal); err != nil {
		return passwordSettingsSnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return passwordSettingsSnapshot{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	var snapshot passwordSettingsSnapshot
	err = tx.QueryRow(ctx, `SELECT u.email,u.display_name,
        u.email_verified_at IS NOT NULL OR EXISTS (SELECT 1 FROM tutorhub.identities i
          WHERE i.user_id=u.id AND i.status='active' AND i.email_verified=true AND lower(i.email_at_provider)=lower(u.email)),
        c.password_hash,c.status,c.updated_at,c.locked_until
        FROM tutorhub.users u LEFT JOIN tutorhub.user_password_credentials c ON c.user_id=u.id
        WHERE u.id=$1 AND u.status='active' AND EXISTS (SELECT 1 FROM tutorhub.sessions s
          WHERE s.id=$2 AND s.user_id=u.id AND s.revoked_at IS NULL AND s.expires_at>$3 AND s.absolute_expires_at>$3
          AND s.context_version=$4)`, principal.User.ID, principal.SessionID, service.now().UTC(), principal.ContextVersion).
		Scan(&snapshot.email, &snapshot.displayName, &snapshot.verified, &snapshot.hash, &snapshot.status, &snapshot.updatedAt, &snapshot.lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return passwordSettingsSnapshot{}, ErrSessionNotFound
	}
	if err != nil {
		return passwordSettingsSnapshot{}, ErrAuthFoundationStorage
	}
	return snapshot, nil
}

func nativePasswordRecentAuthentication(authenticatedAt, now time.Time) error {
	if authenticatedAt.IsZero() || authenticatedAt.After(now.Add(time.Minute)) || now.Sub(authenticatedAt) > nativePasswordRecentAuthTTL {
		return ErrRecentAuthenticationRequired
	}
	return nil
}

func (service *NativeAuthService) UpdateMyPassword(ctx context.Context, principal Principal, input UpdateMyPasswordInput) (UpdateMyPasswordResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return UpdateMyPasswordResult{}, err
	}
	if err := nativePasswordRecentAuthentication(principal.AuthenticatedAt, service.now().UTC()); err != nil {
		return UpdateMyPasswordResult{}, err
	}
	snapshot, err := service.passwordSettingsSnapshot(ctx, principal)
	if err != nil {
		return UpdateMyPasswordResult{}, err
	}
	current := ""
	if input.CurrentPassword != nil {
		current = *input.CurrentPassword
	}
	if snapshot.active() {
		if snapshot.lockedUntil != nil && snapshot.lockedUntil.After(service.now()) {
			if err := service.passwords.DummyVerify(ctx, current); err != nil {
				return UpdateMyPasswordResult{}, err
			}
			return UpdateMyPasswordResult{}, ErrNativeInvalidCurrentPassword
		}
		matched, _, err := service.passwords.Verify(ctx, current, *snapshot.hash)
		if errors.Is(err, ErrPasswordBusy) {
			return UpdateMyPasswordResult{}, err
		}
		if err != nil {
			if err := service.passwords.DummyVerify(ctx, current); err != nil {
				return UpdateMyPasswordResult{}, err
			}
		}
		if !matched || current == "" || err != nil {
			return UpdateMyPasswordResult{}, service.recordInvalidCurrentPassword(ctx, principal, snapshot)
		}
		normalizedCurrent, currentErr := normalizePassword(current, 1)
		normalizedNew, newErr := normalizePassword(input.NewPassword, 1)
		if currentErr == nil && newErr == nil && normalizedCurrent == normalizedNew {
			return UpdateMyPasswordResult{}, ErrNativePasswordUnchanged
		}
	} else {
		if current != "" {
			return UpdateMyPasswordResult{}, ErrNativeInvalidCurrentPassword
		}
		if !snapshot.verified {
			return UpdateMyPasswordResult{}, ErrNativeVerifiedEmailRequired
		}
	}
	// Password verification, policy/HIBP and Argon2 hashing never hold a
	// database connection or lock. All mutable state is rechecked below.
	hash, err := service.passwords.ValidateAndHash(ctx, input.NewPassword, snapshot.email, snapshot.displayName)
	if err != nil {
		return UpdateMyPasswordResult{}, err
	}
	tokens, err := service.newSessionTokens()
	if err != nil {
		return UpdateMyPasswordResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return UpdateMyPasswordResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := service.lockPasswordSettings(ctx, tx, principal, snapshot)
	if err != nil {
		return UpdateMyPasswordResult{}, err
	}
	now := service.now().UTC()
	if _, err := ensureNativeEmailIdentity(ctx, tx, principal.User.ID, snapshot.email, now); err != nil {
		return UpdateMyPasswordResult{}, err
	}
	if err := upsertNativePassword(ctx, tx, principal.User.ID, hash, now); err != nil {
		return UpdateMyPasswordResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason='password_changed'
        WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, principal.User.ID, principal.SessionID, now); err != nil {
		return UpdateMyPasswordResult{}, ErrAuthFoundationStorage
	}
	var admin bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.memberships WHERE user_id=$1 AND role='org_admin' AND status='active')`, principal.User.ID).Scan(&admin); err != nil {
		return UpdateMyPasswordResult{}, ErrAuthFoundationStorage
	}
	absolute, expires, persistent := locked.absolute, locked.expires, locked.persistent
	if admin {
		absolute = minPasswordTime(absolute, locked.created.Add(min(service.config.SessionAbsoluteTTL, 24*time.Hour)))
		expires = minPasswordTime(expires, absolute)
		persistent = false
	}
	if !expires.After(now) || !absolute.After(now) {
		return UpdateMyPasswordResult{}, ErrSessionNotFound
	}
	command, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET token_hash=$3,csrf_token_hash=$4,
        auth_methods=CASE WHEN 'pwd'=ANY(auth_methods) THEN auth_methods ELSE array_append(auth_methods,'pwd') END,
        context_version=context_version+1,persistent=$5,absolute_expires_at=$6,expires_at=$7
        WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, principal.SessionID, principal.User.ID,
		service.crypto.Digest(sessionPurpose, tokens.SessionToken), service.crypto.Digest(csrfPurpose, tokens.CSRFToken), persistent, absolute, expires)
	if err != nil || command.RowsAffected() != 1 {
		return UpdateMyPasswordResult{}, ErrAuthFoundationStorage
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateMyPasswordResult{}, ErrAuthFoundationStorage
	}
	service.dispatchPasswordChanged(ctx, snapshot.email)
	cookieExpiry := expires
	if persistent {
		cookieExpiry = absolute
	}
	return UpdateMyPasswordResult{HasPassword: true, UpdatedAt: now, SessionToken: tokens.SessionToken,
		CSRFToken: tokens.CSRFToken, CookieExpiresAt: cookieExpiry}, nil
}

type lockedPasswordSettings struct {
	created, expires, absolute time.Time
	persistent                 bool
	attempts                   int
}

func (service *NativeAuthService) lockPasswordSettings(ctx context.Context, tx pgx.Tx, principal Principal, snapshot passwordSettingsSnapshot) (lockedPasswordSettings, error) {
	var locked lockedPasswordSettings
	var email, displayName string
	var verified bool
	err := tx.QueryRow(ctx, `SELECT u.email,u.display_name,
        u.email_verified_at IS NOT NULL OR EXISTS (SELECT 1 FROM tutorhub.identities i
          WHERE i.user_id=u.id AND i.status='active' AND i.email_verified=true AND lower(i.email_at_provider)=lower(u.email))
        FROM tutorhub.users u WHERE u.id=$1 AND u.status='active' FOR UPDATE`, principal.User.ID).Scan(&email, &displayName, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return locked, ErrSessionNotFound
	}
	if err != nil {
		return locked, ErrAuthFoundationStorage
	}
	if email != snapshot.email || displayName != snapshot.displayName {
		return locked, ErrSessionContextConflict
	}
	if err := lockNativeUserSessions(ctx, tx, principal.User.ID); err != nil {
		return locked, err
	}
	var authenticatedAt time.Time
	var version int64
	now := service.now().UTC()
	err = tx.QueryRow(ctx, `SELECT created_at,expires_at,absolute_expires_at,persistent,
        GREATEST(auth_time,COALESCE(reauthenticated_at,auth_time)),context_version
        FROM tutorhub.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL
        AND expires_at>$3 AND absolute_expires_at>$3 FOR UPDATE`, principal.SessionID, principal.User.ID, now).
		Scan(&locked.created, &locked.expires, &locked.absolute, &locked.persistent, &authenticatedAt, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return locked, ErrSessionNotFound
	}
	if err != nil {
		return locked, ErrAuthFoundationStorage
	}
	if version != principal.ContextVersion {
		return locked, ErrSessionContextConflict
	}
	if err := nativePasswordRecentAuthentication(authenticatedAt, now); err != nil {
		return locked, err
	}
	// The subsequent email identity upsert must never acquire an identity lock
	// after a credential lock. The user lock serializes identity creation too.
	if _, err := tx.Exec(ctx, `SELECT id FROM tutorhub.identities WHERE user_id=$1 ORDER BY id FOR UPDATE`, principal.User.ID); err != nil {
		return locked, ErrAuthFoundationStorage
	}
	var hash *string
	var status string
	var lockedUntil *time.Time
	err = tx.QueryRow(ctx, `SELECT password_hash,status,failed_attempts,locked_until
        FROM tutorhub.user_password_credentials WHERE user_id=$1 FOR UPDATE`, principal.User.ID).
		Scan(&hash, &status, &locked.attempts, &lockedUntil)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return locked, ErrAuthFoundationStorage
	}
	currentActive := err == nil && status == "active" && hash != nil
	if snapshot.active() != currentActive || (currentActive && *hash != *snapshot.hash) {
		return locked, ErrSessionContextConflict
	}
	if currentActive && lockedUntil != nil && lockedUntil.After(now) {
		return locked, ErrNativeInvalidCurrentPassword
	}
	if !currentActive && !verified {
		return locked, ErrNativeVerifiedEmailRequired
	}
	return locked, nil
}

func (service *NativeAuthService) recordInvalidCurrentPassword(ctx context.Context, principal Principal, snapshot passwordSettingsSnapshot) error {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := service.lockPasswordSettings(ctx, tx, principal, snapshot)
	if err != nil {
		return err
	}
	now := service.now().UTC()
	attempts := min(locked.attempts+1, 1000000)
	var until *time.Time
	if delay := nativePasswordLockDuration(attempts); delay > 0 {
		value := now.Add(delay)
		until = &value
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.user_password_credentials
        SET failed_attempts=$2,last_failed_at=$3,locked_until=$4,updated_at=$3 WHERE user_id=$1`, principal.User.ID, attempts, now, until); err != nil {
		return ErrAuthFoundationStorage
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrAuthFoundationStorage
	}
	return ErrNativeInvalidCurrentPassword
}

func minPasswordTime(first, second time.Time) time.Time {
	if first.Before(second) {
		return first
	}
	return second
}
