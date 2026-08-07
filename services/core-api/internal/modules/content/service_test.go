package content

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/objectstorage"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var contentTestTime = time.Date(2026, time.August, 7, 9, 0, 0, 0, time.UTC)

func TestCreateIntentNormalizesAndBuildsOpaqueServerKey(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	service, err := NewService(repository, &fakeMetadataReader{})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return contentTestTime }
	access := testAccess()
	classID := uuid.New()
	requestID := uuid.New()
	checksum := bytes.Repeat([]byte{0x2a}, 32)
	repository.createResult = CreateIntentResult{Created: true}

	_, err = service.CreateIntent(context.Background(), access, CreateIntentInput{
		ClassID: classID, DisplayName: "  lesson.pdf  ",
		DeclaredMediaType: "APPLICATION/PDF", ExpectedSizeBytes: 42,
		ChecksumSHA256: hex.EncodeToString(checksum), ClientRequestID: requestID,
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}
	command := repository.createCommand
	if command.DisplayName != "lesson.pdf" || command.DeclaredMediaType != "application/pdf" ||
		command.ClassID != classID || command.ClientRequestID != requestID ||
		command.CreatedAt != contentTestTime || command.UploadExpiresAt != contentTestTime.Add(15*time.Minute) {
		t.Fatalf("unexpected normalized command: %+v", command)
	}
	wantKey := "tenants/" + access.TenantID.String() + "/files/" + command.ID.String() + "/original"
	if command.ObjectKey != wantKey || len(command.RequestFingerprint) != 32 ||
		!bytes.Equal(command.ChecksumSHA256, checksum) {
		t.Fatalf("unsafe or incomplete upload command: %+v", command)
	}
}

func TestCreateIntentRejectsUnsafeMetadataBeforeRepository(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	service, err := NewService(repository, &fakeMetadataReader{})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	valid := CreateIntentInput{
		ClassID: uuid.New(), DisplayName: "lesson.pdf", DeclaredMediaType: "application/pdf",
		ExpectedSizeBytes: 42, ChecksumSHA256: hex.EncodeToString(make([]byte, 32)),
		ClientRequestID: uuid.New(),
	}
	tests := []CreateIntentInput{valid, valid, valid, valid}
	tests[0].DisplayName = "lesson\nprivate.pdf"
	tests[1].DeclaredMediaType = "text/html; charset=utf-8"
	tests[2].ChecksumSHA256 = "ABCDEF"
	tests[3].ExpectedSizeBytes = 0
	for _, input := range tests {
		if _, err := service.CreateIntent(context.Background(), testAccess(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid input error=%v, want invalid file input", err)
		}
	}
	if repository.createCalls != 0 {
		t.Fatal("repository must not receive invalid file metadata")
	}
}

func TestFinalizeVerifiesProviderMetadataBeforeCommit(t *testing.T) {
	t.Parallel()
	checksum := bytes.Repeat([]byte{0x33}, 32)
	fileID := uuid.New()
	repository := &fakeRepository{
		finalizeTarget: FinalizeTarget{
			File: File{
				ID: fileID, Status: StatusPending, Version: 1,
				ExpectedSizeBytes: 128, DeclaredMediaType: "text/plain",
			},
			ObjectKey:      "tenants/tenant/files/file/original",
			ChecksumSHA256: checksum,
		},
		commitFile: File{ID: fileID, Status: StatusUploaded, Version: 2},
	}
	storage := &fakeMetadataReader{metadata: objectstorage.Metadata{
		ContentLength: 128, ContentType: "text/plain; charset=utf-8",
		ChecksumSHA256: checksum, ETag: "etag", VersionID: "version-1",
	}}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return contentTestTime }

	item, err := service.Finalize(
		context.Background(), testAccess(), fileID, FinalizeInput{ExpectedVersion: 1},
	)
	if err != nil {
		t.Fatalf("finalize file: %v", err)
	}
	if item.Status != StatusUploaded || repository.commitCalls != 1 ||
		repository.commitProof.ContentType != "text/plain" ||
		repository.commitProof.VersionID != "version-1" {
		t.Fatalf("unexpected finalize result/proof: item=%+v proof=%+v", item, repository.commitProof)
	}
}

