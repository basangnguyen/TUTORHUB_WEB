package identity

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	registrationKind           = "federated_registration"
	registrationBindingPurpose = "registration-binding-v1"
)

func RegistrationTransactionCookieName(secure bool) string {
	if secure {
		return "__Host-tutorhub_registration_tx"
	}
	return "tutorhub_registration_tx"
}

type PendingRegistrationView struct {
	Provider         string `json:"provider"`
	MaskedEmail      string `json:"masked_email,omitempty"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type RegistrationCompleteInput struct {
	ChallengeID                                  uuid.UUID
	Code, TermsVersion, UserAgent, RemoteAddress string
	KeepSignedIn, SignupEnabled                  bool
}

type RegistrationResult struct {
	Session       NativeSessionResult
	LinkBinding   string
	LinkExpiresAt time.Time
	ReturnTo      string
}

func (RegistrationCompleteInput) String() string { return "[redacted registration input]" }
func (RegistrationCompleteInput) LogValue() slog.Value {
	return slog.StringValue("[redacted registration input]")
}
func (RegistrationResult) String() string { return "[redacted registration result]" }
func (RegistrationResult) LogValue() slog.Value {
	return slog.StringValue("[redacted registration result]")
}

type PendingRegistrationAPI interface {
	PreviewRegistration(context.Context, string) (PendingRegistrationView, error)
	StartRegistration(context.Context, string, SignupStartInput) (SignupChallengeResult, error)
	ResendRegistration(context.Context, string, func(context.Context, string) (bool, error)) (SignupChallengeResult, error)
	CompleteRegistration(context.Context, string, RegistrationCompleteInput) (RegistrationResult, error)
	CancelRegistration(context.Context, string) error
}

type registrationPayload struct {
	FederationIdentityPayload
	OTPEmail string `json:"otp_email,omitempty"`
}

type pendingRegistration struct {
	AuthTransaction
	payload registrationPayload
	digest  []byte
}

// ConfigureRegistrationProviders binds pending registrations to the currently
// enabled providers and tenant policy at startup.
func (service *NativeAuthService) ConfigureRegistrationProviders(registry *ProviderRegistry) {
	service.registrationProviders = registry
}

func (service *NativeAuthService) registrationProviderAllowed(p FederationIdentityPayload) bool {
	if !supportsPendingRegistration(p.ProviderSlug) {
		return false
	}
	if service.registrationProviders == nil {
		// Standalone AUTH-06A callers predate the registry and support Facebook only.
		return p.ProviderSlug == "facebook"
	}
	entry, err := service.registrationProviders.lookup(p.ProviderSlug)
	return err == nil && entry.matchesIssuer(p.Issuer)
}

func (service *NativeAuthService) readRegistration(ctx context.Context, tx pgx.Tx, binding string, lock bool) (pendingRegistration, error) {
	var registration pendingRegistration
	if len(binding) != 43 {
		return registration, ErrAuthTransactionUnavailable
	}
	query := `SELECT id,kind,created_at,expires_at,payload_ciphertext,provider_subject_digest
		FROM tutorhub.auth_transactions WHERE binding_hash=$1 AND kind='federated_registration'
		AND state='pending' AND expires_at>$2`
	if lock {
		query += " FOR UPDATE"
	}
	var ciphertext []byte
	err := tx.QueryRow(ctx, query, service.crypto.Digest(registrationBindingPurpose, binding), service.now().UTC()).
		Scan(&registration.ID, &registration.Kind, &registration.CreatedAt, &registration.ExpiresAt, &ciphertext, &registration.digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return registration, ErrAuthTransactionUnavailable
	}
	if err != nil {
		return registration, ErrAuthFoundationStorage
	}
	raw, err := service.crypto.Decrypt(ciphertext)
	if err != nil || len(raw) > 8192 || json.Unmarshal([]byte(raw), &registration.payload) != nil {
		return registration, ErrAuthTransactionUnavailable
	}
	p := registration.payload
	if p.OTPEmail != "" {
		email, err := NormalizeSignupEmail(p.OTPEmail)
		if err != nil || email != p.OTPEmail {
			return registration, ErrAuthTransactionUnavailable
		}
	}
	returnTo, err := NormalizeReturnTo(p.ReturnTo)
	if err != nil || returnTo != p.ReturnTo || !service.registrationProviderAllowed(p.FederationIdentityPayload) || p.Issuer == "" || p.Subject == "" || p.EmailVerified ||
		!hmac.Equal(registration.digest, service.crypto.ProviderSubjectDigest(p.Issuer, p.Subject)) || !registration.ExpiresAt.After(service.now()) {
		return registration, ErrAuthTransactionUnavailable
	}
	return registration, nil
}

func (service *NativeAuthService) registrationSnapshot(ctx context.Context, binding string) (pendingRegistration, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return pendingRegistration{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	return service.readRegistration(ctx, tx, binding, false)
}

func (service *NativeAuthService) PreviewRegistration(ctx context.Context, binding string) (PendingRegistrationView, error) {
	r, err := service.registrationSnapshot(ctx, binding)
	if err != nil {
		return PendingRegistrationView{}, err
	}
	return PendingRegistrationView{Provider: r.payload.ProviderSlug, MaskedEmail: registrationEmailHint(r.payload.Email), ExpiresInSeconds: max(1, int(r.ExpiresAt.Sub(service.now()).Seconds()))}, nil
}

func registrationEmailHint(email string) string {
	normalized, err := NormalizeSignupEmail(email)
	if err != nil {
		return ""
	}
	at := strings.LastIndexByte(normalized, '@')
	if at < 1 {
		return ""
	}
	local := []rune(normalized[:at])
	prefix := ""
	if len(local) > 1 {
		prefix = string(local[0])
	}
	return prefix + "***" + normalized[at:]
}

func (service *NativeAuthService) StartRegistration(ctx context.Context, binding string, input SignupStartInput) (SignupChallengeResult, error) {
	email, err := NormalizeSignupEmail(input.Email)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	return service.issueRegistrationChallenge(ctx, binding, email, input.DestinationAllowed, false)
}

func (service *NativeAuthService) ResendRegistration(ctx context.Context, binding string, allowDestination func(context.Context, string) (bool, error)) (SignupChallengeResult, error) {
	r, err := service.registrationSnapshot(ctx, binding)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	email := r.payload.OTPEmail
	if email == "" {
		return SignupChallengeResult{}, ErrAuthTransactionUnavailable
	}
	if allowDestination == nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	allowed, err := allowDestination(ctx, email)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	return service.issueRegistrationChallenge(ctx, binding, email, allowed, true)
}

func (service *NativeAuthService) issueRegistrationChallenge(ctx context.Context, binding, email string, allowed, resend bool) (SignupChallengeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	r, err := service.readRegistration(ctx, tx, binding, true)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if resend && r.payload.OTPEmail != email {
		return SignupChallengeResult{}, ErrAuthTransactionUnavailable
	}
	challenge, job, err := service.challenges.issueLocked(ctx, tx, r.AuthTransaction, ChallengeInput{Purpose: "signup_email", Channel: DeliveryEmail, Destination: email, Deliverable: allowed})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if r.payload.OTPEmail != email {
		r.payload.OTPEmail = email
		raw, err := json.Marshal(r.payload)
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
		ciphertext, err := service.crypto.Encrypt(string(raw))
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
		if _, err = tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET payload_ciphertext=$2 WHERE id=$1`, r.ID, ciphertext); err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
	}
	if tx.Commit(ctx) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	service.challenges.dispatchCommitted(job)
	return SignupChallengeResult{ChallengeID: challenge.ID, CooldownSeconds: int(ChallengeCooldown.Seconds()), ExpiresInSeconds: max(1, int(challenge.ExpiresAt.Sub(service.now()).Seconds()))}, nil
}

