package protecteddata

import (
	"bytes"
	"errors"
	"testing"
)

func TestProtectorRoundTripUsesBoundContextAndKeyVersion(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x42}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}

	context := Context{
		TenantID: "a0bc4c50-a890-42b0-9eaa-6e3c4e36104f",
		Purpose:  PurposeInvitationRecipientAddress,
		RecordID: "06eb8d82-0f6a-426c-a45f-dd6eb9c8f878",
	}
	plaintext := []byte("learner@example.test")

	sealed, err := protector.Seal(context, plaintext)
	if err != nil {
		t.Fatalf("seal value: %v", err)
	}
	if sealed.KeyVersion != protector.KeyVersion() {
		t.Fatalf("unexpected sealed key version: %d", sealed.KeyVersion)
	}
	if len(sealed.Ciphertext) == 0 || bytes.Equal(sealed.Ciphertext, plaintext) {
		t.Fatal("expected a non-plaintext ciphertext envelope")
	}

	decrypted, err := protector.Open(context, sealed)
	if err != nil {
		t.Fatalf("open value: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("decrypted value does not match the original")
	}
}

func TestProtectorRandomizesNonce(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x23}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	context := validContext()

	first, err := protector.Seal(context, []byte("same private value"))
	if err != nil {
		t.Fatalf("seal first value: %v", err)
	}
	second, err := protector.Seal(context, []byte("same private value"))
	if err != nil {
		t.Fatalf("seal second value: %v", err)
	}
	if bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("expected distinct ciphertexts for distinct nonces")
	}
}

func TestProtectorSupportsSingleByteDisplayNameEnvelope(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x31}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	context := validContext()
	context.Purpose = PurposeInvitationRecipientDisplayName

	sealed, err := protector.Seal(context, []byte("A"))
	if err != nil {
		t.Fatalf("seal short display name: %v", err)
	}
	if len(sealed.Ciphertext) != 30 {
		t.Fatalf("single-byte display-name envelope length=%d, want 30", len(sealed.Ciphertext))
	}
	plaintext, err := protector.Open(context, sealed)
	if err != nil {
		t.Fatalf("open short display name: %v", err)
	}
	if string(plaintext) != "A" {
		t.Fatalf("short display-name round trip=%q", plaintext)
	}
}

func TestProtectorRejectsWrongAADVersionAndTampering(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x6a}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	context := validContext()
	sealed, err := protector.Seal(context, []byte("private calendar value"))
	if err != nil {
		t.Fatalf("seal value: %v", err)
	}

	wrongPurpose := context
	wrongPurpose.Purpose = PurposeInvitationRecipientDisplayName
	if _, err := protector.Open(wrongPurpose, sealed); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("expected context authentication failure, got %v", err)
	}

	wrongVersion := sealed
	wrongVersion.KeyVersion = 2
	if _, err := protector.Open(context, wrongVersion); !errors.Is(err, ErrKeyVersionMismatch) {
		t.Fatalf("expected key version mismatch, got %v", err)
	}

	versionTwoProtector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x6a}, minimumKeyBytes),
		KeyVersion: 2,
	})
	if err != nil {
		t.Fatalf("create second protector: %v", err)
	}
	if _, err := versionTwoProtector.Open(context, wrongVersion); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("expected decimal key version AAD authentication failure, got %v", err)
	}

	tampered := sealed
	tampered.Ciphertext = append([]byte(nil), sealed.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0x01
	if _, err := protector.Open(context, tampered); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("expected tamper authentication failure, got %v", err)
	}
}

