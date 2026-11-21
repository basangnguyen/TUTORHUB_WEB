package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The account ID is captured at start. A dummy flow cannot become a real flow
// when an account is created later, and an email reassignment cannot move proof
// of possession to a different account. Only the encrypted payload stores email.
type nativeEmailPayload struct {
	Email   string    `json:"email"`
	UserID  uuid.UUID `json:"user_id"`
	Purpose string    `json:"purpose"`
}

func (service *NativeAuthService) StartEmailCodeLogin(ctx context.Context, input SignupStartInput) (SignupChallengeResult, error) {
	return service.startNativeEmail(ctx, input, "login", "login_code")
}

func (service *NativeAuthService) StartPasswordRecovery(ctx context.Context, input SignupStartInput) (SignupChallengeResult, error) {
	return service.startNativeEmail(ctx, input, "recovery", "password_recovery")
}

func (service *NativeAuthService) startNativeEmail(ctx context.Context, input SignupStartInput, kind, purpose string) (SignupChallengeResult, error) {
	email, err := NormalizeSignupEmail(input.Email)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	binding, err := service.crypto.RandomToken()
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err := service.allowLocalEmail(ctx, tx, email, false); err != nil {
		return SignupChallengeResult{}, err
	}
	payload := nativeEmailPayload{Email: email, Purpose: purpose}
	err = tx.QueryRow(ctx, `SELECT id FROM tutorhub.users WHERE lower(email)=$1 AND status='active'`, email).Scan(&payload.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	ciphertext, err := service.crypto.Encrypt(string(rawPayload))
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	transaction := AuthTransaction{ID: uuid.New(), Kind: kind, State: "pending", CreatedAt: now, ExpiresAt: now.Add(AuthTransactionTTL)}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions
		(id,binding_hash,kind,state,user_id,payload_ciphertext,ip_bucket_hash,created_at,expires_at)
		VALUES($1,$2,$3,'pending',$4,$5,$6,$7,$8)`, transaction.ID, service.crypto.Digest("auth-tx-binding", binding), kind,
		nullUUID(payload.UserID), ciphertext, service.ipDigest(input.RemoteAddress), now, transaction.ExpiresAt); err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	if err := service.allowLocalEmail(ctx, tx, email, true); err != nil {
		return SignupChallengeResult{}, err
	}
	challenge, job, err := service.challenges.issueLocked(ctx, tx, transaction, ChallengeInput{
		Purpose: purpose, Channel: DeliveryEmail, Destination: email, Deliverable: payload.UserID != uuid.Nil && input.DestinationAllowed,
	})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if tx.Commit(ctx) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	service.challenges.dispatchCommitted(job)
	return SignupChallengeResult{ChallengeID: challenge.ID, CooldownSeconds: 60, ExpiresInSeconds: 600, Cookie: service.transactionCookie(binding, transaction.ExpiresAt)}, nil
}

func nativeEmailMethodEnabled(kind string, flags AuthMethodFlags) bool {
	switch kind {
	case "signup_email":
		return flags.SignupEnabled
	case "login":
		return flags.EmailCodeEnabled
	case "recovery":
		return flags.PasswordEnabled && flags.EmailCodeEnabled
	default:
		return false
	}
}

// ResendEmailChallenge discovers the method only from the browser-bound server
// transaction. The client cannot change either its purpose or its destination.
func (service *NativeAuthService) ResendEmailChallenge(ctx context.Context, binding string, flags AuthMethodFlags, allowDestination func(context.Context, string) (bool, error)) (SignupChallengeResult, error) {
	lookup := NewTransactionService(service.database, service.crypto, service.config.CookieSecure)
	lookup.now = service.now
	transaction, err := lookup.Lookup(ctx, binding)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if transaction.Kind != "signup_email" && transaction.Kind != "login" && transaction.Kind != "recovery" {
		return SignupChallengeResult{}, ErrAuthTransactionUnavailable
	}
	if !nativeEmailMethodEnabled(transaction.Kind, flags) {
		return SignupChallengeResult{}, ErrNativeMethodDisabled
	}
	if transaction.Kind == "signup_email" {
		return service.ResendSignupChallenge(ctx, binding, allowDestination)
	}
	_, snapshot, _, err := service.nativeEmailSnapshot(ctx, binding, transaction.Kind)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	allowed := false
	if allowDestination != nil {
		allowed, err = allowDestination(ctx, snapshot.Email)
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
	transaction, payload, err := service.readNativeEmailTransaction(ctx, tx, binding, transaction.Kind, true)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if payload != snapshot {
		return SignupChallengeResult{}, ErrNativeInvalidCode
	}
	_, active, err := readNativeEmailUser(ctx, tx, payload, false)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if err := service.allowLocalEmail(ctx, tx, payload.Email, true); err != nil {
		return SignupChallengeResult{}, err
	}
	challenge, job, err := service.challenges.issueLocked(ctx, tx, transaction, ChallengeInput{
		Purpose: payload.Purpose, Channel: DeliveryEmail, Destination: payload.Email, Deliverable: active && allowed,
	})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if tx.Commit(ctx) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	service.challenges.dispatchCommitted(job)
	return SignupChallengeResult{ChallengeID: challenge.ID, CooldownSeconds: 60, ExpiresInSeconds: min(600, max(1, int(challenge.ExpiresAt.Sub(service.now()).Seconds())))}, nil
}

func (service *NativeAuthService) VerifyEmailCodeLogin(ctx context.Context, binding string, input EmailCodeVerifyInput) (NativeSessionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, payload, err := service.readNativeEmailTransaction(ctx, tx, binding, "login", true)
	if err != nil {
		return NativeSessionResult{}, err
	}
	_, active, err := readNativeEmailUser(ctx, tx, payload, true)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if err := service.allowLocalEmail(ctx, tx, payload.Email, true); err != nil {
		return NativeSessionResult{}, err
	}
	matched, err := service.verifyNativeEmailCode(ctx, tx, transaction, payload, input.ChallengeID, input.Code)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if !matched || !active {
		if tx.Commit(ctx) != nil {
			return NativeSessionResult{}, ErrAuthFoundationStorage
		}
		return NativeSessionResult{}, ErrNativeInvalidCode
	}
	now := service.now().UTC()
	identityID, err := ensureNativeEmailIdentity(ctx, tx, payload.UserID, payload.Email, now)
	if err != nil {
		return NativeSessionResult{}, err
	}
	result, err := service.finishLoginFirstFactor(ctx, tx, mfaPayload{UserID: payload.UserID, IdentityID: identityID, Methods: []string{"email_code"}, KeepSignedIn: input.KeepSignedIn}, input.UserAgent, input.RemoteAddress)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if err := completeNativeEmailTransaction(ctx, tx, transaction, now); err != nil {
		return NativeSessionResult{}, err
	}
	if tx.Commit(ctx) != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	return result, nil
}

func (service *NativeAuthService) CompletePasswordRecovery(ctx context.Context, binding string, input PasswordRecoveryCompleteInput) (*MFAChallenge, error) {
	snapshot, displayName, snapshotProof, err := service.passwordRecoverySnapshot(ctx, binding, input)
	if err != nil {
		return nil, err
	}
	// Snapshot transactions are closed before policy/HIBP/Argon work. Invalid
	// enrollment policy never consumes a challenge or increments OTP attempts.
	hash, err := service.passwords.ValidateAndHash(ctx, input.NewPassword, snapshot.Email, displayName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, payload, err := service.readNativeEmailTransaction(ctx, tx, binding, "recovery", true)
	if err != nil {
		return nil, err
	}
	if payload != snapshot {
		return nil, ErrNativeInvalidCode
	}
	currentDisplayName, active, err := readNativeEmailUser(ctx, tx, payload, true)
	if err != nil {
		return nil, err
	}
	if err := service.allowLocalEmail(ctx, tx, payload.Email, true); err != nil {
		return nil, err
	}
	matched, err := service.verifyNativeEmailCode(ctx, tx, transaction, payload, input.ChallengeID, input.Code)
	if err != nil {
		return nil, err
	}
	if !matched || !active || !snapshotProof {
		if tx.Commit(ctx) != nil {
			return nil, ErrAuthFoundationStorage
		}
		return nil, ErrNativeInvalidCode
	}
	// A concurrent profile update must not bypass the policy evaluated outside
	// locks. Roll back consumption so the owner can retry with the current name.
	if currentDisplayName != displayName {
		return nil, ErrNativeInvalidCode
	}
	now := service.now().UTC()
	if service.config.MFATOTPEnabled {
		challenge, err := service.beginMFA(ctx, tx, mfaPayload{
			UserID: payload.UserID, Methods: []string{"email_code"},
			RecoveryHash: hash, RecoveryEmail: payload.Email, RecoveryName: currentDisplayName,
		}, false)
		if err != nil {
			return nil, err
		}
		if challenge != nil {
			if err := completeNativeEmailTransaction(ctx, tx, transaction, now); err != nil {
				return nil, err
			}
			if tx.Commit(ctx) != nil {
				return nil, ErrAuthFoundationStorage
			}
			return challenge, nil
		}
	}
	if err := service.applyPasswordRecovery(ctx, tx, payload.UserID, payload.Email, hash, now); err != nil {
		return nil, err
	}
	if err := completeNativeEmailTransaction(ctx, tx, transaction, now); err != nil {
		return nil, err
	}
	if tx.Commit(ctx) != nil {
		return nil, ErrAuthFoundationStorage
	}
	service.dispatchPasswordChanged(ctx, payload.Email)
	return nil, nil
}

func (service *NativeAuthService) finishMFARecovery(ctx context.Context, tx pgx.Tx, p mfaPayload, now time.Time) error {
	name, active, err := readNativeEmailUser(ctx, tx, nativeEmailPayload{UserID: p.UserID, Email: p.RecoveryEmail}, true)
	if err != nil {
		return err
	}
	if !active || name != p.RecoveryName || p.RecoveryHash == "" {
		return ErrNativeInvalidCode
	}
	return service.applyPasswordRecovery(ctx, tx, p.UserID, p.RecoveryEmail, p.RecoveryHash, now)
}

func (service *NativeAuthService) applyPasswordRecovery(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email, hash string, now time.Time) error {
	if err := lockNativeUserSessions(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := ensureNativeEmailIdentity(ctx, tx, userID, email, now); err != nil {
		return err
	}
	if err := upsertNativePassword(ctx, tx, userID, hash, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$2,revoked_reason='password_reset'
		WHERE user_id=$1 AND revoked_at IS NULL`, userID, now); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

// Account-specific password context is private until the caller proves mailbox
// ownership. The read-only proof does not consume an OTP attempt or hold a lock
// during HIBP/Argon work; the final transaction verifies it again atomically.
func (service *NativeAuthService) passwordRecoverySnapshot(ctx context.Context, binding string, input PasswordRecoveryCompleteInput) (nativeEmailPayload, string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nativeEmailPayload{}, "", false, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, payload, err := service.readNativeEmailTransaction(ctx, tx, binding, "recovery", false)
	if err != nil {
		return nativeEmailPayload{}, "", false, err
	}
	displayName, active, err := readNativeEmailUser(ctx, tx, payload, false)
	if err != nil {
		return nativeEmailPayload{}, "", false, err
	}
	proof, err := service.readNativeEmailProof(ctx, tx, transaction, payload, input.ChallengeID, input.Code)
	if err != nil {
		return nativeEmailPayload{}, "", false, err
	}
	if !proof || !active {
		return payload, "", false, nil
	}
	return payload, displayName, true, nil
}

func (service *NativeAuthService) readNativeEmailProof(ctx context.Context, tx pgx.Tx, transaction AuthTransaction, payload nativeEmailPayload, id uuid.UUID, code string) (bool, error) {
	matchesPurpose, err := nativeEmailCodePurposeMatches(ctx, tx, transaction, payload, id)
	if err != nil || !matchesPurpose {
		return false, err
	}
	var hash []byte
	var deliverable bool
	var attempts, maximum int
	var expires time.Time
	var consumed, invalidated *time.Time
	err = tx.QueryRow(ctx, `SELECT code_hash,deliverable,attempts,max_attempts,expires_at,consumed_at,invalidated_at
		FROM tutorhub.auth_challenges WHERE id=$1 AND transaction_id=$2`, id, transaction.ID).
		Scan(&hash, &deliverable, &attempts, &maximum, &expires, &consumed, &invalidated)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrAuthFoundationStorage
	}
	matched := subtle.ConstantTimeCompare(hash, service.crypto.Digest("auth-otp-v1", id.String()+":"+code)) == 1
	now := service.now().UTC()
	return matched && validOTP(code) && deliverable && consumed == nil && invalidated == nil &&
		expires.After(now) && transaction.ExpiresAt.After(now) && attempts < maximum, nil
}

