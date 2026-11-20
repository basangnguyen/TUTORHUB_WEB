package identity

import (
	"bytes"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

func TestTOTPRFC6238WindowAndMonotonicCounter(t *testing.T) {
	secret := base32.StdEncoding.EncodeToString([]byte("12345678901234567890"))
	// RFC 6238 Appendix B, SHA-1, last six digits of the eight-digit vector.
	if step, ok := matchTOTPStep(secret, "081804", time.Unix(1111111109, 0), 0); !ok || step != 1111111109/30 {
		t.Fatal("RFC vector failed")
	}
	now := time.Unix(1700100000, 0)
	for offset := -2; offset <= 2; offset++ {
		code, err := totp.GenerateCode(secret, now.Add(time.Duration(offset)*30*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		step, ok := matchTOTPStep(secret, code, now, 0)
		if ok != (offset >= -1 && offset <= 1) {
			t.Errorf("skew %d", offset)
		}
		if ok {
			if step != now.Unix()/30+int64(offset) {
				t.Fatalf("incorrect matched counter %d", step)
			}
			if _, ok = matchTOTPStep(secret, code, now, step); ok {
				t.Fatal("accepted replay")
			}
			if _, ok = matchTOTPStep(secret, code, now, step+1); ok {
				t.Fatal("accepted counter after rollback")
			}
		}
	}
}

func TestMFASecretsAreRowBoundAndRecoveryCodesAreAccountBound(t *testing.T) {
	c, err := NewCrypto(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := c.totpAEAD()
	if err != nil {
		t.Fatal(err)
	}
	user, row := uuid.New(), uuid.New()
	raw := []byte("fixture enrollment")
	cipher := aead.Seal(nil, nil, raw, totpAAD(user, row))
	if bytes.Contains(cipher, raw) {
		t.Fatal("plaintext in ciphertext")
	}
	for _, aad := range [][]byte{totpAAD(uuid.New(), row), totpAAD(user, uuid.New())} {
		if _, err = aead.Open(nil, nil, cipher, aad); err == nil {
			t.Fatal("substituted ciphertext accepted")
		}
	}
	codes := map[string]bool{}
	for range 10 {
		display, canonical, err := makeRecoveryCode()
		if err != nil || len(canonical) != 16 || len(display) != 19 || strings.ReplaceAll(display, "-", "") != canonical || codes[canonical] {
			t.Fatal("recovery encoding/uniqueness")
		}
		codes[canonical] = true
		if bytes.Equal(c.recoveryDigest(user, canonical), c.recoveryDigest(uuid.New(), canonical)) {
			t.Fatal("recovery digest crossed accounts")
		}
	}
}
