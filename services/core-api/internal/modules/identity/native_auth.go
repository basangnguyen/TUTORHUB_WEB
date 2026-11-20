package identity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrNativeInvalidCredentials = errors.New("invalid email or password")
	ErrNativeSignupUnavailable  = errors.New("unable to complete signup; try signing in")
	ErrNativeTermsVersion       = errors.New("the current legal terms must be accepted")
)

type NativeAuthConfig struct {
	SSO                          SSORealmConfig
	PhoneEnabled                 bool
	PhoneLoginUnavailable        bool
	PhoneAllowedCountries        []string
	PhoneStaleAfter              time.Duration
	PhoneReclaimAfter            time.Duration
	PasswordEnabled              bool
	EmailCodeLoginEnabled        bool
	RequireAdminMFA              bool
	OIDCDefaultEnabled           bool
	MFATOTPEnabled               bool
	WebAuthn                     *PasskeyProtocol
	CookieSecure                 bool
	EmailCodeAvailable           bool
	LegalTermsVersion            string
	SessionTTL                   time.Duration
	SessionAbsoluteTTL           time.Duration
	SessionPersistentAbsoluteTTL time.Duration
	QueryTimeout                 time.Duration
}

type SignupStartInput struct {
	Email, RemoteAddress string
	DestinationAllowed   bool
}
type SignupCompleteInput struct {
	ChallengeID                                                         uuid.UUID
	Code, DisplayName, Password, TermsVersion, UserAgent, RemoteAddress string
	KeepSignedIn                                                        bool
}
type PasswordLoginInput struct {
	Identifier, Password, UserAgent, RemoteAddress string
	KeepSignedIn                                   bool
}
type SignupChallengeResult struct {
	MaskedDestination                 string
	ChallengeID                       uuid.UUID
	CooldownSeconds, ExpiresInSeconds int
	Cookie                            *http.Cookie
}
type NativeSessionResult struct {
	MFA                          *MFAChallenge
	SessionToken, CSRFToken      string
	ExpiresAt, AbsoluteExpiresAt time.Time
	Persistent                   bool
}

func (SignupStartInput) LogValue() slog.Value    { return slog.StringValue("[redacted signup input]") }
func (SignupStartInput) String() string          { return "[redacted signup input]" }
func (SignupCompleteInput) LogValue() slog.Value { return slog.StringValue("[redacted signup input]") }
func (SignupCompleteInput) String() string       { return "[redacted signup input]" }
func (PasswordLoginInput) LogValue() slog.Value  { return slog.StringValue("[redacted password input]") }
func (PasswordLoginInput) String() string        { return "[redacted password input]" }
func (SignupChallengeResult) LogValue() slog.Value {
	return slog.StringValue("[redacted signup result]")
}
func (SignupChallengeResult) String() string { return "[redacted signup result]" }
func (NativeSessionResult) LogValue() slog.Value {
	return slog.StringValue("[redacted session result]")
}
func (NativeSessionResult) String() string { return "[redacted session result]" }

type NativeAuthAPI interface {
	StartEmailSignup(context.Context, SignupStartInput) (SignupChallengeResult, error)
	ResendSignupChallenge(context.Context, string, func(context.Context, string) (bool, error)) (SignupChallengeResult, error)
	CompleteEmailSignup(context.Context, string, SignupCompleteInput) (NativeSessionResult, error)
	PasswordLogin(context.Context, PasswordLoginInput) (NativeSessionResult, error)
}

type NativePasswordService interface {
	ValidateAndHash(context.Context, string, string, string) (string, error)
	Verify(context.Context, string, string) (bool, bool, error)
	Hash(context.Context, string) (string, error)
	DummyVerify(context.Context, string) error
}

type NativeAuthService struct {
	ssoMemberCapacity     featurecontrol.Enforcer
	webauthn              *PasskeyProtocol
	database              Database
	crypto                *Crypto
	passwords             NativePasswordService
	challenges            *ChallengeService
	config                NativeAuthConfig
	registrationProviders *ProviderRegistry
	now                   func() time.Time
}

