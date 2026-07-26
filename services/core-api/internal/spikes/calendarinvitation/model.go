// Package calendarinvitation is an isolated P3-CAL-02 invitation spike.
//
// It validates a recipient-specific calendar snapshot and renders canonical
// iCalendar/MIME bytes. It is deliberately not wired to HTTP handlers,
// persistence, the transactional outbox, or a production email sender.
package calendarinvitation

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/spikes/calendarrecurrence"
)

const (
	maxInvitationIDBytes = 128
	maxAttendeeIDBytes   = 128
	maxDisplayNameBytes  = 256
	maxTitleBytes        = 512
	maxDescriptionBytes  = 16 * 1024
	maxLocationBytes     = 1024
	maxURLBytes          = 2048
	maxTimeZoneBytes     = 128
	maxVisibleAttendees  = 128
	maxExDates           = 512
	maxRecurrenceBytes   = 1024
)

var (
	ErrInvalidSnapshot   = errors.New("invalid invitation snapshot")
	ErrInvalidAudience   = errors.New("invalid invitation audience")
	ErrInvalidCalendar   = errors.New("invalid iCalendar data")
	ErrCanonicalTooLarge = errors.New("canonical invitation is too large")
)

// EffectKind describes the recipient-visible effect represented by a snapshot.
type EffectKind string

const (
	EffectInvite EffectKind = "invite"
	EffectUpdate EffectKind = "update"
	EffectCancel EffectKind = "cancel"
)

// Method is the supported iTIP method subset.
type Method string

const (
	MethodRequest Method = "REQUEST"
	MethodCancel  Method = "CANCEL"
)

// ParticipantRole maps the TutorHub audience role to RFC 5545 ROLE values.
type ParticipantRole string

const (
	RoleRequired ParticipantRole = "required"
	RoleOptional ParticipantRole = "optional"
)

// AudienceSource records how the recipient entered the authoritative audience.
type AudienceSource string

const (
	AudienceSourceRoster AudienceSource = "roster"
	AudienceSourceManual AudienceSource = "manual"
)

// RSVPState is the TutorHub-owned response snapshot shown in an outbound ICS.
type RSVPState string

const (
	RSVPNeedsAction RSVPState = "needs_action"
	RSVPAccepted    RSVPState = "accepted"
	RSVPTentative   RSVPState = "tentative"
	RSVPDeclined    RSVPState = "declined"
)

// RSVPSource records which trusted TutorHub path produced the response.
type RSVPSource string

const (
	RSVPSourceNone          RSVPSource = "none"
	RSVPSourceAuthenticated RSVPSource = "tutorhub_authenticated"
	RSVPSourceCapability    RSVPSource = "tutorhub_external_capability"
	RSVPSourceAdminOverride RSVPSource = "organizer_override"
)

// Locale is the bounded rendering locale persisted with the recipient effect.
type Locale string

const (
	LocaleVietnamese Locale = "vi-VN"
	LocaleEnglish    Locale = "en-US"
)

// Organizer is the organizer snapshot persisted for a single invitation
// revision. Its values must not be looked up again during a retry.
type Organizer struct {
	UserID      string
	Email       string
	DisplayName string
}

// Attendee is a recipient or an explicitly visible guest-list member.
type Attendee struct {
	RecipientID       string
	Email             string
	DisplayName       string
	Role              ParticipantRole
	Source            AudienceSource
	External          bool
	ResponseRequested bool
	RSVP              RSVPState
	RSVPSource        RSVPSource
	CanSeeGuestList   bool
}

// Snapshot is the immutable, recipient-specific source for canonical bytes.
// DTStamp and Sequence are supplied by the caller and remain unchanged when
// the same revision is retried.
type Snapshot struct {
	InvitationID string
	UID          string
	Sequence     uint32
	Effect       EffectKind
	DTStamp      time.Time

	StartsAt            time.Time
	EndsAt              time.Time
	TimeZone            string
	TimeZoneDataVersion string
	TimeZoneHorizonAt   time.Time

	Title       string
	Description string
	Location    string
	DeepLink    string

	Organizer Organizer
	Recipient RecipientSnapshot

	RecurrenceRule string
	OverlapPolicy  calendarrecurrence.OverlapPolicy
	RecurrenceID   *time.Time
	ExDates        []time.Time

	// IncludeGuestList is an explicit server decision. The safe default is
	// false, in which case the ICS contains only Organizer and Recipient.
	IncludeGuestList bool
	VisibleAttendees []Attendee
}

