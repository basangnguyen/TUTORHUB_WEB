package identity

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *NativeAuthService) GetMyPhone(ctx context.Context, p Principal) (*PhoneSummary, error) {
	if !s.config.PhoneEnabled {
		return nil, ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err = s.validateSecuritySession(ctx, tx, p); err != nil {
		return nil, err
	}
	return s.phoneSummary(ctx, tx, p.User.ID)
}

func (s *NativeAuthService) phoneSummary(ctx context.Context, tx pgx.Tx, user uuid.UUID) (*PhoneSummary, error) {
	var summary PhoneSummary
	err := tx.QueryRow(ctx, `SELECT n.id,n.masked_number,n.country_code,n.calling_code,n.last4,n.verified_at,n.last_used_at
		FROM tutorhub.user_phone_numbers n JOIN tutorhub.identities i ON i.id=n.identity_id AND i.user_id=n.user_id
		WHERE n.user_id=$1 AND n.status='active' AND i.status='active' AND i.kind='phone'`, user).
		Scan(&summary.ID, &summary.MaskedNumber, &summary.CountryCode, &summary.CallingCode, &summary.Last4, &summary.VerifiedAt, &summary.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	// last_used_at >= verified_at is enforced by the database.
	summary.Stale = phonePastThreshold(s.now(), summary.LastUsedAt, s.config.PhoneStaleAfter)
	return &summary, nil
}

func markPhoneUsed(ctx context.Context, tx pgx.Tx, user, identity uuid.UUID, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE tutorhub.user_phone_numbers SET last_used_at=GREATEST(last_used_at,$3),updated_at=GREATEST(updated_at,$3)
		WHERE user_id=$1 AND identity_id=$2 AND status='active'`, user, identity, now)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrAuthFoundationStorage
	}
	return nil
}

type activePhoneBinding struct {
	user, identity uuid.UUID
	last           time.Time
}

func activeBinding(ctx context.Context, tx pgx.Tx, digest []byte) (activePhoneBinding, error) {
	var binding activePhoneBinding
	err := tx.QueryRow(ctx, `SELECT user_id,identity_id,GREATEST(verified_at,last_used_at) FROM tutorhub.user_phone_numbers
		WHERE e164_lookup_hash=$1 AND status='active'`, digest).Scan(&binding.user, &binding.identity, &binding.last)
	if errors.Is(err, pgx.ErrNoRows) {
		return activePhoneBinding{}, nil
	}
	if err != nil {
		return activePhoneBinding{}, ErrAuthFoundationStorage
	}
	return binding, nil
}

// Claims of the same number serialize even before a row for it exists.
// Two accounts exchanging different numbers lock the same account pair and
// their sessions in sorted order, before either request mutates a binding.
func lockPhoneClaim(ctx context.Context, tx pgx.Tx, user uuid.UUID, digest []byte) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, int64(binary.BigEndian.Uint64(digest[:8]))); err != nil {
		return ErrAuthFoundationStorage
	}
	b, err := activeBinding(ctx, tx, digest)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM tutorhub.users WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, user, nullUUID(b.user)); err != nil {
		return ErrAuthFoundationStorage
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM tutorhub.sessions WHERE user_id IN ($1,$2) ORDER BY id FOR UPDATE`, user, nullUUID(b.user)); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (s *NativeAuthService) VerifyMyPhone(ctx context.Context, p Principal, binding string, input EmailCodeVerifyInput) (*PhoneSummary, error) {
	if !s.config.PhoneEnabled {
		return nil, ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	_, snapshot, err := s.readPhone(ctx, tx, binding, false)
	if errors.Is(err, ErrAuthTransactionUnavailable) {
		return nil, ErrNativeInvalidCode
	}
	if err != nil {
		return nil, err
	}
	if err = matchPhoneSession(snapshot, &p); err != nil {
		return nil, err
	}
	digest := s.crypto.phoneLookupHash(snapshot.Number.E164)
	if err = lockPhoneClaim(ctx, tx, p.User.ID, digest); err != nil {
		return nil, err
	}
	if _, err = s.lockRecentPasskeySession(ctx, tx, p, true); err != nil {
		return nil, err
	}
	t, proof, err := s.readPhone(ctx, tx, binding, true)
	if errors.Is(err, ErrAuthTransactionUnavailable) {
		return nil, ErrNativeInvalidCode
	}
	if err != nil {
		return nil, err
	}
	if proof != snapshot {
		return nil, ErrNativeInvalidCode
	}
	if err = s.provePhone(ctx, tx, t, proof, input); err != nil {
		return nil, err
	}

	owner, err := activeBinding(ctx, tx, digest)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var oldEmail string
	switch owner.user {
	case p.User.ID:
		if err = markPhoneUsed(ctx, tx, p.User.ID, owner.identity, now); err != nil {
			return nil, err
		}
	default:
		if owner.user != uuid.Nil {
			if !phonePastThreshold(now, owner.last, s.config.PhoneReclaimAfter) {
				return nil, commitPhoneDecision(ctx, tx, ErrPhoneInUse)
			}
			if err = tx.QueryRow(ctx, `SELECT email FROM tutorhub.users WHERE id=$1`, owner.user).Scan(&oldEmail); err != nil {
				return nil, ErrAuthFoundationStorage
			}
			if err = retirePhone(ctx, tx, owner.user, owner.identity, true, now); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=GREATEST(created_at,$3),revoked_reason='phone_released'
				WHERE user_id=$1 AND identity_id=$2 AND revoked_at IS NULL`, owner.user, owner.identity, now); err != nil {
				return nil, ErrAuthFoundationStorage
			}
		}
		if err = s.attachPhone(ctx, tx, p, proof.Number, digest, now); err != nil {
			return nil, err
		}
	}
	summary, err := s.phoneSummary(ctx, tx, p.User.ID)
	if err != nil {
		return nil, err
	}
	if tx.Commit(ctx) != nil {
		return nil, ErrAuthFoundationStorage
	}
	if oldEmail != "" {
		s.dispatchAccountNotice("phone_released", oldEmail)
	}
	return summary, nil
}

func (s *NativeAuthService) attachPhone(ctx context.Context, tx pgx.Tx, p Principal, number NormalizedPhoneNumber, digest []byte, now time.Time) error {
	var previous uuid.UUID
	err := tx.QueryRow(ctx, `SELECT identity_id FROM tutorhub.user_phone_numbers WHERE user_id=$1 AND status='active'`, p.User.ID).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthFoundationStorage
	}
	if previous != uuid.Nil {
		if err = retirePhone(ctx, tx, p.User.ID, previous, false, now); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=GREATEST(created_at,$4),revoked_reason='phone_removed'
			WHERE user_id=$1 AND identity_id=$2 AND id<>$3 AND revoked_at IS NULL`, p.User.ID, previous, p.SessionID, now); err != nil {
			return ErrAuthFoundationStorage
		}
	}
	phone, ident := uuid.New(), uuid.New()
	sealed, err := s.crypto.encryptPhoneNumber(p.User.ID, phone, number.E164)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.identities(id,user_id,provider,provider_slug,subject,status,kind,created_at,updated_at)
		VALUES($1,$2,'urn:tutorhub:phone','phone',$3,'active','phone',$4,$4)`, ident, p.User.ID, phone.String(), now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.user_phone_numbers
		(id,user_id,identity_id,e164_lookup_hash,e164_ciphertext,country_code,calling_code,masked_number,last4,status,verified_at,last_used_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'active',$10,$10,$10,$10)`, phone, p.User.ID, ident, digest, sealed,
		number.CountryCode, number.CallingCode, number.MaskedNumber, number.Last4, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if err = insertIdentityEvent(ctx, tx, uuid.Nil, p.User.ID, "phone_added", ident, now); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func retirePhone(ctx context.Context, tx pgx.Tx, user, identity uuid.UUID, released bool, now time.Time) error {
	event := "phone_removed"
	query := `UPDATE tutorhub.user_phone_numbers SET status='removed',e164_ciphertext=NULL,removed_at=GREATEST(updated_at,$3),updated_at=GREATEST(updated_at,$3)
		WHERE user_id=$1 AND identity_id=$2 AND status='active'`
	if released {
		event = "phone_released"
		query = `UPDATE tutorhub.user_phone_numbers SET status='released',release_reason='reassigned',e164_ciphertext=NULL,
			released_at=GREATEST(updated_at,$3),updated_at=GREATEST(updated_at,$3) WHERE user_id=$1 AND identity_id=$2 AND status='active'`
	}
	tag, err := tx.Exec(ctx, query, user, identity, now)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrAuthFoundationStorage
	}
	tag, err = tx.Exec(ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=GREATEST(updated_at,$3),updated_at=GREATEST(updated_at,$3)
		WHERE id=$1 AND user_id=$2 AND status='active' AND kind='phone'`, identity, user, now)
	if err != nil || tag.RowsAffected() != 1 {
		return ErrAuthFoundationStorage
	}
	if err = insertIdentityEvent(ctx, tx, uuid.Nil, user, event, identity, now); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (s *NativeAuthService) RemoveMyPhone(ctx context.Context, p Principal) (ReauthenticateResult, error) {
	var result ReauthenticateResult
	if !s.config.PhoneEnabled {
		return result, ErrNativeMethodDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := s.lockRecentPasskeySession(ctx, tx, p, true)
	if err != nil {
		return result, err
	}
	var ident uuid.UUID
	err = tx.QueryRow(ctx, `SELECT identity_id FROM tutorhub.user_phone_numbers WHERE user_id=$1 AND status='active'`, p.User.ID).Scan(&ident)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, ErrAuthFoundationStorage
	}
	now := s.now().UTC()
	if ident != uuid.Nil {
		if err = requireAnotherLoginMethod(ctx, tx, p.User.ID, ident, uuid.Nil, s.availableLoginMethods(), now); err != nil {
			return result, err
		}
		if err = retirePhone(ctx, tx, p.User.ID, ident, false, now); err != nil {
			return result, err
		}
		if _, err = tx.Exec(ctx, `UPDATE tutorhub.sessions SET revoked_at=GREATEST(created_at,$3),revoked_reason='phone_removed'
			WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, p.User.ID, p.SessionID, now); err != nil {
			return result, ErrAuthFoundationStorage
		}
	}
	lifetime, err := s.reauthSessionLifetime(ctx, tx, p.User.ID, locked)
	if err != nil {
		return result, err
	}
	tokens, err := s.newSessionTokens()
	if err != nil {
		return result, err
	}
	tag, err := tx.Exec(ctx, `UPDATE tutorhub.sessions SET token_hash=$3,csrf_token_hash=$4,
		context_version=context_version+1,persistent=$5,absolute_expires_at=$6,expires_at=$7
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND context_version=$8`, p.SessionID, p.User.ID,
		s.crypto.Digest(sessionPurpose, tokens.SessionToken), s.crypto.Digest(csrfPurpose, tokens.CSRFToken), lifetime.Persistent, lifetime.AbsoluteExpiresAt, lifetime.ExpiresAt, p.ContextVersion)
	if err != nil || tag.RowsAffected() != 1 || tx.Commit(ctx) != nil {
		return result, ErrAuthFoundationStorage
	}
	return reauthResult(locked, tokens, lifetime, locked.AuthenticatedAt), nil
}
