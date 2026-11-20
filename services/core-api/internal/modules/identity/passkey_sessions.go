package identity

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The caller holds the verified user's lock through policy evaluation and commit.
func (service *NativeAuthService) finishLoginFirstFactor(ctx context.Context, tx pgx.Tx, proof mfaPayload, userAgent, remote string) (NativeSessionResult, error) {
	if service.config.MFATOTPEnabled || (len(proof.Methods) == 1 && proof.Methods[0] == "phone_otp") {
		challenge, err := service.beginMFA(ctx, tx, proof)
		if err != nil {
			return NativeSessionResult{}, err
		}
		if challenge != nil {
			return NativeSessionResult{MFA: challenge}, nil
		}
	}
	return service.issueLoginSession(ctx, tx, proof.UserID, proof.IdentityID, proof.Methods, 1, proof.KeepSignedIn, userAgent, remote)
}

func (service *NativeAuthService) issueLoginSession(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, methods []string, assurance int, keep bool, userAgent, remote string) (NativeSessionResult, error) {
	var admin bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutorhub.memberships WHERE user_id=$1 AND role='org_admin' AND status='active')`, userID).Scan(&admin); err != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	result, err := service.newSessionTokens()
	if err != nil {
		return NativeSessionResult{}, err
	}
	now := service.now().UTC()
	result = service.sessionExpiry(result, now, keep, admin)
	if err := service.insertAssuredSession(ctx, tx, userID, identityID, &result, methods, assurance, userAgent, remote, now); err != nil {
		return NativeSessionResult{}, err
	}
	if len(methods) > 0 && methods[0] == "phone_otp" {
		if err := markPhoneUsed(ctx, tx, userID, identityID, now); err != nil {
			return NativeSessionResult{}, err
		}
	}
	return result, nil
}

// assurance_level is immutable to the runtime role. An AAL2 step-up
// replaces the session in this transaction, preserving its original lifetime.
func (service *NativeAuthService) rotateAssuredSession(ctx context.Context, tx pgx.Tx, p Principal, locked lockedSecuritySession, method string) (ReauthenticateResult, error) {
	if method != "passkey" && method != "totp" {
		return ReauthenticateResult{}, ErrAuthFoundationInput
	}
	lifetime, err := service.reauthSessionLifetime(ctx, tx, p.User.ID, locked)
	if err != nil {
		return ReauthenticateResult{}, err
	}
	tokens, err := service.newSessionTokens()
	if err != nil {
		return ReauthenticateResult{}, err
	}
	now := service.now().UTC()
	cmd, err := tx.Exec(ctx, `INSERT INTO tutorhub.sessions(user_id,identity_id,active_tenant_id,token_hash,csrf_token_hash,user_agent_hash,ip_prefix,
		created_at,last_seen_at,expires_at,absolute_expires_at,auth_time,reauthenticated_at,auth_methods,assurance_level,persistent,device_label,context_version)
		SELECT user_id,identity_id,active_tenant_id,$4,$5,user_agent_hash,ip_prefix,created_at,$6,$7,$8,auth_time,$6,
		CASE WHEN $10::text=ANY(auth_methods) THEN auth_methods ELSE array_append(auth_methods,$10::text) END,
		2,$9,device_label,context_version+1 FROM tutorhub.sessions
		WHERE id=$1 AND user_id=$2 AND context_version=$3 AND revoked_at IS NULL AND expires_at>$6 AND absolute_expires_at>$6`,
		p.SessionID, p.User.ID, p.ContextVersion, service.crypto.Digest(sessionPurpose, tokens.SessionToken),
		service.crypto.Digest(csrfPurpose, tokens.CSRFToken), now, lifetime.ExpiresAt, lifetime.AbsoluteExpiresAt, lifetime.Persistent, method)
	if err != nil || cmd.RowsAffected() != 1 {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	if service.config.SSO.Enabled {
		newID, err := service.schoolSessionForToken(ctx, tx, p.User.ID, tokens.SessionToken)
		if err != nil {
			return ReauthenticateResult{}, err
		}
		// Step-up replaces the session ID. Carry forward only its unexpired
		// school proof with the original expiry; step-up cannot refresh it.
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,user_id,session_id,payload_ciphertext,created_at,expires_at,completed_at)
			SELECT $1,kind,state,user_id,$2,payload_ciphertext,created_at,expires_at,completed_at FROM tutorhub.auth_transactions
			WHERE binding_hash=$3 AND kind='login' AND state='completed' AND user_id=$4 AND session_id=$5 AND expires_at>$6`,
			schoolReceiptBinding(service.crypto, p.User.ID, newID), newID, schoolReceiptBinding(service.crypto, p.User.ID, p.SessionID), p.User.ID, p.SessionID, now); err != nil {
			return ReauthenticateResult{}, ErrAuthFoundationStorage
		}
	}
	cmd, err = tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason=$4 WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, p.SessionID, p.User.ID, now, method+"_reauthenticated")
	if err != nil || cmd.RowsAffected() != 1 {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	return reauthResult(locked, tokens, lifetime, now), nil
}
