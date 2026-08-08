package content

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net/http"
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
			ObjectKey: "tenants/tenant/files/file/original",
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
		context.Background(), testAccess(), fileID,
		FinalizeInput{ExpectedVersion: 1, StorageVersionID: "version-1"},
	)
	if err != nil {
		t.Fatalf("finalize file: %v", err)
	}
	if item.Status != StatusUploaded || repository.commitCalls != 1 ||
		repository.commitProof.ContentType != "text/plain" ||
		repository.commitProof.VersionID != "version-1" || storage.headVersion != "version-1" {
		t.Fatalf("unexpected finalize result/proof: item=%+v proof=%+v", item, repository.commitProof)
	}
}

func TestFinalizeFailsClosedForMissingOrMismatchedVersion(t *testing.T) {
	t.Parallel()
	checksum := bytes.Repeat([]byte{0x44}, 32)
	fileID := uuid.New()
	for _, metadata := range []objectstorage.Metadata{
		{ContentLength: 64, ContentType: "application/pdf", ChecksumSHA256: checksum, ETag: "etag"},
		{ContentLength: 64, ContentType: "application/pdf", ETag: "etag", VersionID: "other-version"},
	} {
		repository := &fakeRepository{finalizeTarget: FinalizeTarget{
			File:      File{ID: fileID, Status: StatusPending, Version: 1, ExpectedSizeBytes: 64, DeclaredMediaType: "application/pdf"},
			ObjectKey: "tenants/tenant/files/file/original",
		}}
		service, err := NewService(repository, &fakeMetadataReader{metadata: metadata})
		if err != nil {
			t.Fatalf("create service: %v", err)
		}
		if _, err := service.Finalize(
			context.Background(), testAccess(), fileID,
			FinalizeInput{ExpectedVersion: 1, StorageVersionID: "version"},
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
		File: File{ID: fileID, Status: StatusUploaded, Version: 2}, StorageVersionID: "version-1",
	}}
	storage := &fakeMetadataReader{err: errors.New("must not be called")}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	item, err := service.Finalize(
		context.Background(), testAccess(), fileID,
		FinalizeInput{ExpectedVersion: 1, StorageVersionID: "version-1"},
	)
	if err != nil || item.Status != StatusUploaded {
		t.Fatalf("finalize replay item=%+v err=%v", item, err)
	}
	if storage.calls != 0 || repository.commitCalls != 0 {
		t.Fatal("uploaded replay must not read storage or write database")
	}
}

func TestTransferCapabilitiesBindPendingUploadAndReadyVersion(t *testing.T) {
	t.Parallel()
	fileID := uuid.New()
	repository := &fakeRepository{
		uploadTarget: UploadTarget{
			File: File{
				ID: fileID, Status: StatusPending, Version: 1,
				ExpectedSizeBytes: 42, DeclaredMediaType: "application/pdf",
				UploadExpiresAt: contentTestTime.Add(3 * time.Minute),
			},
			ObjectKey: "tenants/tenant/files/file/original",
		},
		downloadTarget: DownloadTarget{
			File:      File{ID: fileID, Status: StatusReady},
			ObjectKey: "tenants/tenant/files/file/original",
			VersionID: "version-1", StoredMediaType: "application/pdf",
		},
	}
	storage := &fakeMetadataReader{
		uploadPresigned: objectstorage.PresignedRequest{
			URL: "https://storage.example/upload?signature=redacted", Method: http.MethodPut,
			SignedHeader: http.Header{"Content-Type": []string{"application/pdf"}},
		},
		downloadPresigned: objectstorage.PresignedRequest{
			URL: "https://storage.example/download?signature=redacted", Method: http.MethodGet,
		},
	}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return contentTestTime }
	upload, err := service.IssueUploadCapability(
		context.Background(), testAccess(), fileID, UploadCapabilityInput{ExpectedVersion: 1},
	)
	if err != nil || upload.ExpiresAt != contentTestTime.Add(3*time.Minute) ||
		upload.ContentLengthBytes != 42 || upload.RequiredHeaders["Content-Type"] != "application/pdf" {
		t.Fatalf("unexpected upload capability=%+v err=%v", upload, err)
	}
	if storage.uploadInput.Key != repository.uploadTarget.ObjectKey ||
		storage.uploadInput.Expires != 3*time.Minute {
		t.Fatalf("unexpected upload presign input: %+v", storage.uploadInput)
	}
	download, err := service.IssueDownloadCapability(
		context.Background(), testAccess(), fileID,
	)
	if err != nil || download.Method != http.MethodGet ||
		download.ExpiresAt != contentTestTime.Add(2*time.Minute) {
		t.Fatalf("unexpected download capability=%+v err=%v", download, err)
	}
	if storage.downloadInput.VersionID != "version-1" ||
		storage.downloadInput.ContentDisposition != "attachment" {
		t.Fatalf("download must bind immutable version: %+v", storage.downloadInput)
	}
}

