package identity

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ FederationRepository = (*PostgresRepository)(nil)

func (repository *PostgresRepository) ReadLinkIntent(ctx context.Context, bindingHash []byte, now time.Time) (StoredLinkIntent, error) {
	ctx, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return StoredLinkIntent{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	return readLinkIntent(ctx, tx, bindingHash, now, false)
}

func readLinkIntent(ctx context.Context, tx pgx.Tx, bindingHash []byte, now time.Time, locked bool) (StoredLinkIntent, error) {
	query := `SELECT t.id,t.user_id,t.payload_ciphertext,t.created_at,t.expires_at,u.email
		FROM tutorhub.auth_transactions t JOIN tutorhub.users u ON u.id=t.user_id
		WHERE t.binding_hash=$1 AND t.kind='link_intent' AND t.state='pending' AND t.expires_at>$2`
	if locked {
		query += " FOR UPDATE OF t"
	}
	var result StoredLinkIntent
	err := tx.QueryRow(ctx, query, bindingHash, now).Scan(&result.ID, &result.UserID, &result.PayloadCiphertext, &result.CreatedAt, &result.ExpiresAt, &result.TargetEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredLinkIntent{}, ErrLinkIntentUnavailable
	}
	if err != nil {
		return StoredLinkIntent{}, ErrAuthFoundationStorage
	}
	return result, nil
}

func (repository *PostgresRepository) CancelLinkIntent(ctx context.Context, bindingHash []byte, now time.Time) error {
	ctx, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	command, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='cancelled' WHERE binding_hash=$1 AND kind='link_intent' AND state='pending' AND expires_at>$2`, bindingHash, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if command.RowsAffected() != 1 {
		return ErrLinkIntentUnavailable
	}
	if tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

type lockedFederationSession struct {
	ActiveTenantID                          uuid.UUID
	CreatedAt, ExpiresAt, AbsoluteExpiresAt time.Time
	Persistent                              bool
	ContextVersion                          int64
	AssuranceLevel                          int16
}

func (repository *PostgresRepository) lockFederationSession(ctx context.Context, tx pgx.Tx, userID, sessionID uuid.UUID, expectedVersion int64, now time.Time) (lockedFederationSession, error) {
	var result lockedFederationSession
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return result, ErrSessionNotFound
	}
	if err := lockActiveTenantUser(ctx, tx, userID); err != nil {
		return result, err
	}
	if err := lockNativeUserSessions(ctx, tx, userID); err != nil {
		return result, err
	}
	var tenantID uuid.NullUUID
	err := tx.QueryRow(ctx, `SELECT active_tenant_id,created_at,expires_at,absolute_expires_at,persistent,context_version,assurance_level FROM tutorhub.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>$3 AND absolute_expires_at>$3`, sessionID, userID, now).Scan(&tenantID, &result.CreatedAt, &result.ExpiresAt, &result.AbsoluteExpiresAt, &result.Persistent, &result.ContextVersion, &result.AssuranceLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrSessionNotFound
	}
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	if expectedVersion > 0 && expectedVersion != result.ContextVersion {
		return result, ErrSessionContextConflict
	}
	result.ActiveTenantID = tenantID.UUID
	return result, nil
}

func requireFederationAccountAssurance(ctx context.Context, tx pgx.Tx, userID uuid.UUID, assurance int16, now time.Time) error {
	var required int16
	// Until factor enrollment exists, the strongest active session is the
	// persisted assurance floor. Never trust a caller-supplied assurance value.
	if tx.QueryRow(ctx, `SELECT COALESCE(max(assurance_level),1) FROM tutorhub.sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>$2 AND absolute_expires_at>$2`, userID, now).Scan(&required) != nil {
		return ErrAuthFoundationStorage
	}
	if assurance < required {
		return ErrFederationAALRequired
	}
	return nil
}

func (repository *PostgresRepository) ConfirmLinkIntent(ctx context.Context, input LinkIntentConfirmParams) (Principal, error) {
	if err := validatePrincipal(input.Principal); err != nil {
		return Principal{}, err
	}
	if input.Intent.UserID != input.Principal.User.ID {
		return Principal{}, ErrLinkIntentMismatch
	}
	if input.Payload.ProviderSlug != "facebook" && !input.Payload.EmailVerified {
		return Principal{}, ErrVerifiedEmailRequired
	}
	ctx, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	claims := ProviderClaims{Issuer: input.Payload.Issuer, Subject: input.Payload.Subject, Email: input.Payload.Email, EmailVerified: input.Payload.ProviderSlug != "facebook" && input.Payload.EmailVerified, DisplayName: input.Payload.DisplayName, ProviderOrgID: input.Payload.ProviderOrgID}
	if err := lockProviderSubject(ctx, tx, claims); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	locked, err := repository.lockFederationSession(ctx, tx, input.Principal.User.ID, input.Principal.SessionID, 0, input.Rotation.RotatedAt)
	if err != nil {
		return Principal{}, err
	}
	intent, err := readLinkIntent(ctx, tx, input.BindingHash, input.Rotation.RotatedAt, true)
	if err != nil {
		return Principal{}, err
	}
	if intent.UserID != input.Principal.User.ID {
		return Principal{}, ErrLinkIntentMismatch
	}
	if intent.ID != input.Intent.ID || !bytes.Equal(intent.PayloadCiphertext, input.Intent.PayloadCiphertext) {
		return Principal{}, ErrLinkIntentUnavailable
	}
	if locked.ContextVersion != input.Principal.ContextVersion {
		return Principal{}, ErrSessionContextConflict
	}
	if err := requireFederationAccountAssurance(ctx, tx, input.Principal.User.ID, locked.AssuranceLevel, input.Rotation.RotatedAt); err != nil {
		return Principal{}, err
	}
	identityID, err := linkFederatedIdentity(ctx, tx, input.Principal.User.ID, input.Payload.ProviderSlug, claims, input.Rotation.RotatedAt)
	if err != nil {
		return Principal{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=$2 WHERE id=$1`, intent.ID, input.Rotation.RotatedAt); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	principal, err := repository.finishFederatedLink(ctx, tx, input.Principal.User.ID, input.Principal.SessionID, identityID, locked, input.Rotation, input.SessionAbsoluteTTL)
	if err != nil {
		return Principal{}, err
	}
	if tx.Commit(ctx) != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	return principal, nil
}