type signupPayload struct {
	Email    string `json:"email"`
	Existing bool   `json:"existing"`
}

func NewNativeAuthService(database Database, crypto *Crypto, passwords NativePasswordService, dispatcher *DeliveryDispatcher, cfg NativeAuthConfig) (*NativeAuthService, error) {
	if cfg.PhoneEnabled {
		if cfg.PhoneStaleAfter == 0 {
			cfg.PhoneStaleAfter = 60 * 24 * time.Hour
		}
		if cfg.PhoneReclaimAfter == 0 {
			cfg.PhoneReclaimAfter = 90 * 24 * time.Hour
		}
		if cfg.PhoneStaleAfter < time.Hour || cfg.PhoneReclaimAfter < cfg.PhoneStaleAfter {
			return nil, ErrAuthFoundationInput
		}
		if len(cfg.PhoneAllowedCountries) == 0 {
			cfg.PhoneAllowedCountries = []string{"VN"}
		}
	}
	if database == nil || crypto == nil || passwords == nil || strings.TrimSpace(cfg.LegalTermsVersion) == "" || !utf8.ValidString(cfg.LegalTermsVersion) || utf8.RuneCountInString(cfg.LegalTermsVersion) > 64 ||
		cfg.SessionTTL <= 0 || cfg.SessionAbsoluteTTL < cfg.SessionTTL || cfg.SessionPersistentAbsoluteTTL < cfg.SessionAbsoluteTTL || cfg.SessionPersistentAbsoluteTTL > 30*24*time.Hour {
		return nil, ErrAuthFoundationInput
	}
	return &NativeAuthService{database: database, crypto: crypto, passwords: passwords, challenges: NewChallengeService(database, crypto, dispatcher), config: cfg, webauthn: cfg.WebAuthn, now: time.Now}, nil
}

func NormalizeSignupEmail(value string) (string, error) {
	return normalizeChallengeDestination(DeliveryEmail, value)
}

func (service *NativeAuthService) queryTimeout() time.Duration {
	if service.config.QueryTimeout > 0 {
		return service.config.QueryTimeout
	}
	return 5 * time.Second
}

func (service *NativeAuthService) StartEmailSignup(ctx context.Context, input SignupStartInput) (SignupChallengeResult, error) {
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
	if err := service.allowLocalEmail(ctx, tx, email, true); err != nil {
		return SignupChallengeResult{}, err
	}
	var existing bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.users WHERE lower(email)=$1)`, email).Scan(&existing) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	payloadBytes, err := json.Marshal(signupPayload{Email: email, Existing: existing})
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	ciphertext, err := service.crypto.Encrypt(string(payloadBytes))
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	transaction := AuthTransaction{ID: uuid.New(), Kind: "signup_email", State: "pending", CreatedAt: now, ExpiresAt: now.Add(AuthTransactionTTL)}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions
		(id,binding_hash,kind,state,payload_ciphertext,ip_bucket_hash,created_at,expires_at)
		VALUES($1,$2,'signup_email','pending',$3,$4,$5,$6)`, transaction.ID, service.crypto.Digest("auth-tx-binding", binding), ciphertext, service.ipDigest(input.RemoteAddress), now, transaction.ExpiresAt)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	challenge, job, err := service.challenges.issueLocked(ctx, tx, transaction, ChallengeInput{Purpose: "signup_email", Channel: DeliveryEmail, Destination: email, Deliverable: !existing && input.DestinationAllowed})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if existing && input.DestinationAllowed {
		job.notice = &AccountNotice{ChallengeID: challenge.ID, Kind: "account_exists", Destination: email, ExpiresAt: challenge.ExpiresAt}
		job.message.Code = ""
	}
	if tx.Commit(ctx) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	service.challenges.dispatchCommitted(job)
	return SignupChallengeResult{ChallengeID: challenge.ID, CooldownSeconds: 60, ExpiresInSeconds: 600, Cookie: service.transactionCookie(binding, transaction.ExpiresAt)}, nil
}

