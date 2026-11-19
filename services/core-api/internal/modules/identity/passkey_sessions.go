package identity

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (service *NativeAuthService) issuePasskeySession(ctx context.Context, tx pgx.Tx, userID uuid.UUID, verified, keep bool, userAgent, remote string) (NativeSessionResult, error) {
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
	aal := 1
	if verified {
		aal = 2
	}
	if err := service.insertAssuredSession(ctx, tx, userID, uuid.Nil, &result, []string{"passkey"}, aal, userAgent, remote, now); err != nil {
		return NativeSessionResult{}, err
	}
	return result, nil
}

// assurance_level is immutable to the runtime role. A verified UV step-up
// replaces the session in this transaction, preserving its original lifetime.
func (service *NativeAuthService) rotatePasskeySession(ctx context.Context, tx pgx.Tx, p Principal, locked lockedSecuritySession) (ReauthenticateResult, error) {
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
		CASE WHEN 'passkey'=ANY(auth_methods) THEN auth_methods ELSE array_append(auth_methods,'passkey') END,
		2,$9,device_label,context_version+1 FROM tutorhub.sessions
		WHERE id=$1 AND user_id=$2 AND context_version=$3 AND revoked_at IS NULL AND expires_at>$6 AND absolute_expires_at>$6`,
		p.SessionID, p.User.ID, p.ContextVersion, service.crypto.Digest(sessionPurpose, tokens.SessionToken),
		service.crypto.Digest(csrfPurpose, tokens.CSRFToken), now, lifetime.ExpiresAt, lifetime.AbsoluteExpiresAt, lifetime.Persistent)
	if err != nil || cmd.RowsAffected() != 1 {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	cmd, err = tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason='passkey_reauthenticated' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, p.SessionID, p.User.ID, now)
	if err != nil || cmd.RowsAffected() != 1 {
		return ReauthenticateResult{}, ErrAuthFoundationStorage
	}
	return reauthResult(locked, tokens, lifetime, now), nil
}
