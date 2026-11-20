package identity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

var ErrMFAConflict = errors.New("MFA factor state changed")

type MFAAPI interface {
	EnrollTOTP(context.Context, Principal) (TOTPEnrollment, error)
	ConfirmTOTP(context.Context, Principal, string) (TOTPConfirmation, error)
	DisableTOTP(context.Context, Principal) error
	RegenerateRecoveryCodes(context.Context, Principal) ([]string, error)
	PendingMFA(context.Context, string) (MFAState, error)
	MFAPasskeyOptions(context.Context, string) (json.RawMessage, error)
	VerifyMFA(context.Context, string, MFAVerifyInput) (MFAVerifyResult, error)
}

type TOTPEnrollment struct {
	Secret      string `json:"secret"`
	OTPAuthURI  string `json:"otpauth_uri"`
	Issuer      string `json:"issuer"`
	AccountName string `json:"account_name"`
}
type TOTPConfirmation struct {
	RecoveryCodes []string             `json:"recovery_codes"`
	Session       ReauthenticateResult `json:"-"`
}
type MFAChallenge struct {
	Methods []string     `json:"methods"`
	Cookie  *http.Cookie `json:"-"`
}
type MFAState struct {
	Methods          []string  `json:"methods"`
	ExpiresInSeconds int       `json:"expires_in_seconds"`
	UserID           uuid.UUID `json:"-"`
}
type MFAVerifyInput struct {
	Method, Code, UserAgent, RemoteAddress string
	Assertion                              json.RawMessage
}
type MFAVerifyResult struct {
	Session       NativeSessionResult
	ReturnTo      string
	PasswordReset bool
}

func (TOTPEnrollment) String() string           { return "[redacted TOTP enrollment]" }
func (v TOTPEnrollment) LogValue() slog.Value   { return slog.StringValue(v.String()) }
func (TOTPConfirmation) String() string         { return "[redacted TOTP confirmation]" }
func (v TOTPConfirmation) LogValue() slog.Value { return slog.StringValue(v.String()) }
func (MFAVerifyInput) String() string           { return "[redacted MFA proof]" }
func (v MFAVerifyInput) LogValue() slog.Value   { return slog.StringValue(v.String()) }
func (MFAChallenge) String() string             { return "[redacted MFA challenge]" }
func (v MFAChallenge) LogValue() slog.Value     { return slog.StringValue(v.String()) }

func hasActiveTOTP(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (bool, error) {
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.mfa_totp_factors WHERE user_id=$1 AND status='active')`, userID).Scan(&active)
	if err != nil {
		return false, ErrAuthFoundationStorage
	}
	return active, nil
}

// The caller holds the user lock, shared by all factor changes, and has
// obtained the session assurance from the locked session.
func (s *NativeAuthService) requireRecoveryFactorAssurance(ctx context.Context, tx pgx.Tx, userID uuid.UUID, assurance int) error {
	if !s.config.MFATOTPEnabled || assurance >= 2 {
		return nil
	}
	var enrolled bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.mfa_totp_factors WHERE user_id=$1 AND status='active')
		OR EXISTS(SELECT FROM tutorhub.webauthn_credentials WHERE user_id=$1 AND status='active')`, userID).Scan(&enrolled)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if enrolled {
		return ErrFederationAALRequired
	}
	return nil
}

