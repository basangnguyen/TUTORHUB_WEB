package content

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/objectstorage"
)

const (
	uploadIntentTTL          = 15 * time.Minute
	uploadCapabilityTTL      = 5 * time.Minute
	downloadCapabilityTTL    = 2 * time.Minute
	maximumDisplayNameRunes  = 255
	maximumDisplayNameBytes  = 1024
	maximumMediaTypeBytes    = 127
	maximumStorageProofBytes = 512
	maximumProviderIDBytes   = 2048
	minimumMultipartPartSize = int64(5_000_000)
	maximumMultipartPartSize = int64(5_368_709_120)
)

var mediaTypeToken = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)

type Service struct {
	repository Repository
	storage    ObjectStorage
	now        func() time.Time
}

func NewService(repository Repository, storage ObjectStorage) (*Service, error) {
	if repository == nil || storage == nil {
		return nil, fmt.Errorf("content repository and object metadata reader are required")
	}
	return &Service{repository: repository, storage: storage, now: time.Now}, nil
}

func (service *Service) IssueUploadCapability(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	input UploadCapabilityInput,
) (UploadCapability, error) {
	if service == nil {
		return UploadCapability{}, fmt.Errorf("issue file upload capability: service is unavailable")
	}
	if !validAccess(access) {
		return UploadCapability{}, ErrAccessDenied
	}
	if fileID == uuid.Nil {
		return UploadCapability{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return UploadCapability{}, ErrInvalidInput
	}
	now := service.now().UTC()
	target, err := service.repository.PrepareUpload(
		ctx, access, fileID, input.ExpectedVersion, now,
	)
	if err != nil {
		return UploadCapability{}, normalizeRepositoryError(err)
	}
	ttl := minDuration(uploadCapabilityTTL, target.File.UploadExpiresAt.Sub(now))
	if ttl <= 0 {
		return UploadCapability{}, ErrIntentExpired
	}
	presigned, err := service.storage.PresignUpload(ctx, objectstorage.UploadPresignInput{
		Key: target.ObjectKey, ContentLength: target.File.ExpectedSizeBytes,
		ContentType: target.File.DeclaredMediaType, Expires: ttl,
	})
	if err != nil {
		return UploadCapability{}, fmt.Errorf("%w: presign upload", ErrStorageUnavailable)
	}
	if presigned.Method != http.MethodPut || strings.TrimSpace(presigned.URL) == "" ||
		!strings.EqualFold(
			strings.TrimSpace(presigned.SignedHeader.Get("Content-Type")),
			target.File.DeclaredMediaType,
		) {
		return UploadCapability{}, fmt.Errorf("%w: invalid upload capability", ErrStorageUnavailable)
	}
	return UploadCapability{
		Method: presigned.Method, URL: presigned.URL, ExpiresAt: now.Add(ttl),
		ContentLengthBytes: target.File.ExpectedSizeBytes,
		RequiredHeaders:    map[string]string{"Content-Type": target.File.DeclaredMediaType},
	}, nil
}

func (service *Service) IssueDownloadCapability(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
) (DownloadCapability, error) {
	if service == nil {
		return DownloadCapability{}, fmt.Errorf("issue file download capability: service is unavailable")
	}
	if !validAccess(access) {
		return DownloadCapability{}, ErrAccessDenied
	}
	if fileID == uuid.Nil {
		return DownloadCapability{}, ErrNotFound
	}
	target, err := service.repository.PrepareDownload(ctx, access, fileID)
	if err != nil {
		return DownloadCapability{}, normalizeRepositoryError(err)
	}
	presigned, err := service.storage.PresignDownload(ctx, objectstorage.DownloadPresignInput{
		Key: target.ObjectKey, VersionID: target.VersionID,
		ContentType: target.StoredMediaType, ContentDisposition: "attachment",
		Expires: downloadCapabilityTTL,
	})
	if err != nil {
		return DownloadCapability{}, fmt.Errorf("%w: presign download", ErrStorageUnavailable)
	}
	if presigned.Method != http.MethodGet || strings.TrimSpace(presigned.URL) == "" {
		return DownloadCapability{}, fmt.Errorf("%w: invalid download capability", ErrStorageUnavailable)
	}
	now := service.now().UTC()
	return DownloadCapability{
		Method: presigned.Method, URL: presigned.URL, ExpiresAt: now.Add(downloadCapabilityTTL),
	}, nil
}

func (service *Service) CreateMultipart(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	input CreateMultipartInput,
) (MultipartUpload, error) {
	if service == nil {
		return MultipartUpload{}, fmt.Errorf("create multipart upload: service is unavailable")
	}
	if !validAccess(access) {
		return MultipartUpload{}, ErrAccessDenied
	}
	if fileID == uuid.Nil {
		return MultipartUpload{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return MultipartUpload{}, ErrInvalidInput
	}
	now := service.now().UTC()
	target, err := service.repository.PrepareMultipartCreate(
		ctx, access, fileID, input.ExpectedVersion, now,
	)
	if err != nil {
		return MultipartUpload{}, normalizeRepositoryError(err)
	}
	providerUploadID, err := service.storage.CreateMultipart(ctx, objectstorage.MultipartCreateInput{
		Key: target.ObjectKey, ContentType: target.File.DeclaredMediaType,
	})
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("%w: create multipart upload", ErrStorageUnavailable)
	}
	providerUploadID, err = normalizeProviderUploadID(providerUploadID)
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("%w: invalid multipart provider response", ErrStorageUnavailable)
	}
	command := MultipartCreateCommand{
		ID: uuid.New(), FileID: fileID, ProviderUploadID: providerUploadID,
		ExpectedVersion: input.ExpectedVersion, ExpiresAt: target.File.UploadExpiresAt,
		CreatedAt: now,
	}
	upload, err := service.repository.CreateMultipart(ctx, access, command)
	if err != nil {
		_ = service.storage.AbortMultipart(ctx, objectstorage.MultipartAbortInput{
			Key: target.ObjectKey, UploadID: providerUploadID,
		})
		return MultipartUpload{}, normalizeRepositoryError(err)
	}
	return upload, nil
}

