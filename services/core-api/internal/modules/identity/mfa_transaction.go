package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const mfaTTL = 5 * time.Minute

// The encrypted continuation binds the authenticated first factor and requested
// action to the browser transaction, including any signed school proof.
type mfaPayload struct {
	TransactionID, UserID, IdentityID              uuid.UUID
	Methods                                        []string
	KeepSignedIn                                   bool
	ReturnTo, Provider                             string
	School                                         schoolLoginContext `json:",omitzero"`
	RecoveryHash, RecoveryEmail, RecoveryName      string
	FactorID                                       uuid.UUID
	PasswordDigest, EmailDigest, PasskeyCredential []byte
	PasskeySession                                 *webauthn.SessionData `json:",omitempty"`
}
type mfaTransaction struct {
	proof   mfaPayload
	expires time.Time
	methods []string
}

func (s *NativeAuthService) mfaMethods(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (uuid.UUID, []string, bool, error) {
	var factor uuid.NullUUID
	var registered, usable bool
	err := tx.QueryRow(ctx, `SELECT (SELECT id FROM tutorhub.mfa_totp_factors WHERE user_id=$1 AND status='active'),
      EXISTS(SELECT FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active'),
      EXISTS(SELECT FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active' AND NOT clone_warning)`, userID).Scan(&factor, &registered, &usable)
	if err != nil {
		return uuid.Nil, nil, false, ErrAuthFoundationStorage
	}
	var methods []string
	if factor.Valid {
		methods = append(methods, "totp", "recovery_code")
	}
	if usable && s.webauthn != nil {
		methods = append(methods, "passkey")
	}
	return factor.UUID, methods, registered, nil
}

func (s *NativeAuthService) sealMFA(p mfaPayload) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	sealed, err := s.crypto.Encrypt(string(raw))
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	return sealed, nil
}

func (s *NativeAuthService) passwordGeneration(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]byte, error) {
	var hash string
	if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT password_hash FROM tutorhub.user_password_credentials WHERE user_id=$1 AND status='active'),'')`, userID).Scan(&hash); err != nil {
		return nil, ErrAuthFoundationStorage
	}
	return s.crypto.Digest("mfa-password-generation-v1", hash), nil
}

func (s *NativeAuthService) emailGeneration(ctx context.Context, tx pgx.Tx, p mfaPayload) ([]byte, error) {
	var email string
	err := tx.QueryRow(ctx, `SELECT lower(u.email) FROM tutorhub.users u JOIN tutorhub.identities i ON i.user_id=u.id
      WHERE u.id=$1 AND i.id=$2 AND i.status='active' AND i.kind='email' AND lower(i.email_at_provider)=lower(u.email)`, p.UserID, p.IdentityID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAuthTransactionUnavailable
	}
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	if err := s.allowLocalEmail(ctx, tx, email, true); err != nil {
		return nil, err
	}
	return s.crypto.Digest("mfa-email-generation-v1", email), nil
}

// Callers hold the account lock from their successful first-factor verification.
func (s *NativeAuthService) beginMFA(ctx context.Context, tx pgx.Tx, p mfaPayload, requiredSchool bool) (*MFAChallenge, error) {
	phone := len(p.Methods) == 1 && p.Methods[0] == "phone_otp"
	if !phone && !requiredSchool && !s.config.MFATOTPEnabled {
		return nil, nil
	}
	factor, methods, registered, err := s.mfaMethods(ctx, tx, p.UserID)
	if err != nil {
		return nil, err
	}
	if factor == uuid.Nil && !(registered && (p.RecoveryHash != "" || phone || requiredSchool)) {
		return nil, nil
	}
	if requiredSchool && (!s.config.MFATOTPEnabled || len(methods) == 0) {
		return nil, ErrFederationAALRequired
	}
	if phone && (!s.config.MFATOTPEnabled || len(methods) == 0) {
		return nil, ErrPhoneSecondFactorRequired
	}
	if len(methods) == 0 {
		return nil, ErrNativeMethodDisabled
	}
	if len(p.Methods) != 1 {
		return nil, ErrAuthFoundationInput
	}
	if p.ReturnTo != "" {
		p.ReturnTo, err = NormalizeReturnTo(p.ReturnTo)
		if err != nil {
			return nil, err
		}
	}
	if p.Methods[0] == "pwd" || p.RecoveryHash != "" {
		p.PasswordDigest, err = s.passwordGeneration(ctx, tx, p.UserID)
		if err != nil {
			return nil, err
		}
	}
	if (p.Methods[0] == "email_code" || p.Methods[0] == "pwd") && p.RecoveryHash == "" {
		p.EmailDigest, err = s.emailGeneration(ctx, tx, p)
		if err != nil {
			return nil, err
		}
	}
	p.FactorID = factor
	p.TransactionID = uuid.New()
	sealed, err := s.sealMFA(p)
	if err != nil {
		return nil, err
	}
	binding, err := s.crypto.RandomToken()
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	now := s.now().UTC()
	expires := now.Add(mfaTTL)
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(id,user_id,binding_hash,kind,state,created_at,expires_at,payload_ciphertext)
      VALUES($1,$2,$3,'mfa','pending',$4,$5,$6)`, p.TransactionID, p.UserID, s.crypto.Digest("auth-tx-binding", binding), now, expires, sealed)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	cookie := s.transactionCookie(binding, expires)
	cookie.MaxAge = int(mfaTTL.Seconds())
	return &MFAChallenge{Methods: methods, Cookie: cookie}, nil
}