func (repository *PostgresRepository) finishFederatedLink(ctx context.Context, tx pgx.Tx, userID, sessionID, identityID uuid.UUID, locked lockedFederationSession, rotation SessionRotation, absoluteTTL time.Duration) (Principal, error) {
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason='identity_linked' WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, userID, sessionID, rotation.RotatedAt); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	principal, err := repository.rotateFederationSession(ctx, tx, userID, sessionID, locked, rotation, absoluteTTL, false)
	if err != nil {
		return Principal{}, err
	}
	if err := insertIdentityEvent(ctx, tx, locked.ActiveTenantID, userID, "identity_linked", identityID, rotation.RotatedAt); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	return principal, nil
}

func (repository *PostgresRepository) ReauthenticateFederated(ctx context.Context, input FederationReauthParams) (Principal, error) {
	if input.Flow.Purpose != "reauth" || input.Flow.ProviderSlug == "facebook" || !input.Claims.EmailVerified {
		return Principal{}, ErrInvalidAuthFlow
	}
	if input.Claims.AuthTime.IsZero() || input.Flow.CreatedAt.IsZero() ||
		input.Claims.AuthTime.Before(input.Flow.CreatedAt.Add(-time.Second)) ||
		input.Claims.AuthTime.After(input.Rotation.RotatedAt.Add(time.Minute)) {
		return Principal{}, ErrFederationReauthRequired
	}
	ctx, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err := lockProviderSubject(ctx, tx, input.Claims); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	locked, err := repository.lockFederationSession(ctx, tx, input.Flow.UserID, input.Flow.SessionID, input.Rotation.ExpectedContextVersion, input.Rotation.RotatedAt)
	if err != nil {
		return Principal{}, err
	}
	if locked.AssuranceLevel > 1 {
		return Principal{}, ErrFederationAALRequired
	}
	if err := requireFederationAccountAssurance(ctx, tx, input.Flow.UserID, 1, input.Rotation.RotatedAt); err != nil {
		return Principal{}, err
	}
	existing, err := readFederatedIdentity(ctx, tx, input.Claims, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrFederatedIdentityMismatch
	}
	if err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	if existing.UserID != input.Flow.UserID || existing.Status != "active" || existing.ProviderSlug != input.Flow.ProviderSlug {
		return Principal{}, ErrFederatedIdentityMismatch
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.identities SET last_authenticated_at=$3,updated_at=$3,email_at_provider=$4,email_verified=true WHERE id=$1 AND user_id=$2`, existing.ID, input.Flow.UserID, input.Rotation.RotatedAt, input.Claims.Email); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	principal, err := repository.rotateFederationSession(ctx, tx, input.Flow.UserID, input.Flow.SessionID, locked, input.Rotation, input.SessionAbsoluteTTL, true)
	if err != nil {
		return Principal{}, err
	}
	if tx.Commit(ctx) != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	return principal, nil
}

func (repository *PostgresRepository) rotateFederationSession(ctx context.Context, tx pgx.Tx, userID, sessionID uuid.UUID, locked lockedFederationSession, rotation SessionRotation, absoluteTTL time.Duration, reauth bool) (Principal, error) {
	var admin bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.memberships WHERE user_id=$1 AND role='org_admin' AND status='active')`, userID).Scan(&admin) != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	absolute, expires, persistent := locked.AbsoluteExpiresAt, locked.ExpiresAt, locked.Persistent
	if admin {
		absolute = minPasswordTime(absolute, locked.CreatedAt.Add(min(absoluteTTL, 24*time.Hour)))
		expires = minPasswordTime(expires, absolute)
		persistent = false
	}
	if !expires.After(rotation.RotatedAt) || !absolute.After(rotation.RotatedAt) {
		return Principal{}, ErrSessionNotFound
	}
	command, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET token_hash=$3,csrf_token_hash=$4,context_version=context_version+1,reauthenticated_at=CASE WHEN $5 THEN $6 ELSE reauthenticated_at END,auth_methods=CASE WHEN $5 AND NOT ('fed'=ANY(auth_methods)) THEN array_append(auth_methods,'fed') ELSE auth_methods END,absolute_expires_at=$7,expires_at=$8,persistent=$9 WHERE id=$1 AND user_id=$2 AND context_version=$10 AND revoked_at IS NULL`, sessionID, userID, rotation.TokenHash, rotation.CSRFHash, reauth, rotation.RotatedAt, absolute, expires, persistent, locked.ContextVersion)
	if err != nil || command.RowsAffected() != 1 {
		return Principal{}, ErrAuthFoundationStorage
	}
	return repository.loadPrincipal(ctx, tx, sessionID, userID, locked.ActiveTenantID)
}

type federatedIdentityRecord struct {
	ID, UserID           uuid.UUID
	Status, ProviderSlug string
}

// Provider-subject locking serializes identity creation; email is never proof of ownership.
func readFederatedIdentity(ctx context.Context, tx pgx.Tx, claims ProviderClaims, locked bool) (federatedIdentityRecord, error) {
	query := `SELECT id,user_id,status,COALESCE(provider_slug,'oidc') FROM tutorhub.identities WHERE provider=$1 AND subject=$2 AND kind IN ('oidc','oauth2')`
	if locked {
		query += " FOR UPDATE"
	}
	var result federatedIdentityRecord
	err := tx.QueryRow(ctx, query, claims.Issuer, claims.Subject).Scan(&result.ID, &result.UserID, &result.Status, &result.ProviderSlug)
	return result, err
}

func (repository *PostgresRepository) CompleteFederatedLogin(ctx context.Context, input FederationLoginParams) (FederationLoginRecord, error) {
	if input.ProviderSlug == "facebook" {
		if input.Flow.Purpose != flowPurposeLogin {
			return FederationLoginRecord{}, ErrInvalidAuthFlow
		}
		input.Claims.EmailVerified = false
	} else if !input.Claims.EmailVerified {
		return FederationLoginRecord{}, ErrVerifiedEmailRequired
	}
	if input.ProviderSlug == "" || input.Claims.Issuer == "" || input.Claims.Subject == "" {
		return FederationLoginRecord{}, ErrInvalidAuthFlow
	}
	ctx, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return FederationLoginRecord{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err := lockProviderSubject(ctx, tx, input.Claims); err != nil {
		return FederationLoginRecord{}, ErrAuthFoundationStorage
	}
	now := input.Metadata.CreatedAt
	if input.Flow.Purpose == flowPurposeIdentityLink {
		locked, err := repository.lockFederationSession(ctx, tx, input.Flow.UserID, input.Flow.SessionID, 0, now)
		if err != nil {
			return FederationLoginRecord{}, err
		}
		if err := requireFederationAccountAssurance(ctx, tx, input.Flow.UserID, locked.AssuranceLevel, now); err != nil {
			return FederationLoginRecord{}, err
		}
		identityID, err := linkFederatedIdentity(ctx, tx, input.Flow.UserID, input.ProviderSlug, input.Claims, now)
		if err != nil {
			return FederationLoginRecord{}, err
		}
		principal, err := repository.finishFederatedLink(ctx, tx, input.Flow.UserID, input.Flow.SessionID, identityID, locked, SessionRotation{TokenHash: input.Metadata.TokenHash, CSRFHash: input.Metadata.CSRFHash, RotatedAt: now}, input.SessionAbsoluteTTL)
		if err != nil {
			return FederationLoginRecord{}, err
		}
		if tx.Commit(ctx) != nil {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
		return FederationLoginRecord{Principal: principal}, nil
	}
	if input.Flow.Purpose != "" && input.Flow.Purpose != flowPurposeLogin {
		return FederationLoginRecord{}, ErrInvalidAuthFlow
	}
	existing, lookupErr := readFederatedIdentity(ctx, tx, input.Claims, false)
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return FederationLoginRecord{}, ErrAuthFoundationStorage
	}
	if lookupErr == nil {
		if existing.ProviderSlug != input.ProviderSlug {
			return FederationLoginRecord{}, ErrFederatedIdentityMismatch
		}
		if err := lockActiveTenantUser(ctx, tx, existing.UserID); err != nil {
			return FederationLoginRecord{}, err
		}
		existing, err = readFederatedIdentity(ctx, tx, input.Claims, true)
		if err != nil {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
		if existing.Status == "erased" {
			return FederationLoginRecord{}, ErrIdentityInactive
		}
		if existing.Status == "unlinked" {
			if err := insertFederationLinkIntent(ctx, tx, existing.UserID, input); err != nil {
				return FederationLoginRecord{}, err
			}
			if tx.Commit(ctx) != nil {
				return FederationLoginRecord{}, ErrAuthFoundationStorage
			}
			return FederationLoginRecord{LinkPending: true}, nil
		}
		if existing.Status != "active" {
			return FederationLoginRecord{}, ErrIdentityInactive
		}
		if err := requireFederationAccountAssurance(ctx, tx, existing.UserID, 1, now); err != nil {
			return FederationLoginRecord{}, err
		}
		principal, err := repository.insertFederatedSession(ctx, tx, existing.UserID, existing.ID, input.Claims, input.Metadata, input.SessionAbsoluteTTL)
		if err != nil {
			return FederationLoginRecord{}, err
		}
		if tx.Commit(ctx) != nil {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
		return FederationLoginRecord{Principal: principal}, nil
	}
	if input.ProviderSlug == "facebook" {
		// Only a TutorHub OTP may move a previously unseen Facebook subject to
		// account creation or an existing owner's explicit link intent.
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions
			(binding_hash,kind,state,payload_ciphertext,provider_subject_digest,created_at,expires_at)
			VALUES ($1,'federated_registration','pending',$2,$3,$4,$5)`,
			input.RegistrationBindingHash, input.LinkPayloadCiphertext, input.ProviderSubjectDigest, now, input.LinkExpiresAt); err != nil {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
		if tx.Commit(ctx) != nil {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
		return FederationLoginRecord{RegistrationPending: true}, nil
	}
	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM tutorhub.users WHERE lower(email)=lower($1)`, input.Claims.Email).Scan(&userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return FederationLoginRecord{}, ErrAuthFoundationStorage
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if !input.SignupEnabled {
			return FederationLoginRecord{}, ErrFederationSignupDisabled
		}
		// A concurrent native signup wins without auto-merging either identity.
		err = tx.QueryRow(ctx, `INSERT INTO tutorhub.users(email,display_name,email_verified_at,created_at,updated_at) VALUES($1,$2,$3,$3,$3) ON CONFLICT DO NOTHING RETURNING id`, input.Claims.Email, input.Claims.DisplayName, now).Scan(&userID)
		if err == nil {
			identityID, err := insertFederatedIdentity(ctx, tx, userID, input.ProviderSlug, input.Claims, now)
			if err != nil {
				return FederationLoginRecord{}, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.user_legal_acceptances(user_id,document_kind,version,accepted_at) VALUES($1,'user_agreement',$2,$3),($1,'privacy_policy',$2,$3)`, userID, input.LegalTermsVersion, now); err != nil {
				return FederationLoginRecord{}, ErrAuthFoundationStorage
			}
			principal, err := repository.insertFederatedSession(ctx, tx, userID, identityID, input.Claims, input.Metadata, input.SessionAbsoluteTTL)
			if err != nil {
				return FederationLoginRecord{}, err
			}
			if tx.Commit(ctx) != nil {
				return FederationLoginRecord{}, ErrAuthFoundationStorage
			}
			return FederationLoginRecord{Principal: principal}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
		if tx.QueryRow(ctx, `SELECT id FROM tutorhub.users WHERE lower(email)=lower($1)`, input.Claims.Email).Scan(&userID) != nil {
			return FederationLoginRecord{}, ErrAuthFoundationStorage
		}
	}
	if err := lockActiveTenantUser(ctx, tx, userID); err != nil {
		return FederationLoginRecord{}, err
	}
	if err := insertFederationLinkIntent(ctx, tx, userID, input); err != nil {
		return FederationLoginRecord{}, err
	}
	if tx.Commit(ctx) != nil {
		return FederationLoginRecord{}, ErrAuthFoundationStorage
	}
	return FederationLoginRecord{LinkPending: true}, nil
}

