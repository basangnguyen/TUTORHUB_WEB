package identity

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNativeInvalidCode            = errors.New("invalid or expired verification code")
	ErrNativeMethodDisabled         = errors.New("authentication method is disabled")
	ErrNativeInvalidCurrentPassword = errors.New("invalid current password")
	ErrNativePasswordUnchanged      = errors.New("new password must differ from the current password")
	ErrNativeVerifiedEmailRequired  = errors.New("a verified email is required")
)

type AuthMethodFlags struct {
	SignupEnabled, EmailCodeEnabled, PasswordEnabled bool
}

type EmailCodeVerifyInput struct {
	ChallengeID              uuid.UUID
	Code                     string
	KeepSignedIn             bool
	UserAgent, RemoteAddress string
}

type PasswordRecoveryCompleteInput struct {
	ChallengeID       uuid.UUID
	Code, NewPassword string
}

type MyPasswordStatus struct {
	HasPassword bool
	UpdatedAt   *time.Time
}

type UpdateMyPasswordInput struct {
	CurrentPassword *string
	NewPassword     string
}

type UpdateMyPasswordResult struct {
	HasPassword             bool
	UpdatedAt               time.Time
	SessionToken, CSRFToken string
	CookieExpiresAt         time.Time
}

// Keep the original signup/password interface usable by existing callers.
type NativeAccountAuthAPI interface {
	NativeAuthAPI
	StartEmailCodeLogin(context.Context, SignupStartInput) (SignupChallengeResult, error)
	VerifyEmailCodeLogin(context.Context, string, EmailCodeVerifyInput) (NativeSessionResult, error)
	StartPasswordRecovery(context.Context, SignupStartInput) (SignupChallengeResult, error)
	CompletePasswordRecovery(context.Context, string, PasswordRecoveryCompleteInput) error
	ResendEmailChallenge(context.Context, string, AuthMethodFlags, func(context.Context, string) (bool, error)) (SignupChallengeResult, error)
	GetMyPasswordStatus(context.Context, Principal) (MyPasswordStatus, error)
	UpdateMyPassword(context.Context, Principal, UpdateMyPasswordInput) (UpdateMyPasswordResult, error)
}

var _ NativeAccountAuthAPI = (*NativeAuthService)(nil)

func (EmailCodeVerifyInput) String() string { return "[redacted email verification]" }
func (EmailCodeVerifyInput) LogValue() slog.Value {
	return slog.StringValue("[redacted email verification]")
}
func (PasswordRecoveryCompleteInput) String() string { return "[redacted password recovery]" }
func (PasswordRecoveryCompleteInput) LogValue() slog.Value {
	return slog.StringValue("[redacted password recovery]")
}
func (UpdateMyPasswordInput) String() string { return "[redacted password update]" }
func (UpdateMyPasswordInput) LogValue() slog.Value {
	return slog.StringValue("[redacted password update]")
}
func (UpdateMyPasswordResult) String() string { return "[redacted password update result]" }
func (UpdateMyPasswordResult) LogValue() slog.Value {
	return slog.StringValue("[redacted password update result]")
}

// Call only after verifying email ownership, with the active user row locked.
// The namespace is bound to a user ID; it must never merge another user's identity.
func ensureNativeEmailIdentity(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string, now time.Time) (uuid.UUID, error) {
	var identityID uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO tutorhub.identities
        (user_id,provider,subject,email_at_provider,email_verified,kind,provider_slug,status,last_authenticated_at,created_at,updated_at)
        VALUES($1,'urn:tutorhub:email',$2,$3,true,'email','email','active',$4,$4,$4)
        ON CONFLICT (provider,subject) DO UPDATE SET
            email_at_provider=EXCLUDED.email_at_provider,email_verified=true,
            status='active',unlinked_at=NULL,last_authenticated_at=$4,updated_at=$4
        WHERE tutorhub.identities.user_id=$1 AND tutorhub.identities.kind='email'
        RETURNING id`, userID, userID.String(), email, now).Scan(&identityID)
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.users SET email_verified_at=COALESCE(email_verified_at,$2) WHERE id=$1`, userID, now); err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	return identityID, nil
}

func upsertNativePassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, hash string, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO tutorhub.user_password_credentials(user_id,password_hash,status,created_at,updated_at)
        VALUES($1,$2,'active',$3,$3)
        ON CONFLICT (user_id) DO UPDATE SET password_hash=EXCLUDED.password_hash,
            hash_version=LEAST(tutorhub.user_password_credentials.hash_version::integer+1,32767),
            status='active',failed_attempts=0,last_failed_at=NULL,locked_until=NULL,compromised_at=NULL,updated_at=$3`, userID, hash, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

// Best-effort security notices share the bounded queue and global email budget.
// They contain no OTP and do not depend on an already-consumed challenge remaining active.
func (service *NativeAuthService) dispatchPasswordChanged(_ context.Context, destination string) {
	if service.challenges == nil || service.challenges.dispatcher == nil {
		return
	}
	notice := AccountNotice{Kind: "password_changed", Destination: destination, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	service.challenges.dispatcher.enqueueCommitted(deliveryJob{notice: &notice})
}

func lockNativeUserSessions(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	// Bulk revocation and tenant archive use the same global session order.
	// Locking only the current session first can deadlock with a bulk update.
	if _, err := tx.Exec(ctx, `SELECT id FROM tutorhub.sessions WHERE user_id=$1 ORDER BY id FOR UPDATE`, userID); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}
