package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const AuthTransactionTTL = 15 * time.Minute

var (
	ErrAuthTransactionUnavailable = errors.New("authentication transaction is unavailable")
	ErrAuthFoundationStorage      = errors.New("authentication storage operation failed")
	ErrAuthFoundationInput        = errors.New("invalid authentication input")
)

type AuthTransaction struct {
	ID          uuid.UUID
	Kind        string
	State       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	CompletedAt *time.Time
}

type TransactionInput struct {
	Kind              string
	UserID            *uuid.UUID
	SessionID         *uuid.UUID
	PayloadCiphertext []byte
	IPBucketHash      []byte
}

type TransactionService struct {
	database     Database
	crypto       *Crypto
	cookieSecure bool
	now          func() time.Time
}

func NewTransactionService(database Database, crypto *Crypto, cookieSecure bool) *TransactionService {
	return &TransactionService{database: database, crypto: crypto, cookieSecure: cookieSecure, now: time.Now}
}

func AuthTransactionCookieName(secure bool) string {
	if secure {
		return "__Host-tutorhub_auth_tx"
	}
	return "tutorhub_auth_tx"
}

func (service *TransactionService) Create(ctx context.Context, input TransactionInput) (AuthTransaction, *http.Cookie, error) {
	if !validTransactionKind(input.Kind) || (input.IPBucketHash != nil && len(input.IPBucketHash) != 32) {
		return AuthTransaction{}, nil, ErrAuthFoundationInput
	}
	raw, err := service.crypto.RandomToken()
	if err != nil {
		return AuthTransaction{}, nil, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	result := AuthTransaction{ID: uuid.New(), Kind: input.Kind, State: "pending", CreatedAt: now, ExpiresAt: now.Add(AuthTransactionTTL)}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AuthTransaction{}, nil, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions
		(id,binding_hash,kind,state,user_id,session_id,payload_ciphertext,ip_bucket_hash,created_at,expires_at)
		VALUES ($1,$2,$3,'pending',$4,$5,$6,$7,$8,$9)`,
		result.ID, service.crypto.Digest("auth-tx-binding", raw), input.Kind, input.UserID, input.SessionID,
		input.PayloadCiphertext, input.IPBucketHash, now, result.ExpiresAt)
	if err != nil || tx.Commit(ctx) != nil {
		return AuthTransaction{}, nil, ErrAuthFoundationStorage
	}
	cookie := &http.Cookie{Name: AuthTransactionCookieName(service.cookieSecure), Value: raw,
		HttpOnly: true, Secure: service.cookieSecure, SameSite: http.SameSiteStrictMode,
		Path: "/", MaxAge: int(AuthTransactionTTL.Seconds()), Expires: result.ExpiresAt}
	return result, cookie, nil
}

func (service *TransactionService) Lookup(ctx context.Context, binding string) (AuthTransaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AuthTransaction{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	return readAuthTransaction(ctx, tx, service.crypto, binding, service.now().UTC(), false)
}

// Consume completes the browser-bound transaction exactly once. Session creation
// will use the same locked transaction in the method-specific authentication slices.
func (service *TransactionService) Consume(ctx context.Context, binding string) (AuthTransaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AuthTransaction{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	now := service.now().UTC()
	result, err := readAuthTransaction(ctx, tx, service.crypto, binding, now, true)
	if err != nil {
		return AuthTransaction{}, err
	}
	now = service.now().UTC()
	if !result.ExpiresAt.After(now) {
		return AuthTransaction{}, ErrAuthTransactionUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed', completed_at=$2 WHERE id=$1`, result.ID, now); err != nil {
		return AuthTransaction{}, ErrAuthFoundationStorage
	}
	if tx.Commit(ctx) != nil {
		return AuthTransaction{}, ErrAuthFoundationStorage
	}
	result.State, result.CompletedAt = "completed", &now
	return result, nil
}

func readAuthTransaction(ctx context.Context, tx pgx.Tx, crypto *Crypto, binding string, now time.Time, lock bool) (AuthTransaction, error) {
	if len(binding) != 43 {
		return AuthTransaction{}, ErrAuthTransactionUnavailable
	}
	query := `SELECT id,kind,state,created_at,expires_at,completed_at FROM tutorhub.auth_transactions
		WHERE binding_hash=$1 AND state='pending' AND expires_at>$2`
	if lock {
		query += " FOR UPDATE"
	}
	var result AuthTransaction
	err := tx.QueryRow(ctx, query, crypto.Digest("auth-tx-binding", binding), now).Scan(
		&result.ID, &result.Kind, &result.State, &result.CreatedAt, &result.ExpiresAt, &result.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthTransaction{}, ErrAuthTransactionUnavailable
	}
	if err != nil {
		return AuthTransaction{}, ErrAuthFoundationStorage
	}
	return result, nil
}

func validTransactionKind(kind string) bool {
	switch kind {
	case "signup_email", "login", "recovery", "phone", "mfa", "link_intent", "reauth", "passkey":
		return true
	default:
		return false
	}
}
