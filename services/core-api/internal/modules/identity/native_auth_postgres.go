package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func insertNativeSignup(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, email, displayName, passwordHash, termsVersion string, ipHash []byte, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name,email_verified_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$4,$4)`, userID, email, displayName, now)
	if err != nil {
		return nativeSignupWriteError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.identities(id,user_id,provider,subject,email_at_provider,email_verified,kind,provider_slug,last_authenticated_at,created_at,updated_at)
		VALUES($1,$2,'urn:tutorhub:email',$3,$4,true,'email','email',$5,$5,$5)`, identityID, userID, userID.String(), email, now)
	if err != nil {
		return nativeSignupWriteError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.user_password_credentials(user_id,password_hash,status,created_at,updated_at)
		VALUES($1,$2,'active',$3,$3)`, userID, passwordHash, now)
	if err != nil {
		return nativeSignupWriteError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.user_legal_acceptances(user_id,document_kind,version,accepted_at,ip_bucket_hash)
		VALUES($1,'user_agreement',$2,$3,$4),($1,'privacy_policy',$2,$3,$4)`, userID, termsVersion, now, ipHash)
	return nativeSignupWriteError(err)
}

func nativeSignupWriteError(err error) error {
	if err == nil {
		return nil
	}
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) && postgres.Code == "23505" {
		return ErrNativeSignupUnavailable
	}
	// PostgreSQL errors may include email, hash or constraint data in Detail.
	return ErrAuthFoundationStorage
}

func (service *NativeAuthService) insertNativeSession(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, result *NativeSessionResult, methods []string, userAgent, remote string, now time.Time) error {
	return service.insertAssuredSession(ctx, tx, userID, identityID, result, methods, 1, userAgent, remote, now)
}