// Factor lifecycle operations acquire the same account/session locks and check
// recency inside the write transaction. Only committed secrets reach the caller.
func (s *NativeAuthService) changeMFA(ctx context.Context, p Principal, requireAAL2 bool, change func(context.Context, pgx.Tx, lockedSecuritySession) error) error {
	if !s.config.MFATOTPEnabled {
		return ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := s.lockNativeSecuritySession(ctx, tx, p)
	if err != nil {
		return err
	}
	if requireAAL2 && locked.AssuranceLevel < 2 {
		return ErrFederationAALRequired
	}
	if err = s.requireRecoveryFactorAssurance(ctx, tx, p.User.ID, locked.AssuranceLevel); err != nil {
		return err
	}
	if err = nativePasswordRecentAuthentication(locked.AuthenticatedAt, s.now().UTC()); err != nil {
		return err
	}
	if err = change(ctx, tx, locked); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (s *NativeAuthService) EnrollTOTP(ctx context.Context, p Principal) (TOTPEnrollment, error) {
	var result TOTPEnrollment
	err := s.changeMFA(ctx, p, false, func(ctx context.Context, tx pgx.Tx, locked lockedSecuritySession) error {
		active, err := hasActiveTOTP(ctx, tx, p.User.ID)
		if err != nil {
			return err
		}
		if active {
			return ErrMFAConflict
		}
		const issuer = "TutorHub"
		k, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: locked.Email, SecretSize: 20, Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return ErrAuthFoundationStorage
		}
		id := uuid.New()
		aead, err := s.crypto.totpAEAD()
		if err != nil {
			return ErrAuthFoundationStorage
		}
		sealed := aead.Seal(nil, nil, []byte(k.Secret()), totpAAD(p.User.ID, id))
		now := s.now().UTC()
		_, err = tx.Exec(ctx, `UPDATE tutorhub.mfa_totp_factors SET status='disabled',disabled_at=greatest(updated_at,$2),
            updated_at=greatest(updated_at,$2) WHERE user_id=$1 AND status='pending'`, p.User.ID, now)
		if err != nil {
			return ErrAuthFoundationStorage
		}
		_, err = tx.Exec(ctx, `INSERT INTO tutorhub.mfa_totp_factors(id,user_id,secret_ciphertext,status,created_at,updated_at)
            VALUES($1,$2,$3,'pending',$4,$4)`, id, p.User.ID, sealed, now)
		if err != nil {
			return ErrAuthFoundationStorage
		}
		result = TOTPEnrollment{k.Secret(), k.URL(), issuer, locked.Email}
		return nil
	})
	if err != nil {
		return TOTPEnrollment{}, err
	}
	return result, nil
}

func (s *NativeAuthService) ConfirmTOTP(ctx context.Context, p Principal, code string) (TOTPConfirmation, error) {
	var result TOTPConfirmation
	err := s.changeMFA(ctx, p, false, func(ctx context.Context, tx pgx.Tx, locked lockedSecuritySession) error {
		if err := s.useTOTP(ctx, tx, p.User.ID, code, true); err != nil {
			return err
		}
		now := s.now().UTC()
		codes, err := s.replaceRecoveryCodes(ctx, tx, p.User.ID, now)
		if err != nil {
			return err
		}
		session, err := s.rotateAssuredSession(ctx, tx, p, locked, "totp")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=greatest(created_at,$2),revoked_reason='mfa_totp_enabled'
          WHERE user_id=$1 AND assurance_level<2 AND revoked_at IS NULL`, p.User.ID, now)
		if err != nil {
			return ErrAuthFoundationStorage
		}
		if err = insertIdentityEvent(ctx, tx, uuid.Nil, p.User.ID, "mfa_totp_enabled", p.User.ID, now); err != nil {
			return ErrAuthFoundationStorage
		}
		result = TOTPConfirmation{codes, session}
		return nil
	})
	if err != nil {
		return TOTPConfirmation{}, err
	}
	return result, nil
}

func (s *NativeAuthService) DisableTOTP(ctx context.Context, p Principal) error {
	return s.changeMFA(ctx, p, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		now := s.now().UTC()
		cmd, err := tx.Exec(ctx, `UPDATE tutorhub.mfa_totp_factors SET status='disabled',disabled_at=greatest(updated_at,$2),
           updated_at=greatest(updated_at,$2) WHERE user_id=$1 AND status='active'`, p.User.ID, now)
		if err != nil {
			return ErrAuthFoundationStorage
		}
		if cmd.RowsAffected() != 1 {
			return ErrMFAConflict
		}
		if err = invalidateRecoveryCodes(ctx, tx, p.User.ID, now); err != nil {
			return err
		}
		if err = insertIdentityEvent(ctx, tx, uuid.Nil, p.User.ID, "mfa_totp_disabled", p.User.ID, now); err != nil {
			return ErrAuthFoundationStorage
		}
		return nil
	})
}

func (s *NativeAuthService) RegenerateRecoveryCodes(ctx context.Context, p Principal) ([]string, error) {
	var codes []string
	err := s.changeMFA(ctx, p, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		active, err := hasActiveTOTP(ctx, tx, p.User.ID)
		if err != nil {
			return err
		}
		if !active {
			return ErrMFAConflict
		}
		now := s.now().UTC()
		codes, err = s.replaceRecoveryCodes(ctx, tx, p.User.ID, now)
		if err != nil {
			return err
		}
		if err = insertIdentityEvent(ctx, tx, uuid.Nil, p.User.ID, "mfa_recovery_codes_regenerated", p.User.ID, now); err != nil {
			return ErrAuthFoundationStorage
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

var _ MFAAPI = (*NativeAuthService)(nil)
