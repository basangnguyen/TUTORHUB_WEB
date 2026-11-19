package identity

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The email and session generation are immutable proof targets. A later email
// change or session rotation must never transfer an outstanding code's authority.
type reauthEmailPayload struct {
	Email          string `json:"email"`
	ContextVersion int64  `json:"context_version"`
}

func (service *NativeAuthService) StartReauthEmailCode(ctx context.Context, principal Principal, binding string, allowDestination func(context.Context, string) (bool, error)) (SignupChallengeResult, error) {
	if !service.config.EmailCodeAvailable {
		return SignupChallengeResult{}, ErrNativeMethodDisabled
	}
	snapshot, err := service.passwordSettingsSnapshot(ctx, principal)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if !snapshot.verified {
		return SignupChallengeResult{}, ErrNativeVerifiedEmailRequired
	}
	allowed := false
	if allowDestination != nil {
		allowed, err = allowDestination(ctx, snapshot.email)
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := service.lockNativeSecuritySession(ctx, tx, principal)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if locked.AssuranceLevel > 1 {
		return SignupChallengeResult{}, ErrNativeMethodDisabled
	}
	if !locked.EmailVerified || locked.Email != snapshot.email {
		return SignupChallengeResult{}, ErrNativeVerifiedEmailRequired
	}
	transaction, payload, err := service.readReauthTransaction(ctx, tx, principal, binding)
	if err != nil && !errors.Is(err, ErrNativeInvalidCode) {
		return SignupChallengeResult{}, err
	}
	if err != nil || payload.Email != locked.Email || payload.ContextVersion != principal.ContextVersion {
		binding, err = service.crypto.RandomToken()
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
		now := service.now().UTC()
		transaction = AuthTransaction{ID: uuid.New(), Kind: "reauth", State: "pending", CreatedAt: now, ExpiresAt: now.Add(AuthTransactionTTL)}
		raw, err := json.Marshal(reauthEmailPayload{Email: locked.Email, ContextVersion: principal.ContextVersion})
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
		ciphertext, err := service.crypto.Encrypt(string(raw))
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions
            (id,binding_hash,kind,state,user_id,session_id,payload_ciphertext,created_at,expires_at)
            VALUES($1,$2,'reauth','pending',$3,$4,$5,$6,$7)`, transaction.ID, service.crypto.Digest("auth-tx-binding", binding), principal.User.ID, principal.SessionID, ciphertext, now, transaction.ExpiresAt); err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
	}
	// Enforce the same cooldown/send cap even if a caller drops or replaces its
	// auth_tx cookie. The user lock serializes starts across this user's sessions.
	var sends int
	var lastSent *time.Time
	now := service.now().UTC()
	if err := tx.QueryRow(ctx, `SELECT count(*),max(c.created_at)
        FROM tutorhub.auth_challenges c JOIN tutorhub.auth_transactions t ON t.id=c.transaction_id
        WHERE t.user_id=$1 AND t.session_id=$2 AND t.kind='reauth' AND c.created_at>$3`,
		principal.User.ID, principal.SessionID, now.Add(-AuthTransactionTTL)).Scan(&sends, &lastSent); err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	if sends >= MaxChallengeSends || (lastSent != nil && now.Before(lastSent.Add(ChallengeCooldown))) {
		return SignupChallengeResult{}, ErrAuthChallengeThrottled
	}
	challenge, job, err := service.challenges.issueLocked(ctx, tx, transaction, ChallengeInput{
		Purpose: "reauth", Channel: DeliveryEmail, Destination: locked.Email, Deliverable: allowed,
	})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	service.challenges.dispatchCommitted(job)
	cookie := service.transactionCookie(binding, transaction.ExpiresAt)
	cookie.MaxAge = max(1, int(transaction.ExpiresAt.Sub(service.now()).Seconds()))
	return SignupChallengeResult{ChallengeID: challenge.ID, CooldownSeconds: int(ChallengeCooldown.Seconds()),
		ExpiresInSeconds: max(1, int(challenge.ExpiresAt.Sub(service.now()).Seconds())), Cookie: cookie}, nil
}

func (service *NativeAuthService) readReauthTransaction(ctx context.Context, tx pgx.Tx, principal Principal, binding string) (AuthTransaction, reauthEmailPayload, error) {
	if len(binding) != 43 {
		return AuthTransaction{}, reauthEmailPayload{}, ErrNativeInvalidCode
	}
	var transaction AuthTransaction
	var ciphertext []byte
	err := tx.QueryRow(ctx, `SELECT id,kind,state,created_at,expires_at,completed_at,payload_ciphertext
        FROM tutorhub.auth_transactions WHERE binding_hash=$1 AND user_id=$2 AND session_id=$3
        AND kind='reauth' AND state='pending' AND expires_at>$4 FOR UPDATE`,
		service.crypto.Digest("auth-tx-binding", binding), principal.User.ID, principal.SessionID, service.now().UTC()).
		Scan(&transaction.ID, &transaction.Kind, &transaction.State, &transaction.CreatedAt, &transaction.ExpiresAt, &transaction.CompletedAt, &ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthTransaction{}, reauthEmailPayload{}, ErrNativeInvalidCode
	}
	if err != nil {
		return AuthTransaction{}, reauthEmailPayload{}, ErrAuthFoundationStorage
	}
	raw, err := service.crypto.Decrypt(ciphertext)
	if err != nil {
		return AuthTransaction{}, reauthEmailPayload{}, ErrAuthFoundationStorage
	}
	var payload reauthEmailPayload
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return AuthTransaction{}, reauthEmailPayload{}, ErrAuthFoundationStorage
	}
	if payload.Email == "" {
		// The same browser cookie also binds passkey step-up. Switching to email
		// must create a fresh transaction rather than interpret the WebAuthn proof.
		var passkey passkeyPayload
		if json.Unmarshal([]byte(raw), &passkey) == nil && passkey.Purpose == PasskeyReauth &&
			passkey.ContextVersion > 0 && passkey.Session.Challenge != "" && len(passkey.Session.UserID) == 64 {
			return AuthTransaction{}, reauthEmailPayload{}, ErrNativeInvalidCode
		}
		return AuthTransaction{}, reauthEmailPayload{}, ErrAuthFoundationStorage
	}
	if payload.ContextVersion < 1 {
		return AuthTransaction{}, reauthEmailPayload{}, ErrAuthFoundationStorage
	}
	return transaction, payload, nil
}

func (service *NativeAuthService) Reauthenticate(ctx context.Context, principal Principal, binding string, input ReauthenticateInput) (ReauthenticateResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return ReauthenticateResult{}, err
	}
	if input.Method != "password" && input.Method != "email_code" {
		return ReauthenticateResult{}, ErrAuthFoundationInput
	}
	if input.Method == "email_code" && !service.config.EmailCodeAvailable {
		return ReauthenticateResult{}, ErrNativeMethodDisabled
	}
	var snapshot passwordSettingsSnapshot
	var matched bool
	var replacement string
	var err error
	if input.Method == "password" {
		snapshot, err = service.passwordSettingsSnapshot(ctx, principal)
		if err != nil {
			return ReauthenticateResult{}, err
		}
		if !snapshot.active() || (snapshot.lockedUntil != nil && snapshot.lockedUntil.After(service.now())) {
			if err := service.passwords.DummyVerify(ctx, input.Password); err != nil {
				return ReauthenticateResult{}, err
			}
			return ReauthenticateResult{}, ErrNativeInvalidCurrentPassword
		}
		var rehash bool
		matched, rehash, err = service.passwords.Verify(ctx, input.Password, *snapshot.hash)
		if errors.Is(err, ErrPasswordBusy) {
			return ReauthenticateResult{}, err
		}
		if err != nil {
			matched = false
			if err := service.passwords.DummyVerify(ctx, input.Password); err != nil {
				return ReauthenticateResult{}, err
			}
		}
		if input.Password == "" {
			matched = false
		}
		if matched && rehash {
			replacement, err = service.passwords.Hash(ctx, input.Password)
			if err != nil {
				return ReauthenticateResult{}, err
			}
		}
	}
	// All password verification/rehashing runs without a DB transaction. The
	// authoritative session, credential and lockout are checked after locking.
	tokens, err := service.newSessionTokens()
	if err != nil {
		return ReauthenticateResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := service.lockNativeSecuritySession(ctx, tx, principal)
	if err != nil {
		return ReauthenticateResult{}, err
	}
	if locked.AssuranceLevel > 1 {
		return ReauthenticateResult{}, ErrNativeMethodDisabled
	}
	if input.Method == "password" {
		if err := service.verifyLockedReauthPassword(ctx, tx, principal, snapshot, matched, replacement); err != nil {
			return ReauthenticateResult{}, err
		}
	} else {
		transaction, payload, err := service.readReauthTransaction(ctx, tx, principal, binding)
		if err != nil {
			return ReauthenticateResult{}, err
		}
		if !locked.EmailVerified || payload.Email != locked.Email || payload.ContextVersion != principal.ContextVersion {
			return ReauthenticateResult{}, ErrNativeInvalidCode
		}
		matched, err := service.verifyNativeEmailCode(ctx, tx, transaction, nativeEmailPayload{Purpose: "reauth"}, input.ChallengeID, input.Code)
		if err != nil {
			return ReauthenticateResult{}, err
		}
		if !matched {
			if err := tx.Commit(ctx); err != nil {
				return ReauthenticateResult{}, ErrAuthFoundationStorage
			}
			return ReauthenticateResult{}, ErrNativeInvalidCode
		}
		if err := completeNativeEmailTransaction(ctx, tx, transaction, service.now().UTC()); err != nil {
			return ReauthenticateResult{}, err
		}
	}
	return service.completeReauthentication(ctx, tx, principal, locked, tokens, input.Method)
}

func (service *NativeAuthService) verifyLockedReauthPassword(ctx context.Context, tx pgx.Tx, principal Principal, snapshot passwordSettingsSnapshot, matched bool, replacement string) error {
	var hash *string
	var status string
	var attempts int
	var lockedUntil *time.Time
	err := tx.QueryRow(ctx, `SELECT password_hash,status,failed_attempts,locked_until
        FROM tutorhub.user_password_credentials WHERE user_id=$1 FOR UPDATE`, principal.User.ID).
		Scan(&hash, &status, &attempts, &lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNativeInvalidCurrentPassword
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	if status != "active" || hash == nil || snapshot.hash == nil || *hash != *snapshot.hash || (lockedUntil != nil && lockedUntil.After(now)) {
		return ErrNativeInvalidCurrentPassword
	}
	if !matched {
		attempts = min(attempts+1, 1000000)
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
	if replacement == "" {
		_, err = tx.Exec(ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=0,last_failed_at=NULL,locked_until=NULL WHERE user_id=$1`, principal.User.ID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=0,last_failed_at=NULL,locked_until=NULL,
            password_hash=$2,hash_version=LEAST(hash_version::integer+1,32767),updated_at=$3 WHERE user_id=$1`, principal.User.ID, replacement, now)
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (service *NativeAuthService) reauthSessionLifetime(ctx context.Context, tx pgx.Tx, userID uuid.UUID, locked lockedSecuritySession) (NativeSessionResult, error) {
	var admin bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.memberships WHERE user_id=$1 AND role='org_admin' AND status='active')`, userID).Scan(&admin); err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	absolute, expires, persistent := locked.AbsoluteExpiresAt, locked.ExpiresAt, locked.Persistent
	if admin {
		absolute = minPasswordTime(absolute, locked.CreatedAt.Add(min(service.config.SessionAbsoluteTTL, 24*time.Hour)))
		expires = minPasswordTime(expires, absolute)
		persistent = false
	}
	now := service.now().UTC()
	if !expires.After(now) || !absolute.After(now) {
		return NativeSessionResult{}, ErrSessionNotFound
	}
	return NativeSessionResult{AbsoluteExpiresAt: absolute, ExpiresAt: expires, Persistent: persistent}, nil
}

func (service *NativeAuthService) completeReauthentication(ctx context.Context, tx pgx.Tx, principal Principal, locked lockedSecuritySession, tokens NativeSessionResult, method string) (ReauthenticateResult, error) {
	lifetime, err := service.reauthSessionLifetime(ctx, tx, principal.User.ID, locked)
	if err != nil {
		return ReauthenticateResult{}, err
	}
	now := service.now().UTC()
	if method == "password" {
		method = "pwd"
	}
	command, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET reauthenticated_at=$3,token_hash=$4,csrf_token_hash=$5,
        auth_methods=CASE WHEN $6=ANY(auth_methods) THEN auth_methods ELSE array_append(auth_methods,$6) END,
        context_version=context_version+1,persistent=$7,absolute_expires_at=$8,expires_at=$9
        WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND context_version=$10`,
		principal.SessionID, principal.User.ID, now, service.crypto.Digest(sessionPurpose, tokens.SessionToken),
		service.crypto.Digest(csrfPurpose, tokens.CSRFToken), method, lifetime.Persistent, lifetime.AbsoluteExpiresAt, lifetime.ExpiresAt, principal.ContextVersion)
	if err != nil || command.RowsAffected() != 1 {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	if err := tx.Commit(ctx); err != nil {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	return reauthResult(locked, tokens, lifetime, now), nil
}

func reauthResult(locked lockedSecuritySession, tokens, lifetime NativeSessionResult, now time.Time) ReauthenticateResult {
	cookieExpiry := lifetime.ExpiresAt
	if lifetime.Persistent {
		cookieExpiry = lifetime.AbsoluteExpiresAt
	}
	recentAt := now
	if locked.AuthenticatedAt.After(recentAt) {
		recentAt = locked.AuthenticatedAt
	}
	return ReauthenticateResult{RecentAuthExpiresAt: recentAt.Add(nativePasswordRecentAuthTTL), SessionToken: tokens.SessionToken,
		CSRFToken: tokens.CSRFToken, CookieExpiresAt: cookieExpiry}
}
