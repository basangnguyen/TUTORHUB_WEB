package collaboration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	supportedFormatVersion    = "1"
	supportedEngineVersion    = "0.18.1"
	supportedAuthorityVersion = "13.6.27"
	maximumSnapshotBytes      = 64 * 1024 * 1024
)

var (
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)
	sha256Pattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type ServiceConfig struct {
	Clock func() time.Time
	NewID func() uuid.UUID
}

type Service struct {
	repository Repository
	spaces     SpaceAuthority
	grants     GrantBroker
	artifacts  ArtifactWorkflow
	clock      func() time.Time
	newID      func() uuid.UUID
}

func NewService(
	repository Repository,
	spaces SpaceAuthority,
	grants GrantBroker,
	artifacts ArtifactWorkflow,
	configs ...ServiceConfig,
) (*Service, error) {
	if repository == nil || spaces == nil {
		return nil, ErrUnavailable
	}
	config := ServiceConfig{Clock: time.Now, NewID: uuid.New}
	if len(configs) > 0 {
		if configs[0].Clock != nil {
			config.Clock = configs[0].Clock
		}
		if configs[0].NewID != nil {
			config.NewID = configs[0].NewID
		}
	}
	return &Service{
		repository: repository, spaces: spaces, grants: grants, artifacts: artifacts,
		clock: config.Clock, newID: config.NewID,
	}, nil
}

func (service *Service) Create(
	ctx context.Context,
	access AccessContext,
	input CreateInput,
) (CreateResult, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !validAccess(access) || input.MediaSpaceID == uuid.Nil ||
		!idempotencyPattern.MatchString(input.IdempotencyKey) {
		return CreateResult{}, ErrInvalidRequest
	}
	space, err := service.authorizeSpace(ctx, access, input.MediaSpaceID, true)
	if err != nil {
		return CreateResult{}, err
	}
	documentID := service.newID()
	command := CreateCommand{
		DocumentID: documentID, MediaSpaceID: space.ID,
		ProviderDocumentName: opaqueProviderDocumentName(documentID),
		IdempotencyKey:       input.IdempotencyKey,
		Fingerprint:          createFingerprint(input), OccurredAt: service.clock().UTC(),
	}
	result, err := service.repository.Create(ctx, access, command)
	if err != nil {
		return CreateResult{}, normalizeError(err)
	}
	result.Document, err = service.project(ctx, access, space, result.Document)
	if err != nil {
		return CreateResult{}, err
	}
	return result, nil
}