func (service *NativeAuthService) ResendSignupChallenge(ctx context.Context, binding string, allowDestination func(context.Context, string) (bool, error)) (SignupChallengeResult, error) {
	_, payload, _, err := service.signupSnapshot(ctx, binding)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	allowed := false
	if allowDestination != nil {
		allowed, err = allowDestination(ctx, payload.Email)
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
	transaction, current, _, err := service.readSignup(ctx, tx, binding, true)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if current != payload {
		return SignupChallengeResult{}, ErrNativeSignupUnavailable
	}
	var existing bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.users WHERE lower(email)=$1)`, payload.Email).Scan(&existing) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	existing = existing || payload.Existing
	challenge, job, err := service.challenges.issueLocked(ctx, tx, transaction, ChallengeInput{Purpose: "signup_email", Channel: DeliveryEmail, Destination: payload.Email, Deliverable: !existing && allowed})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if existing && allowed {
		job.notice = &AccountNotice{ChallengeID: challenge.ID, Kind: "account_exists", Destination: payload.Email, ExpiresAt: challenge.ExpiresAt}
		job.message.Code = ""
	}
	if tx.Commit(ctx) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	service.challenges.dispatchCommitted(job)
	return SignupChallengeResult{ChallengeID: challenge.ID, CooldownSeconds: 60, ExpiresInSeconds: min(600, max(1, int(challenge.ExpiresAt.Sub(service.now()).Seconds())))}, nil
}

func (service *NativeAuthService) CompleteEmailSignup(ctx context.Context, binding string, input SignupCompleteInput) (NativeSessionResult, error) {
	if input.TermsVersion != service.config.LegalTermsVersion {
		return NativeSessionResult{}, ErrNativeTermsVersion
	}
	displayName, err := normalizeSignupDisplayName(input.DisplayName)
	if err != nil {
		return NativeSessionResult{}, err
	}
	_, payload, _, err := service.signupSnapshot(ctx, binding)
	if err != nil {
		return NativeSessionResult{}, err
	}
	// Network breach checks and Argon2 run without holding a database connection
	// or row lock. The immutable payload is compared again after obtaining locks.
	passwordHash, err := service.passwords.ValidateAndHash(ctx, input.Password, payload.Email, displayName)
	if err != nil {
		return NativeSessionResult{}, err
	}
	result, err := service.newSessionTokens()
	if err != nil {
		return NativeSessionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, current, ipHash, err := service.readSignup(ctx, tx, binding, true)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if current != payload {
		return NativeSessionResult{}, ErrNativeSignupUnavailable
	}
	matched, err := service.challenges.verifyLocked(ctx, tx, transaction, input.ChallengeID, input.Code)
	if err != nil {
		return NativeSessionResult{}, err
	}
	if !matched || current.Existing {
		// Preserve wrong-code attempts on failure; no user or session is created.
		if tx.Commit(ctx) != nil {
			return NativeSessionResult{}, ErrAuthFoundationStorage
		}
		return NativeSessionResult{}, ErrNativeSignupUnavailable
	}
	now := service.now().UTC()
	if !transaction.ExpiresAt.After(now) {
		return NativeSessionResult{}, ErrNativeSignupUnavailable
	}
	userID, identityID := uuid.New(), uuid.New()
	if err := insertNativeSignup(ctx, tx, userID, identityID, current.Email, displayName, passwordHash, input.TermsVersion, ipHash, now); err != nil {
		return NativeSessionResult{}, err
	}
	result = service.sessionExpiry(result, now, input.KeepSignedIn, false)
	if err := service.insertNativeSession(ctx, tx, userID, identityID, &result, []string{"pwd", "email_code"}, input.UserAgent, input.RemoteAddress, now); err != nil {
		return NativeSessionResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=$2 WHERE id=$1`, transaction.ID, now); err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	if err := tx.Commit(ctx); err != nil {
		return NativeSessionResult{}, nativeSignupWriteError(err)
	}
	return result, nil
}

