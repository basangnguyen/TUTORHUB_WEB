// Package protecteddata protects the small amount of personally identifiable
// Calendar data that must be persisted outside the normal business tables.
//
// It deliberately does not share the identity/session crypto key. Callers must
// bind a value to a tenant, a supported purpose, and the stable record that owns
// it so ciphertext cannot be replayed into another Calendar row.
package protecteddata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minimumKeyBytes            = 32
	ciphertextFormat           = byte(1)
	maxContextBindingSize      = 128
	maxDeliveryAddressBytes    = 320
	aadDomain                  = "tutorhub.calendar.protected-data.v1"
	recipientFingerprintDomain = "tutorhub.calendar.recipient-fingerprint.v1"
)

var (
	// ErrInvalidConfig means the protector cannot be created safely.
	ErrInvalidConfig = errors.New("invalid calendar protected-data configuration")
	// ErrInvalidContext means a caller omitted or supplied an unsupported AAD binding.
	ErrInvalidContext = errors.New("invalid calendar protected-data context")
	// ErrKeyVersionMismatch prevents a value encrypted by another configured key from
	// being decrypted accidentally during key rotation.
	ErrKeyVersionMismatch = errors.New("calendar protected-data key version mismatch")
	// ErrMalformedCiphertext means the persisted envelope is not a recognized format.
	ErrMalformedCiphertext = errors.New("malformed calendar protected-data ciphertext")
	// ErrAuthenticationFailed means authenticated decryption failed. It intentionally
	// does not expose a low-level error or plaintext-derived detail.
	ErrAuthenticationFailed = errors.New("calendar protected-data authentication failed")
	// ErrInvalidDeliveryAddress means a caller did not provide a bounded recipient
	// address to fingerprint. It intentionally does not contain that address.
	ErrInvalidDeliveryAddress = errors.New("invalid calendar recipient delivery address")
)

// Purpose is a closed set of Calendar data classifications. Adding a new kind of
// protected data must be an explicit code change, rather than a caller-selected
// string that could weaken the AAD boundary.
type Purpose string

const (
	PurposeInvitationCanonicalPayload     Purpose = "invitation_canonical_payload"
	PurposeInvitationRecipientAddress     Purpose = "invitation_recipient_address"
	PurposeInvitationRecipientDisplayName Purpose = "invitation_recipient_display_name"
)

var supportedPurposes = map[Purpose]struct{}{
	PurposeInvitationCanonicalPayload:     {},
	PurposeInvitationRecipientAddress:     {},
	PurposeInvitationRecipientDisplayName: {},
}

// Config contains decoded key material and a non-secret positive key rotation
// version. KeyVersion maps directly to the Calendar PostgreSQL smallint column.
// The key must come from a secret store or process environment, never source code.
type Config struct {
	Key        []byte
	KeyVersion int16
}

// Context defines the authenticated-data binding for one persisted value. RecordID
// should be the stable UUID of the invitation revision, recipient, or other record
// that owns the value; it must not be a human-readable title or an email address.
type Context struct {
	TenantID string
	Purpose  Purpose
	RecordID string
}

// SealedValue is the database-friendly envelope. KeyVersion belongs in a separate
// positive smallint column alongside Ciphertext, matching the Calendar migration's
// rotation boundary.
type SealedValue struct {
	KeyVersion int16
	Ciphertext []byte
}

// Protector provides authenticated encryption using a Calendar-only derived key.
// It is safe for concurrent use after construction.
type Protector struct {
	aead           cipher.AEAD
	keyVersion     int16
	fingerprintKey [sha256.Size]byte
	random         io.Reader
}

// New creates a production protector. It fails closed when the key/version is
// absent or invalid; it never generates or falls back to a process-local key.
func New(config Config) (*Protector, error) {
	return newProtector(config, rand.Reader)
}

func newProtector(config Config, random io.Reader) (*Protector, error) {
	if len(config.Key) < minimumKeyBytes {
		return nil, fmt.Errorf("%w: key must contain at least %d bytes", ErrInvalidConfig, minimumKeyBytes)
	}
	if err := ValidateKeyVersion(config.KeyVersion); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if random == nil {
		return nil, fmt.Errorf("%w: secure random source is required", ErrInvalidConfig)
	}

	derivedKey := deriveKey(config.Key)
	fingerprintKey := deriveRecipientFingerprintKey(config.Key)
	block, err := aes.NewCipher(derivedKey[:])
	if err != nil {
		return nil, fmt.Errorf("%w: create AES cipher", ErrInvalidConfig)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: create AES-GCM", ErrInvalidConfig)
	}

	return &Protector{
		aead:           aead,
		keyVersion:     config.KeyVersion,
		fingerprintKey: fingerprintKey,
		random:         random,
	}, nil
}

// KeyVersion returns the non-secret positive smallint version to persist with
// each sealed value.
func (protector *Protector) KeyVersion() int16 {
	if protector == nil {
		return 0
	}

	return protector.keyVersion
}

