//go:build integration

package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func federationIntegrationInput(t *testing.T, f *settingsIntegrationFixture, email string) FederationLoginParams {
	t.Helper()
	claims := ProviderClaims{Issuer: "https://accounts.google.com", Subject: uuid.NewString(), Email: email, EmailVerified: true, DisplayName: "Federation member", ProviderOrgID: "school.example", AuthTime: f.now}
	payload := FederationIdentityPayload{ProviderSlug: "google", Issuer: claims.Issuer, Subject: claims.Subject, Email: email, EmailVerified: true, DisplayName: claims.DisplayName, ProviderOrgID: claims.ProviderOrgID, ReturnTo: "/app/settings"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := f.service.crypto.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return FederationLoginParams{Claims: claims, ProviderSlug: "google", Flow: StoredFlow{Purpose: "login", ProviderSlug: "google", CreatedAt: f.now}, SignupEnabled: true, LegalTermsVersion: "test", SessionAbsoluteTTL: 48 * time.Hour,
		ProviderSubjectDigest: f.service.crypto.ProviderSubjectDigest(claims.Issuer, claims.Subject),
		LinkBindingHash:       f.service.crypto.Digest(linkBindingPurpose, uuid.NewString()), LinkPayloadCiphertext: cipher, LinkExpiresAt: f.now.Add(15 * time.Minute),
		Metadata: SessionMetadata{TokenHash: f.service.crypto.Digest(sessionPurpose, uuid.NewString()), CSRFHash: f.service.crypto.Digest(csrfPurpose, uuid.NewString()), UserAgentHash: f.service.crypto.Digest(userAgentPurpose, "fixture"), DeviceLabel: "Chrome · Windows", CreatedAt: f.now, ExpiresAt: f.now.Add(8 * time.Hour), AbsoluteAt: f.now.Add(48 * time.Hour)}}
}

func federationConfirmInput(t *testing.T, f *settingsIntegrationFixture, repository *PostgresRepository, input FederationLoginParams) LinkIntentConfirmParams {
	t.Helper()
	intent, err := repository.ReadLinkIntent(f.ctx, input.LinkBindingHash, f.now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.service.crypto.Decrypt(intent.PayloadCiphertext)
	if err != nil {
		t.Fatal(err)
	}
	var payload FederationIdentityPayload
	if json.Unmarshal([]byte(raw), &payload) != nil {
		t.Fatal("decode fixture payload")
	}
	return LinkIntentConfirmParams{Principal: f.principal, BindingHash: input.LinkBindingHash, Intent: intent, Payload: payload, SessionAbsoluteTTL: 48 * time.Hour,
		Rotation: SessionRotation{TokenHash: f.service.crypto.Digest(sessionPurpose, uuid.NewString()), CSRFHash: f.service.crypto.Digest(csrfPurpose, uuid.NewString()), ExpectedContextVersion: f.principal.ContextVersion, RotatedAt: f.now}}
}

func TestFederationPostgresFlowProviderIsolationAndOneTime(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	for _, slug := range []string{"oidc", "google"} {
		// auth_flows requires a real AEAD ciphertext (>= 29 bytes).
		verifierCiphertext, err := f.service.crypto.Encrypt("fixture-code-verifier-" + slug)
		if err != nil {
			t.Fatal(err)
		}
		input := CreateFlowParams{ProviderSlug: slug, Purpose: "login", StateHash: f.service.crypto.Digest(statePurpose, uuid.NewString()), BrowserBindingHash: f.service.crypto.Digest(browserBindingPurpose, uuid.NewString()), NonceHash: f.service.crypto.Digest(noncePurpose, "nonce"), CodeVerifierCiphertext: verifierCiphertext, ReturnTo: "/app", CreatedAt: f.now, ExpiresAt: f.now.Add(10 * time.Minute)}
		if err := repository.CreateFlow(f.ctx, input); err != nil {
			t.Fatal(err)
		}
		wrong := "google"
		if slug == "google" {
			wrong = "oidc"
		}
		if _, err := repository.ConsumeProviderFlow(f.ctx, wrong, input.StateHash, input.BrowserBindingHash, f.now); !errors.Is(err, ErrInvalidAuthFlow) {
			t.Fatal("provider mix-up accepted")
		}
		var flow StoredFlow
		if slug == "oidc" {
			flow, err = repository.ConsumeFlow(f.ctx, input.StateHash, input.BrowserBindingHash, f.now)
		} else {
			flow, err = repository.ConsumeProviderFlow(f.ctx, slug, input.StateHash, input.BrowserBindingHash, f.now)
		}
		if err != nil || flow.ProviderSlug != slug || !flow.CreatedAt.Equal(f.now) {
			t.Fatal("correct callback lost provider binding")
		}
		if _, err := repository.ConsumeProviderFlow(f.ctx, slug, input.StateHash, input.BrowserBindingHash, f.now); !errors.Is(err, ErrInvalidAuthFlow) {
			t.Fatal("flow replay accepted")
		}
	}
}

func TestFederationPostgresSignupPolicyLegalAndNoAutoMerge(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	input := federationIntegrationInput(t, f, "federated-"+uuid.NewString()+"@example.test")
	input.SignupEnabled = false
	if _, err := repository.CompleteFederatedLogin(f.ctx, input); !errors.Is(err, ErrFederationSignupDisabled) {
		t.Fatal("signup-off bootstrapped account")
	}
	input.SignupEnabled = true
	input.Claims.EmailVerified = false
	if _, err := repository.CompleteFederatedLogin(f.ctx, input); !errors.Is(err, ErrVerifiedEmailRequired) {
		t.Fatal("unverified provider bootstrapped account")
	}
	input.Claims.EmailVerified = true
	result, err := repository.CompleteFederatedLogin(f.ctx, input)
	if err != nil || result.LinkPending || result.Principal.User.ID == uuid.Nil {
		t.Fatal("verified signup failed")
	}
	var verified bool
	var legal int
	var label, slug, org string
	if f.tx.QueryRow(f.ctx, `SELECT u.email_verified_at IS NOT NULL,(SELECT count(*) FROM tutorhub.user_legal_acceptances WHERE user_id=u.id AND version='test'),s.device_label,i.provider_slug,i.provider_org_id FROM tutorhub.users u JOIN tutorhub.sessions s ON s.user_id=u.id JOIN tutorhub.identities i ON i.id=s.identity_id WHERE s.id=$1`, result.Principal.SessionID).Scan(&verified, &legal, &label, &slug, &org) != nil || !verified || legal != 2 || label != "Chrome · Windows" || slug != "google" || org != "school.example" {
		t.Fatal("atomic signup metadata/legal acceptance missing")
	}
	collision := federationIntegrationInput(t, f, f.principal.User.Email)
	collision.SignupEnabled = false
	pending, err := repository.CompleteFederatedLogin(f.ctx, collision)
	if err != nil || !pending.LinkPending || pending.Principal.SessionID != uuid.Nil {
		t.Fatal("email collision auto-merged or failed to create intent")
	}
	stored, err := repository.ReadLinkIntent(f.ctx, collision.LinkBindingHash, f.now)
	if err != nil || stored.UserID != f.principal.User.ID {
		t.Fatal("collision intent has wrong owner")
	}
	var sessions int
	if f.tx.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.sessions WHERE token_hash=$1`, collision.Metadata.TokenHash).Scan(&sessions) != nil || sessions != 0 {
		t.Fatal("collision created a session")
	}
	// An unlinked identity remains owned by its original account even if its
	// provider email now matches a different user.
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$2 WHERE id=$1`, result.Principal.IdentityID, f.now); err != nil {
		t.Fatal(err)
	}
	input.Claims.Email = f.principal.User.Email
	input.LinkBindingHash = f.service.crypto.Digest(linkBindingPurpose, uuid.NewString())
	pending, err = repository.CompleteFederatedLogin(f.ctx, input)
	if err != nil || !pending.LinkPending {
		t.Fatal("unlinked identity logged in")
	}
	stored, err = repository.ReadLinkIntent(f.ctx, input.LinkBindingHash, f.now)
	if err != nil || stored.UserID != result.Principal.User.ID {
		t.Fatal("unlinked identity changed owner through email")
	}
}

func TestFacebookPostgresLoginRoutesBySubject(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repo := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	for _, sample := range []struct{ subject, hint string }{{"100000123456701", ""}, {"100000123456702", f.principal.User.Email}} {
		hint := sample.hint
		input := federationIntegrationInput(t, f, hint)
		input.ProviderSlug, input.Flow.ProviderSlug = "facebook", "facebook"
		input.Claims = ProviderClaims{ProviderSlug: "facebook", Issuer: FacebookIssuerURL, Subject: sample.subject, Email: hint, AuthTime: f.now}
		payload := FederationIdentityPayload{ProviderSlug: "facebook", Issuer: input.Claims.Issuer, Subject: sample.subject, Email: hint, ReturnTo: "/app"}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		input.LinkPayloadCiphertext, err = f.service.crypto.Encrypt(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		input.ProviderSubjectDigest = f.service.crypto.ProviderSubjectDigest(input.Claims.Issuer, input.Claims.Subject)
		input.RegistrationBindingHash = f.service.crypto.Digest(registrationBindingPurpose, uuid.NewString())
		input.SignupEnabled = false
		result, err := repo.CompleteFederatedLogin(f.ctx, input)
		if err != nil || !result.RegistrationPending || result.LinkPending || result.Principal.SessionID != uuid.Nil {
			t.Fatalf("unknown Facebook subject did not enter pending registration: %v", err)
		}
		var digest []byte
		if err := f.tx.QueryRow(f.ctx, `SELECT provider_subject_digest FROM tutorhub.auth_transactions WHERE kind='federated_registration'
			AND state='pending' AND user_id IS NULL AND binding_hash=$1`, input.RegistrationBindingHash).Scan(&digest); err != nil || !bytes.Equal(digest, input.ProviderSubjectDigest) {
			t.Fatal("new subject did not create a browser-bound pending transaction")
		}
		id, err := insertFederatedIdentity(f.ctx, f.tx, f.principal.User.ID, "facebook", input.Claims, f.now)
		if err != nil {
			t.Fatal(err)
		}
		result, err = repo.CompleteFederatedLogin(f.ctx, input)
		if err != nil || result.Principal.IdentityID != id || result.Principal.User.ID != f.principal.User.ID {
			t.Fatalf("existing subject did not create an owner session: %v", err)
		}
		identities, err := repo.ListIdentities(f.ctx, f.principal.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range identities {
			if item.ID == id {
				found = item.ProviderSlug == "facebook" && !item.EmailVerified && item.Email == hint
			}
		}
		if !found {
			t.Fatal("public identity omitted the provider slug or trusted the Facebook email")
		}
		if _, err = f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$2 WHERE id=$1`, id, f.now); err != nil {
			t.Fatal(err)
		}
		result, err = repo.CompleteFederatedLogin(f.ctx, input)
		if err != nil || !result.LinkPending || result.Principal.SessionID != uuid.Nil {
			t.Fatalf("unlinked subject did not create a link intent: %v", err)
		}
		intent, err := repo.ReadLinkIntent(f.ctx, input.LinkBindingHash, f.now)
		if err != nil || intent.UserID != f.principal.User.ID || intent.TargetEmail != f.principal.User.Email {
			t.Fatal("link intent lost the original owner or the owner's email")
		}
	}
}

func TestFederationPostgresConfirmAtomicAssuranceAndReplay(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	input := federationIntegrationInput(t, f, f.principal.User.Email)
	if result, err := repository.CompleteFederatedLogin(f.ctx, input); err != nil || !result.LinkPending {
		t.Fatal("create link intent")
	}
	confirm := federationConfirmInput(t, f, repository, input)
	wrong := confirm
	wrong.Principal.User.ID = uuid.New()
	if _, err := repository.ConfirmLinkIntent(f.ctx, wrong); !errors.Is(err, ErrLinkIntentMismatch) {
		t.Fatal("different account confirmed intent")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET assurance_level=2 WHERE id=$1`, f.otherSession); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ConfirmLinkIntent(f.ctx, confirm); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("weak session bypassed account assurance")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET assurance_level=1 WHERE id=$1`, f.otherSession); err != nil {
		t.Fatal(err)
	}
	broken := confirm
	broken.Rotation.CSRFHash = nil
	if _, err := repository.ConfirmLinkIntent(f.ctx, broken); !errors.Is(err, ErrAuthFoundationStorage) {
		t.Fatal("invalid rotation did not fail atomically")
	}
	var stillPending, otherActive bool
	var linkedCount int
	if f.tx.QueryRow(f.ctx, `SELECT
        EXISTS(SELECT 1 FROM tutorhub.auth_transactions WHERE id=$1 AND state='pending'),
        EXISTS(SELECT 1 FROM tutorhub.sessions WHERE id=$2 AND revoked_at IS NULL),
        (SELECT count(*) FROM tutorhub.identities WHERE provider=$3 AND subject=$4)`,
		confirm.Intent.ID, f.otherSession, input.Claims.Issuer, input.Claims.Subject).Scan(&stillPending, &otherActive, &linkedCount) != nil || !stillPending || !otherActive || linkedCount != 0 {
		t.Fatal("failed confirmation partially linked, consumed or revoked state")
	}
	principal, err := repository.ConfirmLinkIntent(f.ctx, confirm)
	if err != nil || principal.ContextVersion != 2 || !principal.CookieExpiresAt.Equal(f.now.Add(30*24*time.Hour)) {
		t.Fatal("confirm did not preserve/rotate current session")
	}
	var state, reason, payload string
	var owner uuid.UUID
	var token, csrf []byte
	if f.tx.QueryRow(f.ctx, `SELECT state FROM tutorhub.auth_transactions WHERE id=$1`, confirm.Intent.ID).Scan(&state) != nil || state != "completed" {
		t.Fatal("intent not consumed")
	}
	if f.tx.QueryRow(f.ctx, `SELECT user_id FROM tutorhub.identities WHERE provider=$1 AND subject=$2 AND status='active'`, input.Claims.Issuer, input.Claims.Subject).Scan(&owner) != nil || owner != f.principal.User.ID {
		t.Fatal("identity linked incorrectly")
	}
	if f.tx.QueryRow(f.ctx, `SELECT revoked_reason FROM tutorhub.sessions WHERE id=$1 AND revoked_at IS NOT NULL`, f.otherSession).Scan(&reason) != nil || reason != "identity_linked" {
		t.Fatal("other sessions survived link")
	}
	if f.tx.QueryRow(f.ctx, `SELECT token_hash,csrf_token_hash FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).Scan(&token, &csrf) != nil || !bytes.Equal(token, confirm.Rotation.TokenHash) || !bytes.Equal(csrf, confirm.Rotation.CSRFHash) {
		t.Fatal("session proof not atomically rotated")
	}
	if f.tx.QueryRow(f.ctx, `SELECT payload::text FROM tutorhub.outbox_events WHERE aggregate_id=$1 AND event_type='identity_linked'`, f.principal.User.ID).Scan(&payload) != nil || strings.Contains(payload, input.Claims.Email) || strings.Contains(payload, input.Claims.Subject) {
		t.Fatal("link notice missing or leaks raw identity")
	}
	confirm.Principal = principal
	if _, err := repository.ConfirmLinkIntent(f.ctx, confirm); !errors.Is(err, ErrLinkIntentUnavailable) {
		t.Fatal("consumed intent replayed")
	}
}

func TestFederationPostgresRelinkCancelAndExpiry(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	input := federationIntegrationInput(t, f, f.principal.User.Email)
	identityID, err := insertFederatedIdentity(f.ctx, f.tx, f.principal.User.ID, "google", input.Claims, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$2 WHERE id=$1`, identityID, f.now); err != nil {
		t.Fatal(err)
	}
	if result, err := repository.CompleteFederatedLogin(f.ctx, input); err != nil || !result.LinkPending {
		t.Fatal("unlinked login did not require confirmation")
	}
	confirm := federationConfirmInput(t, f, repository, input)
	confirm.Payload.ProviderOrgID = "different.example"
	principal, err := repository.ConfirmLinkIntent(f.ctx, confirm)
	if err != nil {
		t.Fatal("explicit relink failed")
	}
	f.principal = principal
	var status, org string
	if f.tx.QueryRow(f.ctx, `SELECT status,provider_org_id FROM tutorhub.identities WHERE id=$1`, identityID).Scan(&status, &org) != nil || status != "active" || org != "school.example" {
		t.Fatal("relink failed or changed immutable provider metadata")
	}
	cancelInput := federationIntegrationInput(t, f, f.principal.User.Email)
	if _, err := repository.CompleteFederatedLogin(f.ctx, cancelInput); err != nil {
		t.Fatal(err)
	}
	if err := repository.CancelLinkIntent(f.ctx, cancelInput.LinkBindingHash, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReadLinkIntent(f.ctx, cancelInput.LinkBindingHash, f.now); !errors.Is(err, ErrLinkIntentUnavailable) {
		t.Fatal("cancelled intent visible")
	}
	expired := federationIntegrationInput(t, f, f.principal.User.Email)
	if _, err := repository.CompleteFederatedLogin(f.ctx, expired); err != nil {
		t.Fatal(err)
	}
	expiredConfirm := federationConfirmInput(t, f, repository, expired)
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.auth_transactions SET created_at=$2,expires_at=$3 WHERE id=$1`, expiredConfirm.Intent.ID, f.now.Add(-20*time.Minute), f.now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ConfirmLinkIntent(f.ctx, expiredConfirm); !errors.Is(err, ErrLinkIntentUnavailable) {
		t.Fatal("expired intent consumed")
	}
}

func TestFederationPostgresReauthOwnerFreshnessRotationAndAdminCap(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	own := federationIntegrationInput(t, f, f.principal.User.Email)
	if _, err := insertFederatedIdentity(f.ctx, f.tx, f.principal.User.ID, "google", own.Claims, f.now); err != nil {
		t.Fatal(err)
	}
	foreign := federationIntegrationInput(t, f, "foreign-fed-"+uuid.NewString()+"@example.test")
	if _, err := repository.CompleteFederatedLogin(f.ctx, foreign); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	input := FederationReauthParams{Flow: StoredFlow{ProviderSlug: "google", Purpose: "reauth", UserID: f.principal.User.ID, SessionID: f.principal.SessionID, CreatedAt: f.now}, Claims: foreign.Claims, SessionAbsoluteTTL: 48 * time.Hour,
		Rotation: SessionRotation{RotatedAt: f.now, TokenHash: f.service.crypto.Digest(sessionPurpose, "reauth-new"), CSRFHash: f.service.crypto.Digest(csrfPurpose, "reauth-new")}}
	input.Claims.AuthTime = f.now
	if _, err := repository.ReauthenticateFederated(f.ctx, input); !errors.Is(err, ErrFederatedIdentityMismatch) {
		t.Fatal("another account reauthenticated current session")
	}
	input.Claims = own.Claims
	if _, err := repository.ReauthenticateFederated(f.ctx, input); !errors.Is(err, ErrFederationReauthRequired) {
		t.Fatal("stale provider authentication accepted")
	}
	input.Claims.AuthTime = f.now
	principal, err := repository.ReauthenticateFederated(f.ctx, input)
	if err != nil || !principal.AuthenticatedAt.Equal(f.now) || principal.ContextVersion != 2 || !principal.CookieExpiresAt.Equal(f.now.Add(-time.Minute).Add(30*24*time.Hour)) {
		t.Fatal("federated reauth did not rotate/preserve lifetime")
	}
	tenant := uuid.New()
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Federation admin')`, tenant, "fed-"+tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status,joined_at) VALUES($1,$2,'org_admin','active',$3)`, tenant, f.principal.User.ID, f.now); err != nil {
		t.Fatal(err)
	}
	input.Rotation.TokenHash = f.service.crypto.Digest(sessionPurpose, "reauth-admin")
	input.Rotation.CSRFHash = f.service.crypto.Digest(csrfPurpose, "reauth-admin")
	if _, err := repository.ReauthenticateFederated(f.ctx, input); err != nil {
		t.Fatal(err)
	}
	var persistent bool
	var absolute, created time.Time
	if f.tx.QueryRow(f.ctx, `SELECT persistent,absolute_expires_at,created_at FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).Scan(&persistent, &absolute, &created) != nil || persistent || absolute.After(created.Add(24*time.Hour)) {
		t.Fatal("federated reauth escaped administrator cap")
	}
}

func TestFederationPostgresUnlinkCountsPasswordAndEveryIdentity(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	if err := repository.UnlinkIdentity(f.ctx, f.principal.User.ID, f.principal.SessionID, f.principal.IdentityID, f.now); !errors.Is(err, ErrLastIdentity) {
		t.Fatal("last method removed")
	}
	if err := upsertNativePassword(f.ctx, f.tx, f.principal.User.ID, dummyPasswordPHC, f.now); err != nil {
		t.Fatal(err)
	}
	if err := repository.UnlinkIdentity(f.ctx, f.principal.User.ID, f.principal.SessionID, f.principal.IdentityID, f.now); err != nil {
		t.Fatal("last federated identity blocked despite active password")
	}
	for _, kind := range []string{"email", "phone", "passkey"} {
		t.Run(kind, func(t *testing.T) {
			other := newSettingsIntegrationFixture(t, true)
			repo := NewPostgresRepository(other.tx, 20*time.Second, policy.NewEngine())
			if _, err := other.tx.Exec(other.ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject,kind,provider_slug)
                VALUES($1,$2,$3,$4,$4)`, other.principal.User.ID, "urn:tutorhub:"+kind, uuid.NewString(), kind); err != nil {
				t.Fatal("seed alternative login method")
			}
			if err := repo.UnlinkIdentity(other.ctx, other.principal.User.ID, other.principal.SessionID, other.principal.IdentityID, other.now); err != nil {
				t.Fatal("active non-federated identity was excluded from last-method check")
			}
		})
	}
}

