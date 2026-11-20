package identity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrPhoneDeliveryUnavailable  = errors.New("phone delivery is unavailable")
	ErrPhoneInUse                = errors.New("phone is already in use")
	ErrPhoneStale                = errors.New("phone requires verification in settings")
	ErrPhoneSecondFactorRequired = errors.New("another sign-in method is required")
)

type PhoneSummary struct {
	ID           uuid.UUID `json:"id"`
	MaskedNumber string    `json:"masked_number"`
	CountryCode  string    `json:"country_code"`
	CallingCode  int       `json:"calling_code"`
	Last4        string    `json:"last4"`
	VerifiedAt   time.Time `json:"verified_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	Stale        bool      `json:"stale"`
}

// PhoneSendGuard enforces destination, delivery budget and conversion policy
// on the same database transaction as the challenge being admitted.
type PhoneSendGuard func(context.Context, pgx.Tx, NormalizedPhoneNumber, bool) (bool, error)

type PhoneStartInput struct {
	PhoneNumber, CountryCode, RemoteAddress string
	KeepSignedIn                            bool
	Guard                                   PhoneSendGuard
}

func (PhoneStartInput) String() string         { return "[redacted phone input]" }
func (v PhoneStartInput) LogValue() slog.Value { return slog.StringValue(v.String()) }

type NativePhoneAPI interface {
	StartPhoneLogin(context.Context, PhoneStartInput) (SignupChallengeResult, error)
	StartMyPhone(context.Context, Principal, PhoneStartInput) (SignupChallengeResult, error)
	ResendPhoneChallenge(context.Context, string, *Principal, PhoneSendGuard) (SignupChallengeResult, error)
	VerifyPhoneLogin(context.Context, string, EmailCodeVerifyInput) (NativeSessionResult, error)
	VerifyMyPhone(context.Context, Principal, string, EmailCodeVerifyInput) (*PhoneSummary, error)
	GetMyPhone(context.Context, Principal) (*PhoneSummary, error)
	RemoveMyPhone(context.Context, Principal) (ReauthenticateResult, error)
}

var _ NativePhoneAPI = (*NativeAuthService)(nil)

// The sealed identity ID pins a login to its original binding, including
// across release and re-registration of the very same number by another user.
type phonePayload struct {
	Number                                       NormalizedPhoneNumber
	Purpose                                      string
	TransactionID, UserID, IdentityID, SessionID uuid.UUID
	ContextVersion                               int64
	KeepSignedIn                                 bool
}

func (phonePayload) String() string         { return "[redacted phone transaction]" }
func (v phonePayload) LogValue() slog.Value { return slog.StringValue(v.String()) }

func phonePastThreshold(now, last time.Time, threshold time.Duration) bool {
	return now.Sub(last) > threshold
}

func (s *NativeAuthService) StartPhoneLogin(ctx context.Context, input PhoneStartInput) (SignupChallengeResult, error) {
	return s.startPhone(ctx, nil, input)
}

func (s *NativeAuthService) StartMyPhone(ctx context.Context, p Principal, input PhoneStartInput) (SignupChallengeResult, error) {
	return s.startPhone(ctx, &p, input)
}

func (s *NativeAuthService) startPhone(ctx context.Context, p *Principal, input PhoneStartInput) (SignupChallengeResult, error) {
	if !s.config.PhoneEnabled {
		return SignupChallengeResult{}, ErrNativeMethodDisabled
	}
	number, err := NormalizePhoneNumber(input.PhoneNumber, input.CountryCode, s.config.PhoneAllowedCountries)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	proof := phonePayload{Number: number, TransactionID: uuid.New(), Purpose: "phone_login", KeepSignedIn: input.KeepSignedIn}
	if p != nil {
		if _, err = s.lockRecentPasskeySession(ctx, tx, *p, true); err != nil {
			return SignupChallengeResult{}, err
		}
		proof.Purpose = "phone_verify"
		proof.UserID, proof.SessionID, proof.ContextVersion = p.User.ID, p.SessionID, p.ContextVersion
	}
	if input.Guard == nil {
		return SignupChallengeResult{}, ErrPhoneDeliveryUnavailable
	}
	allowed, err := input.Guard(ctx, tx, number, p != nil)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if !allowed && p != nil {
		return SignupChallengeResult{}, ErrAuthChallengeThrottled
	}
	if p == nil {
		err = tx.QueryRow(ctx, `SELECT n.user_id,n.identity_id FROM tutorhub.user_phone_numbers n
			JOIN tutorhub.identities i ON i.id=n.identity_id AND i.user_id=n.user_id AND i.kind='phone' AND i.status='active'
			JOIN tutorhub.users u ON u.id=n.user_id AND u.status='active'
			WHERE n.e164_lookup_hash=$1 AND n.status='active'`, s.crypto.phoneLookupHash(number.E164)).Scan(&proof.UserID, &proof.IdentityID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
	}
	binding, err := s.crypto.RandomToken()
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	sealed, err := s.crypto.Encrypt(string(raw))
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	now := s.now().UTC()
	transaction := AuthTransaction{ID: proof.TransactionID, Kind: "phone", State: "pending", CreatedAt: now, ExpiresAt: now.Add(AuthTransactionTTL)}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(id,binding_hash,kind,state,user_id,session_id,payload_ciphertext,ip_bucket_hash,created_at,expires_at)
		VALUES($1,$2,'phone','pending',$3,$4,$5,$6,$7,$8)`, transaction.ID, s.crypto.Digest("auth-tx-binding", binding),
		nullUUID(proof.UserID), nullUUID(proof.SessionID), sealed, s.ipDigest(input.RemoteAddress), now, transaction.ExpiresAt)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	result, err := s.issuePhone(ctx, tx, transaction, proof, allowed)
	if err == nil {
		result.Cookie = s.transactionCookie(binding, transaction.ExpiresAt)
	}
	return result, err
}