func (service *Service) IssueMultipartPartCapability(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	partNumber int32,
	input MultipartPartCapabilityInput,
) (MultipartPartCapability, error) {
	if service == nil {
		return MultipartPartCapability{}, fmt.Errorf("issue multipart part capability: service is unavailable")
	}
	if !validAccess(access) {
		return MultipartPartCapability{}, ErrAccessDenied
	}
	if fileID == uuid.Nil || multipartID == uuid.Nil {
		return MultipartPartCapability{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 || partNumber < 1 || partNumber > 10000 ||
		input.ContentLengthBytes < 1 || input.ContentLengthBytes > maximumMultipartPartSize {
		return MultipartPartCapability{}, ErrInvalidInput
	}
	now := service.now().UTC()
	target, err := service.repository.PrepareMultipartPart(
		ctx, access, fileID, multipartID, input.ExpectedVersion,
		partNumber, input.ContentLengthBytes, now,
	)
	if err != nil {
		return MultipartPartCapability{}, normalizeRepositoryError(err)
	}
	ttl := minDuration(uploadCapabilityTTL, target.Upload.ExpiresAt.Sub(now))
	if ttl <= 0 {
		return MultipartPartCapability{}, ErrMultipartExpired
	}
	presigned, err := service.storage.PresignMultipartPart(ctx, objectstorage.MultipartPartPresignInput{
		Key: target.ObjectKey, UploadID: target.ProviderUploadID,
		PartNumber: partNumber, ContentLength: input.ContentLengthBytes, Expires: ttl,
	})
	if err != nil {
		return MultipartPartCapability{}, fmt.Errorf("%w: presign multipart part", ErrStorageUnavailable)
	}
	if presigned.Method != http.MethodPut || strings.TrimSpace(presigned.URL) == "" {
		return MultipartPartCapability{}, fmt.Errorf("%w: invalid multipart capability", ErrStorageUnavailable)
	}
	return MultipartPartCapability{
		Method: presigned.Method, URL: presigned.URL, ExpiresAt: now.Add(ttl),
		PartNumber: partNumber, ContentLengthBytes: input.ContentLengthBytes,
		RequiredHeaders: map[string]string{},
	}, nil
}

func (service *Service) CompleteMultipart(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	input CompleteMultipartInput,
) (CompleteMultipartResult, error) {
	if service == nil {
		return CompleteMultipartResult{}, fmt.Errorf("complete multipart upload: service is unavailable")
	}
	if !validAccess(access) {
		return CompleteMultipartResult{}, ErrAccessDenied
	}
	if fileID == uuid.Nil || multipartID == uuid.Nil {
		return CompleteMultipartResult{}, ErrNotFound
	}
	parts, err := normalizeCompletedParts(input.Parts)
	if input.ExpectedVersion < 1 || err != nil {
		return CompleteMultipartResult{}, ErrInvalidInput
	}
	now := service.now().UTC()
	target, err := service.repository.PrepareMultipartComplete(
		ctx, access, fileID, multipartID, input.ExpectedVersion, parts, now,
	)
	if err != nil {
		return CompleteMultipartResult{}, normalizeRepositoryError(err)
	}
	if target.Upload.Status == MultipartStatusCompleted {
		return CompleteMultipartResult{
			Upload: target.Upload, StorageVersionID: target.CompletedVersionID,
			ETag: target.CompletedETag,
		}, nil
	}
	if err := validateMultipartManifest(target.File.ExpectedSizeBytes, target.IssuedParts, parts); err != nil {
		_ = service.repository.ReleaseMultipartComplete(ctx, access, fileID, multipartID)
		return CompleteMultipartResult{}, err
	}
	providerParts := make([]objectstorage.CompletedPart, len(parts))
	for index, part := range parts {
		providerParts[index] = objectstorage.CompletedPart{PartNumber: part.PartNumber, ETag: part.ETag}
	}
	completed, err := service.storage.CompleteMultipart(ctx, objectstorage.MultipartCompleteInput{
		Key: target.ObjectKey, UploadID: target.ProviderUploadID, Parts: providerParts,
	})
	if err != nil {
		_ = service.repository.ReleaseMultipartComplete(ctx, access, fileID, multipartID)
		return CompleteMultipartResult{}, fmt.Errorf("%w: complete multipart upload", ErrStorageUnavailable)
	}
	versionID, versionErr := normalizeStorageProof(completed.VersionID)
	etag, etagErr := normalizeStorageProof(completed.ETag)
	if versionErr != nil || etagErr != nil {
		return CompleteMultipartResult{}, fmt.Errorf("%w: invalid multipart completion proof", ErrStorageUnavailable)
	}
	committed, err := service.repository.CommitMultipartComplete(
		ctx, access, fileID, multipartID,
		MultipartCompleteProof{VersionID: versionID, ETag: etag, CompletedAt: now},
	)
	if err != nil {
		return CompleteMultipartResult{}, normalizeRepositoryError(err)
	}
	return CompleteMultipartResult{
		Upload: committed.Upload, StorageVersionID: committed.CompletedVersionID,
		ETag: committed.CompletedETag,
	}, nil
}

func (service *Service) AbortMultipart(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	input AbortMultipartInput,
) (MultipartUpload, error) {
	if service == nil {
		return MultipartUpload{}, fmt.Errorf("abort multipart upload: service is unavailable")
	}
	if !validAccess(access) {
		return MultipartUpload{}, ErrAccessDenied
	}
	if fileID == uuid.Nil || multipartID == uuid.Nil {
		return MultipartUpload{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return MultipartUpload{}, ErrInvalidInput
	}
	now := service.now().UTC()
	target, err := service.repository.PrepareMultipartAbort(
		ctx, access, fileID, multipartID, input.ExpectedVersion, now,
	)
	if err != nil {
		return MultipartUpload{}, normalizeRepositoryError(err)
	}
	if target.Upload.Status == MultipartStatusAborted || target.Upload.Status == MultipartStatusExpired {
		return target.Upload, nil
	}
	if err := service.storage.AbortMultipart(ctx, objectstorage.MultipartAbortInput{
		Key: target.ObjectKey, UploadID: target.ProviderUploadID,
	}); err != nil {
		return MultipartUpload{}, fmt.Errorf("%w: abort multipart upload", ErrStorageUnavailable)
	}
	status := MultipartStatusAborted
	if !now.Before(target.Upload.ExpiresAt) {
		status = MultipartStatusExpired
	}
	upload, err := service.repository.CommitMultipartAbort(
		ctx, access, fileID, multipartID, status, now,
	)
	return upload, normalizeRepositoryError(err)
}

func (service *Service) CreateIntent(
	ctx context.Context,
	access AccessContext,
	input CreateIntentInput,
) (CreateIntentResult, error) {
	if service == nil {
		return CreateIntentResult{}, fmt.Errorf("create file upload intent: service is unavailable")
	}
	if !validAccess(access) {
		return CreateIntentResult{}, ErrAccessDenied
	}
	if input.ClassID == uuid.Nil || input.ClientRequestID == uuid.Nil || input.ExpectedSizeBytes < 1 {
		return CreateIntentResult{}, ErrInvalidInput
	}
	displayName, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return CreateIntentResult{}, err
	}
	mediaType, err := normalizeMediaType(input.DeclaredMediaType)
	if err != nil {
		return CreateIntentResult{}, err
	}
	checksum, err := decodeChecksum(input.ChecksumSHA256)
	if err != nil {
		return CreateIntentResult{}, err
	}
	fileID := uuid.New()
	now := service.now().UTC()
	command := CreateCommand{
		ID: fileID, ClassID: input.ClassID, DisplayName: displayName,
		DeclaredMediaType: mediaType, ExpectedSizeBytes: input.ExpectedSizeBytes,
		ChecksumSHA256: checksum, ClientRequestID: input.ClientRequestID,
		ObjectKey: fmt.Sprintf("tenants/%s/files/%s/original", access.TenantID, fileID),
		CreatedAt: now, UploadExpiresAt: now.Add(uploadIntentTTL),
	}
	command.RequestFingerprint = requestFingerprint(command)
	result, err := service.repository.CreateIntent(ctx, access, command)
	return result, normalizeRepositoryError(err)
}

func (service *Service) Get(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
) (File, error) {
	if service == nil {
		return File{}, fmt.Errorf("get file metadata: service is unavailable")
	}
	if !validAccess(access) {
		return File{}, ErrAccessDenied
	}
	if fileID == uuid.Nil {
		return File{}, ErrNotFound
	}
	item, err := service.repository.Get(ctx, access, fileID)
	return item, normalizeRepositoryError(err)
}

func (service *Service) Finalize(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	input FinalizeInput,
) (File, error) {
	if service == nil {
		return File{}, fmt.Errorf("finalize file upload: service is unavailable")
	}
	if !validAccess(access) {
		return File{}, ErrAccessDenied
	}
	if fileID == uuid.Nil {
		return File{}, ErrNotFound
	}
	storageVersionID, err := normalizeStorageProof(input.StorageVersionID)
	if input.ExpectedVersion < 1 || err != nil {
		return File{}, ErrInvalidInput
	}
	now := service.now().UTC()
	target, err := service.repository.PrepareFinalize(
		ctx, access, fileID, input.ExpectedVersion, now,
	)
	if err != nil {
		return File{}, normalizeRepositoryError(err)
	}
	if target.File.Status == StatusUploaded {
		if target.StorageVersionID != storageVersionID {
			return File{}, ErrVersionConflict
		}
		return target.File, nil
	}
	metadata, err := service.storage.HeadVersion(ctx, target.ObjectKey, storageVersionID)
	if err != nil {
		return File{}, fmt.Errorf("%w: read object metadata", ErrStorageUnavailable)
	}
	mediaType, err := normalizeObservedMediaType(metadata.ContentType)
	if err != nil || metadata.ContentLength != target.File.ExpectedSizeBytes ||
		mediaType != target.File.DeclaredMediaType ||
		!validObservedStorageProof(metadata.ETag) ||
		strings.TrimSpace(metadata.VersionID) != storageVersionID {
		return File{}, ErrStorageMismatch
	}
	item, err := service.repository.CommitFinalize(ctx, access, fileID, FinalizeProof{
		ExpectedVersion: input.ExpectedVersion,
		ContentLength:   metadata.ContentLength, ContentType: mediaType,
		ETag: strings.TrimSpace(metadata.ETag), VersionID: storageVersionID,
		FinalizedAt: now,
	})
	return item, normalizeRepositoryError(err)
}

func validObservedStorageProof(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximumStorageProofBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func normalizeStorageProof(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximumStorageProofBytes || !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrInvalidInput
		}
	}
	return value, nil
}

func normalizeProviderUploadID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximumProviderIDBytes || !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrInvalidInput
		}
	}
	return value, nil
}