func TestProtectorRejectsUnsafeConfigurationAndContext(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{Key: bytes.Repeat([]byte{0x01}, minimumKeyBytes-1), KeyVersion: 1},
		{Key: bytes.Repeat([]byte{0x01}, minimumKeyBytes), KeyVersion: 0},
		{Key: bytes.Repeat([]byte{0x01}, minimumKeyBytes), KeyVersion: -1},
	} {
		if _, err := New(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected invalid config for %d, got %v", config.KeyVersion, err)
		}
	}

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x11}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}

	for _, context := range []Context{
		{TenantID: "", Purpose: PurposeInvitationCanonicalPayload, RecordID: "revision-1"},
		{TenantID: "tenant-1", Purpose: "unbounded-purpose", RecordID: "revision-1"},
		{TenantID: "tenant-1", Purpose: PurposeInvitationCanonicalPayload, RecordID: "record\n1"},
	} {
		if _, err := protector.Seal(context, []byte("value")); !errors.Is(err, ErrInvalidContext) {
			t.Fatalf("expected invalid context for %+v, got %v", context, err)
		}
	}
}

func TestProtectorRejectsMalformedCiphertext(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x77}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}

	if _, err := protector.Open(validContext(), SealedValue{
		KeyVersion: protector.KeyVersion(),
		Ciphertext: []byte{ciphertextFormat},
	}); !errors.Is(err, ErrMalformedCiphertext) {
		t.Fatalf("expected malformed ciphertext error, got %v", err)
	}
}

func TestDeliveryAddressFingerprintIsDeterministicAndTenantBound(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x18}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}

	address := []byte("learner@example.test")
	first, err := protector.DeliveryAddressFingerprint("tenant-a", address)
	if err != nil {
		t.Fatalf("fingerprint first value: %v", err)
	}
	second, err := protector.DeliveryAddressFingerprint("tenant-a", address)
	if err != nil {
		t.Fatalf("fingerprint second value: %v", err)
	}
	if !bytes.Equal(first[:], second[:]) {
		t.Fatal("expected a deterministic recipient fingerprint")
	}

	otherTenant, err := protector.DeliveryAddressFingerprint("tenant-b", address)
	if err != nil {
		t.Fatalf("fingerprint tenant-bound value: %v", err)
	}
	if bytes.Equal(first[:], otherTenant[:]) {
		t.Fatal("recipient fingerprint must be tenant-bound")
	}

	otherAddress, err := protector.DeliveryAddressFingerprint("tenant-a", []byte("other@example.test"))
	if err != nil {
		t.Fatalf("fingerprint distinct address: %v", err)
	}
	if bytes.Equal(first[:], otherAddress[:]) {
		t.Fatal("recipient fingerprint must be address-sensitive")
	}
}

func TestDeliveryAddressFingerprintRejectsUnboundedOrInvalidContext(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x93}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}

	for _, input := range []struct {
		tenantID string
		address  []byte
		expected error
	}{
		{tenantID: "", address: []byte("learner@example.test"), expected: ErrInvalidContext},
		{tenantID: "tenant-a", address: nil, expected: ErrInvalidDeliveryAddress},
		{tenantID: "tenant-a", address: bytes.Repeat([]byte{'a'}, maxDeliveryAddressBytes+1), expected: ErrInvalidDeliveryAddress},
	} {
		if _, err := protector.DeliveryAddressFingerprint(input.tenantID, input.address); !errors.Is(err, input.expected) {
			t.Fatalf("expected fingerprint validation error %v, got %v", input.expected, err)
		}
	}
}

func TestRSVPCapabilityTokenDigestIsDeterministicKeyedAndVersioned(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x57}, minimumKeyBytes)
	protector, err := New(Config{Key: key, KeyVersion: 1})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	token := bytes.Repeat([]byte{0xa3}, 32)
	first, err := protector.RSVPCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest capability token: %v", err)
	}
	second, err := protector.RSVPCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest capability token again: %v", err)
	}
	if !bytes.Equal(first[:], second[:]) {
		t.Fatal("capability digest must be deterministic")
	}
	if bytes.Equal(first[:], token) {
		t.Fatal("capability digest must not persist the raw token")
	}

	otherToken, err := protector.RSVPCapabilityTokenDigest(bytes.Repeat([]byte{0xa4}, 32))
	if err != nil {
		t.Fatalf("digest different capability token: %v", err)
	}
	if bytes.Equal(first[:], otherToken[:]) {
		t.Fatal("capability digest must be token-sensitive")
	}

	versionTwo, err := New(Config{Key: key, KeyVersion: 2})
	if err != nil {
		t.Fatalf("create rotated protector: %v", err)
	}
	rotated, err := versionTwo.RSVPCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest capability token with rotated key version: %v", err)
	}
	if bytes.Equal(first[:], rotated[:]) {
		t.Fatal("capability digest must be key-version-sensitive")
	}
}