// RecipientSnapshot keeps attendee data and the bounded CTA label separate
// from future delivery credentials. The spike never stores or logs a raw
// external capability token.
type RecipientSnapshot struct {
	Attendee
	CTALabel       string
	Locale         Locale
	ViewerTimeZone string
}

// ICalendarMethod derives the MIME and VCALENDAR METHOD from the effect.
func (snapshot Snapshot) ICalendarMethod() (Method, error) {
	switch snapshot.Effect {
	case EffectInvite, EffectUpdate:
		return MethodRequest, nil
	case EffectCancel:
		return MethodCancel, nil
	default:
		return "", fmt.Errorf("%w: unsupported effect %q", ErrInvalidSnapshot, snapshot.Effect)
	}
}

// Validate rejects unbounded data, header injection, invalid identities and
// recurrence shapes outside the accepted Phase 3 subset.
func (snapshot Snapshot) Validate() error {
	if err := validateBoundedSingleLine(
		"invitation_id",
		snapshot.InvitationID,
		1,
		maxInvitationIDBytes,
	); err != nil {
		return err
	}
	if err := validateUID(snapshot.UID); err != nil {
		return err
	}
	if _, err := snapshot.ICalendarMethod(); err != nil {
		return err
	}
	if snapshot.DTStamp.IsZero() {
		return fmt.Errorf("%w: dtstamp is required", ErrInvalidSnapshot)
	}
	if snapshot.StartsAt.IsZero() || snapshot.EndsAt.IsZero() ||
		!snapshot.StartsAt.Before(snapshot.EndsAt) {
		return fmt.Errorf("%w: starts_at must be before ends_at", ErrInvalidSnapshot)
	}
	if err := validateIANAZoneName(snapshot.TimeZone); err != nil {
		return fmt.Errorf("%w: invalid time zone", ErrInvalidSnapshot)
	}
	eventLocation, err := time.LoadLocation(snapshot.TimeZone)
	if err != nil {
		return fmt.Errorf("%w: invalid IANA time zone %q: %v", ErrInvalidSnapshot, snapshot.TimeZone, err)
	}
	if err := validateBoundedSingleLine(
		"time_zone_data_version",
		snapshot.TimeZoneDataVersion,
		1,
		64,
	); err != nil {
		return err
	}
	if err := validateBoundedText("title", snapshot.Title, 1, maxTitleBytes, false); err != nil {
		return err
	}
	if err := validateBoundedText(
		"description",
		snapshot.Description,
		0,
		maxDescriptionBytes,
		true,
	); err != nil {
		return err
	}
	if err := validateBoundedText("location", snapshot.Location, 0, maxLocationBytes, false); err != nil {
		return err
	}
	if err := validateHTTPSURL(snapshot.DeepLink); err != nil {
		return err
	}
	if err := validateOrganizer(snapshot.Organizer); err != nil {
		return err
	}
	if err := validateAttendee(snapshot.Recipient.Attendee); err != nil {
		return fmt.Errorf("%w: recipient: %v", ErrInvalidSnapshot, err)
	}
	if err := validateBoundedText(
		"cta_label",
		snapshot.Recipient.CTALabel,
		1,
		maxDisplayNameBytes,
		false,
	); err != nil {
		return err
	}
	switch snapshot.Recipient.Locale {
	case LocaleVietnamese, LocaleEnglish:
	default:
		return fmt.Errorf(
			"%w: unsupported recipient locale %q",
			ErrInvalidSnapshot,
			snapshot.Recipient.Locale,
		)
	}
	if err := validateIANAZoneName(snapshot.Recipient.ViewerTimeZone); err != nil {
		return fmt.Errorf("%w: invalid viewer time zone", ErrInvalidSnapshot)
	}
	if _, err := time.LoadLocation(snapshot.Recipient.ViewerTimeZone); err != nil {
		return fmt.Errorf(
			"%w: invalid viewer IANA time zone %q: %v",
			ErrInvalidSnapshot,
			snapshot.Recipient.ViewerTimeZone,
			err,
		)
	}

	if len(snapshot.ExDates) > maxExDates {
		return fmt.Errorf("%w: exdate exceeds %d entries", ErrInvalidSnapshot, maxExDates)
	}
	if err := validateRecurrenceRule(snapshot, eventLocation); err != nil {
		return err
	}
	if snapshot.RecurrenceRule != "" {
		expectedHorizon := snapshot.StartsAt.In(eventLocation).
			AddDate(0, 0, calendarrecurrence.MaxSeriesHorizonDays)
		if snapshot.TimeZoneHorizonAt.IsZero() ||
			!snapshot.TimeZoneHorizonAt.Equal(expectedHorizon) {
			return fmt.Errorf(
				"%w: recurring snapshot requires the canonical %d-day civil horizon",
				ErrInvalidSnapshot,
				calendarrecurrence.MaxSeriesHorizonDays,
			)
		}
	} else if !snapshot.TimeZoneHorizonAt.IsZero() {
		return fmt.Errorf(
			"%w: time zone horizon is only valid for recurrence",
			ErrInvalidSnapshot,
		)
	}
	if snapshot.RecurrenceRule == "" && len(snapshot.ExDates) != 0 {
		return fmt.Errorf("%w: EXDATE requires a recurring master", ErrInvalidCalendar)
	}
	if snapshot.RecurrenceID != nil && snapshot.RecurrenceRule != "" {
		return fmt.Errorf(
			"%w: recurrence override cannot also define RRULE",
			ErrInvalidCalendar,
		)
	}
	if snapshot.RecurrenceID != nil && len(snapshot.ExDates) != 0 {
		return fmt.Errorf(
			"%w: recurrence override cannot also define EXDATE",
			ErrInvalidCalendar,
		)
	}
	seenExDates := make(map[int64]struct{}, len(snapshot.ExDates))
	for _, exDate := range snapshot.ExDates {
		if exDate.IsZero() {
			return fmt.Errorf("%w: EXDATE cannot be zero", ErrInvalidCalendar)
		}
		key := exDate.UnixNano()
		if _, duplicate := seenExDates[key]; duplicate {
			return fmt.Errorf("%w: duplicate EXDATE", ErrInvalidCalendar)
		}
		seenExDates[key] = struct{}{}
	}
	if snapshot.RecurrenceID != nil && snapshot.RecurrenceID.IsZero() {
		return fmt.Errorf("%w: RECURRENCE-ID cannot be zero", ErrInvalidCalendar)
	}

	if len(snapshot.VisibleAttendees) > maxVisibleAttendees {
		return fmt.Errorf(
			"%w: guest list exceeds %d attendees",
			ErrInvalidSnapshot,
			maxVisibleAttendees,
		)
	}
	if snapshot.IncludeGuestList && !snapshot.Recipient.CanSeeGuestList {
		return fmt.Errorf(
			"%w: recipient does not have guest-list permission",
			ErrInvalidSnapshot,
		)
	}
	if !snapshot.IncludeGuestList && len(snapshot.VisibleAttendees) != 0 {
		return fmt.Errorf(
			"%w: visible attendees require explicit guest-list permission",
			ErrInvalidSnapshot,
		)
	}
	seen := map[string]struct{}{snapshot.Recipient.RecipientID: {}}
	for index, attendee := range snapshot.VisibleAttendees {
		if err := validateAttendee(attendee); err != nil {
			return fmt.Errorf("%w: visible attendee %d: %v", ErrInvalidSnapshot, index, err)
		}
		if _, duplicate := seen[attendee.RecipientID]; duplicate {
			return fmt.Errorf(
				"%w: duplicate attendee %q",
				ErrInvalidSnapshot,
				attendee.RecipientID,
			)
		}
		seen[attendee.RecipientID] = struct{}{}
	}
	return nil
}