func normalizeCompletedParts(input []MultipartCompletedPart) ([]MultipartCompletedPart, error) {
	if len(input) == 0 || len(input) > 10000 {
		return nil, ErrInvalidInput
	}
	parts := make([]MultipartCompletedPart, len(input))
	for index, part := range input {
		etag, err := normalizeStorageProof(part.ETag)
		if err != nil || part.PartNumber != int32(index+1) {
			return nil, ErrInvalidInput
		}
		parts[index] = MultipartCompletedPart{PartNumber: part.PartNumber, ETag: etag}
	}
	return parts, nil
}

func validateMultipartManifest(
	expectedSize int64,
	issued []MultipartIssuedPart,
	completed []MultipartCompletedPart,
) error {
	if len(issued) != len(completed) || len(issued) == 0 {
		return ErrMultipartConflict
	}
	var total int64
	for index, part := range issued {
		if part.PartNumber != int32(index+1) || part.PartNumber != completed[index].PartNumber ||
			part.ContentLengthBytes < 1 || part.ContentLengthBytes > maximumMultipartPartSize ||
			(index < len(issued)-1 && part.ContentLengthBytes < minimumMultipartPartSize) ||
			total > expectedSize-part.ContentLengthBytes {
			return ErrMultipartConflict
		}
		total += part.ContentLengthBytes
	}
	if total != expectedSize {
		return ErrMultipartConflict
	}
	return nil
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func validAccess(access AccessContext) bool {
	return access.TenantID != uuid.Nil && access.ActorID != uuid.Nil &&
		access.MembershipActive && len(access.OrganizationRoles) > 0
}

func normalizeDisplayName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximumDisplayNameRunes ||
		len(value) > maximumDisplayNameBytes {
		return "", ErrInvalidInput
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrInvalidInput
		}
	}
	return value, nil
}