// Seal encrypts plaintext and returns an envelope bound to context. Ciphertext
// includes only the format marker and nonce; the key version remains explicit in
// SealedValue so callers can enforce a rotation policy before decrypting.
func (protector *Protector) Seal(context Context, plaintext []byte) (SealedValue, error) {
	if protector == nil || protector.aead == nil || protector.random == nil {
		return SealedValue{}, ErrInvalidConfig
	}

	associatedData, err := context.associatedData(protector.keyVersion)
	if err != nil {
		return SealedValue{}, err
	}

	nonce := make([]byte, protector.aead.NonceSize())
	if _, err := io.ReadFull(protector.random, nonce); err != nil {
		return SealedValue{}, fmt.Errorf("generate calendar protected-data nonce: %w", err)
	}

	ciphertext := make([]byte, 1, 1+len(nonce)+len(plaintext)+protector.aead.Overhead())
	ciphertext[0] = ciphertextFormat
	ciphertext = append(ciphertext, nonce...)
	ciphertext = protector.aead.Seal(ciphertext, nonce, plaintext, associatedData)

	return SealedValue{
		KeyVersion: protector.keyVersion,
		Ciphertext: ciphertext,
	}, nil
}

// Open verifies the persisted key version and context before returning plaintext.
// The method deliberately returns generic errors for authentication failures so
// callers cannot log or disclose cryptographic details.
func (protector *Protector) Open(context Context, sealed SealedValue) ([]byte, error) {
	if protector == nil || protector.aead == nil {
		return nil, ErrInvalidConfig
	}
	if sealed.KeyVersion != protector.keyVersion {
		return nil, ErrKeyVersionMismatch
	}

	associatedData, err := context.associatedData(protector.keyVersion)
	if err != nil {
		return nil, err
	}

	minimumLength := 1 + protector.aead.NonceSize() + protector.aead.Overhead()
	if len(sealed.Ciphertext) < minimumLength || sealed.Ciphertext[0] != ciphertextFormat {
		return nil, ErrMalformedCiphertext
	}

	nonceEnd := 1 + protector.aead.NonceSize()
	plaintext, err := protector.aead.Open(
		nil,
		sealed.Ciphertext[1:nonceEnd],
		sealed.Ciphertext[nonceEnd:],
		associatedData,
	)
	if err != nil {
		return nil, ErrAuthenticationFailed
	}

	return plaintext, nil
}

// DeliveryAddressFingerprint returns a deterministic 32-byte HMAC for a
// previously normalized recipient delivery address. It is tenant-bound so the
// same address in another tenant does not produce the same database lookup key.
//
// The fingerprint intentionally does not include RecordID: it is used for
// recipient deduplication across invitation revisions. It is derived from a
// Calendar-only subkey and a separate HMAC domain, never an unkeyed email hash.
// Callers must keep it out of logs, metrics labels, and client responses.
func (protector *Protector) DeliveryAddressFingerprint(
	tenantID string,
	deliveryAddress []byte,
) ([sha256.Size]byte, error) {
	var fingerprint [sha256.Size]byte
	if protector == nil || protector.aead == nil {
		return fingerprint, ErrInvalidConfig
	}
	if err := validateContextBinding(tenantID); err != nil {
		return fingerprint, ErrInvalidContext
	}
	if len(deliveryAddress) == 0 || len(deliveryAddress) > maxDeliveryAddressBytes {
		return fingerprint, ErrInvalidDeliveryAddress
	}

	mac := hmac.New(sha256.New, protector.fingerprintKey[:])
	_, _ = mac.Write(lengthDelimitedBytes(
		[]byte(recipientFingerprintDomain),
		[]byte(tenantID),
		deliveryAddress,
	))
	copy(fingerprint[:], mac.Sum(nil))

	return fingerprint, nil
}

// ValidateKeyVersion rejects the zero value and negative values. Its int16 type
// ensures the valid range also matches PostgreSQL smallint. The version is
// non-secret, but it is authenticated as a canonical decimal value in the AAD.
func ValidateKeyVersion(value int16) error {
	if value <= 0 {
		return errors.New("key version must be a positive smallint")
	}

	return nil
}

func (context Context) associatedData(keyVersion int16) ([]byte, error) {
	if _, ok := supportedPurposes[context.Purpose]; !ok {
		return nil, ErrInvalidContext
	}
	if err := validateContextBinding(context.TenantID); err != nil {
		return nil, ErrInvalidContext
	}
	if err := validateContextBinding(context.RecordID); err != nil {
		return nil, ErrInvalidContext
	}
	if err := ValidateKeyVersion(keyVersion); err != nil {
		return nil, ErrInvalidContext
	}

	return lengthDelimitedAAD(
		aadDomain,
		strconv.FormatInt(int64(keyVersion), 10),
		string(context.Purpose),
		context.TenantID,
		context.RecordID,
	), nil
}

func validateContextBinding(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maxContextBindingSize || !utf8.ValidString(value) {
		return ErrInvalidContext
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return ErrInvalidContext
		}
	}

	return nil
}

func deriveKey(key []byte) [sha256.Size]byte {
	return deriveKeyWithDomain("tutorhub.calendar.protected-data.key.v1", key)
}

func deriveRecipientFingerprintKey(key []byte) [sha256.Size]byte {
	return deriveKeyWithDomain("tutorhub.calendar.recipient-fingerprint.key.v1", key)
}

func deriveKeyWithDomain(domain string, key []byte) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(key)

	var derived [sha256.Size]byte
	copy(derived[:], hash.Sum(nil))
	return derived
}

func lengthDelimitedAAD(values ...string) []byte {
	fields := make([][]byte, 0, len(values))
	for _, value := range values {
		fields = append(fields, []byte(value))
	}

	return lengthDelimitedBytes(fields...)
}

func lengthDelimitedBytes(values ...[]byte) []byte {
	var buffer bytes.Buffer
	for _, value := range values {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = buffer.Write(length[:])
		_, _ = buffer.Write(value)
	}

	return buffer.Bytes()
}
