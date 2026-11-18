package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) deleteFacebookData(ctx context.Context, event facebookCallbackEvent) (string, error) {
	ctx, cancel := r.contextWithTimeout(ctx)
	defer cancel()
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err = lockProviderSubject(ctx, tx, ProviderClaims{Issuer: event.issuer, Subject: event.subject}); err != nil {
		return "", ErrAuthFoundationStorage
	}

	// Serialize both new and repeated events with login and relink for this subject.
	// A retry of an already applied deletion cannot erase a subsequent authorization.
	var code string
	var retained bool
	err = tx.QueryRow(ctx, `SELECT confirmation_code,created_at > $2::timestamptz - interval '1 year'
 FROM tutorhub.external_data_deletion_requests WHERE provider='facebook' AND idempotency_key=$1 AND status='completed'`,
		event.eventDigest, event.now).Scan(&code, &retained)
	if err == nil {
		if !retained {
			return "", ErrFacebookSignedRequest
		}
		return code, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAuthFoundationStorage
	}
	if !event.fresh {
		return "", ErrFacebookSignedRequest
	}
	code, err = newFacebookConfirmationCode()
	if err != nil {
		return "", err
	}
	var receiptID uuid.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO tutorhub.external_data_deletion_requests(provider,idempotency_key,confirmation_code,status,created_at)
 VALUES('facebook',$1,$2,'received',$3) RETURNING id`, event.eventDigest, code, event.now).Scan(&receiptID); err != nil {
		return "", ErrAuthFoundationStorage
	}
	if _, err = tx.Exec(ctx, `SELECT tutorhub.erase_provider_identity('facebook',$1,$2,$3,$4)`, event.issuer, event.subject, event.subjectDigest, event.now); err != nil {
		return "", ErrAuthFoundationStorage
	}
	if _, err = tx.Exec(ctx, `UPDATE tutorhub.external_data_deletion_requests SET status='completed',completed_at=$2 WHERE id=$1`, receiptID, event.now); err != nil {
		return "", ErrAuthFoundationStorage
	}
	if _, err = tx.Exec(ctx, `INSERT INTO tutorhub.outbox_events(aggregate_type,aggregate_id,event_type,payload,occurred_at,available_at)
 VALUES('provider_data_request',$1,'identity.provider_data_deleted','{"provider":"facebook","status":"completed"}'::jsonb,$2,$2)`, receiptID, event.now); err != nil {
		return "", ErrAuthFoundationStorage
	}
	if tx.Commit(ctx) != nil {
		return "", ErrAuthFoundationStorage
	}
	return code, nil
}

func (r *PostgresRepository) deauthorizeFacebook(ctx context.Context, event facebookCallbackEvent) error {
	ctx, cancel := r.contextWithTimeout(ctx)
	defer cancel()
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	if err = lockProviderSubject(ctx, tx, ProviderClaims{Issuer: event.issuer, Subject: event.subject}); err != nil {
		return ErrAuthFoundationStorage
	}
	// A duplicate callback, including one arriving after the user explicitly relinks,
	// must not revoke the new authorization. The outbox marker holds no account identifier.
	var marker uuid.UUID
	copy(marker[:], event.eventDigest)
	result, err := tx.Exec(ctx, `INSERT INTO tutorhub.outbox_events(aggregate_type,aggregate_id,event_type,payload,occurred_at,available_at)
 VALUES('provider_deauthorization',$1,'identity.provider_deauthorized','{"provider":"facebook"}'::jsonb,$2,$2)
 ON CONFLICT DO NOTHING`, marker, event.now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	if !event.fresh {
		return ErrFacebookSignedRequest
	}
	// Match account settings and password recovery lock order: user before sessions and identities.
	if _, err = tx.Exec(ctx, `SELECT u.id FROM tutorhub.users u JOIN tutorhub.identities i ON i.user_id=u.id
    WHERE i.provider_slug='facebook' AND i.provider=$1 AND i.subject=$2
      AND i.kind IN ('oidc','oauth2') AND i.status IN ('active','unlinked') FOR UPDATE OF u`, event.issuer, event.subject); err != nil {
		return ErrAuthFoundationStorage
	}
	_, err = tx.Exec(ctx, `WITH target AS MATERIALIZED (
    SELECT id FROM tutorhub.identities WHERE provider=$1 AND subject=$2 AND provider_slug='facebook'
      AND kind IN ('oidc','oauth2') AND status IN ('active','unlinked') FOR UPDATE
 ), removed AS (
    UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$3,updated_at=$3
    WHERE id IN (SELECT id FROM target) AND status='active')
 UPDATE tutorhub.sessions SET revoked_at=$3,revoked_reason='identity_unlinked'
 WHERE identity_id IN (SELECT id FROM target) AND revoked_at IS NULL`, event.issuer, event.subject, event.now)
	if err != nil || tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (r *PostgresRepository) facebookReceiptStatus(ctx context.Context, code string, now time.Time) (string, error) {
	ctx, cancel := r.contextWithTimeout(ctx)
	defer cancel()
	tx, err := r.database.Begin(ctx)
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	defer rollback(tx)
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM tutorhub.external_data_deletion_requests
 WHERE confirmation_code=$1 AND created_at > $2::timestamptz - interval '1 year'`, code, now).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDeletionRequestNotFound
	}
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	return status, nil
}
