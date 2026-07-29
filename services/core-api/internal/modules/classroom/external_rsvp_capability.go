package classroom

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
)

const (
	externalCapabilitySecretBytes = 32
	externalCapabilityMaxLifetime = 90 * 24 * time.Hour
	externalCapabilityEventGrace  = 7 * 24 * time.Hour
)

var (
	// ErrExternalRSVPCapabilityUnavailable deliberately collapses unknown,
	// malformed, expired, revoked, superseded and closed capabilities so a
	// public caller cannot enumerate invitations or recipients.
	ErrExternalRSVPCapabilityUnavailable = errors.New("external RSVP capability unavailable")
	ErrExternalRSVPVersionConflict       = errors.New("external RSVP attendee version is stale")
)

// ExternalRSVPCapabilityPurpose is a closed purpose set. Resolve can only read
// the minimum RSVP projection; Respond can mutate exactly one bound attendee.
type ExternalRSVPCapabilityPurpose string

const (
	ExternalRSVPCapabilityResolve ExternalRSVPCapabilityPurpose = "resolve"
	ExternalRSVPCapabilityRespond ExternalRSVPCapabilityPurpose = "respond"
)

// ExternalRSVPCapabilityToken is returned exactly once to the trusted caller
// that constructs the fragment-only CTA. It must never be logged or persisted.
type ExternalRSVPCapabilityToken struct {
	Raw       string
	Version   int16
	Digest    [sha256.Size]byte
	ExpiresAt time.Time
}

// ExternalRSVPCapabilityIssue binds a token to one immutable invitation
// recipient revision. EventEndsAt is used only to enforce the ADR-0020 expiry
// ceiling: event end + seven days, at most ninety days after issue.
type ExternalRSVPCapabilityIssue struct {
	InvitationRevisionID  uuid.UUID
	InvitationRecipientID uuid.UUID
	Purpose               ExternalRSVPCapabilityPurpose
	EventEndsAt           time.Time
	IssuedAt              time.Time
}

// ExternalRSVPProjection is the maximum public response returned after a
// successful capability exchange. It intentionally contains no tenant, class,
// roster, guest list, recipient address, organizer address or join secret.
type ExternalRSVPProjection struct {
	Title               string
	StartsAt            time.Time
	EndsAt              time.Time
	Timezone            string
	RSVPState           RSVPState
	ResponseRequested   bool
	AttendeeVersion     int64
	InvitationSequence  int64
	CapabilityExpiresAt time.Time
}

type ExternalRSVPResponseInput struct {
	RawToken                string
	State                   RSVPState
	Note                    string
	ExpectedAttendeeVersion int64
	IdempotencyKey          string
}

type externalRSVPResponseParams struct {
	RawToken                string
	State                   RSVPState
	Note                    string
	ExpectedAttendeeVersion int64
	IdempotencyKey          string
	Fingerprint             string
}

func (input ExternalRSVPResponseInput) normalized() (externalRSVPResponseParams, error) {
	key, err := normalizeParticipationIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return externalRSVPResponseParams{}, err
	}
	if input.ExpectedAttendeeVersion < 1 {
		return externalRSVPResponseParams{}, fmt.Errorf(
			"%w: expected attendee version is required",
			ErrInvalidParticipationInput,
		)
	}
	switch input.State {
	case RSVPStateAccepted, RSVPStateTentative, RSVPStateDeclined:
	default:
		return externalRSVPResponseParams{}, fmt.Errorf(
			"%w: unsupported RSVP state",
			ErrInvalidParticipationInput,
		)
	}
	note := strings.TrimSpace(input.Note)
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > maximumRSVPResponseNoteLength {
		return externalRSVPResponseParams{}, fmt.Errorf(
			"%w: RSVP note cannot exceed %d characters",
			ErrInvalidParticipationInput,
			maximumRSVPResponseNoteLength,
		)
	}
	if strings.TrimSpace(input.RawToken) != input.RawToken || input.RawToken == "" {
		return externalRSVPResponseParams{}, ErrExternalRSVPCapabilityUnavailable
	}
	params := externalRSVPResponseParams{
		RawToken:                input.RawToken,
		State:                   input.State,
		Note:                    note,
		ExpectedAttendeeVersion: input.ExpectedAttendeeVersion,
		IdempotencyKey:          key,
	}
	params.Fingerprint, err = fingerprintParticipationInput("external_rsvp_respond", struct {
		State                   RSVPState `json:"state"`
		Note                    string    `json:"note"`
		ExpectedAttendeeVersion int64     `json:"expected_attendee_version"`
	}{
		State:                   params.State,
		Note:                    params.Note,
		ExpectedAttendeeVersion: params.ExpectedAttendeeVersion,
	})
	if err != nil {
		return externalRSVPResponseParams{}, err
	}
	return params, nil
}