func TestRSVPCapabilityTokenDigestRejectsUnsafeTokenLength(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x25}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	for _, token := range [][]byte{
		nil,
		bytes.Repeat([]byte{0x01}, 15),
		bytes.Repeat([]byte{0x01}, 129),
	} {
		if _, err := protector.RSVPCapabilityTokenDigest(token); !errors.Is(err, ErrInvalidContext) {
			t.Fatalf("token length %d: expected invalid context, got %v", len(token), err)
		}
	}
}

func TestPollCapabilityTokenDigestIsDeterministicKeyedVersionedAndDomainSeparated(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x58}, minimumKeyBytes)
	protector, err := New(Config{Key: key, KeyVersion: 1})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	token := bytes.Repeat([]byte{0xb3}, 32)
	first, err := protector.PollCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest poll capability token: %v", err)
	}
	second, err := protector.PollCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest poll capability token again: %v", err)
	}
	if !bytes.Equal(first[:], second[:]) {
		t.Fatal("poll capability digest must be deterministic")
	}
	if bytes.Equal(first[:], token) {
		t.Fatal("poll capability digest must not persist the raw token")
	}

	otherToken, err := protector.PollCapabilityTokenDigest(bytes.Repeat([]byte{0xb4}, 32))
	if err != nil {
		t.Fatalf("digest different poll capability token: %v", err)
	}
	if bytes.Equal(first[:], otherToken[:]) {
		t.Fatal("poll capability digest must be token-sensitive")
	}

	otherKeyProtector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x59}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector with a different key: %v", err)
	}
	otherKey, err := otherKeyProtector.PollCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest poll capability token with a different key: %v", err)
	}
	if bytes.Equal(first[:], otherKey[:]) {
		t.Fatal("poll capability digest must be key-sensitive")
	}

	versionTwo, err := New(Config{Key: key, KeyVersion: 2})
	if err != nil {
		t.Fatalf("create rotated protector: %v", err)
	}
	rotated, err := versionTwo.PollCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest poll capability token with rotated key version: %v", err)
	}
	if bytes.Equal(first[:], rotated[:]) {
		t.Fatal("poll capability digest must be key-version-sensitive")
	}

	rsvp, err := protector.RSVPCapabilityTokenDigest(token)
	if err != nil {
		t.Fatalf("digest RSVP capability token: %v", err)
	}
	if bytes.Equal(first[:], rsvp[:]) {
		t.Fatal("poll and RSVP capability digests must be domain-separated")
	}
}

func TestPollCapabilityTokenDigestRejectsUnsafeTokenLengthAndInvalidProtector(t *testing.T) {
	t.Parallel()

	protector, err := New(Config{
		Key:        bytes.Repeat([]byte{0x26}, minimumKeyBytes),
		KeyVersion: 1,
	})
	if err != nil {
		t.Fatalf("create protector: %v", err)
	}
	for _, token := range [][]byte{
		nil,
		bytes.Repeat([]byte{0x01}, 15),
		bytes.Repeat([]byte{0x01}, 129),
	} {
		if _, err := protector.PollCapabilityTokenDigest(token); !errors.Is(err, ErrInvalidContext) {
			t.Fatalf("token length %d: expected invalid context, got %v", len(token), err)
		}
	}

	var nilProtector *Protector
	if _, err := nilProtector.PollCapabilityTokenDigest(bytes.Repeat([]byte{0x01}, 32)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config for a nil protector, got %v", err)
	}
}

func validContext() Context {
	return Context{
		TenantID: "a0bc4c50-a890-42b0-9eaa-6e3c4e36104f",
		Purpose:  PurposeInvitationCanonicalPayload,
		RecordID: "06eb8d82-0f6a-426c-a45f-dd6eb9c8f878",
	}
}