func (s *NativeAuthService) readPhone(ctx context.Context, tx pgx.Tx, binding string, lock bool) (AuthTransaction, phonePayload, error) {
	var transaction AuthTransaction
	var proof phonePayload
	if len(binding) != 43 {
		return transaction, proof, ErrAuthTransactionUnavailable
	}
	query := `SELECT id,created_at,expires_at,user_id,session_id,payload_ciphertext FROM tutorhub.auth_transactions
		WHERE binding_hash=$1 AND kind='phone' AND state='pending' AND expires_at>$2`
	if lock {
		query += " FOR UPDATE"
	}
	var user, session uuid.NullUUID
	var sealed []byte
	err := tx.QueryRow(ctx, query, s.crypto.Digest("auth-tx-binding", binding), s.now().UTC()).Scan(&transaction.ID, &transaction.CreatedAt, &transaction.ExpiresAt, &user, &session, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return transaction, proof, ErrAuthTransactionUnavailable
	}
	if err != nil {
		return transaction, proof, ErrAuthFoundationStorage
	}
	raw, err := s.crypto.Decrypt(sealed)
	if err != nil || json.Unmarshal([]byte(raw), &proof) != nil || proof.TransactionID != transaction.ID || proof.UserID != user.UUID || proof.SessionID != session.UUID || !transaction.ExpiresAt.After(s.now()) {
		return transaction, proof, ErrAuthTransactionUnavailable
	}
	transaction.Kind, transaction.State = "phone", "pending"
	return transaction, proof, nil
}

func (s *NativeAuthService) issuePhone(ctx context.Context, tx pgx.Tx, t AuthTransaction, p phonePayload, allowed bool) (SignupChallengeResult, error) {
	c, job, err := s.challenges.issueLocked(ctx, tx, t, ChallengeInput{Purpose: p.Purpose, Channel: DeliverySMS, Destination: p.Number.E164,
		PhoneCountryCode: p.Number.CountryCode, Deliverable: allowed && p.UserID != uuid.Nil})
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if tx.Commit(ctx) != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	s.challenges.dispatchCommitted(job)
	remaining := c.ExpiresAt.Sub(s.challenges.now().UTC())
	return SignupChallengeResult{ChallengeID: c.ID, CooldownSeconds: int(ChallengeCooldown / time.Second),
		ExpiresInSeconds: max(1, int((remaining+time.Second-1)/time.Second)), MaskedDestination: p.Number.MaskedNumber}, nil
}

func matchPhoneSession(proof phonePayload, p *Principal) error {
	if p == nil {
		return ErrInvalidCSRFToken
	}
	if proof.Purpose != "phone_verify" || proof.UserID != p.User.ID || proof.SessionID != p.SessionID || proof.ContextVersion != p.ContextVersion {
		return ErrNativeInvalidCode
	}
	return nil
}