func (s *NativeAuthService) readMFA(ctx context.Context, tx pgx.Tx, binding string, lock bool) (mfaTransaction, error) {
	var m mfaTransaction
	if !s.config.MFATOTPEnabled || len(binding) != 43 {
		return m, ErrAuthTransactionUnavailable
	}
	digest := s.crypto.Digest("auth-tx-binding", binding)
	// Match account/session lock order used by factor management and erasure.
	if lock {
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT u.id FROM tutorhub.users u JOIN tutorhub.auth_transactions t ON t.user_id=u.id
          WHERE t.binding_hash=$1 AND t.kind='mfa' AND t.state='pending' AND t.expires_at>$2 AND u.status='active' FOR UPDATE OF u`, digest, s.now().UTC()).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return m, ErrAuthTransactionUnavailable
		}
		if err != nil {
			return m, ErrAuthFoundationStorage
		}
	}
	q := `SELECT t.id,t.user_id,t.payload_ciphertext,t.expires_at FROM tutorhub.auth_transactions t
        JOIN tutorhub.users u ON u.id=t.user_id AND u.status='active'
        WHERE t.binding_hash=$1 AND t.kind='mfa' AND t.state='pending' AND t.expires_at>$2`
	if lock {
		q += " FOR UPDATE OF t"
	}
	var sealed []byte
	var id, userID uuid.UUID
	err := tx.QueryRow(ctx, q, digest, s.now().UTC()).Scan(&id, &userID, &sealed, &m.expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrAuthTransactionUnavailable
	}
	if err != nil {
		return m, ErrAuthFoundationStorage
	}
	raw, err := s.crypto.Decrypt(sealed)
	if err != nil || json.Unmarshal([]byte(raw), &m.proof) != nil || m.proof.TransactionID != id || m.proof.UserID != userID || len(m.proof.Methods) != 1 {
		return m, ErrAuthTransactionUnavailable
	}
	factor, methods, _, err := s.mfaMethods(ctx, tx, userID)
	if err != nil {
		return m, err
	}
	if m.proof.FactorID != factor || len(methods) == 0 || !m.expires.After(s.now().UTC()) {
		return m, ErrAuthTransactionUnavailable
	}
	m.methods = methods
	return m, nil
}

func (s *NativeAuthService) PendingMFA(ctx context.Context, binding string) (MFAState, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return MFAState{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	m, err := s.readMFA(ctx, tx, binding, false)
	if err != nil {
		return MFAState{}, err
	}
	// PostgreSQL stores microseconds. Round up the remaining fraction so a
	// newly issued five-minute challenge reports its full lifetime.
	remaining := m.expires.Sub(s.now())
	if remaining <= 0 {
		return MFAState{}, ErrAuthTransactionUnavailable
	}
	seconds := (min(remaining, mfaTTL) + time.Second - 1) / time.Second
	return MFAState{Methods: m.methods, ExpiresInSeconds: int(seconds), UserID: m.proof.UserID}, nil
}

func (s *NativeAuthService) MFAPasskeyOptions(ctx context.Context, binding string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	m, err := s.readMFA(ctx, tx, binding, true)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(m.methods, "passkey") {
		return nil, ErrNativeMethodDisabled
	}
	user, err := loadPasskeyUser(ctx, tx, m.proof.UserID, false)
	if err != nil {
		return nil, err
	}
	user.Credentials = slices.DeleteFunc(user.Credentials, func(c webauthn.Credential) bool { return c.Authenticator.CloneWarning })
	options, session, err := s.webauthn.AssertionOptions(&user)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	m.proof.PasskeySession = session
	sealed, err := s.sealMFA(m.proof)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET payload_ciphertext=$2 WHERE id=$1`, m.proof.TransactionID, sealed)
	if err != nil || tx.Commit(ctx) != nil {
		return nil, ErrAuthFoundationStorage
	}
	return options, nil
}