func (service *NativeAuthService) signupSnapshot(ctx context.Context, binding string) (AuthTransaction, signupPayload, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AuthTransaction{}, signupPayload{}, nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	return service.readSignup(ctx, tx, binding, false)
}

func (service *NativeAuthService) readSignup(ctx context.Context, tx pgx.Tx, binding string, lock bool) (AuthTransaction, signupPayload, []byte, error) {
	transaction, err := readAuthTransaction(ctx, tx, service.crypto, binding, service.now().UTC(), lock)
	if err != nil {
		return AuthTransaction{}, signupPayload{}, nil, err
	}
	if transaction.Kind != "signup_email" || !transaction.ExpiresAt.After(service.now()) {
		return AuthTransaction{}, signupPayload{}, nil, ErrNativeSignupUnavailable
	}
	var ciphertext, ipHash []byte
	if tx.QueryRow(ctx, `SELECT payload_ciphertext,ip_bucket_hash FROM tutorhub.auth_transactions WHERE id=$1`, transaction.ID).Scan(&ciphertext, &ipHash) != nil {
		return AuthTransaction{}, signupPayload{}, nil, ErrAuthFoundationStorage
	}
	plaintext, err := service.crypto.Decrypt(ciphertext)
	if err != nil || len(plaintext) > 4096 {
		return AuthTransaction{}, signupPayload{}, nil, ErrNativeSignupUnavailable
	}
	var payload signupPayload
	if json.Unmarshal([]byte(plaintext), &payload) != nil {
		return AuthTransaction{}, signupPayload{}, nil, ErrNativeSignupUnavailable
	}
	email, err := NormalizeSignupEmail(payload.Email)
	if err != nil || email != payload.Email {
		return AuthTransaction{}, signupPayload{}, nil, ErrNativeSignupUnavailable
	}
	if err := service.allowLocalEmail(ctx, tx, email, lock); err != nil {
		return AuthTransaction{}, signupPayload{}, nil, err
	}
	return transaction, payload, ipHash, nil
}

func normalizeSignupDisplayName(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > 512 {
		return "", ErrInvalidProfile
	}
	for _, r := range value {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return "", ErrInvalidProfile
		}
	}
	value = norm.NFC.String(strings.Join(strings.Fields(value), " "))
	if len([]rune(value)) < 1 || len([]rune(value)) > profileNameMaximum {
		return "", ErrInvalidProfile
	}
	return value, nil
}

func (service *NativeAuthService) ipDigest(remote string) []byte {
	prefix := IPPrefix(remote)
	if prefix == "" {
		return nil
	}
	return service.crypto.Digest("auth-ip-v1", prefix)
}

func (service *NativeAuthService) transactionCookie(binding string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: AuthTransactionCookieName(service.config.CookieSecure), Value: binding, HttpOnly: true, Secure: service.config.CookieSecure, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: 900, Expires: expires}
}

func (service *NativeAuthService) newSessionTokens() (NativeSessionResult, error) {
	session, err := service.crypto.RandomToken()
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	csrf, err := service.crypto.RandomToken()
	if err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	return NativeSessionResult{SessionToken: session, CSRFToken: csrf}, nil
}

func (service *NativeAuthService) sessionExpiry(result NativeSessionResult, now time.Time, persistent, admin bool) NativeSessionResult {
	absoluteTTL := service.config.SessionAbsoluteTTL
	if admin {
		persistent = false
		absoluteTTL = min(absoluteTTL, 24*time.Hour)
	} else if persistent {
		absoluteTTL = service.config.SessionPersistentAbsoluteTTL
	}
	result.Persistent = persistent
	result.AbsoluteExpiresAt = now.Add(absoluteTTL)
	result.ExpiresAt = now.Add(min(service.config.SessionTTL, absoluteTTL))
	return result
}