func (service *NativeAuthService) CompleteRegistration(ctx context.Context, binding string, input RegistrationCompleteInput) (RegistrationResult, error) {
	if input.TermsVersion != service.config.LegalTermsVersion {
		return RegistrationResult{}, ErrNativeTermsVersion
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	// Erasure, link confirmation and registration all acquire the subject lock first.
	snapshot, err := service.readRegistration(ctx, tx, binding, false)
	if err != nil {
		return RegistrationResult{}, err
	}
	claims := snapshot.payload.claims()
	if err := lockProviderSubject(ctx, tx, claims); err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	r, err := service.readRegistration(ctx, tx, binding, true)
	if err != nil {
		return RegistrationResult{}, err
	}
	if r.ID != snapshot.ID || r.payload.FederationIdentityPayload != snapshot.payload.FederationIdentityPayload || r.payload.OTPEmail == "" {
		return RegistrationResult{}, ErrAuthTransactionUnavailable
	}

	var bound bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tutorhub.auth_challenges WHERE id=$1 AND transaction_id=$2
		AND channel='email' AND purpose='signup_email' AND destination_hash=$3)`, input.ChallengeID, r.ID,
		service.crypto.Digest("auth-dest-v1", "email:"+r.payload.OTPEmail)).Scan(&bound); err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	if !bound {
		return RegistrationResult{}, ErrNativeInvalidCode
	}
	matched, err := service.challenges.verifyLocked(ctx, tx, r.AuthTransaction, input.ChallengeID, input.Code)
	if err != nil {
		return RegistrationResult{}, err
	}
	if !matched {
		if tx.Commit(ctx) != nil {
			return RegistrationResult{}, ErrAuthFoundationStorage
		}
		return RegistrationResult{}, ErrNativeInvalidCode
	}

	// Another flow may already have attached the same subject. Require a new provider login.
	_, err = readFederatedIdentity(ctx, tx, claims, false)
	if err == nil {
		return RegistrationResult{}, ErrAuthTransactionUnavailable
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	if !r.ExpiresAt.After(now) {
		return RegistrationResult{}, ErrAuthTransactionUnavailable
	}
	userID, created, err := registrationOwner(ctx, tx, r.payload.OTPEmail, input.SignupEnabled, now)
	if err != nil {
		return RegistrationResult{}, err
	}
	var result RegistrationResult
	if created {
		result, err = service.registerFederatedUser(ctx, tx, userID, r, claims, input, now)
	} else {
		result, err = service.registrationLink(ctx, tx, userID, r, now)
	}
	if err != nil {
		return RegistrationResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=$2 WHERE id=$1`, r.ID, now); err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	if tx.Commit(ctx) != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	return result, nil
}

