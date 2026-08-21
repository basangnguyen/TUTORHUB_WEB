package collaboration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var collaborationTestTime = time.Date(2026, 8, 21, 9, 30, 0, 0, time.UTC)

func TestServiceCreateUsesOpaqueProviderNameAndProjectsAuthority(t *testing.T) {
	t.Parallel()
	documentID, spaceID := uuid.New(), uuid.New()
	repository := &fakeRepository{
		createResult: CreateResult{Created: true, Document: Document{
			ID: documentID, MediaSpaceID: spaceID, Status: DocumentCreated,
			Version: 1, CurrentGeneration: 1, RevokeGeneration: 1,
		}},
		policies: defaultCapabilityPolicies(),
	}
	spaces := &fakeSpaceAuthority{space: manageableSpace(spaceID)}
	service := newTestService(t, repository, spaces, nil, nil, documentID)

	result, err := service.Create(context.Background(), testAccess(), CreateInput{
		MediaSpaceID: spaceID, IdempotencyKey: "whiteboard-create-0001",
	})
	if err != nil {
		t.Fatalf("create whiteboard: %v", err)
	}
	if !result.Created || repository.createCalls != 1 ||
		repository.createCommand.ProviderDocumentName != opaqueProviderDocumentName(documentID) ||
		repository.createCommand.ProviderDocumentName == documentID.String() ||
		len(repository.createCommand.Fingerprint) != 32 {
		t.Fatalf("unexpected create command/result: command=%+v result=%+v", repository.createCommand, result)
	}
	if result.Document.Viewer.Capability != CapabilityPresent || !result.Document.Viewer.CanOpen {
		t.Fatalf("unexpected create projection: %+v", result.Document.Viewer)
	}
}

