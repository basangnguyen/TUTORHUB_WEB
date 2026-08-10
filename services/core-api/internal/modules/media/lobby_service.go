package media

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	defaultLobbyListLimit    = 25
	maximumLobbyListLimit    = 100
	defaultLobbyAdmissionTTL = 10 * time.Minute
)

type LobbyAdmissionStatus string

const (
	LobbyAdmissionWaiting             LobbyAdmissionStatus = "waiting"
	LobbyAdmissionAdmitted            LobbyAdmissionStatus = "admitted"
	LobbyAdmissionDenied              LobbyAdmissionStatus = "denied"
	LobbyAdmissionCancelled           LobbyAdmissionStatus = "cancelled"
	LobbyAdmissionMeetingEnded        LobbyAdmissionStatus = "meeting_ended"
	LobbyAdmissionTimeout             LobbyAdmissionStatus = "timeout"
	LobbyAdmissionProviderUnavailable LobbyAdmissionStatus = "provider_unavailable"
)

type LobbyMemberStatus string

const (
	LobbyMemberActive  LobbyMemberStatus = "active"
	LobbyMemberRevoked LobbyMemberStatus = "revoked"
)

var (
	ErrLobbyUnavailable           = errors.New("media lobby unavailable")
	ErrInvalidLobbyRequest        = errors.New("invalid media lobby request")
	ErrAdmissionNotFound          = errors.New("media admission not found")
	ErrAdmissionVersionConflict   = errors.New("media admission version conflict")
	ErrAdmissionTransition        = errors.New("invalid media admission transition")
	ErrLobbyMemberNotFound        = errors.New("media lobby member not found")
	ErrLobbyMemberVersionConflict = errors.New("media lobby member version conflict")
)

// LobbyAdmission deliberately excludes email, user/provider identifiers,
// ParticipantSession IDs, and join-attempt IDs. A moderator receives only the
// bounded display projection needed to operate the waiting queue.
type LobbyAdmission struct {
	ID          uuid.UUID            `json:"id"`
	Status      LobbyAdmissionStatus `json:"status"`
	Version     int64                `json:"version"`
	DisplayName string               `json:"display_name"`
	CreatedAt   time.Time            `json:"created_at"`
	ExpiresAt   time.Time            `json:"expires_at"`
}

type LobbyAdmissionPage struct {
	Items []LobbyAdmission `json:"items"`
}

