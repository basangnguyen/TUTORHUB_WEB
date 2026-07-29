package classroom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maximumAudienceAttendees           = 128
	minimumParticipationIdempotencyKey = 16
	maximumParticipationIdempotencyKey = 128
	maximumRSVPResponseNoteLength      = 500
)

var ErrInvalidParticipationInput = errors.New("invalid class session participation input")

// ParticipationSourceKind identifies the authoritative classroom source whose
// audience is being read or mutated. A recurring occurrence is never addressed
// by a materialized session id: its stable identity is the owning series plus
// occurrence key.
type ParticipationSourceKind string

const (
	ParticipationSourceSession    ParticipationSourceKind = "session"
	ParticipationSourceSeries     ParticipationSourceKind = "series"
	ParticipationSourceOccurrence ParticipationSourceKind = "occurrence"
)

// ParticipationSourceRef is a tagged union:
//   - session: SessionID only;
//   - series: SeriesID only;
//   - occurrence: SeriesID and OccurrenceKey.
//
// Keeping the union in the source domain prevents transports from inventing
// ambiguous combinations or treating a recurrence occurrence as a one-time
// session.
type ParticipationSourceRef struct {
	Kind          ParticipationSourceKind
	SessionID     uuid.UUID
	SeriesID      uuid.UUID
	OccurrenceKey string
}

func SessionParticipationSource(sessionID uuid.UUID) ParticipationSourceRef {
	return ParticipationSourceRef{
		Kind:      ParticipationSourceSession,
		SessionID: sessionID,
	}
}

func SeriesParticipationSource(seriesID uuid.UUID) ParticipationSourceRef {
	return ParticipationSourceRef{
		Kind:     ParticipationSourceSeries,
		SeriesID: seriesID,
	}
}

func OccurrenceParticipationSource(
	seriesID uuid.UUID,
	occurrenceKey string,
) ParticipationSourceRef {
	return ParticipationSourceRef{
		Kind:          ParticipationSourceOccurrence,
		SeriesID:      seriesID,
		OccurrenceKey: occurrenceKey,
	}
}

// Validate rejects every non-canonical union shape. Call Normalized before
// validation when values originated at a text boundary.
func (source ParticipationSourceRef) Validate() error {
	switch source.Kind {
	case ParticipationSourceSession:
		if source.SessionID == uuid.Nil ||
			source.SeriesID != uuid.Nil ||
			source.OccurrenceKey != "" {
			return fmt.Errorf(
				"%w: session source requires only session id",
				ErrInvalidParticipationInput,
			)
		}
	case ParticipationSourceSeries:
		if source.SessionID != uuid.Nil ||
			source.SeriesID == uuid.Nil ||
			source.OccurrenceKey != "" {
			return fmt.Errorf(
				"%w: series source requires only series id",
				ErrInvalidParticipationInput,
			)
		}
	case ParticipationSourceOccurrence:
		if source.SessionID != uuid.Nil ||
			source.SeriesID == uuid.Nil ||
			source.OccurrenceKey != strings.TrimSpace(source.OccurrenceKey) ||
			utf8.RuneCountInString(source.OccurrenceKey) < 8 ||
			utf8.RuneCountInString(source.OccurrenceKey) > 128 ||
			!utf8.ValidString(source.OccurrenceKey) {
			return fmt.Errorf(
				"%w: occurrence source requires series id and an occurrence key between 8 and 128 characters",
				ErrInvalidParticipationInput,
			)
		}
	default:
		return fmt.Errorf(
			"%w: unsupported participation source kind %q",
			ErrInvalidParticipationInput,
			source.Kind,
		)
	}
	return nil
}

func (source ParticipationSourceRef) Normalized() (ParticipationSourceRef, error) {
	if err := source.Validate(); err != nil {
		return ParticipationSourceRef{}, err
	}
	return source, nil
}