func insertFederationLinkIntent(ctx context.Context, tx pgx.Tx, userID uuid.UUID, input FederationLoginParams) error {
	_, err := tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,user_id,payload_ciphertext,created_at,expires_at,provider_subject_digest) VALUES($1,'link_intent','pending',$2,$3,$4,$5,$6)`, input.LinkBindingHash, userID, input.LinkPayloadCiphertext, input.Metadata.CreatedAt, input.LinkExpiresAt, input.ProviderSubjectDigest)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func insertFederatedIdentity(ctx context.Context, tx pgx.Tx, userID uuid.UUID, slug string, claims ProviderClaims, now time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject,email_at_provider,email_verified,kind,provider_slug,provider_org_id,status,last_authenticated_at,created_at,updated_at) VALUES($1,$2,$3,NULLIF($4,''),$8,'oidc',$5,NULLIF($6,''),'active',$7,$7,$7) RETURNING id`, userID, claims.Issuer, claims.Subject, claims.Email, slug, claims.ProviderOrgID, now, slug != "facebook" && claims.EmailVerified).Scan(&id)
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	return id, nil
}

func linkFederatedIdentity(ctx context.Context, tx pgx.Tx, userID uuid.UUID, slug string, claims ProviderClaims, now time.Time) (uuid.UUID, error) {
	existing, err := readFederatedIdentity(ctx, tx, claims, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return insertFederatedIdentity(ctx, tx, userID, slug, claims, now)
	}
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	if existing.UserID != userID || existing.ProviderSlug != slug {
		return uuid.Nil, ErrIdentityConflict
	}
	existing, err = readFederatedIdentity(ctx, tx, claims, true)
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	if existing.Status == "erased" {
		return uuid.Nil, ErrIdentityInactive
	}
	// Retain immutable provider metadata and ownership on relink.
	_, err = tx.Exec(ctx, `UPDATE tutorhub.identities SET status='active',unlinked_at=NULL,email_at_provider=NULLIF($3,''),email_verified=$5,last_authenticated_at=$4,updated_at=$4 WHERE id=$1 AND user_id=$2`, existing.ID, userID, claims.Email, now, slug != "facebook" && claims.EmailVerified)
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	return existing.ID, nil
}