func registrationOwner(ctx context.Context, tx pgx.Tx, email string, signupEnabled bool, now time.Time) (uuid.UUID, bool, error) {
	var userID uuid.UUID
	if signupEnabled {
		localName := email[:strings.LastIndexByte(email, '@')]
		displayName, err := normalizeSignupDisplayName(localName)
		if err != nil {
			displayName = "TutorHub"
		}
		err = tx.QueryRow(ctx, `INSERT INTO tutorhub.users(email,display_name,email_verified_at,created_at,updated_at)
			VALUES($1,$2,$3,$3,$3) ON CONFLICT DO NOTHING RETURNING id`, email, displayName, now).Scan(&userID)
		if err == nil {
			return userID, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, ErrAuthFoundationStorage
		}
	}
	err := tx.QueryRow(ctx, `SELECT id FROM tutorhub.users WHERE lower(email)=$1 AND status='active' FOR UPDATE`, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		if !signupEnabled {
			return uuid.Nil, false, ErrFederationSignupDisabled
		}
		return uuid.Nil, false, ErrNativeSignupUnavailable
	}
	if err != nil {
		return uuid.Nil, false, ErrAuthFoundationStorage
	}
	return userID, false, nil
}

func (service *NativeAuthService) registerFederatedUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID, r pendingRegistration, claims ProviderClaims, input RegistrationCompleteInput, now time.Time) (RegistrationResult, error) {
	if _, err := ensureNativeEmailIdentity(ctx, tx, userID, r.payload.OTPEmail, now); err != nil {
		return RegistrationResult{}, err
	}
	identityID, err := insertFederatedIdentity(ctx, tx, userID, claims.ProviderSlug, claims, now)
	if err != nil {
		return RegistrationResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.user_legal_acceptances(user_id,document_kind,version,accepted_at,ip_bucket_hash)
		VALUES($1,'user_agreement',$2,$3,$4),($1,'privacy_policy',$2,$3,$4)`, userID, input.TermsVersion, now, service.ipDigest(input.RemoteAddress)); err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	session, err := service.newSessionTokens()
	if err != nil {
		return RegistrationResult{}, err
	}
	session = service.sessionExpiry(session, now, input.KeepSignedIn, false)
	if err := service.insertNativeSession(ctx, tx, userID, identityID, &session, []string{"fed", "email_code"}, input.UserAgent, input.RemoteAddress, now); err != nil {
		return RegistrationResult{}, err
	}
	return RegistrationResult{Session: session, ReturnTo: r.payload.ReturnTo}, nil
}

func (service *NativeAuthService) registrationLink(ctx context.Context, tx pgx.Tx, userID uuid.UUID, r pendingRegistration, now time.Time) (RegistrationResult, error) {
	raw, err := json.Marshal(r.payload.FederationIdentityPayload)
	if err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	ciphertext, err := service.crypto.Encrypt(string(raw))
	if err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	binding, err := service.crypto.RandomToken()
	if err != nil {
		return RegistrationResult{}, ErrAuthFoundationStorage
	}
	expires := now.Add(AuthTransactionTTL)
	err = insertFederationLinkIntent(ctx, tx, userID, FederationLoginParams{ProviderSlug: r.payload.ProviderSlug, ProviderSubjectDigest: r.digest,
		LinkBindingHash: service.crypto.Digest(linkBindingPurpose, binding), LinkPayloadCiphertext: ciphertext, LinkExpiresAt: expires, Metadata: SessionMetadata{CreatedAt: now}})
	if err != nil {
		return RegistrationResult{}, err
	}
	return RegistrationResult{LinkBinding: binding, LinkExpiresAt: expires, ReturnTo: "/account-link"}, nil
}

func (service *NativeAuthService) CancelRegistration(ctx context.Context, binding string) error {
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	r, err := service.readRegistration(ctx, tx, binding, true)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='cancelled' WHERE id=$1`, r.ID); err != nil {
		return ErrAuthFoundationStorage
	}
	if tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}