func TestFinalizeFailsClosedForMissingVersionOrChecksum(t *testing.T) {
	t.Parallel()
	checksum := bytes.Repeat([]byte{0x44}, 32)
	fileID := uuid.New()
	for _, metadata := range []objectstorage.Metadata{
		{ContentLength: 64, ContentType: "application/pdf", ChecksumSHA256: checksum, ETag: "etag"},
		{ContentLength: 64, ContentType: "application/pdf", ETag: "etag", VersionID: "version"},
	} {
		repository := &fakeRepository{finalizeTarget: FinalizeTarget{
			File:      File{ID: fileID, Status: StatusPending, Version: 1, ExpectedSizeBytes: 64, DeclaredMediaType: "application/pdf"},
			ObjectKey: "tenants/tenant/files/file/original", ChecksumSHA256: checksum,
		}}
		service, err := NewService(repository, &fakeMetadataReader{metadata: metadata})
		if err != nil {
			t.Fatalf("create service: %v", err)
		}
		if _, err := service.Finalize(
			context.Background(), testAccess(), fileID, FinalizeInput{ExpectedVersion: 1},
		); !errors.Is(err, ErrStorageMismatch) {
			t.Fatalf("finalize error=%v, want storage mismatch", err)
		}
		if repository.commitCalls != 0 {
			t.Fatal("unverifiable provider metadata must not reach commit")
		}
	}
}

func TestFinalizeUploadedReplayDoesNotReadStorage(t *testing.T) {
	t.Parallel()
	fileID := uuid.New()
	repository := &fakeRepository{finalizeTarget: FinalizeTarget{
		File: File{ID: fileID, Status: StatusUploaded, Version: 2},
	}}
	storage := &fakeMetadataReader{err: errors.New("must not be called")}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	item, err := service.Finalize(
		context.Background(), testAccess(), fileID, FinalizeInput{ExpectedVersion: 1},
	)
	if err != nil || item.Status != StatusUploaded {
		t.Fatalf("finalize replay item=%+v err=%v", item, err)
	}
	if storage.calls != 0 || repository.commitCalls != 0 {
		t.Fatal("uploaded replay must not read storage or write database")
	}
}

func TestRepositoryFailureDoesNotExposePrivateDatabaseDetail(t *testing.T) {
	t.Parallel()
	service, err := NewService(
		&fakeRepository{getErr: errors.New("duplicate object_key tenants/private")},
		&fakeMetadataReader{},
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	_, err = service.Get(context.Background(), testAccess(), uuid.New())
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "object_key") {
		t.Fatalf("repository error was not redacted: %v", err)
	}
}

func testAccess() AccessContext {
	return AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleTeacher},
	}
}

type fakeRepository struct {
	createResult   CreateIntentResult
	createErr      error
	createCommand  CreateCommand
	createCalls    int
	getFile        File
	getErr         error
	finalizeTarget FinalizeTarget
	prepareErr     error
	commitFile     File
	commitErr      error
	commitProof    FinalizeProof
	commitCalls    int
}

func (repository *fakeRepository) CreateIntent(
	_ context.Context,
	_ AccessContext,
	command CreateCommand,
) (CreateIntentResult, error) {
	repository.createCalls++
	repository.createCommand = command
	return repository.createResult, repository.createErr
}

func (repository *fakeRepository) Get(
	context.Context, AccessContext, uuid.UUID,
) (File, error) {
	return repository.getFile, repository.getErr
}

func (repository *fakeRepository) PrepareFinalize(
	context.Context, AccessContext, uuid.UUID, int64, time.Time,
) (FinalizeTarget, error) {
	return repository.finalizeTarget, repository.prepareErr
}

func (repository *fakeRepository) CommitFinalize(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	proof FinalizeProof,
) (File, error) {
	repository.commitCalls++
	repository.commitProof = proof
	return repository.commitFile, repository.commitErr
}

type fakeMetadataReader struct {
	metadata objectstorage.Metadata
	err      error
	calls    int
}

func (reader *fakeMetadataReader) Head(context.Context, string) (objectstorage.Metadata, error) {
	reader.calls++
	return reader.metadata, reader.err
}
