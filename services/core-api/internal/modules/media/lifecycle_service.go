package media

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	maximumInstantTitleRunes = 200
	maximumInstantDuration   = 24 * time.Hour
)

var (
	mediaIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)
	mediaReasonCodePattern  = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
)

type LifecycleServiceConfig struct {
	Clock func() time.Time
	NewID func() uuid.UUID
}

type LifecycleService struct {
	repository LifecycleRepository
	clock      func() time.Time
	newID      func() uuid.UUID
}

func NewLifecycleService(
	repository LifecycleRepository,
	configurations ...LifecycleServiceConfig,
) (*LifecycleService, error) {
	if repository == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	if len(configurations) > 1 {
		return nil, fmt.Errorf("only one media lifecycle service configuration is supported")
	}
	config := LifecycleServiceConfig{}
	if len(configurations) == 1 {
		config = configurations[0]
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}
	return &LifecycleService{repository: repository, clock: config.Clock, newID: config.NewID}, nil
}

func (service *LifecycleService) CreateSpace(
	ctx context.Context,
	access AccessContext,
	input CreateSpaceInput,
) (CreateSpaceResult, error) {
	if service == nil || service.repository == nil {
		return CreateSpaceResult{}, ErrLifecycleUnavailable
	}
	if !validLifecycleAccess(access) {
		return CreateSpaceResult{}, ErrSpaceAccessDenied
	}
	normalized, err := normalizeCreateSpaceInput(input)
	if err != nil {
		return CreateSpaceResult{}, err
	}
	command := CreateSpaceCommand{
		SpaceID: service.newID(), Source: normalized.Source,
		IdempotencyKey: normalized.IdempotencyKey,
		Fingerprint:    createSpaceFingerprint(normalized),
		CreatedAt:      service.clock().UTC(),
	}
	if normalized.Source.Kind == SourceInstant {
		command.InstantMeetingID = service.newID()
	}
	result, err := service.repository.CreateSpace(ctx, access, command)
	return result, normalizeLifecycleError(err)
}

func (service *LifecycleService) GetSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
) (MediaSpace, error) {
	if service == nil || service.repository == nil {
		return MediaSpace{}, ErrLifecycleUnavailable
	}
	if !validLifecycleAccess(access) {
		return MediaSpace{}, ErrSpaceAccessDenied
	}
	if spaceID == uuid.Nil {
		return MediaSpace{}, ErrSpaceNotFound
	}
	space, err := service.repository.GetSpace(ctx, access, spaceID)
	return space, normalizeLifecycleError(err)
}

func (service *LifecycleService) StartSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	return service.transition(ctx, access, spaceID, "start", input)
}

func (service *LifecycleService) EndSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	return service.transition(ctx, access, spaceID, "end", input)
}

func (service *LifecycleService) CancelSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	return service.transition(ctx, access, spaceID, "cancel", input)
}

func (service *LifecycleService) transition(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	operation string,
	input TransitionInput,
) (MediaSpace, error) {
	if service == nil || service.repository == nil {
		return MediaSpace{}, ErrLifecycleUnavailable
	}
	if !validLifecycleAccess(access) {
		return MediaSpace{}, ErrSpaceAccessDenied
	}
	if spaceID == uuid.Nil {
		return MediaSpace{}, ErrSpaceNotFound
	}
	normalized, err := normalizeTransitionInput(input)
	if err != nil {
		return MediaSpace{}, err
	}
	command := TransitionCommand{
		SpaceID: spaceID, Operation: operation,
		ExpectedVersion: normalized.ExpectedVersion,
		IdempotencyKey:  normalized.IdempotencyKey,
		ReasonCode:      normalized.ReasonCode,
		OccurredAt:      service.clock().UTC(),
	}
	if operation == "start" {
		command.RoomInstanceID = service.newID()
		command.ProviderRoomName = opaqueProviderRoomName(command.RoomInstanceID)
	}
	command.Fingerprint = transitionFingerprint(command)
	space, err := service.repository.TransitionSpace(ctx, access, command)
	return space, normalizeLifecycleError(err)
}

