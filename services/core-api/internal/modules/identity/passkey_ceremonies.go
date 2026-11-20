package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PasskeyPurpose string

const (
	PasskeyLogin        PasskeyPurpose = "assertion"
	PasskeyRegistration PasskeyPurpose = "registration"
	PasskeyReauth       PasskeyPurpose = "step_up"
	passkeyTTL                         = 5 * time.Minute
)

type PasskeyOptions struct {
	Options json.RawMessage
	Cookie  *http.Cookie
}

type PasskeyAPI interface {
	BeginPasskey(context.Context, PasskeyPurpose, *Principal) (PasskeyOptions, error)
	FinishPasskeyRegistration(context.Context, Principal, string, json.RawMessage, string) (Passkey, error)
	FinishPasskeyLogin(context.Context, string, json.RawMessage, bool, string, string) (NativeSessionResult, error)
	FinishPasskeyReauth(context.Context, Principal, string, json.RawMessage) (ReauthenticateResult, error)
	ListMyPasskeys(context.Context, Principal) ([]Passkey, error)
	RenameMyPasskey(context.Context, Principal, uuid.UUID, string) (Passkey, error)
	RevokeMyPasskey(context.Context, Principal, uuid.UUID) error
}

type passkeyPayload struct {
	Purpose        PasskeyPurpose
	Session        webauthn.SessionData
	ContextVersion int64
}

func (purpose PasskeyPurpose) transactionKind() string {
	if purpose == PasskeyReauth {
		return "reauth"
	}
	return "passkey"
}

// Authenticated ceremonies take the same user/session locks as password changes.
// Check recency again on completion since the options can outlive recent auth.
func (service *NativeAuthService) lockPasskeyPrincipal(ctx context.Context, tx pgx.Tx, purpose PasskeyPurpose, p *Principal) (lockedSecuritySession, error) {
	if purpose == PasskeyLogin && p == nil {
		return lockedSecuritySession{}, nil
	}
	if p == nil || (purpose != PasskeyRegistration && purpose != PasskeyReauth) {
		return lockedSecuritySession{}, ErrAuthFoundationInput
	}
	if purpose == PasskeyRegistration {
		return service.lockRecentPasskeySession(ctx, tx, *p, true)
	}
	return service.lockNativeSecuritySession(ctx, tx, *p)
}

func loadPasskeyUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID, create bool) (PasskeyUser, error) {
	handle, err := passkeyUserHandle(ctx, tx, userID, create)
	if err != nil {
		return PasskeyUser{}, err
	}
	credentials, err := activePasskeyCredentials(ctx, tx, userID)
	if err != nil {
		return PasskeyUser{}, err
	}
	return PasskeyUser{Handle: handle, Credentials: credentials}, nil
}