func TestMultipartLifecycleBindsSessionPartsAndImmutableCompletionVersion(t *testing.T) {
	t.Parallel()
	fileID, multipartID := uuid.New(), uuid.New()
	objectKey := "tenants/tenant/files/file/original"
	upload := MultipartUpload{
		ID: multipartID, FileID: fileID, Status: MultipartStatusActive,
		ExpiresAt: contentTestTime.Add(10 * time.Minute),
	}
	repository := &fakeRepository{
		multipartCreateTarget: UploadTarget{File: File{
			ID: fileID, Version: 1, Status: StatusPending,
			DeclaredMediaType: "application/pdf", ExpectedSizeBytes: 5_000_001,
			UploadExpiresAt: upload.ExpiresAt,
		}, ObjectKey: objectKey},
		multipartCreated: upload,
		multipartPartTarget: MultipartTarget{
			Upload: upload, File: File{ID: fileID, Version: 1, Status: StatusPending},
			ObjectKey: objectKey, ProviderUploadID: "provider-upload",
		},
		multipartCompleteTarget: MultipartTarget{
			Upload:    MultipartUpload{ID: multipartID, FileID: fileID, Status: MultipartStatusCompleting, ExpiresAt: upload.ExpiresAt},
			File:      File{ID: fileID, Version: 1, Status: StatusPending, ExpectedSizeBytes: 5_000_001},
			ObjectKey: objectKey, ProviderUploadID: "provider-upload",
			IssuedParts: []MultipartIssuedPart{
				{PartNumber: 1, ContentLengthBytes: 5_000_000},
				{PartNumber: 2, ContentLengthBytes: 1},
			},
		},
		multipartCommittedTarget: MultipartTarget{
			Upload:             MultipartUpload{ID: multipartID, FileID: fileID, Status: MultipartStatusCompleted, ExpiresAt: upload.ExpiresAt},
			CompletedVersionID: "version-2", CompletedETag: "multipart-etag",
		},
	}
	storage := &fakeMetadataReader{
		multipartCreateID: "provider-upload",
		multipartPartPresigned: objectstorage.PresignedRequest{
			URL: "https://storage.example/part?signature=redacted", Method: http.MethodPut,
		},
		multipartCompleteResult: objectstorage.MultipartCompleteResult{
			ETag: "multipart-etag", VersionID: "version-2",
		},
	}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return contentTestTime }
	created, err := service.CreateMultipart(
		context.Background(), testAccess(), fileID, CreateMultipartInput{ExpectedVersion: 1},
	)
	if err != nil || created.ID != multipartID || repository.multipartCreateCommand.ProviderUploadID != "provider-upload" {
		t.Fatalf("create multipart: upload=%+v command=%+v err=%v", created, repository.multipartCreateCommand, err)
	}
	capability, err := service.IssueMultipartPartCapability(
		context.Background(), testAccess(), fileID, multipartID, 1,
		MultipartPartCapabilityInput{ExpectedVersion: 1, ContentLengthBytes: 5_000_000},
	)
	if err != nil || capability.PartNumber != 1 || capability.ContentLengthBytes != 5_000_000 ||
		storage.multipartPartInput.UploadID != "provider-upload" {
		t.Fatalf("issue multipart part: capability=%+v input=%+v err=%v", capability, storage.multipartPartInput, err)
	}
	completed, err := service.CompleteMultipart(
		context.Background(), testAccess(), fileID, multipartID,
		CompleteMultipartInput{ExpectedVersion: 1, Parts: []MultipartCompletedPart{
			{PartNumber: 1, ETag: "part-1"}, {PartNumber: 2, ETag: "part-2"},
		}},
	)
	if err != nil || completed.StorageVersionID != "version-2" || completed.ETag != "multipart-etag" ||
		repository.multipartCompleteProof.VersionID != "version-2" ||
		len(storage.multipartCompleteInput.Parts) != 2 {
		t.Fatalf("complete multipart: result=%+v proof=%+v provider=%+v err=%v", completed, repository.multipartCompleteProof, storage.multipartCompleteInput, err)
	}
}