func validateUID(value string) error {
	const prefix = "urn:uuid:"
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("%w: uid must use urn:uuid", ErrInvalidSnapshot)
	}
	suffix := strings.TrimPrefix(value, prefix)
	parsed, err := uuid.Parse(suffix)
	if err != nil || parsed.String() != suffix {
		return fmt.Errorf("%w: invalid uid: %v", ErrInvalidSnapshot, err)
	}
	return nil
}

func validateOrganizer(organizer Organizer) error {
	if err := validateBoundedSingleLine("organizer user_id", organizer.UserID, 1, maxAttendeeIDBytes); err != nil {
		return err
	}
	if err := validateEmail(organizer.Email); err != nil {
		return fmt.Errorf("%w: organizer email: %v", ErrInvalidSnapshot, err)
	}
	if err := validateBoundedText(
		"organizer display_name",
		organizer.DisplayName,
		1,
		maxDisplayNameBytes,
		false,
	); err != nil {
		return err
	}
	return nil
}

func validateAttendee(attendee Attendee) error {
	if err := validateBoundedSingleLine(
		"recipient_id",
		attendee.RecipientID,
		1,
		maxAttendeeIDBytes,
	); err != nil {
		return err
	}
	if err := validateEmail(attendee.Email); err != nil {
		return err
	}
	if err := validateBoundedText(
		"display_name",
		attendee.DisplayName,
		1,
		maxDisplayNameBytes,
		false,
	); err != nil {
		return err
	}
	if attendee.Role != RoleRequired && attendee.Role != RoleOptional {
		return fmt.Errorf("%w: unsupported role %q", ErrInvalidAudience, attendee.Role)
	}
	switch attendee.Source {
	case AudienceSourceRoster, AudienceSourceManual:
	default:
		return fmt.Errorf("%w: unsupported audience source %q", ErrInvalidAudience, attendee.Source)
	}
	if attendee.External && attendee.Source == AudienceSourceRoster {
		return fmt.Errorf("%w: external attendee cannot come from roster", ErrInvalidAudience)
	}
	switch attendee.RSVP {
	case RSVPNeedsAction, RSVPAccepted, RSVPTentative, RSVPDeclined:
	default:
		return fmt.Errorf("%w: unsupported RSVP state %q", ErrInvalidAudience, attendee.RSVP)
	}
	switch attendee.RSVPSource {
	case RSVPSourceNone,
		RSVPSourceAuthenticated,
		RSVPSourceCapability,
		RSVPSourceAdminOverride:
	default:
		return fmt.Errorf("%w: unsupported RSVP source %q", ErrInvalidAudience, attendee.RSVPSource)
	}
	if attendee.RSVP == RSVPNeedsAction && attendee.RSVPSource != RSVPSourceNone {
		return fmt.Errorf(
			"%w: needs_action cannot use response source %q",
			ErrInvalidAudience,
			attendee.RSVPSource,
		)
	}
	if attendee.RSVP != RSVPNeedsAction && attendee.RSVPSource == RSVPSourceNone {
		return fmt.Errorf("%w: responded attendee requires a source", ErrInvalidAudience)
	}
	if attendee.RSVP != RSVPNeedsAction {
		if attendee.External &&
			attendee.RSVPSource != RSVPSourceCapability &&
			attendee.RSVPSource != RSVPSourceAdminOverride {
			return fmt.Errorf(
				"%w: external response requires capability or organizer override",
				ErrInvalidAudience,
			)
		}
		if !attendee.External && attendee.RSVPSource == RSVPSourceCapability {
			return fmt.Errorf(
				"%w: internal response cannot use external capability",
				ErrInvalidAudience,
			)
		}
	}
	return nil
}

