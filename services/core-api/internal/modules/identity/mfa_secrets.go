package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

const recoveryAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var recoveryEncoding = base32.NewEncoding(recoveryAlphabet).WithPadding(base32.NoPadding)

// Factor secrets outlive a browser transaction and are account scoped. The key
// and associated data bind this envelope to its purpose and immutable owner/row.
func (c *Crypto) totpAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.Digest("identity-encryption-key-v1", "totp_secret"))
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func totpAAD(userID, factorID uuid.UUID) []byte {
	return []byte("totp_secret:v1:" + userID.String() + ":" + factorID.String())
}

func matchTOTPStep(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	current := now.Unix() / 30
	var matched int64
	for _, step := range []int64{current - 1, current, current + 1} {
		if step < 1 {
			continue
		}
		candidate, err := hotp.GenerateCodeCustom(secret, uint64(step), hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			// Adjacent counters can coincidentally generate the same six digits.
			if step <= last {
				return 0, false
			}
			matched = step
		}
	}
	return matched, matched > 0
}

// All TOTP uses share this row and monotonic counter, including the initial
// enrollment proof. An error rolls back both the proof and its protected action.
func (s *NativeAuthService) useTOTP(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string, pending bool) error {
	status := "active"
	if pending {
		status = "pending"
	}
	var id uuid.UUID
	var sealed []byte
	var last int64
	err := tx.QueryRow(ctx, `SELECT id,secret_ciphertext,coalesce(last_used_step,0) FROM tutorhub.mfa_totp_factors
        WHERE user_id=$1 AND status=$2 FOR UPDATE`, userID, status).Scan(&id, &sealed, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNativeInvalidCode
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	aead, err := s.crypto.totpAEAD()
	if err != nil {
		return ErrAuthFoundationStorage
	}
	secret, err := aead.Open(nil, nil, sealed, totpAAD(userID, id))
	if err != nil {
		return ErrAuthFoundationStorage
	}
	now := s.now().UTC()
	step, ok := matchTOTPStep(string(secret), code, now, last)
	if !ok {
		return ErrNativeInvalidCode
	}
	_, err = tx.Exec(ctx, `UPDATE tutorhub.mfa_totp_factors SET status='active',last_used_step=$2,
      confirmed_at=coalesce(confirmed_at,greatest(updated_at,$3)),updated_at=greatest(updated_at,$3) WHERE id=$1`, id, step, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func makeRecoveryCode() (string, string, error) {
	var raw [10]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", ErrAuthFoundationStorage
	}
	canonical := recoveryEncoding.EncodeToString(raw[:])
	return canonical[:4] + "-" + canonical[4:8] + "-" + canonical[8:12] + "-" + canonical[12:], canonical, nil
}

func (c *Crypto) recoveryDigest(userID uuid.UUID, code string) []byte {
	return c.Digest("mfa-recovery-code-v1:"+userID.String(), code)
}

func invalidateRecoveryCodes(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE tutorhub.mfa_recovery_codes SET invalidated_at=greatest(created_at,$2)
      WHERE user_id=$1 AND consumed_at IS NULL AND invalidated_at IS NULL`, userID, now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func (s *NativeAuthService) replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) ([]string, error) {
	if err := invalidateRecoveryCodes(ctx, tx, userID, now); err != nil {
		return nil, err
	}
	codes := make([]string, 10)
	for i := range codes {
		display, canonical, err := makeRecoveryCode()
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO tutorhub.mfa_recovery_codes(user_id,code_hash,created_at) VALUES($1,$2,$3)`, userID, s.crypto.recoveryDigest(userID, canonical), now)
		if err != nil {
			return nil, ErrAuthFoundationStorage
		}
		codes[i] = display
	}
	return codes, nil
}

func (s *NativeAuthService) useRecoveryCode(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string) error {
	canonical := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(canonical) != 16 || strings.Trim(canonical, recoveryAlphabet) != "" {
		return ErrNativeInvalidCode
	}
	cmd, err := tx.Exec(ctx, `UPDATE tutorhub.mfa_recovery_codes SET consumed_at=greatest(created_at,$3)
      WHERE user_id=$1 AND code_hash=$2 AND consumed_at IS NULL AND invalidated_at IS NULL`, userID, s.crypto.recoveryDigest(userID, canonical), s.now().UTC())
	if err != nil {
		return ErrAuthFoundationStorage
	}
	if cmd.RowsAffected() != 1 {
		return ErrNativeInvalidCode
	}
	return nil
}