func (s *NativeAuthService) validateMFAFirstFactor(ctx context.Context, tx pgx.Tx, p mfaPayload) error {
	if p.Methods[0] == "phone_otp" || p.Methods[0] == "passkey" {
		if err := s.allowLocalAccountLogin(ctx, tx, p.UserID); err != nil {
			return err
		}
	}
	if len(p.PasswordDigest) > 0 {
		generation, err := s.passwordGeneration(ctx, tx, p.UserID)
		if err != nil {
			return err
		}
		if !bytes.Equal(p.PasswordDigest, generation) {
			return ErrAuthTransactionUnavailable
		}
	}
	switch p.Methods[0] {
	case "phone_otp":
		if !s.config.PhoneEnabled {
			return ErrAuthTransactionUnavailable
		}
		var lastUse time.Time
		err := tx.QueryRow(ctx, `SELECT GREATEST(n.verified_at,n.last_used_at) FROM tutorhub.user_phone_numbers n
			JOIN tutorhub.identities i ON i.id=n.identity_id AND i.user_id=n.user_id
			WHERE n.user_id=$1 AND n.identity_id=$2 AND n.status='active' AND i.status='active' AND i.kind='phone'
			FOR UPDATE OF n,i`, p.UserID, p.IdentityID).Scan(&lastUse)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAuthTransactionUnavailable
		}
		if err != nil {
			return ErrAuthFoundationStorage
		}
		if phonePastThreshold(s.now().UTC(), lastUse, s.config.PhoneStaleAfter) {
			return ErrAuthTransactionUnavailable
		}
	case "fed":
		return s.validateMFAFederatedIdentity(ctx, tx, p.UserID, p.IdentityID, p.Provider)
	case "pwd", "email_code":
		if p.RecoveryHash != "" {
			return s.allowLocalEmail(ctx, tx, p.RecoveryEmail, true)
		} // finishMFARecovery rechecks the mailbox and profile.
		generation, err := s.emailGeneration(ctx, tx, p)
		if err != nil {
			return err
		}
		if !bytes.Equal(p.EmailDigest, generation) {
			return ErrAuthTransactionUnavailable
		}
	case "passkey":
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND credential_id=$2
            AND status='active' AND NOT clone_warning)`, p.UserID, p.PasskeyCredential).Scan(&active); err != nil {
			return ErrAuthFoundationStorage
		}
		if !active || s.webauthn == nil {
			return ErrAuthTransactionUnavailable
		}
	default:
		return ErrAuthTransactionUnavailable
	}
	return nil
}

func (s *NativeAuthService) VerifyMFA(ctx context.Context, binding string, input MFAVerifyInput) (MFAVerifyResult, error) {
	var result MFAVerifyResult
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	m, err := s.readMFA(ctx, tx, binding, true)
	if err != nil {
		return result, err
	}
	p := m.proof
	if !slices.Contains(m.methods, input.Method) {
		return result, ErrNativeInvalidCode
	}
	if err = s.validateMFAFirstFactor(ctx, tx, p); err != nil {
		return result, err
	}
	switch input.Method {
	case "totp":
		err = s.useTOTP(ctx, tx, p.UserID, input.Code, false)
	case "recovery_code":
		err = s.useRecoveryCode(ctx, tx, p.UserID, input.Code)
	case "passkey":
		if p.PasskeySession == nil {
			return result, ErrNativeInvalidCode
		}
		_, err = s.verifyPasskeyAssertion(ctx, tx, *p.PasskeySession, input.Assertion, &Principal{User: User{ID: p.UserID}})
		if errors.Is(err, ErrInvalidPasskey) || errors.Is(err, ErrPasskeyUnknown) || errors.Is(err, ErrPasskeyClone) {
			// Invalidate the issued WebAuthn challenge and persist any clone warning.
			p.PasskeySession = nil
			sealed, sealErr := s.sealMFA(p)
			if sealErr != nil {
				return result, sealErr
			}
			_, updateErr := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET payload_ciphertext=$2 WHERE id=$1`, p.TransactionID, sealed)
			if updateErr != nil || tx.Commit(ctx) != nil {
				return result, ErrAuthFoundationStorage
			}
			return result, ErrNativeInvalidCode
		}
	}
	if err != nil {
		return result, err
	}
	now := s.now().UTC()
	cmd, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=greatest(created_at,$2),payload_ciphertext=NULL
       WHERE id=$1 AND state='pending' AND expires_at>$2`, p.TransactionID, now)
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	if cmd.RowsAffected() != 1 {
		return result, ErrAuthTransactionUnavailable
	}
	if p.RecoveryHash != "" {
		if err = s.finishMFARecovery(ctx, tx, p, now); err != nil {
			return result, err
		}
		result.PasswordReset = true
	} else {
		methods := slices.Clone(p.Methods)
		if !slices.Contains(methods, input.Method) {
			methods = append(methods, input.Method)
		}
		if p.Methods[0] == "fed" {
			realm, err := s.checkSchoolLogin(ctx, tx, p.UserID, p.School)
			if err != nil {
				return result, err
			}
			if err := joinSchoolTenant(ctx, tx, realm, p.UserID, s.ssoMemberCapacity, now); err != nil {
				return result, err
			}
		}
		session, err := s.issueLoginSession(ctx, tx, p.UserID, p.IdentityID, methods, 2, p.KeepSignedIn, input.UserAgent, input.RemoteAddress)
		if err != nil {
			return result, err
		}
		if s.config.SSO.Enabled && p.School.Proof != nil {
			id, err := s.schoolSessionForToken(ctx, tx, p.UserID, session.SessionToken)
			if err != nil {
				return result, err
			}
			if err := saveSchoolReceipt(ctx, tx, s.crypto, p.UserID, id, p.School.Proof, now); err != nil {
				return result, err
			}
		}
		result = MFAVerifyResult{Session: session, ReturnTo: p.ReturnTo}
	}
	if tx.Commit(ctx) != nil {
		return MFAVerifyResult{}, ErrAuthFoundationStorage
	}
	if result.PasswordReset {
		s.dispatchPasswordChanged(ctx, p.RecoveryEmail)
	}
	return result, nil
}