func (service *NativeAuthService) insertAssuredSession(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, result *NativeSessionResult, methods []string, assurance int, userAgent, remote string, now time.Time) error {
	activeTenant, err := selectActiveTenant(ctx, tx, userID, service.config.RequireAdminMFA && assurance < 2)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	var ipPrefix any
	if value := IPPrefix(remote); value != "" {
		ipPrefix = value
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.sessions
		(user_id,active_tenant_id,token_hash,csrf_token_hash,user_agent_hash,ip_prefix,created_at,last_seen_at,expires_at,identity_id,absolute_expires_at,auth_time,auth_methods,assurance_level,persistent,device_label)
		VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$10,$7,$11,$14,$12,$13)`,
		userID, nullUUID(activeTenant), service.crypto.Digest(sessionPurpose, result.SessionToken), service.crypto.Digest(csrfPurpose, result.CSRFToken), service.crypto.Digest(userAgentPurpose, userAgent), ipPrefix, now, result.ExpiresAt, nullUUID(identityID), result.AbsoluteExpiresAt, methods, result.Persistent, DeviceLabel(userAgent), assurance)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if identityID == uuid.Nil {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE tutorhub.identities SET last_authenticated_at=GREATEST(created_at,last_authenticated_at,$2),
		updated_at=GREATEST(updated_at,last_authenticated_at,$2) WHERE id=$1`, identityID, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

type nativePasswordSnapshot struct {
	userID       uuid.UUID
	userStatus   string
	verifiedAt   *time.Time
	hash, status *string
	lockedUntil  *time.Time
	identityID   uuid.NullUUID
}

func (snapshot nativePasswordSnapshot) available(now time.Time) bool {
	return snapshot.userID != uuid.Nil && snapshot.userStatus == "active" && snapshot.verifiedAt != nil && snapshot.hash != nil && snapshot.status != nil && *snapshot.status == "active" && snapshot.identityID.Valid &&
		(snapshot.lockedUntil == nil || !snapshot.lockedUntil.After(now))
}

func (service *NativeAuthService) passwordSnapshot(ctx context.Context, email string) (nativePasswordSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nativePasswordSnapshot{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	var snapshot nativePasswordSnapshot
	if err := service.allowLocalEmail(ctx, tx, email, false); err != nil {
		return snapshot, err
	}
	err = tx.QueryRow(ctx, `SELECT u.id,u.status,u.email_verified_at,c.password_hash,c.status,c.locked_until,
		(SELECT i.id FROM tutorhub.identities i WHERE i.user_id=u.id AND i.kind='email'
		 AND i.provider='urn:tutorhub:email' AND i.status='active' AND i.email_verified=true
		 AND lower(i.email_at_provider)=u.email ORDER BY i.created_at,i.id LIMIT 1)
		FROM tutorhub.users u LEFT JOIN tutorhub.user_password_credentials c ON c.user_id=u.id WHERE lower(u.email)=$1`, email).
		Scan(&snapshot.userID, &snapshot.userStatus, &snapshot.verifiedAt, &snapshot.hash, &snapshot.status, &snapshot.lockedUntil, &snapshot.identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nativePasswordSnapshot{}, nil
	}
	if err != nil {
		return nativePasswordSnapshot{}, ErrAuthFoundationStorage
	}
	return snapshot, nil
}

func (service *NativeAuthService) PasswordLogin(ctx context.Context, input PasswordLoginInput) (NativeSessionResult, error) {
	email, err := NormalizeSignupEmail(input.Identifier)
	if err != nil {
		return service.dummyLogin(ctx, input.Password)
	}
	snapshot, err := service.passwordSnapshot(ctx, email)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if !snapshot.available(service.now()) {
		return service.dummyLogin(ctx, input.Password)
	}
	matched, rehash, err := service.passwords.Verify(ctx, input.Password, *snapshot.hash)
	if errors.Is(err, ErrPasswordBusy) {
		return NativeSessionResult{}, err
	}
	if err != nil {
		return service.dummyLogin(ctx, input.Password)
	}
	replacement := ""
	if matched && rehash {
		replacement, err = service.passwords.Hash(ctx, input.Password)
		if err != nil {
			return NativeSessionResult{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	// Recheck mutable account and password state after hashing, with no Argon2
	// work under database locks. A concurrent reset/removal/revocation wins.
	var currentStatus string
	var verifiedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT status,email_verified_at FROM tutorhub.users WHERE id=$1 FOR UPDATE`, snapshot.userID).Scan(&currentStatus, &verifiedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NativeSessionResult{}, ErrNativeInvalidCredentials
		}
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	var identityID uuid.UUID
	if err := service.allowLocalEmail(ctx, tx, email, true); err != nil {
		return NativeSessionResult{}, err
	}
	err = tx.QueryRow(ctx, `SELECT id FROM tutorhub.identities WHERE id=$1 AND user_id=$2 AND kind='email' AND provider='urn:tutorhub:email'
		AND status='active' AND email_verified=true AND lower(email_at_provider)=$3 FOR UPDATE`, snapshot.identityID.UUID, snapshot.userID, email).Scan(&identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return NativeSessionResult{}, ErrNativeInvalidCredentials
	}
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	var currentHash *string
	var credentialStatus string
	var attempts int
	var lockedUntil *time.Time
	err = tx.QueryRow(ctx, `SELECT password_hash,status,failed_attempts,locked_until FROM tutorhub.user_password_credentials WHERE user_id=$1 FOR UPDATE`, snapshot.userID).
		Scan(&currentHash, &credentialStatus, &attempts, &lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return NativeSessionResult{}, ErrNativeInvalidCredentials
	}
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	if currentStatus != "active" || verifiedAt == nil || credentialStatus != "active" || currentHash == nil || *currentHash != *snapshot.hash || (lockedUntil != nil && lockedUntil.After(now)) {
		return NativeSessionResult{}, ErrNativeInvalidCredentials
	}
	if !matched {
		attempts = min(attempts+1, 1000000)
		var until *time.Time
		if delay := nativePasswordLockDuration(attempts); delay > 0 {
			value := now.Add(delay)
			until = &value
		}
		_, err = tx.Exec(ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=$2,last_failed_at=$3,locked_until=$4,updated_at=$3 WHERE user_id=$1`, snapshot.userID, attempts, now, until)
		if err != nil || tx.Commit(ctx) != nil {
			return NativeSessionResult{}, ErrAuthFoundationStorage
		}
		return NativeSessionResult{}, ErrNativeInvalidCredentials
	}
	if replacement != "" {
		_, err = tx.Exec(ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=0,locked_until=NULL,last_failed_at=NULL,password_hash=$2,hash_version=hash_version+1,updated_at=$3 WHERE user_id=$1`, snapshot.userID, replacement, now)
	} else {
		_, err = tx.Exec(ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=0,locked_until=NULL,last_failed_at=NULL,updated_at=$2 WHERE user_id=$1`, snapshot.userID, now)
	}
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	result, err := service.finishLoginFirstFactor(ctx, tx, mfaPayload{UserID: snapshot.userID, IdentityID: identityID, Methods: []string{"pwd"}, KeepSignedIn: input.KeepSignedIn}, input.UserAgent, input.RemoteAddress)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if tx.Commit(ctx) != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	return result, nil
}

func (service *NativeAuthService) dummyLogin(ctx context.Context, password string) (NativeSessionResult, error) {
	if err := service.passwords.DummyVerify(ctx, password); err != nil {
		return NativeSessionResult{}, err
	}
	return NativeSessionResult{}, ErrNativeInvalidCredentials
}

func nativePasswordLockDuration(attempts int) time.Duration {
	if attempts < 5 {
		return 0
	}
	return min(time.Minute*time.Duration(1<<min(attempts-5, 4)), 15*time.Minute)
}
