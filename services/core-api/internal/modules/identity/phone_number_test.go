package identity

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPhoneNormalizationRegionTypeAndMask(t *testing.T) {
	vn := []string{"VN"}
	for _, tc := range []struct{ raw, region string }{
		{"+84912345678", ""}, {"0912 345 678", ""}, {"84912345678", "vn"}, {"+84 (912) 345-678", "US"},
	} {
		n, err := NormalizePhoneNumber(tc.raw, tc.region, vn)
		if err != nil || n.E164 != "+84912345678" || n.CallingCode != 84 || n.CountryCode != "VN" ||
			n.MaskedNumber != "+84 ***5678" || n.Last4 != "5678" {
			t.Fatal("valid Vietnamese mobile did not resolve to expected canonical form")
		}
	}
	for _, raw := range []string{"", "024 3822 1234", "028 3822 1234", "18001090", "19001090", "+12025550123", "0912345678 ext 123", "dial 0912345678", "0912345678\nBCC", "++84912345678"} {
		if _, err := NormalizePhoneNumber(raw, "VN", vn); !errors.Is(err, ErrInvalidPhoneNumber) {
			t.Fatal("accepted disallowed destination")
		}
	}
	// Shared calling code +1 does not let a Canadian destination use US policy.
	if _, err := NormalizePhoneNumber("+14165550123", "US", []string{"US"}); !errors.Is(err, ErrInvalidPhoneNumber) {
		t.Fatal("trusted client country for an international number")
	}
	n, err := NormalizePhoneNumber("2025550123", "", []string{"US", "VN"})
	if err != nil || n.CountryCode != "US" || n.CallingCode != 1 {
		t.Fatal("fixed-line-or-mobile destination or first configured region rejected")
	}
}

func TestPhoneCiphertextPurposeOwnerAndRowBinding(t *testing.T) {
	c, err := NewCrypto(bytes.Repeat([]byte{0x77}, 32))
	if err != nil {
		t.Fatal(err)
	}
	user, phone := uuid.New(), uuid.New()
	const number = "+84912345678"
	first, err := c.encryptPhoneNumber(user, phone, number)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.encryptPhoneNumber(user, phone, number)
	if err != nil || len(first) < 29 || bytes.Equal(first, second) || bytes.Contains(first, []byte(number)) {
		t.Fatal("phone envelope is not randomized")
	}
	plain, err := c.decryptPhoneNumber(user, phone, first)
	if err != nil || plain != number {
		t.Fatal("valid phone envelope did not open")
	}
	if _, err := c.decryptPhoneNumber(uuid.New(), phone, first); err == nil {
		t.Fatal("user substitution accepted")
	}
	if _, err := c.decryptPhoneNumber(user, uuid.New(), first); err == nil {
		t.Fatal("phone row substitution accepted")
	}
	totp, err := c.totpAEAD()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := totp.Open(nil, nil, first, phoneNumberAAD(user, phone)); err == nil {
		t.Fatal("phone ciphertext opened with a key for another purpose")
	}
	lookup := c.phoneLookupHash(number)
	if len(lookup) != 32 || !c.EqualDigest(lookup, "phone-lookup-v1", number) || c.EqualDigest(lookup, "auth-dest-v1", number) {
		t.Fatal("phone lookup digest was not purpose-bound")
	}
}

func TestPhoneRedaction(t *testing.T) {
	const number, code = "+84912345678", "049321"
	n, err := NormalizePhoneNumber(number, "", []string{"VN"})
	if err != nil {
		t.Fatal(err)
	}
	message := ChallengeMessage{Destination: number, Code: code}
	for _, input := range []any{n, message, deliveryJob{message: message}, ChallengeInput{Destination: number}, DeliveryReceipt{ProviderMessageRef: number}} {
		var logs bytes.Buffer
		slog.New(slog.NewJSONHandler(&logs, nil)).Info("redaction", "input", input)
		printed := fmt.Sprintf("%+v %s", input, logs.String())
		if strings.Contains(printed, number) || strings.Contains(printed, code) || !strings.Contains(printed, "redacted") {
			t.Fatal("private phone material in diagnostic output")
		}
	}
}