func validateEmail(value string) error {
	if value == "" || len(value) > 320 || hasControl(value, false) {
		return fmt.Errorf("%w: invalid email address", ErrInvalidAudience)
	}
	for index := 0; index < len(value); index++ {
		// SES does not support SMTPUTF8. IDNs must be converted to Punycode
		// before an immutable delivery snapshot is created.
		if value[index] > 0x7f {
			return fmt.Errorf("%w: email address must be 7-bit ASCII", ErrInvalidAudience)
		}
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || address.Name != "" {
		return fmt.Errorf("%w: invalid bare email address", ErrInvalidAudience)
	}
	return nil
}

func validateHTTPSURL(value string) error {
	if value == "" || len(value) > maxURLBytes || hasControl(value, false) {
		return fmt.Errorf("%w: invalid deep link", ErrInvalidSnapshot)
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%w: deep link must be an HTTPS URL", ErrInvalidSnapshot)
	}
	return nil
}

func validateIANAZoneName(value string) error {
	if value == "" || len(value) > maxTimeZoneBytes || hasControl(value, false) {
		return fmt.Errorf("%w: invalid IANA time-zone name", ErrInvalidSnapshot)
	}
	if value == "Local" || value == "local" ||
		strings.Contains(value, `\`) ||
		strings.HasPrefix(value, "/") ||
		strings.Contains(value, "..") {
		return fmt.Errorf("%w: host-local time zone is forbidden", ErrInvalidSnapshot)
	}
	if value != "UTC" && !strings.Contains(value, "/") {
		return fmt.Errorf("%w: canonical IANA time-zone name is required", ErrInvalidSnapshot)
	}
	for _, character := range value {
		if !strings.ContainsRune(
			"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+-/",
			character,
		) {
			return fmt.Errorf("%w: invalid IANA time-zone name", ErrInvalidSnapshot)
		}
	}
	return nil
}

func validateBoundedSingleLine(name string, value string, minimum int, maximum int) error {
	return validateBoundedText(name, value, minimum, maximum, false)
}

func validateBoundedText(
	name string,
	value string,
	minimum int,
	maximum int,
	allowNewline bool,
) error {
	if !utf8.ValidString(value) || len(value) < minimum || len(value) > maximum ||
		hasControl(value, allowNewline) {
		return fmt.Errorf(
			"%w: %s must be valid UTF-8 between %d and %d bytes",
			ErrInvalidSnapshot,
			name,
			minimum,
			maximum,
		)
	}
	return nil
}

func hasControl(value string, allowNewline bool) bool {
	for _, character := range value {
		switch character {
		case '\r', '\n':
			if allowNewline {
				continue
			}
			return true
		case '\t':
			continue
		}
		if character < 0x20 || character == 0x7f {
			return true
		}
		if isDangerousUnicodeFormat(character) {
			return true
		}
	}
	return false
}

func isDangerousUnicodeFormat(character rune) bool {
	switch {
	case character == '\u061c',
		character >= '\u200b' && character <= '\u200f',
		character >= '\u202a' && character <= '\u202e',
		character >= '\u2060' && character <= '\u206f',
		character == '\ufeff',
		character >= '\ufff9' && character <= '\ufffb':
		return true
	default:
		return false
	}
}

func validateRecurrenceRule(snapshot Snapshot, location *time.Location) error {
	value := snapshot.RecurrenceRule
	if value == "" {
		return nil
	}
	if len(value) > maxRecurrenceBytes || value != strings.ToUpper(value) ||
		hasControl(value, false) {
		return fmt.Errorf("%w: invalid RRULE", ErrInvalidCalendar)
	}
	startLocal := snapshot.StartsAt.In(location)
	_, err := calendarrecurrence.Compile(calendarrecurrence.Series{
		ID:            snapshot.InvitationID,
		StartLocal:    startLocal.Format("2006-01-02T15:04:05"),
		TimeZone:      snapshot.TimeZone,
		Duration:      snapshot.EndsAt.Sub(snapshot.StartsAt),
		Rule:          value,
		OverlapPolicy: snapshot.OverlapPolicy,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCalendar, err)
	}
	return nil
}