func TestServiceConcealsForeignOrUnauthorizedResourceAsNotFound(t *testing.T) {
	t.Parallel()
	documentID, spaceID := uuid.New(), uuid.New()
	repository := &fakeRepository{document: Document{ID: documentID, MediaSpaceID: spaceID}}
	spaces := &fakeSpaceAuthority{err: media.ErrSpaceAccessDenied}
	service := newTestService(t, repository, spaces, nil, nil, uuid.New())

	_, err := service.Get(context.Background(), testAccess(), documentID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized whiteboard error = %v, want concealed not found", err)
	}

	repository.getError = ErrNotFound
	spaces.err = nil
	_, err = service.Get(context.Background(), testAccess(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tenant whiteboard error = %v, want not found", err)
	}
}

func TestServiceLifecycleRequiresCurrentManagementAuthorityAndStableCommand(t *testing.T) {
	t.Parallel()
	documentID, spaceID := uuid.New(), uuid.New()
	document := Document{
		ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
		Version: 4, CurrentGeneration: 2, RevokeGeneration: 3,
	}
	repository := &fakeRepository{
		document: document, transitionResult: Document{
			ID: documentID, MediaSpaceID: spaceID, Status: DocumentSuspended,
			Version: 5, CurrentGeneration: 2, RevokeGeneration: 4,
		}, policies: defaultCapabilityPolicies(),
	}
	spaces := &fakeSpaceAuthority{space: manageableSpace(spaceID)}
	broker := &fakeGrantBroker{}
	service := newTestService(t, repository, spaces, broker, nil, uuid.New())

	result, err := service.Suspend(context.Background(), testAccess(), documentID, TransitionInput{
		ExpectedVersion: 4, IdempotencyKey: "whiteboard-suspend-0001",
	})
	if err != nil {
		t.Fatalf("suspend whiteboard: %v", err)
	}
	if repository.transitionCommand.Operation != "suspend" ||
		repository.transitionCommand.ExpectedVersion != 4 ||
		len(repository.transitionCommand.Fingerprint) != 32 || result.Status != DocumentSuspended {
		t.Fatalf("unexpected lifecycle command/result: command=%+v result=%+v", repository.transitionCommand, result)
	}
	if len(broker.revoked) != 1 || broker.revoked[0] != documentID {
		t.Fatalf("suspend did not revoke active authority: %v", broker.revoked)
	}

	spaces.space = media.MediaSpace{ID: spaceID}
	_, err = service.Close(context.Background(), testAccess(), documentID, TransitionInput{
		ExpectedVersion: 4, IdempotencyKey: "whiteboard-close-000001",
	})
	if !errors.Is(err, ErrNotFound) || repository.transitionCalls != 1 {
		t.Fatalf("non-manager mutation must be concealed: err=%v calls=%d", err, repository.transitionCalls)
	}
}

func TestServiceGrantExchangeRequiresExactGenerationOriginAndCapability(t *testing.T) {
	t.Parallel()
	documentID, spaceID := uuid.New(), uuid.New()
	document := Document{
		ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
		Version: 2, CurrentGeneration: 3, RevokeGeneration: 5,
	}
	repository := &fakeRepository{document: document, policies: map[Audience]Capability{
		AudienceAttendee: CapabilityView,
	}}
	spaces := &fakeSpaceAuthority{space: media.MediaSpace{ID: spaceID}}
	broker := &fakeGrantBroker{credential: GrantCredential{Credential: "redacted", DocumentID: documentID}}
	service := newTestService(t, repository, spaces, broker, nil, uuid.New())
	access := testAccess()
	access.OrganizationRoles = []policy.OrganizationRole{policy.OrganizationRoleStudent}

	_, err := service.ExchangeGrant(context.Background(), access, documentID, GrantExchangeInput{
		Capability: CapabilityEdit, ExpectedGeneration: 3,
		ExpectedRevokeGeneration: 5, Origin: "https://app.example.test",
	})
	if !errors.Is(err, ErrNotFound) || broker.calls != 0 {
		t.Fatalf("capability escalation must be concealed: err=%v calls=%d", err, broker.calls)
	}

	_, err = service.ExchangeGrant(context.Background(), access, documentID, GrantExchangeInput{
		Capability: CapabilityView, ExpectedGeneration: 2,
		ExpectedRevokeGeneration: 5, Origin: "https://app.example.test",
	})
	if !errors.Is(err, ErrVersionConflict) || broker.calls != 0 {
		t.Fatalf("stale generation must fail before broker: err=%v calls=%d", err, broker.calls)
	}

	credential, err := service.ExchangeGrant(context.Background(), access, documentID, GrantExchangeInput{
		Capability: CapabilityView, ExpectedGeneration: 3,
		ExpectedRevokeGeneration: 5, Origin: "https://app.example.test",
	})
	if err != nil || broker.calls != 1 || credential.DocumentID != documentID ||
		broker.issueCapability != CapabilityView {
		t.Fatalf("valid exchange: credential=%+v err=%v calls=%d", credential, err, broker.calls)
	}
}

func TestServiceInternalGrantRevalidatesCurrentAuthority(t *testing.T) {
	t.Parallel()
	documentID, spaceID := uuid.New(), uuid.New()
	access := testAccess()
	document := Document{
		ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
		Version: 2, CurrentGeneration: 3, RevokeGeneration: 5,
	}
	scope := GrantScope{
		AuthorityLease: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ActorID:        access.ActorID, Capability: CapabilityEdit, DocumentID: documentID,
		Generation: 3, ProviderDocumentName: "wb_test_provider_document_0001",
		SessionID: access.SessionID, TenantID: access.TenantID, WriterFence: 5,
	}
	repository := &fakeRepository{document: document, policies: defaultCapabilityPolicies()}
	spaces := &fakeSpaceAuthority{space: manageableSpace(spaceID)}
	broker := &fakeGrantBroker{resolution: GrantResolution{Access: access, Scope: scope}}
	service := newTestService(t, repository, spaces, broker, nil, uuid.New())

	consumed, err := service.ConsumeGrant(context.Background(), GrantConsumeInput{
		Credential: "one-time-grant-that-is-long-enough", Origin: "https://app.example.test",
		ProviderDocumentName: scope.ProviderDocumentName,
	})
	if err != nil || consumed != scope {
		t.Fatalf("consume current grant: scope=%+v err=%v", consumed, err)
	}
	valid, err := service.ValidateGrant(context.Background(), GrantValidationInput{
		Origin: "https://app.example.test", Scope: scope,
	})
	if err != nil || !valid {
		t.Fatalf("validate current authority: valid=%v err=%v", valid, err)
	}

	repository.document.RevokeGeneration++
	valid, err = service.ValidateGrant(context.Background(), GrantValidationInput{
		Origin: "https://app.example.test", Scope: scope,
	})
	if err != nil || valid || len(broker.invalidated) != 1 || broker.invalidated[0] != scope.AuthorityLease {
		t.Fatalf("stale authority valid=%v err=%v invalidated=%v", valid, err, broker.invalidated)
	}
}

func TestServiceProjectsExactMediaAudienceAndAuthorizesBeforeFailClosedDependencies(t *testing.T) {
	t.Parallel()
	documentID, spaceID := uuid.New(), uuid.New()
	document := Document{
		ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
		Version: 2, CurrentGeneration: 3, RevokeGeneration: 5,
	}
	repository := &fakeRepository{document: document, policies: defaultCapabilityPolicies()}
	spaces := &fakeSpaceAuthority{space: media.MediaSpace{
		ID: spaceID, ViewerRole: media.InstanceRoleTeachingAssistant,
		ViewerOperations: media.ViewerOperations{CanManageAdmissions: true},
	}}
	service := newTestService(t, repository, spaces, nil, nil, uuid.New())

	projected, err := service.Get(context.Background(), testAccess(), documentID)
	if err != nil || projected.Viewer.Capability != CapabilityEdit {
		t.Fatalf("teaching assistant projection: document=%+v err=%v", projected, err)
	}
	_, err = service.ExchangeGrant(context.Background(), testAccess(), documentID, GrantExchangeInput{
		Capability: CapabilityEdit, ExpectedGeneration: 3,
		ExpectedRevokeGeneration: 5, Origin: "https://app.example.test",
	})
	if !errors.Is(err, ErrGrantUnavailable) {
		t.Fatalf("authorized grant without broker must fail closed: %v", err)
	}
	_, err = service.Export(context.Background(), testAccess(), documentID, ExportInput{
		ExpectedGeneration: 3, IdempotencyKey: "whiteboard-export-failclosed-0001",
	})
	if !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("authorized export without workflow must fail closed: %v", err)
	}

	spaces.err = media.ErrSpaceAccessDenied
	_, err = service.ExchangeGrant(context.Background(), testAccess(), documentID, GrantExchangeInput{
		Capability: CapabilityEdit, ExpectedGeneration: 3,
		ExpectedRevokeGeneration: 5, Origin: "https://app.example.test",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized grant must be concealed before dependency state: %v", err)
	}
	_, err = service.Export(context.Background(), testAccess(), documentID, ExportInput{
		ExpectedGeneration: 3, IdempotencyKey: "whiteboard-export-failclosed-0002",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized export must be concealed before dependency state: %v", err)
	}
}

func TestServiceArtifactAndRestoreRequireCurrentAuthority(t *testing.T) {
	t.Parallel()
	documentID, spaceID, snapshotID := uuid.New(), uuid.New(), uuid.New()
	document := Document{
		ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
		Version: 7, CurrentGeneration: 4, RevokeGeneration: 4,
	}
	repository := &fakeRepository{
		document: document, restoreResult: Document{
			ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
			Version: 8, CurrentGeneration: 5, RevokeGeneration: 5,
		}, policies: defaultCapabilityPolicies(),
	}
	workflow := &fakeArtifactWorkflow{result: ArtifactCommand{
		ID: uuid.New(), DocumentID: documentID, Generation: 4, Status: ArtifactCommandAccepted,
	}}
	spaces := &fakeSpaceAuthority{space: manageableSpace(spaceID)}
	broker := &fakeGrantBroker{}
	service := newTestService(t, repository, spaces, broker, workflow, uuid.New())

	_, err := service.Export(context.Background(), testAccess(), documentID, ExportInput{
		ExpectedGeneration: 3, IdempotencyKey: "whiteboard-export-00001",
	})
	if !errors.Is(err, ErrVersionConflict) || workflow.exportCalls != 0 {
		t.Fatalf("stale export must fail: err=%v calls=%d", err, workflow.exportCalls)
	}

	if _, err := service.Export(context.Background(), testAccess(), documentID, ExportInput{
		ExpectedGeneration: 4, IdempotencyKey: "whiteboard-export-00002",
	}); err != nil || workflow.exportCalls != 1 {
		t.Fatalf("current export failed: err=%v calls=%d", err, workflow.exportCalls)
	}

	restored, err := service.Restore(context.Background(), testAccess(), documentID, RestoreInput{
		SnapshotID: snapshotID, ExpectedVersion: 7, ExpectedGeneration: 4,
		IdempotencyKey: "whiteboard-restore-0001",
	})
	if err != nil || restored.CurrentGeneration != 5 ||
		repository.restoreCommand.SnapshotID != snapshotID ||
		len(repository.restoreCommand.Fingerprint) != 32 {
		t.Fatalf("restore result=%+v command=%+v err=%v", restored, repository.restoreCommand, err)
	}
	if len(broker.revoked) != 1 || broker.revoked[0] != documentID {
		t.Fatalf("restore did not revoke previous generation: %v", broker.revoked)
	}
}

func TestServiceImportValidationIsStrictAndBounded(t *testing.T) {
	t.Parallel()
	service := newTestService(t, &fakeRepository{}, &fakeSpaceAuthority{}, nil, nil, uuid.New())
	validHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	valid, err := service.ValidateImport(context.Background(), ImportManifest{
		FormatVersion: "1", EngineVersion: "0.18.1", AuthorityVersion: "13.6.27",
		SchemaVersion: 1, ContentSHA256: validHash, SizeBytes: 1024,
	})
	if err != nil || !valid.Valid || len(valid.Problems) != 0 {
		t.Fatalf("valid import manifest rejected: %+v err=%v", valid, err)
	}

	invalid, err := service.ValidateImport(context.Background(), ImportManifest{
		FormatVersion: "2", EngineVersion: "latest", AuthorityVersion: "unknown",
		SchemaVersion: 0, ContentSHA256: "not-a-hash", SizeBytes: maximumSnapshotBytes + 1,
	})
	if err != nil || invalid.Valid || len(invalid.Problems) != 6 {
		t.Fatalf("invalid import manifest result: %+v err=%v", invalid, err)
	}
}

func newTestService(
	t *testing.T,
	repository Repository,
	spaces SpaceAuthority,
	grants GrantBroker,
	artifacts ArtifactWorkflow,
	newID uuid.UUID,
) *Service {
	t.Helper()
	service, err := NewService(repository, spaces, grants, artifacts, ServiceConfig{
		Clock: func() time.Time { return collaborationTestTime },
		NewID: func() uuid.UUID { return newID },
	})
	if err != nil {
		t.Fatalf("new collaboration service: %v", err)
	}
	return service
}

func testAccess() AccessContext {
	return AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), SessionID: uuid.New(), MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleTeacher},
	}
}

