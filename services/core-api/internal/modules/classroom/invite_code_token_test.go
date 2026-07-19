package classroom

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

type recordingInviteTokenCodec struct {
	randomValue string
	randomErr   error
	digest      []byte
	purpose     string
	value       string
}

func (codec *recordingInviteTokenCodec) RandomToken() (string, error) {
	return codec.randomValue, codec.randomErr
}

func (codec *recordingInviteTokenCodec) Digest(purpose string, value string) []byte {
	codec.purpose = purpose
	codec.value = value
	return codec.digest
}

func TestClassInviteCodeTokenGenerationUsesCanonicalPurposeBoundValue(t *testing.T) {
	randomValue := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, 32))
	digest := bytes.Repeat([]byte{0x7b}, 32)
	codec := &recordingInviteTokenCodec{randomValue: randomValue, digest: digest}

	token, hash, err := generateClassInviteCodeToken(codec)
	if err != nil {
		t.Fatalf("generate class invite token: %v", err)
	}
	wantToken := classInviteCodeTokenPrefix + randomValue
	if token != wantToken || codec.value != wantToken ||
		codec.purpose != classInviteCodeTokenPurpose || !bytes.Equal(hash, digest) {
		t.Fatalf(
			"unexpected token binding: token=%q purpose=%q value=%q hash=%x",
			token,
			codec.purpose,
			codec.value,
			hash,
		)
	}
	hash[0] ^= 0xff
	if hash[0] == digest[0] {
		t.Fatal("returned hash must not alias the codec-owned digest")
	}

	digested, err := digestClassInviteCodeToken(codec, "  "+wantToken+"  ")
	if err != nil || !bytes.Equal(digested, digest) || codec.value != wantToken {
		t.Fatalf("digest normalized class invite token: hash=%x error=%v", digested, err)
	}
}

func TestClassInviteCodeTokenRejectsMalformedOrNonCanonicalValues(t *testing.T) {
	validRandom := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x21}, 32))
	tests := []string{
		"",
		validRandom,
		classInviteCodeTokenPrefix + "short",
		classInviteCodeTokenPrefix + validRandom + "=",
		classInviteCodeTokenPrefix + validRandom + "/",
		"thciv2_" + validRandom,
	}
	codec := &recordingInviteTokenCodec{digest: bytes.Repeat([]byte{0x11}, 32)}
	for _, value := range tests {
		if _, err := normalizeClassInviteCodeToken(value); !errors.Is(
			err,
			ErrClassInviteCodeUnavailable,
		) {
			t.Errorf("normalize %q returned %v", value, err)
		}
		if _, err := digestClassInviteCodeToken(codec, value); !errors.Is(
			err,
			ErrClassInviteCodeUnavailable,
		) {
			t.Errorf("digest %q returned %v", value, err)
		}
	}
}

func TestClassInviteCodeTokenRejectsInvalidCodecOutput(t *testing.T) {
	validRandom := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x34}, 32))
	tests := []struct {
		name  string
		codec *recordingInviteTokenCodec
	}{
		{
			name: "random error",
			codec: &recordingInviteTokenCodec{
				randomErr: errors.New("entropy unavailable"),
				digest:    bytes.Repeat([]byte{0x01}, 32),
			},
		},
		{
			name: "short random value",
			codec: &recordingInviteTokenCodec{
				randomValue: "short",
				digest:      bytes.Repeat([]byte{0x02}, 32),
			},
		},
		{
			name: "wrong digest length",
			codec: &recordingInviteTokenCodec{
				randomValue: validRandom,
				digest:      bytes.Repeat([]byte{0x03}, 31),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := generateClassInviteCodeToken(test.codec); err == nil {
				t.Fatal("expected invalid codec output to fail")
			}
		})
	}
}