func (service *NativeAuthService) BeginPasskey(ctx context.Context, purpose PasskeyPurpose, p *Principal) (PasskeyOptions, error) {
	if service.webauthn == nil {
		return PasskeyOptions{}, ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return PasskeyOptions{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if _, err = service.lockPasskeyPrincipal(ctx, tx, purpose, p); err != nil {
		return PasskeyOptions{}, err
	}
	var user *PasskeyUser
	var userID, sessionID *uuid.UUID
	if p != nil {
		account, err := loadPasskeyUser(ctx, tx, p.User.ID, purpose == PasskeyRegistration)
		if err != nil {
			return PasskeyOptions{}, err
		}
		if purpose == PasskeyReauth {
			account.Credentials = slices.DeleteFunc(account.Credentials, func(c webauthn.Credential) bool {
				return c.Authenticator.CloneWarning
			})
			if len(account.Credentials) == 0 {
				return PasskeyOptions{}, ErrPasskeyNotFound
			}
		}
		account.Name = p.User.DisplayName
		user = &account
		userID, sessionID = &p.User.ID, &p.SessionID
	}
	var options json.RawMessage
	var session *webauthn.SessionData
	if purpose == PasskeyRegistration {
		options, session, err = service.webauthn.RegistrationOptions(*user)
	} else {
		options, session, err = service.webauthn.AssertionOptions(user)
	}
	if err != nil {
		return PasskeyOptions{}, ErrAuthFoundationStorage
	}
	envelope := passkeyPayload{Purpose: purpose, Session: *session}
	if p != nil {
		envelope.ContextVersion = p.ContextVersion
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return PasskeyOptions{}, ErrAuthFoundationStorage
	}
	sealed, err := service.crypto.Encrypt(string(payload))
	if err != nil {
		return PasskeyOptions{}, ErrAuthFoundationStorage
	}
	binding, err := service.crypto.RandomToken()
	if err != nil {
		return PasskeyOptions{}, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	expires := now.Add(passkeyTTL)
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(binding_hash,kind,user_id,session_id,payload_ciphertext,created_at,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, service.crypto.Digest("auth-tx-binding", binding), purpose.transactionKind(), userID, sessionID, sealed, now, expires)
	if err != nil || tx.Commit(ctx) != nil {
		return PasskeyOptions{}, ErrAuthFoundationStorage
	}
	cookie := service.transactionCookie(binding, expires)
	cookie.MaxAge = int(passkeyTTL.Seconds())
	return PasskeyOptions{options, cookie}, nil
}

func (service *NativeAuthService) consumePasskey(ctx context.Context, tx pgx.Tx, purpose PasskeyPurpose, p *Principal, binding string) (webauthn.SessionData, error) {
	var empty webauthn.SessionData
	if len(binding) != 43 {
		return empty, ErrInvalidPasskey
	}
	var id uuid.UUID
	var userID, sessionID *uuid.UUID
	if p != nil {
		userID, sessionID = &p.User.ID, &p.SessionID
	}
	var sealed []byte
	var expires time.Time
	err := tx.QueryRow(ctx, `SELECT id,payload_ciphertext,expires_at FROM tutorhub.auth_transactions
		WHERE binding_hash=$1 AND kind=$2 AND state='pending'
		AND user_id IS NOT DISTINCT FROM $3::uuid AND session_id IS NOT DISTINCT FROM $4::uuid
		AND expires_at>$5 FOR UPDATE`, service.crypto.Digest("auth-tx-binding", binding), purpose.transactionKind(),
		userID, sessionID, service.now().UTC()).Scan(&id, &sealed, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrInvalidPasskey
	}
	if err != nil {
		return empty, ErrAuthFoundationStorage
	}
	plaintext, err := service.crypto.Decrypt(sealed)
	if err != nil {
		return empty, ErrInvalidPasskey
	}
	var payload passkeyPayload
	if json.Unmarshal([]byte(plaintext), &payload) != nil || payload.Purpose != purpose ||
		(p != nil && payload.ContextVersion != p.ContextVersion) {
		return empty, ErrInvalidPasskey
	}
	now := service.now().UTC()
	if !expires.After(now) {
		return empty, ErrInvalidPasskey
	}
	if _, err = tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=$2,payload_ciphertext=NULL WHERE id=$1`, id, now); err != nil {
		return empty, ErrAuthFoundationStorage
	}
	return payload.Session, nil
}

type passkeyVerification func(context.Context, pgx.Tx, webauthn.SessionData, lockedSecuritySession) error

// Commit expected verification rejections so a failed proof burns the challenge
// and a clone warning survives. Infrastructure failures roll back the entire change.
func (service *NativeAuthService) finishPasskey(ctx context.Context, purpose PasskeyPurpose, p *Principal, binding string, verify passkeyVerification) error {
	if service.webauthn == nil {
		return ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, service.queryTimeout())
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := service.lockPasskeyPrincipal(ctx, tx, purpose, p)
	if err != nil {
		return err
	}
	session, err := service.consumePasskey(ctx, tx, purpose, p, binding)
	if err != nil {
		return err
	}
	err = verify(ctx, tx, session, locked)
	if err != nil && !errors.Is(err, ErrInvalidPasskey) && !errors.Is(err, ErrPasskeyUnknown) && !errors.Is(err, ErrPasskeyClone) && !errors.Is(err, ErrPasskeyConflict) && !errors.Is(err, ErrAuthFoundationInput) {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return err
}

func (service *NativeAuthService) FinishPasskeyRegistration(ctx context.Context, p Principal, binding string, attestation json.RawMessage, label string) (Passkey, error) {
	var registered Passkey
	err := service.finishPasskey(ctx, PasskeyRegistration, &p, binding, func(ctx context.Context, tx pgx.Tx, session webauthn.SessionData, _ lockedSecuritySession) error {
		user, err := loadPasskeyUser(ctx, tx, p.User.ID, false)
		if err != nil {
			return err
		}
		credential, err := service.webauthn.VerifyRegistration(user, session, attestation)
		if err != nil {
			return ErrInvalidPasskey
		}
		registered, err = saveRegisteredPasskey(ctx, tx, p.User.ID, credential, label, service.now().UTC())
		return err
	})
	if err != nil {
		return Passkey{}, err
	}
	return registered, nil
}

type verifiedPasskey struct {
	UserID       uuid.UUID
	UV           bool
	CredentialID []byte
}

// Inspect clientData and authenticatorData before consulting credential existence.
// A revoked credential still has an owner, so it can never produce a Signal hint.
func (service *NativeAuthService) verifyPasskeyAssertion(ctx context.Context, tx pgx.Tx, session webauthn.SessionData, raw json.RawMessage, p *Principal) (verifiedPasskey, error) {
	assertion, err := service.webauthn.ParseAssertion(session, raw)
	if err != nil {
		return verifiedPasskey{}, ErrInvalidPasskey
	}
	var userID uuid.UUID
	if p == nil {
		if len(assertion.Response.UserHandle) != 64 {
			return verifiedPasskey{}, ErrInvalidPasskey
		}
		userID, err = lookupPasskeyOwner(ctx, tx, assertion.RawID)
		if err != nil {
			return verifiedPasskey{}, err
		}
		if err = lockPasskeyUser(ctx, tx, userID); err != nil {
			return verifiedPasskey{}, err
		}
	} else {
		userID = p.User.ID
	}
	user, err := loadPasskeyUser(ctx, tx, userID, false)
	if err != nil {
		return verifiedPasskey{}, err
	}
	if len(assertion.Response.UserHandle) > 0 && !bytes.Equal(user.Handle, assertion.Response.UserHandle) {
		return verifiedPasskey{}, ErrInvalidPasskey
	}
	credential, err := service.webauthn.VerifyAssertion(user, session, assertion)
	if err != nil {
		return verifiedPasskey{}, ErrInvalidPasskey
	}
	uv := assertion.Response.AuthenticatorData.Flags.HasUserVerified()
	if p != nil && !uv {
		return verifiedPasskey{}, ErrInvalidPasskey
	}
	err = recordPasskeyAssertion(ctx, tx, userID, credential, service.now().UTC())
	return verifiedPasskey{userID, uv, credential.ID}, err
}

func (service *NativeAuthService) FinishPasskeyLogin(ctx context.Context, binding string, assertion json.RawMessage, keep bool, userAgent, remote string) (NativeSessionResult, error) {
	var result NativeSessionResult
	err := service.finishPasskey(ctx, PasskeyLogin, nil, binding, func(ctx context.Context, tx pgx.Tx, session webauthn.SessionData, _ lockedSecuritySession) error {
		proof, err := service.verifyPasskeyAssertion(ctx, tx, session, assertion, nil)
		if err != nil {
			return err
		}
		if proof.UV {
			result, err = service.issueLoginSession(ctx, tx, proof.UserID, uuid.Nil, []string{"passkey"}, 2, keep, userAgent, remote)
		} else {
			result, err = service.finishLoginFirstFactor(ctx, tx, mfaPayload{UserID: proof.UserID, Methods: []string{"passkey"}, PasskeyCredential: proof.CredentialID, KeepSignedIn: keep}, userAgent, remote)
		}
		return err
	})
	if err != nil {
		return NativeSessionResult{}, err
	}
	return result, nil
}

func (service *NativeAuthService) FinishPasskeyReauth(ctx context.Context, p Principal, binding string, assertion json.RawMessage) (ReauthenticateResult, error) {
	var result ReauthenticateResult
	err := service.finishPasskey(ctx, PasskeyReauth, &p, binding, func(ctx context.Context, tx pgx.Tx, session webauthn.SessionData, locked lockedSecuritySession) error {
		if _, err := service.verifyPasskeyAssertion(ctx, tx, session, assertion, &p); err != nil {
			return err
		}
		var err error
		result, err = service.rotateAssuredSession(ctx, tx, p, locked, "passkey")
		return err
	})
	if err != nil {
		return ReauthenticateResult{}, err
	}
	return result, nil
}

var _ PasskeyAPI = (*NativeAuthService)(nil)
