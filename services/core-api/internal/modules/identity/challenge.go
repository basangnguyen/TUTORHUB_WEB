package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	EmailChallengeTTL = 10 * time.Minute
	PhoneChallengeTTL = 5 * time.Minute
	ChallengeCooldown = time.Minute
	MaxChallengeSends = 5
)

var ErrAuthChallengeThrottled = errors.New("authentication challenge is temporarily unavailable")

type ChallengeInput struct {
	PhoneCountryCode string
	Purpose          string
	Channel          DeliveryChannel
	Destination      string
	Deliverable      bool
}

func (ChallengeInput) String() string       { return "[redacted challenge input]" }
func (ChallengeInput) LogValue() slog.Value { return slog.StringValue("[redacted challenge input]") }

type AuthChallenge struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	Purpose       string
	Channel       DeliveryChannel
	ExpiresAt     time.Time
}

type ChallengeService struct {
	database   Database
	crypto     *Crypto
	dispatcher *DeliveryDispatcher
	now        func() time.Time
	code       func() (string, error)
}

func NewChallengeService(database Database, crypto *Crypto, dispatcher *DeliveryDispatcher) *ChallengeService {
	return &ChallengeService{database: database, crypto: crypto, dispatcher: dispatcher, now: time.Now, code: newOTP}
}

// Issue commits both the hashed challenge and a redacted delivery record before
// admitting plaintext to the bounded dispatcher. The caller must return the same
// generic 202 response for real, dummy and destination-throttled requests.
func (service *ChallengeService) Issue(ctx context.Context, binding string, input ChallengeInput) (AuthChallenge, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AuthChallenge{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, err := readAuthTransaction(ctx, tx, service.crypto, binding, service.now().UTC(), true)
	if err != nil {
		return AuthChallenge{}, err
	}
	result, job, err := service.issueLocked(ctx, tx, transaction, input)
	if err != nil {
		return AuthChallenge{}, err
	}
	if tx.Commit(ctx) != nil {
		return AuthChallenge{}, ErrAuthFoundationStorage
	}
	service.dispatchCommitted(job)
	return result, nil
}

// issueLocked participates in the caller's transaction. No delivery is possible
// until the caller commits and explicitly dispatches the returned ephemeral job.
func (service *ChallengeService) issueLocked(ctx context.Context, tx pgx.Tx, transaction AuthTransaction, input ChallengeInput) (AuthChallenge, deliveryJob, error) {
	destination, err := normalizeChallengeDestination(input.Channel, input.Destination)
	if err != nil {
		return AuthChallenge{}, deliveryJob{}, err
	}
	code, err := service.code()
	if err != nil || !validOTP(code) {
		return AuthChallenge{}, deliveryJob{}, ErrAuthFoundationStorage
	}
	now := service.now().UTC()
	if !transaction.ExpiresAt.After(now) {
		return AuthChallenge{}, deliveryJob{}, ErrAuthTransactionUnavailable
	}
	if !challengePurposeMatches(transaction.Kind, input.Purpose, input.Channel) {
		return AuthChallenge{}, deliveryJob{}, ErrAuthFoundationInput
	}
	var sends int
	var lastSent *time.Time
	if tx.QueryRow(ctx, `SELECT count(*),max(created_at) FROM tutorhub.auth_challenges WHERE transaction_id=$1`,
		transaction.ID).Scan(&sends, &lastSent) != nil {
		return AuthChallenge{}, deliveryJob{}, ErrAuthFoundationStorage
	}
	if sends >= MaxChallengeSends || (lastSent != nil && now.Before(lastSent.Add(ChallengeCooldown))) {
		return AuthChallenge{}, deliveryJob{}, ErrAuthChallengeThrottled
	}
	ttl := PhoneChallengeTTL
	if input.Channel == DeliveryEmail {
		ttl = EmailChallengeTTL
	}
	expires := now.Add(ttl)
	if expires.After(transaction.ExpiresAt) {
		expires = transaction.ExpiresAt
	}
	result := AuthChallenge{ID: uuid.New(), TransactionID: transaction.ID, Purpose: input.Purpose, Channel: input.Channel, ExpiresAt: expires}
	if _, err = tx.Exec(ctx, `UPDATE tutorhub.auth_challenges SET invalidated_at=$2
		WHERE transaction_id=$1 AND consumed_at IS NULL AND invalidated_at IS NULL`, transaction.ID, now); err != nil {
		return AuthChallenge{}, deliveryJob{}, ErrAuthFoundationStorage
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_challenges
		(id,transaction_id,purpose,channel,destination_hash,code_hash,deliverable,created_at,expires_at,phone_country_code)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''))`, result.ID, transaction.ID, input.Purpose, string(input.Channel),
		service.crypto.Digest("auth-dest-v1", string(input.Channel)+":"+destination),
		service.crypto.Digest("auth-otp-v1", result.ID.String()+":"+code), input.Deliverable, now, expires, input.PhoneCountryCode)
	if err != nil {
		return AuthChallenge{}, deliveryJob{}, ErrAuthFoundationStorage
	}
	deliveryID := uuid.New()
	deliveryChannel := input.Channel
	provider := "disabled"
	if service.dispatcher != nil {
		deliveryChannel, provider = service.dispatcher.plannedRoute(input.Channel, destination)
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_challenge_deliveries
		(id,challenge_id,channel,provider,status,created_at) VALUES ($1,$2,$3,$4,'queued',$5)`,
		deliveryID, result.ID, string(deliveryChannel), provider, now)
	if err != nil {
		return AuthChallenge{}, deliveryJob{}, ErrAuthFoundationStorage
	}
	job := deliveryJob{id: deliveryID, deliverable: input.Deliverable, message: ChallengeMessage{
		ChallengeID: result.ID, Purpose: input.Purpose, Channel: input.Channel, Destination: destination, Code: code, ExpiresAt: expires}}
	return result, job, nil
}

