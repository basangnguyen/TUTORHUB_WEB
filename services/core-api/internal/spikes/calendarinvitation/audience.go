package calendarinvitation

import (
	"fmt"
	"sort"
	"strings"
)

// AudienceChangeKind is the deterministic roster diff used to plan later
// recipient-specific effects. It does not send an email or mutate RSVP state.
type AudienceChangeKind string

const (
	AudienceAdded          AudienceChangeKind = "added"
	AudienceRemoved        AudienceChangeKind = "removed"
	AudienceUnchanged      AudienceChangeKind = "unchanged"
	AudienceRoleChanged    AudienceChangeKind = "role_changed"
	AudienceAddressChanged AudienceChangeKind = "address_changed"
	AudiencePolicyChanged  AudienceChangeKind = "response_policy_changed"
)

// AudienceChange documents whether an existing RSVP may be retained. Address
// replacement is treated as a new delivery identity and always resets RSVP.
type AudienceChange struct {
	RecipientID string
	Kind        AudienceChangeKind
	Previous    *Attendee
	Current     *Attendee
	RetainRSVP  bool
}

// DiffAudience creates a stable, recipient-ID ordered roster diff.
func DiffAudience(previous []Attendee, current []Attendee) ([]AudienceChange, error) {
	previousByID, err := indexAudience(previous)
	if err != nil {
		return nil, err
	}
	currentByID, err := indexAudience(current)
	if err != nil {
		return nil, err
	}

	identifiers := make([]string, 0, len(previousByID)+len(currentByID))
	seen := make(map[string]struct{}, len(previousByID)+len(currentByID))
	for identifier := range previousByID {
		identifiers = append(identifiers, identifier)
		seen[identifier] = struct{}{}
	}
	for identifier := range currentByID {
		if _, exists := seen[identifier]; !exists {
			identifiers = append(identifiers, identifier)
		}
	}
	sort.Strings(identifiers)

	changes := make([]AudienceChange, 0, len(identifiers))
	for _, identifier := range identifiers {
		before, existedBefore := previousByID[identifier]
		after, existsNow := currentByID[identifier]
		change := AudienceChange{RecipientID: identifier}
		switch {
		case !existedBefore:
			change.Kind = AudienceAdded
			change.Current = cloneAttendee(after)
		case !existsNow:
			change.Kind = AudienceRemoved
			change.Previous = cloneAttendee(before)
		case !strings.EqualFold(before.Email, after.Email) || before.External != after.External:
			change.Kind = AudienceAddressChanged
			change.Previous = cloneAttendee(before)
			change.Current = cloneAttendee(after)
		case before.Role != after.Role:
			change.Kind = AudienceRoleChanged
			change.Previous = cloneAttendee(before)
			change.Current = cloneAttendee(after)
		case before.ResponseRequested != after.ResponseRequested:
			change.Kind = AudiencePolicyChanged
			change.Previous = cloneAttendee(before)
			change.Current = cloneAttendee(after)
		default:
			change.Kind = AudienceUnchanged
			change.Previous = cloneAttendee(before)
			change.Current = cloneAttendee(after)
			change.RetainRSVP = true
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// ResolveAudience deterministically collapses roster and manual audience
// sources. Manual entries override roster entries with the same stable
// recipient identity; one recipient can never produce two effects.
func ResolveAudience(roster []Attendee, manual []Attendee) ([]Attendee, error) {
	rosterByID, err := indexAudienceFromSource(roster, AudienceSourceRoster)
	if err != nil {
		return nil, err
	}
	manualByID, err := indexAudienceFromSource(manual, AudienceSourceManual)
	if err != nil {
		return nil, err
	}
	for identifier, attendee := range manualByID {
		rosterByID[identifier] = attendee
	}
	identifiers := make([]string, 0, len(rosterByID))
	for identifier := range rosterByID {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	result := make([]Attendee, 0, len(identifiers))
	for _, identifier := range identifiers {
		result = append(result, rosterByID[identifier])
	}
	return result, nil
}

func indexAudienceFromSource(
	audience []Attendee,
	expected AudienceSource,
) (map[string]Attendee, error) {
	indexed, err := indexAudience(audience)
	if err != nil {
		return nil, err
	}
	for _, attendee := range indexed {
		if attendee.Source != expected {
			return nil, fmt.Errorf(
				"%w: source %q is not valid in the %s input",
				ErrInvalidAudience,
				attendee.Source,
				expected,
			)
		}
	}
	return indexed, nil
}

func indexAudience(audience []Attendee) (map[string]Attendee, error) {
	indexed := make(map[string]Attendee, len(audience))
	for index, attendee := range audience {
		if err := validateAttendee(attendee); err != nil {
			return nil, fmt.Errorf("%w: attendee %d: %v", ErrInvalidAudience, index, err)
		}
		if _, duplicate := indexed[attendee.RecipientID]; duplicate {
			return nil, fmt.Errorf(
				"%w: duplicate recipient_id %q",
				ErrInvalidAudience,
				attendee.RecipientID,
			)
		}
		indexed[attendee.RecipientID] = attendee
	}
	return indexed, nil
}

func cloneAttendee(attendee Attendee) *Attendee {
	copy := attendee
	return &copy
}