func TestMultipartCompleteRejectsNonContiguousOrWrongSizedManifestBeforeProvider(t *testing.T) {
	t.Parallel()
	fileID, multipartID := uuid.New(), uuid.New()
	repository := &fakeRepository{multipartCompleteTarget: MultipartTarget{
		Upload: MultipartUpload{
			ID: multipartID, FileID: fileID, Status: MultipartStatusCompleting,
			ExpiresAt: contentTestTime.Add(time.Minute),
		},
		File: File{ID: fileID, ExpectedSizeBytes: 5_000_002},
		IssuedParts: []MultipartIssuedPart{
			{PartNumber: 1, ContentLengthBytes: 5_000_000},
			{PartNumber: 2, ContentLengthBytes: 1},
		},
	}}
	storage := &fakeMetadataReader{}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return contentTestTime }
	_, err = service.CompleteMultipart(
		context.Background(), testAccess(), fileID, multipartID,
		CompleteMultipartInput{ExpectedVersion: 1, Parts: []MultipartCompletedPart{
			{PartNumber: 1, ETag: "part-1"}, {PartNumber: 2, ETag: "part-2"},
		}},
	)
	if !errors.Is(err, ErrMultipartConflict) || storage.multipartCompleteCalls != 0 || repository.multipartReleaseCalls != 1 {
		t.Fatalf("manifest error=%v provider_calls=%d release_calls=%d", err, storage.multipartCompleteCalls, repository.multipartReleaseCalls)
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
	createResult             CreateIntentResult
	createErr                error
	createCommand            CreateCommand
	createCalls              int
	getFile                  File
	getErr                   error
	uploadTarget             UploadTarget
	uploadErr                error
	downloadTarget           DownloadTarget
	downloadErr              error
	finalizeTarget           FinalizeTarget
	prepareErr               error
	commitFile               File
	commitErr                error
	commitProof              FinalizeProof
	commitCalls              int
	multipartCreateTarget    UploadTarget
	multipartCreated         MultipartUpload
	multipartCreateCommand   MultipartCreateCommand
	multipartPartTarget      MultipartTarget
	multipartCompleteTarget  MultipartTarget
	multipartCommittedTarget MultipartTarget
	multipartCompleteProof   MultipartCompleteProof
	multipartReleaseCalls    int
	multipartAbortTarget     MultipartTarget
	multipartAborted         MultipartUpload
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

func (repository *fakeRepository) PrepareUpload(
	context.Context, AccessContext, uuid.UUID, int64, time.Time,
) (UploadTarget, error) {
	return repository.uploadTarget, repository.uploadErr
}

func (repository *fakeRepository) PrepareDownload(
	context.Context, AccessContext, uuid.UUID,
) (DownloadTarget, error) {
	return repository.downloadTarget, repository.downloadErr
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

func (repository *fakeRepository) PrepareMultipartCreate(
	context.Context, AccessContext, uuid.UUID, int64, time.Time,
) (UploadTarget, error) {
	return repository.multipartCreateTarget, nil
}

func (repository *fakeRepository) CreateMultipart(
	_ context.Context, _ AccessContext, command MultipartCreateCommand,
) (MultipartUpload, error) {
	repository.multipartCreateCommand = command
	return repository.multipartCreated, nil
}

func (repository *fakeRepository) PrepareMultipartPart(
	context.Context, AccessContext, uuid.UUID, uuid.UUID, int64, int32, int64, time.Time,
) (MultipartTarget, error) {
	return repository.multipartPartTarget, nil
}

func (repository *fakeRepository) PrepareMultipartComplete(
	context.Context, AccessContext, uuid.UUID, uuid.UUID, int64, []MultipartCompletedPart, time.Time,
) (MultipartTarget, error) {
	return repository.multipartCompleteTarget, nil
}

func (repository *fakeRepository) ReleaseMultipartComplete(
	context.Context, AccessContext, uuid.UUID, uuid.UUID,
) error {
	repository.multipartReleaseCalls++
	return nil
}

func (repository *fakeRepository) CommitMultipartComplete(
	_ context.Context, _ AccessContext, _ uuid.UUID, _ uuid.UUID, proof MultipartCompleteProof,
) (MultipartTarget, error) {
	repository.multipartCompleteProof = proof
	return repository.multipartCommittedTarget, nil
}

func (repository *fakeRepository) PrepareMultipartAbort(
	context.Context, AccessContext, uuid.UUID, uuid.UUID, int64, time.Time,
) (MultipartTarget, error) {
	return repository.multipartAbortTarget, nil
}

func (repository *fakeRepository) CommitMultipartAbort(
	context.Context, AccessContext, uuid.UUID, uuid.UUID, MultipartStatus, time.Time,
) (MultipartUpload, error) {
	return repository.multipartAborted, nil
}

type fakeMetadataReader struct {
	metadata                objectstorage.Metadata
	err                     error
	calls                   int
	headVersion             string
	uploadInput             objectstorage.UploadPresignInput
	uploadPresigned         objectstorage.PresignedRequest
	uploadErr               error
	downloadInput           objectstorage.DownloadPresignInput
	downloadPresigned       objectstorage.PresignedRequest
	downloadErr             error
	multipartCreateID       string
	multipartCreateInput    objectstorage.MultipartCreateInput
	multipartPartInput      objectstorage.MultipartPartPresignInput
	multipartPartPresigned  objectstorage.PresignedRequest
	multipartCompleteInput  objectstorage.MultipartCompleteInput
	multipartCompleteResult objectstorage.MultipartCompleteResult
	multipartCompleteCalls  int
	multipartAbortInput     objectstorage.MultipartAbortInput
}

func (reader *fakeMetadataReader) Head(context.Context, string) (objectstorage.Metadata, error) {
	reader.calls++
	return reader.metadata, reader.err
}

func (reader *fakeMetadataReader) HeadVersion(
	_ context.Context, _ string, versionID string,
) (objectstorage.Metadata, error) {
	reader.calls++
	reader.headVersion = versionID
	return reader.metadata, reader.err
}

func (reader *fakeMetadataReader) PresignUpload(
	_ context.Context, input objectstorage.UploadPresignInput,
) (objectstorage.PresignedRequest, error) {
	reader.uploadInput = input
	return reader.uploadPresigned, reader.uploadErr
}

func (reader *fakeMetadataReader) PresignDownload(
	_ context.Context, input objectstorage.DownloadPresignInput,
) (objectstorage.PresignedRequest, error) {
	reader.downloadInput = input
	return reader.downloadPresigned, reader.downloadErr
}

func (reader *fakeMetadataReader) CreateMultipart(
	_ context.Context, input objectstorage.MultipartCreateInput,
) (string, error) {
	reader.multipartCreateInput = input
	if reader.multipartCreateID == "" {
		return "provider-upload", nil
	}
	return reader.multipartCreateID, nil
}

func (reader *fakeMetadataReader) PresignMultipartPart(
	_ context.Context, input objectstorage.MultipartPartPresignInput,
) (objectstorage.PresignedRequest, error) {
	reader.multipartPartInput = input
	if reader.multipartPartPresigned.URL == "" {
		return objectstorage.PresignedRequest{URL: "https://storage.example/part", Method: "PUT"}, nil
	}
	return reader.multipartPartPresigned, nil
}

func (reader *fakeMetadataReader) CompleteMultipart(
	_ context.Context, input objectstorage.MultipartCompleteInput,
) (objectstorage.MultipartCompleteResult, error) {
	reader.multipartCompleteCalls++
	reader.multipartCompleteInput = input
	if reader.multipartCompleteResult.VersionID == "" {
		return objectstorage.MultipartCompleteResult{ETag: "etag", VersionID: "version"}, nil
	}
	return reader.multipartCompleteResult, nil
}

func (reader *fakeMetadataReader) AbortMultipart(
	_ context.Context, input objectstorage.MultipartAbortInput,
) error {
	reader.multipartAbortInput = input
	return nil
}
