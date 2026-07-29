package classroom

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// AudienceDiffKind is the stable business classification consumed by the
// invitation snapshot/delivery boundary. A worker must never infer this diff
// later from the current class roster.
type AudienceDiffKind string

const (
	AudienceDiffAdded      AudienceDiffKind = "added"
	AudienceDiffRemoved    AudienceDiffKind = "removed"
	AudienceDiffUnchanged  AudienceDiffKind = "unchanged"
	AudienceDiffRoleChange AudienceDiffKind = "role_change"
)

// AudienceRSVPDisposition describes how the current RSVP authority changes.
// Retain is intentionally limited to metadata-only changes that do not alter
// the recipient's attendance commitment.
type AudienceRSVPDisposition string

const (
	AudienceRSVPRetain AudienceRSVPDisposition = "retain"
	AudienceRSVPReset  AudienceRSVPDisposition = "reset"
	AudienceRSVPClose  AudienceRSVPDisposition = "close"
)

// audienceDiffMember is a PII-free, storage-neutral representation of one
// active internal recipient. UserID is the stable recipient identity.
type audienceDiffMember struct {
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
	BusinessRole      string
	AudienceSource    string
	ResponseRequested bool
}

type audienceDiffEntry struct {
	UserID          uuid.UUID
	Kind            AudienceDiffKind
	RSVPDisposition AudienceRSVPDisposition
	Before          *audienceDiffMember
	After           *audienceDiffMember
	MetadataChanged bool
}

// planAudienceDiff returns a deterministic total order by stable recipient
// identity. required/optional or response_requested changes reset RSVP;
// roster/business metadata changes retain it. Removed recipients are closed
// and a later re-add is therefore classified as added by the next plan.
func planAudienceDiff(
	current []audienceDiffMember,
	desired []audienceDiffMember,
) ([]audienceDiffEntry, error) {
	currentByID, err := indexAudienceDiffMembers(current)
	if err != nil {
		return nil, err
	}
	desiredByID, err := indexAudienceDiffMembers(desired)
	if err != nil {
		return nil, err
	}

	identities := make([]uuid.UUID, 0, len(currentByID)+len(desiredByID))
	seen := make(map[uuid.UUID]struct{}, len(currentByID)+len(desiredByID))
	for userID := range currentByID {
		seen[userID] = struct{}{}
		identities = append(identities, userID)
	}
	for userID := range desiredByID {
		if _, exists := seen[userID]; exists {
			continue
		}
		identities = append(identities, userID)
	}
	sort.Slice(identities, func(left, right int) bool {
		return identities[left].String() < identities[right].String()
	})

	entries := make([]audienceDiffEntry, 0, len(identities))
	for _, userID := range identities {
		before, existed := currentByID[userID]
		after, remains := desiredByID[userID]
		entry := audienceDiffEntry{UserID: userID}
		switch {
		case !existed && remains:
			entry.Kind = AudienceDiffAdded
			entry.RSVPDisposition = AudienceRSVPReset
			entry.After = copyAudienceDiffMember(after)
			entry.MetadataChanged = true
		case existed && !remains:
			entry.Kind = AudienceDiffRemoved
			entry.RSVPDisposition = AudienceRSVPClose
			entry.Before = copyAudienceDiffMember(before)
			entry.MetadataChanged = true
		case existed && remains:
			entry.Before = copyAudienceDiffMember(before)
			entry.After = copyAudienceDiffMember(after)
			entry.MetadataChanged = before != after
			if before.ParticipationRole != after.ParticipationRole {
				entry.Kind = AudienceDiffRoleChange
				entry.RSVPDisposition = AudienceRSVPReset
			} else {
				entry.Kind = AudienceDiffUnchanged
				entry.RSVPDisposition = AudienceRSVPRetain
				if before.ResponseRequested != after.ResponseRequested {
					entry.RSVPDisposition = AudienceRSVPReset
				}
			}
		default:
			return nil, fmt.Errorf(
				"%w: audience diff contains an unreachable identity",
				ErrInvalidParticipationInput,
			)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func indexAudienceDiffMembers(
	members []audienceDiffMember,
) (map[uuid.UUID]audienceDiffMember, error) {
	indexed := make(map[uuid.UUID]audienceDiffMember, len(members))
	for _, member := range members {
		if member.UserID == uuid.Nil {
			return nil, fmt.Errorf(
				"%w: audience diff identity is required",
				ErrInvalidParticipationInput,
			)
		}
		if _, exists := indexed[member.UserID]; exists {
			return nil, fmt.Errorf(
				"%w: audience diff identity %s is duplicated",
				ErrInvalidParticipationInput,
				member.UserID,
			)
		}
		indexed[member.UserID] = member
	}
	return indexed, nil
}

func copyAudienceDiffMember(member audienceDiffMember) *audienceDiffMember {
	copy := member
	return &copy
}

func planResolvedAudienceDiff(
	current []persistedSessionAttendee,
	desired []resolvedAudienceMember,
	desiredResponseRequested bool,
) (map[uuid.UUID]audienceDiffEntry, error) {
	currentMembers := make([]audienceDiffMember, 0, len(current))
	for _, attendee := range current {
		currentMembers = append(currentMembers, audienceDiffMember{
			UserID:            attendee.UserID,
			ParticipationRole: attendee.ParticipationRole,
			BusinessRole:      attendee.BusinessRole,
			AudienceSource:    attendee.AudienceSource,
			ResponseRequested: attendee.ResponseRequested,
		})
	}
	desiredMembers := make([]audienceDiffMember, 0, len(desired))
	for _, attendee := range desired {
		desiredMembers = append(desiredMembers, audienceDiffMember{
			UserID:            attendee.UserID,
			ParticipationRole: attendee.ParticipationRole,
			BusinessRole:      attendee.BusinessRole,
			AudienceSource:    attendee.AudienceSource,
			ResponseRequested: desiredResponseRequested,
		})
	}
	diff, err := planAudienceDiff(currentMembers, desiredMembers)
	if err != nil {
		return nil, err
	}
	byUserID := make(map[uuid.UUID]audienceDiffEntry, len(diff))
	for _, entry := range diff {
		byUserID[entry.UserID] = entry
	}
	return byUserID, nil
}