func normalizeMediaType(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || len(parameters) != 0 || len(mediaType) > maximumMediaTypeBytes ||
		!mediaTypeToken.MatchString(mediaType) {
		return "", ErrInvalidInput
	}
	return mediaType, nil
}

func normalizeObservedMediaType(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || len(mediaType) > maximumMediaTypeBytes || !mediaTypeToken.MatchString(mediaType) {
		return "", ErrStorageMismatch
	}
	return mediaType, nil
}

func decodeChecksum(value string) ([]byte, error) {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return nil, ErrInvalidInput
	}
	checksum, err := hex.DecodeString(value)
	if err != nil || len(checksum) != sha256.Size {
		return nil, ErrInvalidInput
	}
	return checksum, nil
}

func requestFingerprint(command CreateCommand) []byte {
	digest := sha256.New()
	_, _ = digest.Write(command.ClassID[:])
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(command.DisplayName))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(command.DeclaredMediaType))
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(command.ExpectedSizeBytes))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(command.ChecksumSHA256)
	return digest.Sum(nil)
}

func normalizeRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrInvalidInput, ErrAccessDenied, ErrNotFound, ErrReadOnly,
		ErrIdempotencyConflict, ErrIntentExpired, ErrQuotaExceeded,
		ErrRateLimited, ErrStorageMismatch, ErrStorageUnavailable,
		ErrNotReady, ErrVersionConflict, ErrUnavailable,
		ErrMultipartConflict, ErrMultipartExpired,
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
	return ErrUnavailable
}