func manageableSpace(id uuid.UUID) media.MediaSpace {
	return media.MediaSpace{
		ID: id, ViewerRole: media.InstanceRoleHost,
		ViewerOperations: media.ViewerOperations{CanEnd: true},
	}
}

type fakeRepository struct {
	createCalls       int
	transitionCalls   int
	restoreCalls      int
	createCommand     CreateCommand
	transitionCommand TransitionCommand
	restoreCommand    RestoreCommand
	createResult      CreateResult
	document          Document
	grantAuthority    GrantAuthority
	transitionResult  Document
	restoreResult     Document
	policies          map[Audience]Capability
	snapshots         []Snapshot
	createError       error
	getError          error
	transitionError   error
	restoreError      error
}

func (repository *fakeRepository) Create(_ context.Context, _ AccessContext, command CreateCommand) (CreateResult, error) {
	repository.createCalls++
	repository.createCommand = command
	return repository.createResult, repository.createError
}

func (repository *fakeRepository) Get(_ context.Context, _ AccessContext, _ uuid.UUID) (Document, error) {
	return repository.document, repository.getError
}

func (repository *fakeRepository) GrantAuthority(_ context.Context, _ AccessContext, _ uuid.UUID) (GrantAuthority, error) {
	if repository.grantAuthority.Document.ID == uuid.Nil {
		return GrantAuthority{
			Document: repository.document, ProviderDocumentName: "wb_test_provider_document_0001",
			WriterFence: repository.document.RevokeGeneration,
		}, repository.getError
	}
	return repository.grantAuthority, repository.getError
}

