package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrFacebookSignedRequest   = errors.New("invalid Facebook callback")
	ErrDeletionRequestNotFound = errors.New("deletion request unavailable")
)

func (crypto *Crypto) ProviderSubjectDigest(issuer, subject string) []byte {
	return crypto.Digest("federation-subject", issuer+"|"+subject)
}

type FacebookCallbackAPI interface {
	FacebookDelete(ctx context.Context, signed, secret, issuer string) (string, error)
	FacebookDeauthorize(ctx context.Context, signed, secret, issuer string) error
	FacebookDeletionStatus(context.Context, string) (string, error)
}

type facebookCallbackRepository interface {
	deleteFacebookData(context.Context, facebookCallbackEvent) (string, error)
	deauthorizeFacebook(context.Context, facebookCallbackEvent) error
	facebookReceiptStatus(context.Context, string, time.Time) (string, error)
}

type facebookCallbackEvent struct {
	issuer, subject            string
	subjectDigest, eventDigest []byte
	now                        time.Time
	fresh                      bool
}

func (s *Service) facebookCallback(signed, secret, issuer string) (facebookCallbackRepository, facebookCallbackEvent, error) {
	issuer = strings.TrimRight(issuer, "/")
	if issuer == "" {
		issuer = FacebookIssuerURL
	}
	repo, ok := s.repository.(facebookCallbackRepository)
	if !ok {
		return nil, facebookCallbackEvent{}, ErrAuthFoundationStorage
	}
	now := s.clock().UTC()
	subject, payload, fresh, err := verifyFacebookSignedRequest(signed, secret, now)
	if err != nil {
		return nil, facebookCallbackEvent{}, err
	}
	return repo, facebookCallbackEvent{issuer: issuer, subject: subject, subjectDigest: s.crypto.ProviderSubjectDigest(issuer, subject),
		eventDigest: s.crypto.Digest("facebook-signed-payload", payload), now: now, fresh: fresh}, nil
}

func (s *Service) FacebookDelete(ctx context.Context, signed, secret, issuer string) (string, error) {
	repo, event, err := s.facebookCallback(signed, secret, issuer)
	if err != nil {
		return "", err
	}
	return repo.deleteFacebookData(ctx, event)
}

func (s *Service) FacebookDeauthorize(ctx context.Context, signed, secret, issuer string) error {
	repo, event, err := s.facebookCallback(signed, secret, issuer)
	if err != nil {
		return err
	}
	return repo.deauthorizeFacebook(ctx, event)
}

func (s *Service) FacebookDeletionStatus(ctx context.Context, code string) (string, error) {
	if len(code) != 32 {
		return "", ErrDeletionRequestNotFound
	}
	if _, err := hex.DecodeString(code); err != nil {
		return "", ErrDeletionRequestNotFound
	}
	repo, ok := s.repository.(facebookCallbackRepository)
	if !ok {
		return "", ErrAuthFoundationStorage
	}
	return repo.facebookReceiptStatus(ctx, code, s.clock().UTC())
}

// Only authenticated data gets parsed. Keep the original base64url payload for event idempotency.
func verifyFacebookSignedRequest(signed, secret string, now time.Time) (string, string, bool, error) {
	signature, payload, ok := strings.Cut(signed, ".")
	if !ok || secret == "" || len(signed) > 64<<10 || len(signature) != 43 || payload == "" {
		return "", "", false, ErrFacebookSignedRequest
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return "", "", false, ErrFacebookSignedRequest
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(payload)
	if err != nil {
		return "", "", false, ErrFacebookSignedRequest
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", "", false, ErrFacebookSignedRequest
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if !ok || err != nil || fields[key] != nil {
			return "", "", false, ErrFacebookSignedRequest
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return "", "", false, ErrFacebookSignedRequest
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return "", "", false, ErrFacebookSignedRequest
	}
	if _, err = decoder.Token(); err != io.EOF {
		return "", "", false, ErrFacebookSignedRequest
	}
	var algorithm, subject string
	if json.Unmarshal(fields["algorithm"], &algorithm) != nil || algorithm != "HMAC-SHA256" ||
		json.Unmarshal(fields["user_id"], &subject) != nil || !validFacebookASID(subject) {
		return "", "", false, ErrFacebookSignedRequest
	}
	fresh := true
	if stamp, has := fields["issued_at"]; has {
		issued, err := strconv.ParseInt(string(stamp), 10, 64)
		if err != nil || issued <= 0 || issued > now.Add(5*time.Minute).Unix() {
			return "", "", false, ErrFacebookSignedRequest
		}
		fresh = issued >= now.Add(-24*time.Hour).Unix()
	}
	if stamp, has := fields["expires"]; has {
		expires, err := strconv.ParseInt(string(stamp), 10, 64)
		if err != nil || expires < 0 {
			return "", "", false, ErrFacebookSignedRequest
		}
		fresh = fresh && (expires == 0 || now.Unix() < expires)
	}
	return subject, payload, fresh, nil
}

func validFacebookASID(subject string) bool {
	if subject == "" || len(subject) > 128 {
		return false
	}
	for _, r := range subject {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newFacebookConfirmationCode() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	return hex.EncodeToString(id[:]), nil
}