func (s *NativeAuthService) ResendPhoneChallenge(ctx context.Context, binding string, p *Principal, guard PhoneSendGuard) (SignupChallengeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return SignupChallengeResult{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	_, snapshot, err := s.readPhone(ctx, tx, binding, false)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if !s.config.PhoneEnabled {
		return SignupChallengeResult{}, ErrNativeMethodDisabled
	}
	manage := snapshot.Purpose == "phone_verify"
	if manage {
		if err = matchPhoneSession(snapshot, p); err != nil {
			return SignupChallengeResult{}, err
		}
		if _, err = s.lockRecentPasskeySession(ctx, tx, *p, true); err != nil {
			return SignupChallengeResult{}, err
		}
	}
	t, proof, err := s.readPhone(ctx, tx, binding, true)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if proof != snapshot {
		return SignupChallengeResult{}, ErrNativeInvalidCode
	}
	if guard == nil {
		return SignupChallengeResult{}, ErrPhoneDeliveryUnavailable
	}
	allowed, err := guard(ctx, tx, proof.Number, manage)
	if err != nil {
		return SignupChallengeResult{}, err
	}
	if manage && !allowed {
		return SignupChallengeResult{}, ErrAuthChallengeThrottled
	}
	if !manage && proof.UserID != uuid.Nil {
		var active bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.user_phone_numbers n JOIN tutorhub.users u ON u.id=n.user_id
			JOIN tutorhub.identities i ON i.id=n.identity_id AND i.user_id=u.id
			WHERE n.user_id=$1 AND n.identity_id=$2 AND n.status='active' AND u.status='active' AND i.status='active' AND i.kind='phone')`, proof.UserID, proof.IdentityID).Scan(&active)
		if err != nil {
			return SignupChallengeResult{}, ErrAuthFoundationStorage
		}
		allowed = allowed && active
	}
	return s.issuePhone(ctx, tx, t, proof, allowed)
}

// Invalid attempts commit independently of the successful workflow, including
// challenge lockout. Other errors leave the transaction for its caller to roll back.
func (s *NativeAuthService) provePhone(ctx context.Context, tx pgx.Tx, t AuthTransaction, proof phonePayload, input EmailCodeVerifyInput) error {
	var matches bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.auth_challenges WHERE id=$1 AND transaction_id=$2 AND purpose=$3 AND channel<>'email')`, input.ChallengeID, t.ID, proof.Purpose).Scan(&matches)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if !matches {
		return ErrNativeInvalidCode
	}
	matched, err := s.challenges.verifyLocked(ctx, tx, t, input.ChallengeID, input.Code)
	if err != nil {
		return err
	}
	if !matched {
		if tx.Commit(ctx) != nil {
			return ErrAuthFoundationStorage
		}
		return ErrNativeInvalidCode
	}
	now := s.now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE tutorhub.auth_transactions SET state='completed',completed_at=GREATEST(created_at,$2),payload_ciphertext=NULL
		WHERE id=$1 AND state='pending' AND expires_at>$2`, t.ID, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if tag.RowsAffected() != 1 {
		return ErrNativeInvalidCode
	}
	return nil
}

func commitPhoneDecision(ctx context.Context, tx pgx.Tx, decision error) error {
	if tx.Commit(ctx) != nil {
		return ErrAuthFoundationStorage
	}
	return decision
}

func (s *NativeAuthService) VerifyPhoneLogin(ctx context.Context, binding string, input EmailCodeVerifyInput) (NativeSessionResult, error) {
	var result NativeSessionResult
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
	_, snapshot, err := s.readPhone(ctx, tx, binding, false)
	if errors.Is(err, ErrAuthTransactionUnavailable) {
		return result, ErrNativeInvalidCode
	}
	if err != nil {
		return result, err
	}
	if snapshot.Purpose != "phone_login" {
		return result, ErrNativeInvalidCode
	}
	// Follow the account-first lock order shared by MFA, credential changes and
	// reclaim, then lock the transaction and its challenge for one-time use.
	if snapshot.UserID != uuid.Nil {
		if _, err = tx.Exec(ctx, `SELECT id FROM tutorhub.users WHERE id=$1 ORDER BY id FOR UPDATE`, snapshot.UserID); err != nil {
			return result, ErrAuthFoundationStorage
		}
	}
	t, proof, err := s.readPhone(ctx, tx, binding, true)
	if errors.Is(err, ErrAuthTransactionUnavailable) {
		return result, ErrNativeInvalidCode
	}
	if err != nil {
		return result, err
	}
	if proof != snapshot {
		return result, ErrNativeInvalidCode
	}
	if err = s.provePhone(ctx, tx, t, proof, input); err != nil {
		return result, err
	}
	state, err := readPhoneLoginState(ctx, tx, proof.UserID, proof.IdentityID, s.crypto.phoneLookupHash(proof.Number.E164), uuid.Nil, uuid.Nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, commitPhoneDecision(ctx, tx, ErrNativeInvalidCode)
	}
	if err != nil {
		return result, ErrAuthFoundationStorage
	}
	if state.staleForLogin(s.now(), s.config.PhoneStaleAfter) {
		return result, commitPhoneDecision(ctx, tx, ErrPhoneStale)
	}
	result, err = s.finishLoginFirstFactor(ctx, tx, mfaPayload{UserID: proof.UserID, IdentityID: proof.IdentityID, Methods: []string{"phone_otp"}, KeepSignedIn: proof.KeepSignedIn || input.KeepSignedIn}, input.UserAgent, input.RemoteAddress)
	if errors.Is(err, ErrPhoneSecondFactorRequired) {
		return NativeSessionResult{}, commitPhoneDecision(ctx, tx, err)
	}
	if err != nil {
		return NativeSessionResult{}, err
	}
	if tx.Commit(ctx) != nil {
		return NativeSessionResult{}, ErrAuthFoundationStorage
	}
	return result, nil
}