// Fingerprint returns a stable, non-secret identity digest suitable for
// binding mutation fingerprints and idempotency receipts to exactly one
// source.
func (source ParticipationSourceRef) Fingerprint() (string, error) {
	normalized, err := source.Normalized()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Kind          ParticipationSourceKind `json:"kind"`
		SessionID     uuid.UUID               `json:"session_id,omitempty"`
		SeriesID      uuid.UUID               `json:"series_id,omitempty"`
		OccurrenceKey string                  `json:"occurrence_key,omitempty"`
	}{
		Kind:          normalized.Kind,
		SessionID:     normalized.SessionID,
		SeriesID:      normalized.SeriesID,
		OccurrenceKey: normalized.OccurrenceKey,
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint participation source: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ParticipationRole describes a recipient's scheduling importance. It is
// intentionally independent from the class business role, which is resolved
// authoritatively from the class roster by the repository layer.
type ParticipationRole string

const (
	ParticipationRoleRequired ParticipationRole = "required"
	ParticipationRoleOptional ParticipationRole = "optional"
)

// RSVPState is the TutorHub-owned response state persisted on an active
// attendee. needs_action is valid persisted state, but is not a self-response
// command value because it would erase a response without an explicit command.
type RSVPState string

const (
	RSVPStateNeedsAction RSVPState = "needs_action"
	RSVPStateAccepted    RSVPState = "accepted"
	RSVPStateTentative   RSVPState = "tentative"
	RSVPStateDeclined    RSVPState = "declined"
)

// InternalAudienceAttendeeInput is deliberately limited to an internal user
// identity and participation role. It accepts neither delivery addresses nor
// client-selected class roles, visibility, or guest permissions.
type InternalAudienceAttendeeInput struct {
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
}

// InternalAudienceAttendee is the canonical form used by the source-domain
// repository after authoritative roster validation.
type InternalAudienceAttendee struct {
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
}

// ExternalAudienceAttendeeInput is manual-only delivery identity. It never
// accepts a tenant/class role or guest permission from the client. Repository
// code protects the normalized address before persistence and rejects an
// address that already belongs to an active internal tenant member.
type ExternalAudienceAttendeeInput struct {
	Email             string
	DisplayName       string
	ParticipationRole ParticipationRole
	Locale            string
	ViewerTimezone    string
}

type ExternalAudienceAttendee struct {
	Email             string
	DisplayName       string
	ParticipationRole ParticipationRole
	Locale            string
	ViewerTimezone    string
}

// ReplaceAudienceInput is a full replacement of an event source's internal
// audience. expected_audience_revision may be zero for the first replacement,
// because new sources start at revision zero. The HTTP boundary must still
// require the client to explicitly provide that field.
type ReplaceAudienceInput struct {
	ExpectedAudienceRevision int64
	IdempotencyKey           string
	ResponseRequested        bool
	Attendees                []InternalAudienceAttendeeInput
	ExternalAttendees        []ExternalAudienceAttendeeInput
}

// AudienceReplacementParams is the deterministic, storage-ready subset of a
// replacement request. Fingerprint excludes the idempotency key because the
// receipt key scopes the comparison; it covers every business mutation input.
type AudienceReplacementParams struct {
	ExpectedAudienceRevision int64
	IdempotencyKey           string
	ResponseRequested        bool
	Attendees                []InternalAudienceAttendee
	ExternalAttendees        []ExternalAudienceAttendee
	Fingerprint              string
}

// SelfRSVPInput describes a participant's response for their own active
// attendee row. The actor identity and source scope are derived by the service,
// never supplied by this input.
type SelfRSVPInput struct {
	State                   RSVPState
	Note                    string
	ExpectedAttendeeVersion int64
	IdempotencyKey          string
}

// SelfRSVPParams is the canonical form used by the authoritative RSVP command.
type SelfRSVPParams struct {
	State                   RSVPState
	Note                    string
	ExpectedAttendeeVersion int64
	IdempotencyKey          string
	Fingerprint             string
}

func (input ReplaceAudienceInput) normalized() (AudienceReplacementParams, error) {
	key, err := normalizeParticipationIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	if input.ExpectedAudienceRevision < 0 {
		return AudienceReplacementParams{}, fmt.Errorf(
			"%w: expected audience revision cannot be negative",
			ErrInvalidParticipationInput,
		)
	}

	attendees, err := normalizeInternalAudienceAttendees(input.Attendees)
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	externalAttendees, err := normalizeExternalAudienceAttendees(input.ExternalAttendees)
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	if len(attendees)+len(externalAttendees) > maximumAudienceAttendees {
		return AudienceReplacementParams{}, fmt.Errorf(
			"%w: audience cannot exceed %d distinct attendees",
			ErrInvalidParticipationInput,
			maximumAudienceAttendees,
		)
	}
	params := AudienceReplacementParams{
		ExpectedAudienceRevision: input.ExpectedAudienceRevision,
		IdempotencyKey:           key,
		ResponseRequested:        input.ResponseRequested,
		Attendees:                attendees,
		ExternalAttendees:        externalAttendees,
	}
	params.Fingerprint, err = fingerprintAudienceReplacement(params)
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	return params, nil
}

func normalizeExternalAudienceAttendees(
	inputs []ExternalAudienceAttendeeInput,
) ([]ExternalAudienceAttendee, error) {
	byAddress := make(map[string]ExternalAudienceAttendee, len(inputs))
	for _, input := range inputs {
		email := strings.ToLower(strings.TrimSpace(input.Email))
		if len(email) < 3 || len(email) > 320 || strings.ContainsAny(email, "\r\n\t ") {
			return nil, fmt.Errorf("%w: invalid external attendee address", ErrInvalidParticipationInput)
		}
		for _, character := range email {
			if character > 0x7f {
				return nil, fmt.Errorf("%w: external attendee address must be ASCII", ErrInvalidParticipationInput)
			}
		}
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email || parsed.Name != "" {
			return nil, fmt.Errorf("%w: invalid external attendee address", ErrInvalidParticipationInput)
		}
		displayName := strings.TrimSpace(input.DisplayName)
		if !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) < 1 ||
			utf8.RuneCountInString(displayName) > 256 || strings.ContainsAny(displayName, "\r\n") {
			return nil, fmt.Errorf("%w: invalid external attendee display name", ErrInvalidParticipationInput)
		}
		if input.ParticipationRole != ParticipationRoleRequired &&
			input.ParticipationRole != ParticipationRoleOptional {
			return nil, fmt.Errorf("%w: unsupported external participation role", ErrInvalidParticipationInput)
		}
		locale := normalizeInvitationLocale(input.Locale)
		viewerTimezone := strings.TrimSpace(input.ViewerTimezone)
		if viewerTimezone == "" || strings.EqualFold(viewerTimezone, "local") {
			return nil, fmt.Errorf("%w: canonical viewer timezone is required", ErrInvalidParticipationInput)
		}
		if _, err := time.LoadLocation(viewerTimezone); err != nil {
			return nil, fmt.Errorf("%w: canonical viewer timezone is invalid", ErrInvalidParticipationInput)
		}
		attendee := ExternalAudienceAttendee{
			Email:             email,
			DisplayName:       displayName,
			ParticipationRole: input.ParticipationRole,
			Locale:            locale,
			ViewerTimezone:    viewerTimezone,
		}
		if current, exists := byAddress[email]; exists {
			if current != attendee {
				return nil, fmt.Errorf(
					"%w: duplicate external attendee has conflicting attributes",
					ErrInvalidParticipationInput,
				)
			}
			continue
		}
		byAddress[email] = attendee
	}
	attendees := make([]ExternalAudienceAttendee, 0, len(byAddress))
	for _, attendee := range byAddress {
		attendees = append(attendees, attendee)
	}
	sort.Slice(attendees, func(left int, right int) bool {
		return attendees[left].Email < attendees[right].Email
	})
	return attendees, nil
}