func TestFederationPostgresConcurrentConfirmHasOneWinner(t *testing.T) {
	// Committed concurrency fixture writes auth rows and an outbox event only.
	// It never creates audit_events; all fixtures involving audit use outer rollback.
	ctx, pool, _ := authSchemaTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	crypto, err := NewCrypto(bytes.Repeat([]byte{0x41}, 32))
	if err != nil {
		t.Fatal(err)
	}
	userID, sessionID, intentID := uuid.New(), uuid.New(), uuid.New()
	email := "federation-race-" + userID.String() + "@example.test"
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Federation race')`, userID, email); err != nil {
		t.Fatal("seed federation race user")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, err := pool.Begin(cleanup)
		if err != nil {
			t.Error("begin race cleanup")
			return
		}
		defer rollback(tx)
		if _, err = tx.Exec(cleanup, `DELETE FROM tutorhub.outbox_events WHERE aggregate_id=$1 AND event_type='identity_linked'`, userID); err != nil {
			t.Error("clean race outbox")
			return
		}
		if _, err = tx.Exec(cleanup, `DELETE FROM tutorhub.users WHERE id=$1`, userID); err != nil {
			t.Error("clean race user")
			return
		}
		if tx.Commit(cleanup) != nil {
			t.Error("commit race cleanup")
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time) VALUES($1,$2,$3,$4,$5,$5,$6,$7,$5)`, sessionID, userID, crypto.Digest(sessionPurpose, "race-old"), crypto.Digest(csrfPurpose, "race-old"), now, now.Add(8*time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal("seed federation race session")
	}
	payload := FederationIdentityPayload{ProviderSlug: "google", Issuer: "https://accounts.google.com", Subject: uuid.NewString(), Email: email, EmailVerified: true, DisplayName: "Federation race", ReturnTo: "/app"}
	raw, _ := json.Marshal(payload)
	cipher, err := crypto.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	binding := crypto.Digest(linkBindingPurpose, uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(id,binding_hash,kind,state,user_id,payload_ciphertext,created_at,expires_at) VALUES($1,$2,'link_intent','pending',$3,$4,$5,$6)`, intentID, binding, userID, cipher, now, now.Add(15*time.Minute)); err != nil {
		t.Fatal("seed federation race intent")
	}
	repository := NewPostgresRepository(pool, 20*time.Second, policy.NewEngine())
	intent, err := repository.ReadLinkIntent(ctx, binding, now)
	if err != nil {
		t.Fatal(err)
	}
	var wins, unexpected atomic.Int32
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := repository.ConfirmLinkIntent(ctx, LinkIntentConfirmParams{Principal: Principal{User: User{ID: userID}, SessionID: sessionID, ContextVersion: 1}, Intent: intent, Payload: payload, BindingHash: binding, SessionAbsoluteTTL: 24 * time.Hour,
				Rotation: SessionRotation{TokenHash: crypto.Digest(sessionPurpose, uuid.NewString()), CSRFHash: crypto.Digest(csrfPurpose, uuid.NewString()), RotatedAt: now, ExpectedContextVersion: 1}})
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrLinkIntentUnavailable) {
				unexpected.Add(1)
			}
		}()
	}
	close(gate)
	wg.Wait()
	var identities, notices int
	if pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tutorhub.identities WHERE user_id=$1),(SELECT count(*) FROM tutorhub.outbox_events WHERE aggregate_id=$1 AND event_type='identity_linked')`, userID).Scan(&identities, &notices) != nil || wins.Load() != 1 || unexpected.Load() != 0 || identities != 1 || notices != 1 {
		t.Fatal("concurrent confirmation did not have exactly one atomic winner")
	}
}
