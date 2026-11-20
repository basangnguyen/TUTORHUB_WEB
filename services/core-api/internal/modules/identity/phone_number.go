package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/nyaruka/phonenumbers"
)

var ErrInvalidPhoneNumber = errors.New("invalid phone number")

// The full number belongs only in the short-lived encrypted transaction or
// the purpose-bound ciphertext of an active phone binding.
type NormalizedPhoneNumber struct {
	E164         string `json:"e164"`
	CountryCode  string `json:"country_code"`
	CallingCode  int    `json:"calling_code"`
	MaskedNumber string `json:"masked_number"`
	Last4        string `json:"last4"`
}

func (NormalizedPhoneNumber) String() string         { return "[redacted phone input]" }
func (n NormalizedPhoneNumber) LogValue() slog.Value { return slog.StringValue(n.String()) }

func NormalizePhoneNumber(raw, country string, allowed []string) (NormalizedPhoneNumber, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 64 || len(allowed) == 0 {
		return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
	}
	// libphonenumber also recognizes vanity numbers, embedded text and extensions.
	// An OTP destination accepts only a dial string with presentation separators.
	for i, ch := range raw {
		if !(ch >= '0' && ch <= '9') && !strings.ContainsRune(" -().", ch) && !(i == 0 && ch == '+') {
			return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
		}
	}
	region := strings.ToUpper(strings.TrimSpace(country))
	international := strings.HasPrefix(raw, "+")
	if international {
		region = phonenumbers.UNKNOWN_REGION
	} else {
		if region == "" {
			region = allowed[0]
		}
		if !slices.Contains(allowed, region) {
			return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
		}
	}
	number, err := phonenumbers.Parse(raw, region)
	if err != nil {
		return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
	}
	actualRegion := phonenumbers.GetRegionCodeForNumber(number)
	if (!international && actualRegion != region) || !slices.Contains(allowed, actualRegion) || !phonenumbers.IsValidNumberForRegion(number, actualRegion) {
		return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
	}
	switch phonenumbers.GetNumberType(number) {
	case phonenumbers.MOBILE, phonenumbers.FIXED_LINE_OR_MOBILE:
	default:
		return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
	}
	e164 := phonenumbers.Format(number, phonenumbers.E164)
	national := phonenumbers.GetNationalSignificantNumber(number)
	if len(e164) > 16 || len(national) < 4 {
		return NormalizedPhoneNumber{}, ErrInvalidPhoneNumber
	}
	// Even for a short national number keep at least two digits hidden.
	last := national[len(national)-min(4, len(national)-2):]
	calling := int(number.GetCountryCode())
	return NormalizedPhoneNumber{E164: e164, CountryCode: actualRegion, CallingCode: calling,
		MaskedNumber: fmt.Sprintf("+%d ***%s", calling, last), Last4: last}, nil
}

func (c *Crypto) phoneLookupHash(e164 string) []byte {
	return c.Digest("phone-lookup-v1", e164)
}

func (c *Crypto) phoneAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.Digest("identity-encryption-key-v1", "phone_number"))
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func phoneNumberAAD(userID, phoneID uuid.UUID) []byte {
	return []byte("phone_number:v1:" + userID.String() + ":" + phoneID.String())
}

func (c *Crypto) encryptPhoneNumber(userID, phoneID uuid.UUID, e164 string) ([]byte, error) {
	gcm, err := c.phoneAEAD()
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	return gcm.Seal(nil, nil, []byte(e164), phoneNumberAAD(userID, phoneID)), nil
}

func (c *Crypto) decryptPhoneNumber(userID, phoneID uuid.UUID, sealed []byte) (string, error) {
	gcm, err := c.phoneAEAD()
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	clear, err := gcm.Open(nil, nil, sealed, phoneNumberAAD(userID, phoneID))
	if err != nil {
		return "", ErrAuthFoundationStorage
	}
	return string(clear), nil
}