func (service *ChallengeService) dispatchCommitted(job deliveryJob) {
	if service.dispatcher != nil {
		if !service.dispatcher.enqueueCommitted(job) {
			service.dispatcher.record(job.id, "skipped", "queue_full", DeliveryReceipt{})
		}
	} else {
		disabled := DeliveryDispatcher{database: service.database, crypto: service.crypto}
		disabled.record(job.id, "disabled", "sender_disabled", DeliveryReceipt{})
	}
}

// Verify locks the parent before the challenge. Resend and parallel verification
// use the same lock order, so at most one request can consume the current code.
// Dummy challenges follow the same SELECT, HMAC comparison and UPDATE path.
func (service *ChallengeService) Verify(ctx context.Context, binding string, challengeID uuid.UUID, code string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return false, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	transaction, err := readAuthTransaction(ctx, tx, service.crypto, binding, service.now().UTC(), true)
	if err != nil {
		return false, err
	}
	matched, err := service.verifyLocked(ctx, tx, transaction, challengeID, code)
	if err != nil {
		return false, err
	}
	if tx.Commit(ctx) != nil {
		return false, ErrAuthFoundationStorage
	}
	return matched, nil
}

func (service *ChallengeService) verifyLocked(ctx context.Context, tx pgx.Tx, transaction AuthTransaction, challengeID uuid.UUID, code string) (bool, error) {
	var hash []byte
	var deliverable bool
	var attempts, maximum int
	var expires time.Time
	var consumed, invalidated *time.Time
	err := tx.QueryRow(ctx, `SELECT code_hash,deliverable,attempts,max_attempts,expires_at,consumed_at,invalidated_at
		FROM tutorhub.auth_challenges WHERE id=$1 AND transaction_id=$2 FOR UPDATE`, challengeID, transaction.ID).
		Scan(&hash, &deliverable, &attempts, &maximum, &expires, &consumed, &invalidated)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrAuthFoundationStorage
	}
	// A row lock may have waited until after the original timestamp expired.
	now := service.now().UTC()
	matched := subtle.ConstantTimeCompare(hash, service.crypto.Digest("auth-otp-v1", challengeID.String()+":"+code)) == 1
	if consumed != nil || invalidated != nil || !expires.After(now) || !transaction.ExpiresAt.After(now) || attempts >= maximum {
		return false, nil
	}
	matched = matched && validOTP(code) && deliverable
	if matched {
		consumed = &now
	} else {
		attempts++
		if attempts >= maximum {
			invalidated = &now
		}
	}
	_, err = tx.Exec(ctx, `UPDATE tutorhub.auth_challenges SET attempts=$2,consumed_at=$3,invalidated_at=$4 WHERE id=$1`,
		challengeID, attempts, consumed, invalidated)
	if err != nil {
		return false, ErrAuthFoundationStorage
	}
	return matched, nil
}

func newOTP() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func validOTP(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func normalizeChallengeDestination(channel DeliveryChannel, value string) (string, error) {
	value = strings.TrimSpace(value)
	if channel == DeliveryEmail {
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
			return "", ErrAuthFoundationInput
		}
		return strings.ToLower(value), nil
	}
	switch channel {
	case "sms", "zalo_zns", "whatsapp", "voice":
		if len(value) < 8 || len(value) > 16 || value[0] != '+' || value[1] == '0' {
			return "", ErrAuthFoundationInput
		}
		for _, digit := range value[1:] {
			if digit < '0' || digit > '9' {
				return "", ErrAuthFoundationInput
			}
		}
		return value, nil
	default:
		return "", ErrAuthFoundationInput
	}
}

func challengePurposeMatches(kind, purpose string, channel DeliveryChannel) bool {
	switch kind {
	case "signup_email", registrationKind:
		return purpose == "signup_email" && channel == DeliveryEmail
	case "login":
		return (purpose == "login_code" && channel == DeliveryEmail) || (purpose == "phone_login" && channel != DeliveryEmail)
	case "recovery":
		return purpose == "password_recovery" && channel == DeliveryEmail
	case "phone":
		return (purpose == "phone_verify" || purpose == "phone_login") && channel != DeliveryEmail
	case "mfa":
		return purpose == "mfa"
	case "link_intent":
		return purpose == "link_identity"
	case "reauth":
		return purpose == "reauth"
	default:
		return false
	}
}
