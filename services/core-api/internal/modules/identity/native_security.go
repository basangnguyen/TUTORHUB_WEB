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
	ErrCannotRevokeCurrentSession = errors.New("use logout to revoke the current session")
	ErrManagedSessionNotFound     = errors.New("session is unavailable")
)

type MySecurity struct {
	HasPassword         bool       `json:"has_password"`
	EmailVerified       bool       `json:"email_verified"`
	MFAEnabled          bool       `json:"mfa_enabled"`
	ReauthMethods       []string   `json:"reauth_methods"`
	RecentAuthExpiresAt *time.Time `json:"recent_auth_expires_at"`
}

type LoginMethodIdentity struct {
	ID                  uuid.UUID  `json:"id"`
	Kind                string     `json:"kind"`
	ProviderSlug        *string    `json:"provider_slug"`
	EmailAtProvider     *string    `json:"email_at_provider"`
	EmailVerified       bool       `json:"email_verified"`
	LastAuthenticatedAt *time.Time `json:"last_authenticated_at"`
	CreatedAt           time.Time  `json:"created_at"`
}

type LoginMethodPassword struct {
	HasPassword bool       `json:"has_password"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

type MyLoginMethods struct {
	Password   LoginMethodPassword   `json:"password"`
	Identities []LoginMethodIdentity `json:"identities"`
}

type MySession struct {
	ID          uuid.UUID `json:"id"`
	DeviceLabel *string   `json:"device_label"`
	AuthMethods []string  `json:"auth_methods"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Persistent  bool      `json:"persistent"`
	IsCurrent   bool      `json:"is_current"`
}

type ReauthenticateInput struct {
	Method, Password, Code string
	ChallengeID            uuid.UUID
}

type ReauthenticateResult struct {
	RecentAuthExpiresAt     time.Time
	SessionToken, CSRFToken string
	CookieExpiresAt         time.Time
}

func (ReauthenticateInput) String() string { return "[redacted reauthentication input]" }
func (ReauthenticateInput) LogValue() slog.Value {
	return slog.StringValue("[redacted reauthentication input]")
}
func (ReauthenticateResult) String() string { return "[redacted reauthentication result]" }
func (ReauthenticateResult) LogValue() slog.Value {
	return slog.StringValue("[redacted reauthentication result]")
}

type NativeSecurityAPI interface {
	GetMySecurity(context.Context, Principal) (MySecurity, error)
	ListMyLoginMethods(context.Context, Principal) (MyLoginMethods, error)
	StartReauthEmailCode(context.Context, Principal, string, func(context.Context, string) (bool, error)) (SignupChallengeResult, error)
	Reauthenticate(context.Context, Principal, string, ReauthenticateInput) (ReauthenticateResult, error)
	ListMySessions(context.Context, Principal) ([]MySession, error)
	RevokeMySession(context.Context, Principal, uuid.UUID) error
	RevokeOtherSessions(context.Context, Principal) (int, error)
}

type lockedSecuritySession struct {
	Email                                                    string
	EmailVerified                                            bool
	CreatedAt, ExpiresAt, AbsoluteExpiresAt, AuthenticatedAt time.Time
	Persistent                                               bool
	AssuranceLevel                                           int
}

// All authenticated security mutations acquire user, then every owned session in
// stable order. Credentials and identities may be locked only after this helper.
func (service *NativeAuthService) lockNativeSecuritySession(ctx context.Context, tx pgx.Tx, principal Principal) (lockedSecuritySession, error) {
	var result lockedSecuritySession
	if err := validatePrincipal(principal); err != nil {
		return result, err
	}
	err := tx.QueryRow(ctx, `SELECT u.email,
        u.email_verified_at IS NOT NULL OR EXISTS (SELECT 1 FROM tutorhub.identities i
          WHERE i.user_id=u.id AND i.status='active' AND i.email_verified=true AND lower(i.email_at_provider)=lower(u.email))
        FROM tutorhub.users u WHERE u.id=$1 AND u.status='active' FOR UPDATE`, principal.User.ID).Scan(&result.Email, &result.EmailVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrSessionNotFound
	}
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	if err := lockNativeUserSessions(ctx, tx, principal.User.ID); err != nil {
		return result, err
	}
	var version int64
	err = tx.QueryRow(ctx, `SELECT created_at,expires_at,absolute_expires_at,persistent,
        GREATEST(auth_time,COALESCE(reauthenticated_at,auth_time)),context_version,assurance_level
        FROM tutorhub.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL
        AND expires_at>$3 AND absolute_expires_at>$3`, principal.SessionID, principal.User.ID, service.now().UTC()).
		Scan(&result.CreatedAt, &result.ExpiresAt, &result.AbsoluteExpiresAt, &result.Persistent, &result.AuthenticatedAt, &version, &result.AssuranceLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrSessionNotFound
	}
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	if version != principal.ContextVersion {
		return result, ErrSessionContextConflict
	}
	return result, nil
}

func (service *NativeAuthService) GetMySecurity(ctx context.Context, principal Principal) (MySecurity, error) {
	snapshot, err := service.passwordSettingsSnapshot(ctx, principal)
	if err != nil {
		return MySecurity{}, err
	}
	result := MySecurity{HasPassword: snapshot.active(), EmailVerified: snapshot.verified, ReauthMethods: []string{}}
	if principal.AssuranceLevel <= 1 {
		if result.HasPassword {
			result.ReauthMethods = append(result.ReauthMethods, "password")
		}
		if result.EmailVerified && service.config.EmailCodeAvailable {
			result.ReauthMethods = append(result.ReauthMethods, "email_code")
		}
	}
	// Principal.AuthenticatedAt is loaded using GREATEST(auth_time, reauthenticated_at).
	expires := principal.AuthenticatedAt.Add(nativePasswordRecentAuthTTL)
	now := service.now().UTC()
	if !principal.AuthenticatedAt.IsZero() && !principal.AuthenticatedAt.After(now.Add(time.Minute)) && expires.After(now) {
		result.RecentAuthExpiresAt = &expires
	}
	return result, nil
}