func (repository *PostgresRepository) insertFederatedSession(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, claims ProviderClaims, metadata SessionMetadata, absoluteTTL time.Duration) (Principal, error) {
	activeTenant, err := selectActiveTenant(ctx, tx, userID)
	if err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	var admin bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.memberships WHERE user_id=$1 AND role='org_admin' AND status='active')`, userID).Scan(&admin) != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	if admin {
		metadata.AbsoluteAt = minPasswordTime(metadata.AbsoluteAt, metadata.CreatedAt.Add(min(absoluteTTL, 24*time.Hour)))
		metadata.ExpiresAt = minPasswordTime(metadata.ExpiresAt, metadata.AbsoluteAt)
	}
	var ip any
	if metadata.IPPrefix != "" {
		ip = metadata.IPPrefix
	}
	var sessionID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO tutorhub.sessions(user_id,identity_id,active_tenant_id,token_hash,csrf_token_hash,user_agent_hash,ip_prefix,device_label,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time,auth_methods,assurance_level,persistent) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$9,$10,$11,$12,ARRAY['fed'],1,false) RETURNING id`, userID, identityID, nullUUID(activeTenant), metadata.TokenHash, metadata.CSRFHash, metadata.UserAgentHash, ip, metadata.DeviceLabel, metadata.CreatedAt, metadata.ExpiresAt, metadata.AbsoluteAt, claims.AuthTime).Scan(&sessionID)
	if err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	if _, err := tx.Exec(ctx, `UPDATE tutorhub.identities SET last_authenticated_at=$3,updated_at=$3,email_at_provider=NULLIF($4,''),email_verified=$5 WHERE id=$1 AND user_id=$2`, identityID, userID, metadata.CreatedAt, claims.Email, claims.EmailVerified); err != nil {
		return Principal{}, ErrAuthFoundationStorage
	}
	return repository.loadPrincipal(ctx, tx, sessionID, userID, activeTenant)
}