func completeNativeEmailTransaction(ctx context.Context, tx pgx.Tx, transaction AuthTransaction, now time.Time) error {
	if !transaction.ExpiresAt.After(now) {
		return ErrNativeInvalidCode
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=$2 WHERE id=$1`, transaction.ID, now); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (service *NativeAuthService) nativeEmailSnapshot(ctx context.Context, binding, kind string) (AuthTransaction, nativeEmailPayload, string, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AuthTransaction{}, nativeEmailPayload{}, "", ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, payload, err := service.readNativeEmailTransaction(ctx, tx, binding, kind, false)
	if err != nil {
		return AuthTransaction{}, nativeEmailPayload{}, "", err
	}
	displayName, _, err := readNativeEmailUser(ctx, tx, payload, false)
	return transaction, payload, displayName, err
}

func (service *NativeAuthService) readNativeEmailTransaction(ctx context.Context, tx pgx.Tx, binding, kind string, lock bool) (AuthTransaction, nativeEmailPayload, error) {
	transaction, err := readAuthTransaction(ctx, tx, service.crypto, binding, service.now().UTC(), lock)
	if errors.Is(err, ErrAuthTransactionUnavailable) {
		err = ErrNativeInvalidCode
	}
	if err != nil {
		return AuthTransaction{}, nativeEmailPayload{}, err
	}
	if transaction.Kind != kind || (kind != "login" && kind != "recovery") {
		return AuthTransaction{}, nativeEmailPayload{}, ErrNativeInvalidCode
	}
	var ciphertext []byte
	if tx.QueryRow(ctx, `SELECT payload_ciphertext FROM tutorhub.auth_transactions WHERE id=$1`, transaction.ID).Scan(&ciphertext) != nil {
		return AuthTransaction{}, nativeEmailPayload{}, ErrAuthFoundationStorage
	}
	plaintext, err := service.crypto.Decrypt(ciphertext)
	if err != nil || len(plaintext) > 4096 {
		return AuthTransaction{}, nativeEmailPayload{}, ErrNativeInvalidCode
	}
	var payload nativeEmailPayload
	if json.Unmarshal([]byte(plaintext), &payload) != nil {
		return AuthTransaction{}, nativeEmailPayload{}, ErrNativeInvalidCode
	}
	email, err := NormalizeSignupEmail(payload.Email)
	if err != nil || email != payload.Email || (kind == "login" && payload.Purpose != "login_code") || (kind == "recovery" && payload.Purpose != "password_recovery") {
		return AuthTransaction{}, nativeEmailPayload{}, ErrNativeInvalidCode
	}
	if err := service.allowLocalEmail(ctx, tx, email, false); err != nil {
		return AuthTransaction{}, nativeEmailPayload{}, err
	}
	return transaction, payload, nil
}

func readNativeEmailUser(ctx context.Context, tx pgx.Tx, payload nativeEmailPayload, lock bool) (string, bool, error) {
	query := `SELECT display_name FROM tutorhub.users WHERE id=$1 AND lower(email)=$2 AND status='active'`
	if lock {
		query += " FOR UPDATE"
	}
	var displayName string
	err := tx.QueryRow(ctx, query, payload.UserID, payload.Email).Scan(&displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, ErrAuthFoundationStorage
	}
	return displayName, payload.UserID != uuid.Nil, nil
}

func (service *NativeAuthService) verifyNativeEmailCode(ctx context.Context, tx pgx.Tx, transaction AuthTransaction, payload nativeEmailPayload, id uuid.UUID, code string) (bool, error) {
	matchesPurpose, err := nativeEmailCodePurposeMatches(ctx, tx, transaction, payload, id)
	if err != nil || !matchesPurpose {
		return false, err
	}
	return service.challenges.verifyLocked(ctx, tx, transaction, id, code)
}

func nativeEmailCodePurposeMatches(ctx context.Context, tx pgx.Tx, transaction AuthTransaction, payload nativeEmailPayload, id uuid.UUID) (bool, error) {
	var matchesPurpose bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.auth_challenges WHERE id=$1 AND transaction_id=$2 AND purpose=$3 AND channel='email')`, id, transaction.ID, payload.Purpose).Scan(&matchesPurpose) != nil {
		return false, ErrAuthFoundationStorage
	}
	return matchesPurpose, nil
}
