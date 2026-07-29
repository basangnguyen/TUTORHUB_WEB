package classroom

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
)

func TestExternalRSVPCapabilityTokenRoundTripAndRotationBoundary(t *testing.T) {
	t.Parallel()

	protector := testExternalCapabilityProtector(t, 3)
	expiresAt := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	token, err := newExternalRSVPCapabilityToken(
		protector,
		expiresAt,
		bytes.NewReader(bytes.Repeat([]byte{0x7a}, externalCapabilitySecretBytes)),
	)
	if err != nil {
		t.Fatalf("generate capability: %v", err)
	}
	if strings.Contains(token.Raw, "+") || strings.Contains(token.Raw, "/") ||
		strings.Contains(token.Raw, "=") {
		t.Fatalf("capability is not raw URL-safe base64: %q", token.Raw)
	}
	version, digest, err := digestExternalRSVPCapabilityToken(protector, token.Raw)
	if err != nil {
		t.Fatalf("digest generated capability: %v", err)
	}
	if version != 3 || !bytes.Equal(digest[:], token.Digest[:]) {
		t.Fatalf("capability round trip mismatch: version=%d", version)
	}

	rotated := testExternalCapabilityProtector(t, 4)
	if _, _, err := digestExternalRSVPCapabilityToken(rotated, token.Raw); !errors.Is(
		err,
		ErrExternalRSVPCapabilityUnavailable,
	) {
		t.Fatalf("rotated key version should fail closed, got %v", err)
	}
}

func TestExternalRSVPCapabilityRejectsMalformedTokenWithoutDetail(t *testing.T) {
	t.Parallel()

	protector := testExternalCapabilityProtector(t, 1)
	for _, raw := range []string{
		"",
		" v1.not-a-token",
		"v0.not-a-token",
		"v1.not-a-token",
		"v1.YWJjZA",
		"v1." + strings.Repeat("a", 43) + ".extra",
	} {
		if _, _, err := digestExternalRSVPCapabilityToken(protector, raw); !errors.Is(
			err,
			ErrExternalRSVPCapabilityUnavailable,
		) {
			t.Fatalf("raw token %q: expected generic unavailable, got %v", raw, err)
		}
	}
}

func TestExternalRSVPCapabilityExpiryUsesEventGraceAndNinetyDayCeiling(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	base := ExternalRSVPCapabilityIssue{
		InvitationRevisionID:  uuid.New(),
		InvitationRecipientID: uuid.New(),
		Purpose:               ExternalRSVPCapabilityRespond,
		IssuedAt:              issuedAt,
	}

	base.EventEndsAt = issuedAt.Add(2 * time.Hour)
	expiresAt, err := base.validate()
	if err != nil {
		t.Fatalf("validate normal issue: %v", err)
	}
	want := base.EventEndsAt.Add(externalCapabilityEventGrace)
	if !expiresAt.Equal(want) {
		t.Fatalf("expiry=%s, want %s", expiresAt, want)
	}

	base.EventEndsAt = issuedAt.Add(365 * 24 * time.Hour)
	expiresAt, err = base.validate()
	if err != nil {
		t.Fatalf("validate long event horizon: %v", err)
	}
	want = issuedAt.Add(externalCapabilityMaxLifetime)
	if !expiresAt.Equal(want) {
		t.Fatalf("capped expiry=%s, want %s", expiresAt, want)
	}

	base.EventEndsAt = issuedAt.Add(-externalCapabilityEventGrace)
	if _, err := base.validate(); !errors.Is(err, ErrExternalRSVPCapabilityUnavailable) {
		t.Fatalf("expired issue should fail closed, got %v", err)
	}
}

func TestExternalRSVPResponseNormalizationKeepsTokenOutOfFingerprint(t *testing.T) {
	t.Parallel()

	input := ExternalRSVPResponseInput{
		RawToken:                "v1." + strings.Repeat("a", 43),
		State:                   RSVPStateAccepted,
		Note:                    "  Có mặt  ",
		ExpectedAttendeeVersion: 2,
		IdempotencyKey:          "external-rsvp-key-0001",
	}
	params, err := input.normalized()
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if params.Note != "Có mặt" || params.Fingerprint == "" {
		t.Fatalf("unexpected normalized response: %+v", params)
	}
	if strings.Contains(params.Fingerprint, input.RawToken) {
		t.Fatal("request fingerprint must not contain the raw capability")
	}
}

func testExternalCapabilityProtector(t *testing.T, version int16) *protecteddata.Protector {
	t.Helper()
	protector, err := protecteddata.New(protecteddata.Config{
		Key:        bytes.Repeat([]byte{0x39}, 32),
		KeyVersion: version,
	})
	if err != nil {
		t.Fatalf("create protected-data protector: %v", err)
	}
	return protector
}