// LobbyMember is safe to render to an authorized StudyMeeting owner. Email is
// lookup-only input and is never retained in this projection.
type LobbyMember struct {
	UserID      uuid.UUID         `json:"user_id"`
	DisplayName string            `json:"display_name"`
	Status      LobbyMemberStatus `json:"status"`
	Version     int64             `json:"version"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type LobbyMemberPage struct {
	Items []LobbyMember `json:"items"`
}

type ListLobbyAdmissionsInput struct {
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	Limit                       int
}

type AdmissionMutationInput struct {
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	ExpectedAdmissionVersion    int64
	IdempotencyKey              string
	ReasonCode                  string
}

type ListLobbyMembersInput struct {
	ExpectedSpaceVersion int64
	Limit                int
}

type InviteLobbyMemberInput struct {
	Email                string
	ExpectedSpaceVersion int64
	IdempotencyKey       string
}

type MemberMutationInput struct {
	ExpectedSpaceVersion  int64
	ExpectedMemberVersion int64
	IdempotencyKey        string
	ReasonCode            string
}

type AdmissionMutationCommand struct {
	SpaceID                     uuid.UUID
	AdmissionID                 uuid.UUID
	JoinAttemptID               uuid.UUID
	Operation                   string
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	ExpectedAdmissionVersion    int64
	IdempotencyKey              string
	ReasonCode                  string
	Fingerprint                 []byte
	OccurredAt                  time.Time
	AdmissionTTL                time.Duration
}

type InviteLobbyMemberCommand struct {
	SpaceID              uuid.UUID
	TargetEmail          string
	ExpectedSpaceVersion int64
	IdempotencyKey       string
	Fingerprint          []byte
	OccurredAt           time.Time
}

type LobbyMemberMutationCommand struct {
	SpaceID               uuid.UUID
	TargetUserID          uuid.UUID
	Operation             string
	ExpectedSpaceVersion  int64
	ExpectedMemberVersion int64
	IdempotencyKey        string
	ReasonCode            string
	Fingerprint           []byte
	OccurredAt            time.Time
}

type LobbyRepository interface {
	ListAdmissions(context.Context, AccessContext, uuid.UUID, ListLobbyAdmissionsInput, time.Time, time.Duration) (LobbyAdmissionPage, error)
	GetJoinAttempt(context.Context, AccessContext, uuid.UUID, uuid.UUID, ListLobbyAdmissionsInput, time.Time, time.Duration) (JoinAttempt, error)
	MutateAdmission(context.Context, AccessContext, AdmissionMutationCommand) (LobbyAdmission, error)
	CancelJoinAttempt(context.Context, AccessContext, AdmissionMutationCommand) (JoinAttempt, error)
	ListMembers(context.Context, AccessContext, uuid.UUID, ListLobbyMembersInput) (LobbyMemberPage, error)
	InviteMember(context.Context, AccessContext, InviteLobbyMemberCommand) (LobbyMember, error)
	MutateMember(context.Context, AccessContext, LobbyMemberMutationCommand) (LobbyMember, error)
}

type LobbyServiceAPI interface {
	ListAdmissions(context.Context, AccessContext, uuid.UUID, ListLobbyAdmissionsInput) (LobbyAdmissionPage, error)
	GetJoinAttempt(context.Context, AccessContext, uuid.UUID, uuid.UUID, ListLobbyAdmissionsInput) (JoinAttempt, error)
	Admit(context.Context, AccessContext, uuid.UUID, uuid.UUID, AdmissionMutationInput) (LobbyAdmission, error)
	Deny(context.Context, AccessContext, uuid.UUID, uuid.UUID, AdmissionMutationInput) (LobbyAdmission, error)
	CancelJoinAttempt(context.Context, AccessContext, uuid.UUID, uuid.UUID, AdmissionMutationInput) (JoinAttempt, error)
	RestoreAdmission(context.Context, AccessContext, uuid.UUID, uuid.UUID, AdmissionMutationInput) (LobbyAdmission, error)
	ListMembers(context.Context, AccessContext, uuid.UUID, ListLobbyMembersInput) (LobbyMemberPage, error)
	InviteMember(context.Context, AccessContext, uuid.UUID, InviteLobbyMemberInput) (LobbyMember, error)
	RevokeMember(context.Context, AccessContext, uuid.UUID, uuid.UUID, MemberMutationInput) (LobbyMember, error)
	RestoreMember(context.Context, AccessContext, uuid.UUID, uuid.UUID, MemberMutationInput) (LobbyMember, error)
}

type LobbyServiceConfig struct {
	Clock        func() time.Time
	AdmissionTTL time.Duration
}

type LobbyService struct {
	repository   LobbyRepository
	clock        func() time.Time
	admissionTTL time.Duration
}

func NewLobbyService(repository LobbyRepository, configurations ...LobbyServiceConfig) (*LobbyService, error) {
	if repository == nil {
		return nil, fmt.Errorf("media lobby repository is required")
	}
	if len(configurations) > 1 {
		return nil, fmt.Errorf("only one media lobby service configuration is supported")
	}
	config := LobbyServiceConfig{}
	if len(configurations) == 1 {
		config = configurations[0]
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.AdmissionTTL <= 0 {
		config.AdmissionTTL = defaultLobbyAdmissionTTL
	}
	return &LobbyService{
		repository: repository, clock: config.Clock, admissionTTL: config.AdmissionTTL,
	}, nil
}

func (service *LobbyService) ListAdmissions(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input ListLobbyAdmissionsInput,
) (LobbyAdmissionPage, error) {
	if service == nil || service.repository == nil {
		return LobbyAdmissionPage{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return LobbyAdmissionPage{}, ErrSpaceAccessDenied
	}
	normalized, err := normalizeAdmissionListInput(spaceID, input)
	if err != nil {
		return LobbyAdmissionPage{}, err
	}
	page, err := service.repository.ListAdmissions(
		ctx, access, spaceID, normalized, service.clock().UTC(), service.admissionTTL,
	)
	if page.Items == nil {
		page.Items = []LobbyAdmission{}
	}
	return page, normalizeLobbyError(err)
}

func (service *LobbyService) GetJoinAttempt(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	joinAttemptID uuid.UUID,
	input ListLobbyAdmissionsInput,
) (JoinAttempt, error) {
	if service == nil || service.repository == nil {
		return JoinAttempt{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return JoinAttempt{}, ErrSpaceAccessDenied
	}
	if joinAttemptID == uuid.Nil {
		return JoinAttempt{}, ErrAdmissionNotFound
	}
	normalized, err := normalizeAdmissionListInput(spaceID, input)
	if err != nil {
		return JoinAttempt{}, err
	}
	item, err := service.repository.GetJoinAttempt(
		ctx, access, spaceID, joinAttemptID, normalized,
		service.clock().UTC(), service.admissionTTL,
	)
	return item, normalizeLobbyError(err)
}

func (service *LobbyService) Admit(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	admissionID uuid.UUID,
	input AdmissionMutationInput,
) (LobbyAdmission, error) {
	return service.mutateAdmission(ctx, access, spaceID, admissionID, "admission_admit", input)
}

func (service *LobbyService) Deny(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	admissionID uuid.UUID,
	input AdmissionMutationInput,
) (LobbyAdmission, error) {
	return service.mutateAdmission(ctx, access, spaceID, admissionID, "admission_deny", input)
}

func (service *LobbyService) CancelJoinAttempt(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	joinAttemptID uuid.UUID,
	input AdmissionMutationInput,
) (JoinAttempt, error) {
	if service == nil || service.repository == nil {
		return JoinAttempt{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return JoinAttempt{}, ErrSpaceAccessDenied
	}
	if spaceID == uuid.Nil || joinAttemptID == uuid.Nil {
		return JoinAttempt{}, ErrAdmissionNotFound
	}
	normalized, err := normalizeAdmissionMutationInput("admission_cancel", input)
	if err != nil {
		return JoinAttempt{}, err
	}
	command := AdmissionMutationCommand{
		SpaceID: spaceID, JoinAttemptID: joinAttemptID, Operation: "admission_cancel",
		ExpectedSpaceVersion:        normalized.ExpectedSpaceVersion,
		ExpectedRoomInstanceID:      normalized.ExpectedRoomInstanceID,
		ExpectedRoomInstanceVersion: normalized.ExpectedRoomInstanceVersion,
		ExpectedAdmissionVersion:    normalized.ExpectedAdmissionVersion,
		IdempotencyKey:              normalized.IdempotencyKey, ReasonCode: normalized.ReasonCode,
		OccurredAt: service.clock().UTC(), AdmissionTTL: service.admissionTTL,
	}
	command.Fingerprint = lobbyAdmissionFingerprint(command)
	item, err := service.repository.CancelJoinAttempt(ctx, access, command)
	return item, normalizeLobbyError(err)
}

func (service *LobbyService) RestoreAdmission(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	admissionID uuid.UUID,
	input AdmissionMutationInput,
) (LobbyAdmission, error) {
	return service.mutateAdmission(ctx, access, spaceID, admissionID, "admission_restore", input)
}

func (service *LobbyService) mutateAdmission(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	admissionID uuid.UUID,
	operation string,
	input AdmissionMutationInput,
) (LobbyAdmission, error) {
	if service == nil || service.repository == nil {
		return LobbyAdmission{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return LobbyAdmission{}, ErrSpaceAccessDenied
	}
	if spaceID == uuid.Nil || admissionID == uuid.Nil {
		return LobbyAdmission{}, ErrAdmissionNotFound
	}
	normalized, err := normalizeAdmissionMutationInput(operation, input)
	if err != nil {
		return LobbyAdmission{}, err
	}
	command := AdmissionMutationCommand{
		SpaceID: spaceID, AdmissionID: admissionID, Operation: operation,
		ExpectedSpaceVersion:        normalized.ExpectedSpaceVersion,
		ExpectedRoomInstanceID:      normalized.ExpectedRoomInstanceID,
		ExpectedRoomInstanceVersion: normalized.ExpectedRoomInstanceVersion,
		ExpectedAdmissionVersion:    normalized.ExpectedAdmissionVersion,
		IdempotencyKey:              normalized.IdempotencyKey, ReasonCode: normalized.ReasonCode,
		OccurredAt: service.clock().UTC(), AdmissionTTL: service.admissionTTL,
	}
	command.Fingerprint = lobbyAdmissionFingerprint(command)
	item, err := service.repository.MutateAdmission(ctx, access, command)
	return item, normalizeLobbyError(err)
}

func (service *LobbyService) ListMembers(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input ListLobbyMembersInput,
) (LobbyMemberPage, error) {
	if service == nil || service.repository == nil {
		return LobbyMemberPage{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return LobbyMemberPage{}, ErrSpaceAccessDenied
	}
	normalized, err := normalizeMemberListInput(spaceID, input)
	if err != nil {
		return LobbyMemberPage{}, err
	}
	page, err := service.repository.ListMembers(ctx, access, spaceID, normalized)
	if page.Items == nil {
		page.Items = []LobbyMember{}
	}
	return page, normalizeLobbyError(err)
}

func (service *LobbyService) InviteMember(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input InviteLobbyMemberInput,
) (LobbyMember, error) {
	if service == nil || service.repository == nil {
		return LobbyMember{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return LobbyMember{}, ErrSpaceAccessDenied
	}
	if spaceID == uuid.Nil || input.ExpectedSpaceVersion < 1 {
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	email, err := normalizeLobbyMemberEmail(input.Email)
	if err != nil {
		return LobbyMember{}, err
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !mediaIdempotencyPattern.MatchString(input.IdempotencyKey) {
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	command := InviteLobbyMemberCommand{
		SpaceID: spaceID, TargetEmail: email,
		ExpectedSpaceVersion: input.ExpectedSpaceVersion,
		IdempotencyKey:       input.IdempotencyKey, OccurredAt: service.clock().UTC(),
	}
	command.Fingerprint = lobbyInviteFingerprint(command)
	member, err := service.repository.InviteMember(ctx, access, command)
	return member, normalizeLobbyError(err)
}

func (service *LobbyService) RevokeMember(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	targetUserID uuid.UUID,
	input MemberMutationInput,
) (LobbyMember, error) {
	return service.mutateMember(ctx, access, spaceID, targetUserID, "member_revoke", input)
}

func (service *LobbyService) RestoreMember(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	targetUserID uuid.UUID,
	input MemberMutationInput,
) (LobbyMember, error) {
	return service.mutateMember(ctx, access, spaceID, targetUserID, "member_restore", input)
}

func (service *LobbyService) mutateMember(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	targetUserID uuid.UUID,
	operation string,
	input MemberMutationInput,
) (LobbyMember, error) {
	if service == nil || service.repository == nil {
		return LobbyMember{}, ErrLobbyUnavailable
	}
	if !validLifecycleAccess(access) {
		return LobbyMember{}, ErrSpaceAccessDenied
	}
	if spaceID == uuid.Nil || targetUserID == uuid.Nil || input.ExpectedSpaceVersion < 1 ||
		input.ExpectedMemberVersion < 1 {
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ReasonCode = strings.TrimSpace(input.ReasonCode)
	if !mediaIdempotencyPattern.MatchString(input.IdempotencyKey) ||
		(input.ReasonCode != "" && !mediaReasonCodePattern.MatchString(input.ReasonCode)) {
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	command := LobbyMemberMutationCommand{
		SpaceID: spaceID, TargetUserID: targetUserID, Operation: operation,
		ExpectedSpaceVersion:  input.ExpectedSpaceVersion,
		ExpectedMemberVersion: input.ExpectedMemberVersion,
		IdempotencyKey:        input.IdempotencyKey, ReasonCode: input.ReasonCode,
		OccurredAt: service.clock().UTC(),
	}
	command.Fingerprint = lobbyMemberFingerprint(command)
	member, err := service.repository.MutateMember(ctx, access, command)
	return member, normalizeLobbyError(err)
}

func normalizeAdmissionListInput(
	spaceID uuid.UUID,
	input ListLobbyAdmissionsInput,
) (ListLobbyAdmissionsInput, error) {
	if spaceID == uuid.Nil || input.ExpectedSpaceVersion < 1 ||
		input.ExpectedRoomInstanceID == uuid.Nil || input.ExpectedRoomInstanceVersion < 1 {
		return ListLobbyAdmissionsInput{}, ErrInvalidLobbyRequest
	}
	if input.Limit == 0 {
		input.Limit = defaultLobbyListLimit
	}
	if input.Limit < 1 || input.Limit > maximumLobbyListLimit {
		return ListLobbyAdmissionsInput{}, ErrInvalidLobbyRequest
	}
	return input, nil
}

func normalizeAdmissionMutationInput(
	operation string,
	input AdmissionMutationInput,
) (AdmissionMutationInput, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ReasonCode = strings.TrimSpace(input.ReasonCode)
	if input.ExpectedSpaceVersion < 1 || input.ExpectedRoomInstanceID == uuid.Nil ||
		input.ExpectedRoomInstanceVersion < 1 || input.ExpectedAdmissionVersion < 1 ||
		!mediaIdempotencyPattern.MatchString(input.IdempotencyKey) ||
		(input.ReasonCode != "" && !mediaReasonCodePattern.MatchString(input.ReasonCode)) {
		return AdmissionMutationInput{}, ErrInvalidLobbyRequest
	}
	switch operation {
	case "admission_admit", "admission_cancel", "admission_restore":
	case "admission_deny":
		if input.ReasonCode == "" {
			return AdmissionMutationInput{}, ErrInvalidLobbyRequest
		}
	default:
		return AdmissionMutationInput{}, ErrInvalidLobbyRequest
	}
	return input, nil
}

func normalizeMemberListInput(
	spaceID uuid.UUID,
	input ListLobbyMembersInput,
) (ListLobbyMembersInput, error) {
	if spaceID == uuid.Nil || input.ExpectedSpaceVersion < 1 {
		return ListLobbyMembersInput{}, ErrInvalidLobbyRequest
	}
	if input.Limit == 0 {
		input.Limit = defaultLobbyListLimit
	}
	if input.Limit < 1 || input.Limit > maximumLobbyListLimit {
		return ListLobbyMembersInput{}, ErrInvalidLobbyRequest
	}
	return input, nil
}

func normalizeLobbyMemberEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) < 3 || len(value) > 320 {
		return "", ErrInvalidLobbyRequest
	}
	return value, nil
}

func lobbyAdmissionFingerprint(command AdmissionMutationCommand) []byte {
	digest := sha256.New()
	writeLobbyFingerprintString(digest, command.Operation)
	_, _ = digest.Write(command.SpaceID[:])
	_, _ = digest.Write(command.AdmissionID[:])
	_, _ = digest.Write(command.JoinAttemptID[:])
	_, _ = digest.Write(command.ExpectedRoomInstanceID[:])
	writeLobbyFingerprintInt64(digest, command.ExpectedSpaceVersion)
	writeLobbyFingerprintInt64(digest, command.ExpectedRoomInstanceVersion)
	writeLobbyFingerprintInt64(digest, command.ExpectedAdmissionVersion)
	writeLobbyFingerprintString(digest, command.ReasonCode)
	return digest.Sum(nil)
}

func lobbyInviteFingerprint(command InviteLobbyMemberCommand) []byte {
	digest := sha256.New()
	writeLobbyFingerprintString(digest, "member_invite")
	_, _ = digest.Write(command.SpaceID[:])
	writeLobbyFingerprintInt64(digest, command.ExpectedSpaceVersion)
	writeLobbyFingerprintString(digest, command.TargetEmail)
	return digest.Sum(nil)
}

func lobbyMemberFingerprint(command LobbyMemberMutationCommand) []byte {
	digest := sha256.New()
	writeLobbyFingerprintString(digest, command.Operation)
	_, _ = digest.Write(command.SpaceID[:])
	_, _ = digest.Write(command.TargetUserID[:])
	writeLobbyFingerprintInt64(digest, command.ExpectedSpaceVersion)
	writeLobbyFingerprintInt64(digest, command.ExpectedMemberVersion)
	writeLobbyFingerprintString(digest, command.ReasonCode)
	return digest.Sum(nil)
}

type lobbyFingerprintWriter interface {
	Write([]byte) (int, error)
}

func writeLobbyFingerprintString(writer lobbyFingerprintWriter, value string) {
	_, _ = writer.Write([]byte(value))
	_, _ = writer.Write([]byte{0})
}

func writeLobbyFingerprintInt64(writer lobbyFingerprintWriter, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = writer.Write(encoded[:])
}

func normalizeLobbyError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrLobbyUnavailable, ErrInvalidLobbyRequest, ErrAdmissionNotFound,
		ErrAdmissionVersionConflict, ErrAdmissionTransition, ErrLobbyMemberNotFound,
		ErrLobbyMemberVersionConflict, ErrSpaceAccessDenied, ErrSpaceNotFound,
		ErrSourceUnavailable, ErrSpaceVersionConflict, ErrSpaceIdempotency,
		ErrRoomNotOpen, ErrRoomLocked, ErrParticipantConflict,
		featurecontrol.ErrInvalidControl, featurecontrol.ErrAccessDenied,
		featurecontrol.ErrTenantNotFound, featurecontrol.ErrFeatureDisabled,
		featurecontrol.ErrQuotaExceeded, featurecontrol.ErrUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	return ErrLobbyUnavailable
}