func (input SelfRSVPInput) normalized() (SelfRSVPParams, error) {
	key, err := normalizeParticipationIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return SelfRSVPParams{}, err
	}
	if input.ExpectedAttendeeVersion < 1 {
		return SelfRSVPParams{}, fmt.Errorf(
			"%w: expected attendee version is required",
			ErrInvalidParticipationInput,
		)
	}
	switch input.State {
	case RSVPStateAccepted, RSVPStateTentative, RSVPStateDeclined:
	default:
		return SelfRSVPParams{}, fmt.Errorf(
			"%w: unsupported self RSVP state %q",
			ErrInvalidParticipationInput,
			input.State,
		)
	}
	note := strings.TrimSpace(input.Note)
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > maximumRSVPResponseNoteLength {
		return SelfRSVPParams{}, fmt.Errorf(
			"%w: RSVP note cannot exceed %d characters",
			ErrInvalidParticipationInput,
			maximumRSVPResponseNoteLength,
		)
	}
	params := SelfRSVPParams{
		State:                   input.State,
		Note:                    note,
		ExpectedAttendeeVersion: input.ExpectedAttendeeVersion,
		IdempotencyKey:          key,
	}
	params.Fingerprint, err = fingerprintSelfRSVP(params)
	if err != nil {
		return SelfRSVPParams{}, err
	}
	return params, nil
}

func normalizeInternalAudienceAttendees(
	inputs []InternalAudienceAttendeeInput,
) ([]InternalAudienceAttendee, error) {
	byUserID := make(map[uuid.UUID]ParticipationRole, len(inputs))
	for _, input := range inputs {
		if input.UserID == uuid.Nil {
			return nil, fmt.Errorf(
				"%w: attendee user id is required",
				ErrInvalidParticipationInput,
			)
		}
		if input.ParticipationRole != ParticipationRoleRequired &&
			input.ParticipationRole != ParticipationRoleOptional {
			return nil, fmt.Errorf(
				"%w: unsupported attendee participation role %q",
				ErrInvalidParticipationInput,
				input.ParticipationRole,
			)
		}
		if current, exists := byUserID[input.UserID]; exists {
			if current != input.ParticipationRole {
				return nil, fmt.Errorf(
					"%w: duplicate attendee %s has conflicting participation roles",
					ErrInvalidParticipationInput,
					input.UserID,
				)
			}
			continue
		}
		byUserID[input.UserID] = input.ParticipationRole
	}
	if len(byUserID) > maximumAudienceAttendees {
		return nil, fmt.Errorf(
			"%w: audience cannot exceed %d distinct attendees",
			ErrInvalidParticipationInput,
			maximumAudienceAttendees,
		)
	}

	attendees := make([]InternalAudienceAttendee, 0, len(byUserID))
	for userID, role := range byUserID {
		attendees = append(attendees, InternalAudienceAttendee{
			UserID:            userID,
			ParticipationRole: role,
		})
	}
	sort.Slice(attendees, func(left int, right int) bool {
		return attendees[left].UserID.String() < attendees[right].UserID.String()
	})
	return attendees, nil
}

func normalizeParticipationIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if !utf8.ValidString(key) ||
		utf8.RuneCountInString(key) < minimumParticipationIdempotencyKey ||
		utf8.RuneCountInString(key) > maximumParticipationIdempotencyKey {
		return "", fmt.Errorf(
			"%w: idempotency key must contain between %d and %d characters",
			ErrInvalidParticipationInput,
			minimumParticipationIdempotencyKey,
			maximumParticipationIdempotencyKey,
		)
	}
	return key, nil
}

func fingerprintAudienceReplacement(params AudienceReplacementParams) (string, error) {
	payload := struct {
		ExpectedAudienceRevision int64                      `json:"expected_audience_revision"`
		ResponseRequested        bool                       `json:"response_requested"`
		Attendees                []InternalAudienceAttendee `json:"attendees"`
		ExternalAttendees        []ExternalAudienceAttendee `json:"external_attendees"`
	}{
		ExpectedAudienceRevision: params.ExpectedAudienceRevision,
		ResponseRequested:        params.ResponseRequested,
		Attendees:                params.Attendees,
		ExternalAttendees:        params.ExternalAttendees,
	}
	return fingerprintParticipationInput("audience_replace", payload)
}

func fingerprintSelfRSVP(params SelfRSVPParams) (string, error) {
	payload := struct {
		State                   RSVPState `json:"state"`
		Note                    string    `json:"note"`
		ExpectedAttendeeVersion int64     `json:"expected_attendee_version"`
	}{
		State:                   params.State,
		Note:                    params.Note,
		ExpectedAttendeeVersion: params.ExpectedAttendeeVersion,
	}
	return fingerprintParticipationInput("rsvp_respond", payload)
}

func fingerprintParticipationInput(operation string, payload any) (string, error) {
	encoded, err := json.Marshal(struct {
		Operation string `json:"operation"`
		Payload   any    `json:"payload"`
	}{
		Operation: operation,
		Payload:   payload,
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint participation mutation: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func bindAudienceReplacementToSource(
	source ParticipationSourceRef,
	params AudienceReplacementParams,
) (AudienceReplacementParams, error) {
	sourceFingerprint, err := source.Fingerprint()
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	payload := struct {
		SourceFingerprint        string                     `json:"source_fingerprint"`
		ExpectedAudienceRevision int64                      `json:"expected_audience_revision"`
		ResponseRequested        bool                       `json:"response_requested"`
		Attendees                []InternalAudienceAttendee `json:"attendees"`
		ExternalAttendees        []ExternalAudienceAttendee `json:"external_attendees"`
	}{
		SourceFingerprint:        sourceFingerprint,
		ExpectedAudienceRevision: params.ExpectedAudienceRevision,
		ResponseRequested:        params.ResponseRequested,
		Attendees:                params.Attendees,
		ExternalAttendees:        params.ExternalAttendees,
	}
	params.Fingerprint, err = fingerprintParticipationInput(
		"audience_replace",
		payload,
	)
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	return params, nil
}

func bindSelfRSVPToSource(
	source ParticipationSourceRef,
	params SelfRSVPParams,
) (SelfRSVPParams, error) {
	sourceFingerprint, err := source.Fingerprint()
	if err != nil {
		return SelfRSVPParams{}, err
	}
	payload := struct {
		SourceFingerprint       string    `json:"source_fingerprint"`
		State                   RSVPState `json:"state"`
		Note                    string    `json:"note"`
		ExpectedAttendeeVersion int64     `json:"expected_attendee_version"`
	}{
		SourceFingerprint:       sourceFingerprint,
		State:                   params.State,
		Note:                    params.Note,
		ExpectedAttendeeVersion: params.ExpectedAttendeeVersion,
	}
	params.Fingerprint, err = fingerprintParticipationInput("rsvp_respond", payload)
	if err != nil {
		return SelfRSVPParams{}, err
	}
	return params, nil
}