func (repository *fakeRepository) Transition(_ context.Context, _ AccessContext, command TransitionCommand) (Document, error) {
	repository.transitionCalls++
	repository.transitionCommand = command
	return repository.transitionResult, repository.transitionError
}

func (repository *fakeRepository) CapabilityPolicies(_ context.Context, _ AccessContext, _ uuid.UUID) (map[Audience]Capability, error) {
	return repository.policies, nil
}

func (repository *fakeRepository) ListSnapshots(_ context.Context, _ AccessContext, _ uuid.UUID, _ int) ([]Snapshot, error) {
	return repository.snapshots, nil
}

func (repository *fakeRepository) Restore(_ context.Context, _ AccessContext, command RestoreCommand) (Document, error) {
	repository.restoreCalls++
	repository.restoreCommand = command
	return repository.restoreResult, repository.restoreError
}

type fakeSpaceAuthority struct {
	space media.MediaSpace
	err   error
}

func (authority *fakeSpaceAuthority) GetSpace(_ context.Context, _ media.AccessContext, _ uuid.UUID) (media.MediaSpace, error) {
	return authority.space, authority.err
}

type fakeGrantBroker struct {
	calls           int
	credential      GrantCredential
	resolution      GrantResolution
	issueCapability Capability
	invalidated     []string
	revoked         []uuid.UUID
}

