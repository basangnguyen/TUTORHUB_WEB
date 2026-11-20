package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type phoneLoginState struct {
	lastUse                                     time.Time
	otherIdentity, totp, passkey, usablePasskey bool
}

func (state phoneLoginState) staleForLogin(now time.Time, threshold time.Duration) bool {
	return (state.otherIdentity || state.passkey) && phonePastThreshold(now, state.lastUse, threshold)
}

func (state phoneLoginState) usableAfterRemoval(now time.Time, methods loginMethodAvailability) bool {
	if state.staleForLogin(now, methods.staleAfter) {
		return false
	}
	if !state.totp && !state.passkey {
		return true
	}
	// Completing the MFA continuation independently checks the freshness of
	// the bound phone, even on an account with TOTP as its only other factor.
	return methods.mfa && (state.totp || (methods.passkeys && state.usablePasskey)) &&
		!phonePastThreshold(now, state.lastUse, methods.staleAfter)
}

// Called with the account locked. A login supplies its original identity and
// number digest. An unlink supplies the methods being removed so eligibility is
// evaluated for the account as it will be after that same transaction commits.
func readPhoneLoginState(ctx context.Context, tx pgx.Tx, user, identity uuid.UUID, digest []byte, removingIdentity, removingPasskey uuid.UUID) (phoneLoginState, error) {
	var state phoneLoginState
	err := tx.QueryRow(ctx, `SELECT GREATEST(n.verified_at,n.last_used_at),
		(u.email_verified_at IS NOT NULL
			OR EXISTS(SELECT FROM tutorhub.user_password_credentials WHERE user_id=u.id AND status='active')
			OR EXISTS(SELECT FROM tutorhub.identities other WHERE other.user_id=u.id AND other.status='active'
				AND other.id IS DISTINCT FROM $4::uuid AND (other.kind IN ('oidc','oauth2')
					OR (other.email_verified AND lower(other.email_at_provider)=u.email)))),
		EXISTS(SELECT FROM tutorhub.mfa_totp_factors WHERE user_id=u.id AND status='active'), keys.enrolled, keys.usable
		FROM tutorhub.user_phone_numbers n JOIN tutorhub.users u ON u.id=n.user_id AND u.status='active'
		JOIN tutorhub.identities i ON i.id=n.identity_id AND i.user_id=u.id AND i.kind='phone' AND i.status='active'
		CROSS JOIN LATERAL (SELECT count(*)>0 AS enrolled,count(*) FILTER (WHERE NOT clone_warning)>0 AS usable
			FROM tutorhub.webauthn_credentials WHERE user_id=u.id AND status='active' AND id IS DISTINCT FROM $5::uuid) keys
		WHERE n.user_id=$1 AND n.status='active' AND ($2::uuid IS NULL OR n.identity_id=$2)
			AND ($3::bytea IS NULL OR n.e164_lookup_hash=$3) AND n.identity_id IS DISTINCT FROM $4::uuid
		FOR UPDATE OF n,i`, user, nullUUID(identity), digest, nullUUID(removingIdentity), nullUUID(removingPasskey)).
		Scan(&state.lastUse, &state.otherIdentity, &state.totp, &state.passkey, &state.usablePasskey)
	return state, err
}