func (issue ExternalRSVPCapabilityIssue) validate() (time.Time, error) {
	if issue.InvitationRevisionID == uuid.Nil || issue.InvitationRecipientID == uuid.Nil ||
		issue.IssuedAt.IsZero() || issue.EventEndsAt.IsZero() {
		return time.Time{}, ErrExternalRSVPCapabilityUnavailable
	}
	switch issue.Purpose {
	case ExternalRSVPCapabilityResolve, ExternalRSVPCapabilityRespond:
	default:
		return time.Time{}, ErrExternalRSVPCapabilityUnavailable
	}
	issuedAt := issue.IssuedAt.UTC()
	expiresAt := issue.EventEndsAt.UTC().Add(externalCapabilityEventGrace)
	maxExpiry := issuedAt.Add(externalCapabilityMaxLifetime)
	if expiresAt.After(maxExpiry) {
		expiresAt = maxExpiry
	}
	if !expiresAt.After(issuedAt) {
		return time.Time{}, ErrExternalRSVPCapabilityUnavailable
	}
	return expiresAt, nil
}

func newExternalRSVPCapabilityToken(
	protector *protecteddata.Protector,
	expiresAt time.Time,
	random io.Reader,
) (ExternalRSVPCapabilityToken, error) {
	if protector == nil || random == nil || expiresAt.IsZero() {
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}
	secret := make([]byte, externalCapabilitySecretBytes)
	if _, err := io.ReadFull(random, secret); err != nil {
		return ExternalRSVPCapabilityToken{}, fmt.Errorf("generate RSVP capability: %w", err)
	}
	digest, err := protector.RSVPCapabilityTokenDigest(secret)
	if err != nil {
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}
	version := protector.KeyVersion()
	raw := "v" + strconv.FormatInt(int64(version), 10) + "." +
		base64.RawURLEncoding.EncodeToString(secret)
	return ExternalRSVPCapabilityToken{
		Raw:       raw,
		Version:   version,
		Digest:    digest,
		ExpiresAt: expiresAt.UTC(),
	}, nil
}

func generateExternalRSVPCapabilityToken(
	protector *protecteddata.Protector,
	expiresAt time.Time,
) (ExternalRSVPCapabilityToken, error) {
	return newExternalRSVPCapabilityToken(protector, expiresAt, rand.Reader)
}

func digestExternalRSVPCapabilityToken(
	protector *protecteddata.Protector,
	raw string,
) (int16, [sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if protector == nil || raw == "" || raw != strings.TrimSpace(raw) {
		return 0, empty, ErrExternalRSVPCapabilityUnavailable
	}
	prefix, encoded, found := strings.Cut(raw, ".")
	if !found || !strings.HasPrefix(prefix, "v") || strings.Contains(encoded, ".") {
		return 0, empty, ErrExternalRSVPCapabilityUnavailable
	}
	versionValue, err := strconv.ParseInt(strings.TrimPrefix(prefix, "v"), 10, 16)
	if err != nil || versionValue <= 0 || int16(versionValue) != protector.KeyVersion() {
		return 0, empty, ErrExternalRSVPCapabilityUnavailable
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(secret) != externalCapabilitySecretBytes {
		return 0, empty, ErrExternalRSVPCapabilityUnavailable
	}
	digest, err := protector.RSVPCapabilityTokenDigest(secret)
	if err != nil {
		return 0, empty, ErrExternalRSVPCapabilityUnavailable
	}
	return int16(versionValue), digest, nil
}