func normalizeCreateSpaceInput(input CreateSpaceInput) (CreateSpaceInput, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !mediaIdempotencyPattern.MatchString(input.IdempotencyKey) {
		return CreateSpaceInput{}, ErrInvalidSpaceRequest
	}
	input.Source.OccurrenceKey = strings.TrimSpace(input.Source.OccurrenceKey)
	switch input.Source.Kind {
	case SourceClassSession:
		if input.Source.ClassSessionID == uuid.Nil || input.Source.SeriesID != uuid.Nil ||
			input.Source.OccurrenceKey != "" || input.Source.StudyMeetingID != uuid.Nil ||
			!emptyInstantInput(input.Source.Instant) {
			return CreateSpaceInput{}, ErrInvalidSpaceRequest
		}
	case SourceClassSessionOccurrence:
		if input.Source.SeriesID == uuid.Nil || len(input.Source.OccurrenceKey) < 8 ||
			len(input.Source.OccurrenceKey) > 128 || input.Source.ClassSessionID != uuid.Nil ||
			input.Source.StudyMeetingID != uuid.Nil || !emptyInstantInput(input.Source.Instant) {
			return CreateSpaceInput{}, ErrInvalidSpaceRequest
		}
	case SourceStudyMeeting:
		if input.Source.StudyMeetingID == uuid.Nil || input.Source.ClassSessionID != uuid.Nil ||
			input.Source.SeriesID != uuid.Nil || input.Source.OccurrenceKey != "" ||
			!emptyInstantInput(input.Source.Instant) {
			return CreateSpaceInput{}, ErrInvalidSpaceRequest
		}
	case SourceInstant:
		if input.Source.ClassSessionID != uuid.Nil || input.Source.SeriesID != uuid.Nil ||
			input.Source.OccurrenceKey != "" || input.Source.StudyMeetingID != uuid.Nil {
			return CreateSpaceInput{}, ErrInvalidSpaceRequest
		}
		input.Source.Instant.Title = strings.TrimSpace(input.Source.Instant.Title)
		input.Source.Instant.Timezone = strings.TrimSpace(input.Source.Instant.Timezone)
		if !utf8.ValidString(input.Source.Instant.Title) || input.Source.Instant.Title == "" ||
			utf8.RuneCountInString(input.Source.Instant.Title) > maximumInstantTitleRunes ||
			input.Source.Instant.DurationMinutes < 1 ||
			time.Duration(input.Source.Instant.DurationMinutes)*time.Minute > maximumInstantDuration ||
			len(input.Source.Instant.Timezone) < 1 || len(input.Source.Instant.Timezone) > 100 ||
			strings.EqualFold(input.Source.Instant.Timezone, "local") {
			return CreateSpaceInput{}, ErrInvalidSpaceRequest
		}
		if _, err := time.LoadLocation(input.Source.Instant.Timezone); err != nil {
			return CreateSpaceInput{}, ErrInvalidSpaceRequest
		}
	default:
		return CreateSpaceInput{}, ErrInvalidSpaceRequest
	}
	return input, nil
}

func normalizeTransitionInput(input TransitionInput) (TransitionInput, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ReasonCode = strings.TrimSpace(input.ReasonCode)
	if input.ExpectedVersion < 1 || !mediaIdempotencyPattern.MatchString(input.IdempotencyKey) ||
		(input.ReasonCode != "" && !mediaReasonCodePattern.MatchString(input.ReasonCode)) {
		return TransitionInput{}, ErrInvalidSpaceRequest
	}
	return input, nil
}

func emptyInstantInput(input InstantSourceInput) bool {
	return input.Title == "" && input.DurationMinutes == 0 && input.Timezone == ""
}

func validLifecycleAccess(access AccessContext) bool {
	return access.TenantID != uuid.Nil && access.ActorID != uuid.Nil &&
		access.MembershipActive && len(access.OrganizationRoles) > 0
}

func createSpaceFingerprint(input CreateSpaceInput) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(input.Source.Kind))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(input.Source.ClassSessionID[:])
	_, _ = digest.Write(input.Source.SeriesID[:])
	_, _ = digest.Write([]byte(input.Source.OccurrenceKey))
	_, _ = digest.Write(input.Source.StudyMeetingID[:])
	_, _ = digest.Write([]byte(input.Source.Instant.Title))
	_, _ = digest.Write([]byte{0})
	var duration [8]byte
	binary.BigEndian.PutUint64(duration[:], uint64(input.Source.Instant.DurationMinutes))
	_, _ = digest.Write(duration[:])
	_, _ = digest.Write([]byte(input.Source.Instant.Timezone))
	return digest.Sum(nil)
}

func transitionFingerprint(command TransitionCommand) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(command.Operation))
	_, _ = digest.Write(command.SpaceID[:])
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], uint64(command.ExpectedVersion))
	_, _ = digest.Write(version[:])
	_, _ = digest.Write([]byte(command.ReasonCode))
	return digest.Sum(nil)
}

func opaqueProviderRoomName(instanceID uuid.UUID) string {
	return "r_" + strings.ReplaceAll(instanceID.String(), "-", "")
}

func normalizeLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrLifecycleUnavailable, ErrInvalidSpaceRequest, ErrSpaceAccessDenied,
		ErrSpaceNotFound, ErrSourceUnavailable, ErrSpaceVersionConflict,
		ErrSpaceIdempotency, ErrSpaceTransition,
		featurecontrol.ErrInvalidControl, featurecontrol.ErrAccessDenied,
		featurecontrol.ErrTenantNotFound, featurecontrol.ErrFeatureDisabled,
		featurecontrol.ErrQuotaExceeded, featurecontrol.ErrVersionConflict,
		featurecontrol.ErrUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	return ErrLifecycleUnavailable
}