func (broker *fakeGrantBroker) Issue(_ context.Context, _ AccessContext, _ GrantAuthority, capability Capability, _ GrantExchangeInput) (GrantCredential, error) {
	broker.calls++
	broker.issueCapability = capability
	return broker.credential, nil
}

func (broker *fakeGrantBroker) Consume(_ context.Context, _ GrantConsumeInput) (GrantResolution, error) {
	return broker.resolution, nil
}

func (broker *fakeGrantBroker) Validate(_ context.Context, _ GrantValidationInput) (GrantResolution, error) {
	return broker.resolution, nil
}

func (broker *fakeGrantBroker) InvalidateLease(lease string) {
	broker.invalidated = append(broker.invalidated, lease)
}

func (broker *fakeGrantBroker) Revoke(documentID uuid.UUID) {
	broker.revoked = append(broker.revoked, documentID)
}

type fakeArtifactWorkflow struct {
	snapshotCalls int
	exportCalls   int
	result        ArtifactCommand
}

func (workflow *fakeArtifactWorkflow) RequestSnapshot(_ context.Context, _ AccessContext, _ Document, _ SnapshotCreateInput) (ArtifactCommand, error) {
	workflow.snapshotCalls++
	return workflow.result, nil
}

func (workflow *fakeArtifactWorkflow) RequestExport(_ context.Context, _ AccessContext, _ Document, _ ExportInput) (ArtifactCommand, error) {
	workflow.exportCalls++
	return workflow.result, nil
}