func (service *Service) Get(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (Document, error) {
	document, space, err := service.authorizedDocument(ctx, access, documentID, false)
	if err != nil {
		return Document{}, err
	}
	return service.project(ctx, access, space, document)
}

func (service *Service) Capabilities(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (ViewerCapabilities, error) {
	document, err := service.Get(ctx, access, documentID)
	if err != nil {
		return ViewerCapabilities{}, err
	}
	return document.Viewer, nil
}

func (service *Service) Open(ctx context.Context, access AccessContext, id uuid.UUID, input TransitionInput) (Document, error) {
	return service.transition(ctx, access, id, "open", input)
}

func (service *Service) Suspend(ctx context.Context, access AccessContext, id uuid.UUID, input TransitionInput) (Document, error) {
	return service.transition(ctx, access, id, "suspend", input)
}

func (service *Service) Resume(ctx context.Context, access AccessContext, id uuid.UUID, input TransitionInput) (Document, error) {
	return service.transition(ctx, access, id, "resume", input)
}

func (service *Service) Close(ctx context.Context, access AccessContext, id uuid.UUID, input TransitionInput) (Document, error) {
	return service.transition(ctx, access, id, "close", input)
}

func (service *Service) transition(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	operation string,
	input TransitionInput,
) (Document, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if documentID == uuid.Nil || input.ExpectedVersion < 1 ||
		!idempotencyPattern.MatchString(input.IdempotencyKey) {
		return Document{}, ErrInvalidRequest
	}
	document, space, err := service.authorizedDocument(ctx, access, documentID, true)
	if err != nil {
		return Document{}, err
	}
	command := TransitionCommand{
		DocumentID: document.ID, Operation: operation, ExpectedVersion: input.ExpectedVersion,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    transitionFingerprint(operation, document.ID, input.ExpectedVersion),
		OccurredAt:     service.clock().UTC(),
	}
	updated, err := service.repository.Transition(ctx, access, command)
	if err != nil {
		return Document{}, normalizeError(err)
	}
	return service.project(ctx, access, space, updated)
}

func (service *Service) ExchangeGrant(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	input GrantExchangeInput,
) (GrantCredential, error) {
	if !validCapability(input.Capability) || input.ExpectedGeneration < 1 ||
		input.ExpectedRevokeGeneration < 1 || !validOrigin(input.Origin) {
		return GrantCredential{}, ErrInvalidRequest
	}
	document, space, err := service.authorizedDocument(ctx, access, documentID, false)
	if err != nil {
		return GrantCredential{}, err
	}
	projected, err := service.project(ctx, access, space, document)
	if err != nil {
		return GrantCredential{}, err
	}
	if document.CurrentGeneration != input.ExpectedGeneration ||
		document.RevokeGeneration != input.ExpectedRevokeGeneration {
		return GrantCredential{}, ErrVersionConflict
	}
	if capabilityRank(input.Capability) > capabilityRank(projected.Viewer.Capability) {
		return GrantCredential{}, ErrNotFound
	}
	if service.grants == nil {
		return GrantCredential{}, ErrGrantUnavailable
	}
	credential, err := service.grants.Exchange(ctx, access, document, projected.Viewer.Capability, input)
	if err != nil {
		return GrantCredential{}, normalizeError(err)
	}
	return credential, nil
}

func (service *Service) ListSnapshots(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	limit int,
) (SnapshotList, error) {
	document, _, err := service.authorizedDocument(ctx, access, documentID, false)
	if err != nil {
		return SnapshotList{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return SnapshotList{}, ErrInvalidRequest
	}
	items, err := service.repository.ListSnapshots(ctx, access, document.ID, limit)
	if err != nil {
		return SnapshotList{}, normalizeError(err)
	}
	if items == nil {
		items = []Snapshot{}
	}
	return SnapshotList{Items: items}, nil
}

func (service *Service) CreateSnapshot(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	input SnapshotCreateInput,
) (ArtifactCommand, error) {
	document, _, err := service.authorizedDocument(ctx, access, documentID, true)
	if err != nil {
		return ArtifactCommand{}, err
	}
	if err := validateArtifactRequest(document, input.ExpectedGeneration, input.IdempotencyKey); err != nil {
		return ArtifactCommand{}, err
	}
	if service.artifacts == nil {
		return ArtifactCommand{}, ErrArtifactUnavailable
	}
	result, err := service.artifacts.RequestSnapshot(ctx, access, document, input)
	if err != nil {
		return ArtifactCommand{}, normalizeError(err)
	}
	return result, nil
}

func (service *Service) Export(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	input ExportInput,
) (ArtifactCommand, error) {
	document, _, err := service.authorizedDocument(ctx, access, documentID, true)
	if err != nil {
		return ArtifactCommand{}, err
	}
	if err := validateArtifactRequest(document, input.ExpectedGeneration, input.IdempotencyKey); err != nil {
		return ArtifactCommand{}, err
	}
	if service.artifacts == nil {
		return ArtifactCommand{}, ErrArtifactUnavailable
	}
	result, err := service.artifacts.RequestExport(ctx, access, document, input)
	if err != nil {
		return ArtifactCommand{}, normalizeError(err)
	}
	return result, nil
}

func (service *Service) ValidateImport(
	_ context.Context,
	manifest ImportManifest,
) (ImportValidation, error) {
	manifest.FormatVersion = strings.TrimSpace(manifest.FormatVersion)
	manifest.EngineVersion = strings.TrimSpace(manifest.EngineVersion)
	manifest.AuthorityVersion = strings.TrimSpace(manifest.AuthorityVersion)
	manifest.ContentSHA256 = strings.ToLower(strings.TrimSpace(manifest.ContentSHA256))
	problems := make([]string, 0, 6)
	if manifest.FormatVersion != supportedFormatVersion {
		problems = append(problems, "unsupported_format_version")
	}
	if manifest.EngineVersion != supportedEngineVersion {
		problems = append(problems, "unsupported_engine_version")
	}
	if manifest.AuthorityVersion != supportedAuthorityVersion {
		problems = append(problems, "unsupported_authority_version")
	}
	if manifest.SchemaVersion < 1 || manifest.SchemaVersion > 1000 {
		problems = append(problems, "invalid_schema_version")
	}
	if !sha256Pattern.MatchString(manifest.ContentSHA256) {
		problems = append(problems, "invalid_content_sha256")
	}
	if manifest.SizeBytes < 1 || manifest.SizeBytes > maximumSnapshotBytes {
		problems = append(problems, "invalid_size_bytes")
	}
	return ImportValidation{Valid: len(problems) == 0, Manifest: manifest, Problems: problems}, nil
}

func (service *Service) Restore(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	input RestoreInput,
) (Document, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if documentID == uuid.Nil || input.SnapshotID == uuid.Nil || input.ExpectedVersion < 1 ||
		input.ExpectedGeneration < 1 || !idempotencyPattern.MatchString(input.IdempotencyKey) {
		return Document{}, ErrInvalidRequest
	}
	document, space, err := service.authorizedDocument(ctx, access, documentID, true)
	if err != nil {
		return Document{}, err
	}
	if document.Version != input.ExpectedVersion || document.CurrentGeneration != input.ExpectedGeneration {
		return Document{}, ErrVersionConflict
	}
	nextID := service.newID()
	command := RestoreCommand{
		DocumentID: document.ID, SnapshotID: input.SnapshotID,
		ExpectedVersion: input.ExpectedVersion, ExpectedGeneration: input.ExpectedGeneration,
		ProviderDocumentName: opaqueProviderDocumentName(nextID),
		IdempotencyKey:       input.IdempotencyKey, Fingerprint: restoreFingerprint(document.ID, input),
		OccurredAt: service.clock().UTC(),
	}
	restored, err := service.repository.Restore(ctx, access, command)
	if err != nil {
		return Document{}, normalizeError(err)
	}
	return service.project(ctx, access, space, restored)
}

func (service *Service) authorizedDocument(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	manage bool,
) (Document, media.MediaSpace, error) {
	if !validAccess(access) || documentID == uuid.Nil {
		return Document{}, media.MediaSpace{}, ErrInvalidRequest
	}
	document, err := service.repository.Get(ctx, access, documentID)
	if err != nil {
		return Document{}, media.MediaSpace{}, normalizeConcealedError(err)
	}
	space, err := service.authorizeSpace(ctx, access, document.MediaSpaceID, manage)
	if err != nil {
		return Document{}, media.MediaSpace{}, err
	}
	return document, space, nil
}

func (service *Service) authorizeSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	manage bool,
) (media.MediaSpace, error) {
	space, err := service.spaces.GetSpace(ctx, media.AccessContext{
		TenantID: access.TenantID, ActorID: access.ActorID, SessionID: access.SessionID,
		MembershipActive: access.MembershipActive, OrganizationRoles: access.OrganizationRoles,
	}, spaceID)
	if err != nil {
		if errors.Is(err, media.ErrSpaceNotFound) || errors.Is(err, media.ErrSpaceAccessDenied) ||
			errors.Is(err, media.ErrSourceUnavailable) {
			return media.MediaSpace{}, ErrNotFound
		}
		return media.MediaSpace{}, ErrUnavailable
	}
	if manage && !canManageSpace(access, space) {
		return media.MediaSpace{}, ErrNotFound
	}
	return space, nil
}

func (service *Service) project(
	ctx context.Context,
	access AccessContext,
	space media.MediaSpace,
	document Document,
) (Document, error) {
	policies, err := service.repository.CapabilityPolicies(ctx, access, document.ID)
	if err != nil {
		return Document{}, normalizeConcealedError(err)
	}
	capability := policies[audienceFor(access, space)]
	if !validCapability(capability) {
		capability = CapabilityView
	}
	manage := canManageSpace(access, space)
	document.Viewer = ViewerCapabilities{
		Capability:        capability,
		CanOpen:           manage && (document.Status == DocumentCreated),
		CanSuspend:        manage && document.Status == DocumentOpen,
		CanResume:         manage && document.Status == DocumentSuspended,
		CanClose:          manage && document.Status != DocumentClosed,
		CanCreateSnapshot: manage && document.Status != DocumentClosed,
		CanExport:         manage,
		CanRestore:        manage && document.Status != DocumentClosed,
		CanExchangeGrant:  document.Status == DocumentOpen && capabilityRank(capability) >= capabilityRank(CapabilityView),
	}
	return document, nil
}

func validateArtifactRequest(document Document, expectedGeneration int64, key string) error {
	if expectedGeneration < 1 || document.CurrentGeneration != expectedGeneration ||
		!idempotencyPattern.MatchString(strings.TrimSpace(key)) {
		if expectedGeneration > 0 && document.CurrentGeneration != expectedGeneration {
			return ErrVersionConflict
		}
		return ErrInvalidRequest
	}
	return nil
}

func validAccess(access AccessContext) bool {
	return access.TenantID != uuid.Nil && access.ActorID != uuid.Nil && access.MembershipActive &&
		len(access.OrganizationRoles) > 0
}

func canManageSpace(access AccessContext, space media.MediaSpace) bool {
	for _, role := range access.OrganizationRoles {
		if role == policy.OrganizationRoleAdmin {
			return true
		}
	}
	operations := space.ViewerOperations
	return operations.CanStart || operations.CanEnd || operations.CanCancel ||
		operations.CanManageAdmissions || operations.CanManageInvites
}

func audienceFor(access AccessContext, space media.MediaSpace) Audience {
	for _, role := range access.OrganizationRoles {
		if role == policy.OrganizationRoleAdmin {
			return AudienceOrganizationAdmin
		}
	}
	switch space.ViewerRole {
	case media.InstanceRoleHost:
		return AudienceHost
	case media.InstanceRoleCoHost:
		return AudienceCoHost
	case media.InstanceRoleTeachingAssistant:
		return AudienceTeachingAssistant
	case media.InstanceRoleAttendee:
		return AudienceAttendee
	}
	if canManageSpace(access, space) {
		return AudienceHost
	}
	return AudienceAttendee
}

func validCapability(capability Capability) bool {
	return capability == CapabilityView || capability == CapabilityEdit || capability == CapabilityPresent
}

func capabilityRank(capability Capability) int {
	switch capability {
	case CapabilityPresent:
		return 3
	case CapabilityEdit:
		return 2
	case CapabilityView:
		return 1
	default:
		return 0
	}
}

func validOrigin(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Path != "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func createFingerprint(input CreateInput) []byte {
	digest := sha256.New()
	_, _ = digest.Write(input.MediaSpaceID[:])
	return digest.Sum(nil)
}

func transitionFingerprint(operation string, documentID uuid.UUID, expectedVersion int64) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(operation))
	_, _ = digest.Write(documentID[:])
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(expectedVersion))
	_, _ = digest.Write(encoded[:])
	return digest.Sum(nil)
}

func restoreFingerprint(documentID uuid.UUID, input RestoreInput) []byte {
	digest := sha256.New()
	_, _ = digest.Write(documentID[:])
	_, _ = digest.Write(input.SnapshotID[:])
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(input.ExpectedVersion))
	_, _ = digest.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(input.ExpectedGeneration))
	_, _ = digest.Write(encoded[:])
	return digest.Sum(nil)
}

func opaqueProviderDocumentName(id uuid.UUID) string {
	return "wb_" + strings.ReplaceAll(id.String(), "-", "")
}

func normalizeConcealedError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return normalizeError(err)
}

func normalizeError(err error) error {
	known := []error{
		ErrUnavailable, ErrInvalidRequest, ErrNotFound, ErrVersionConflict,
		ErrIdempotencyConflict, ErrTransitionConflict, ErrArtifactUnavailable, ErrGrantUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	return ErrUnavailable
}
