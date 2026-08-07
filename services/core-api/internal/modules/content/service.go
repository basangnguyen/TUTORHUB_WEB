package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	uploadIntentTTL         = 15 * time.Minute
	maximumDisplayNameRunes = 255
	maximumDisplayNameBytes = 1024
	maximumMediaTypeBytes   = 127
)

var mediaTypeToken = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)

type Service struct {
	repository Repository
	storage    MetadataReader
	now        func() time.Time
}

func NewService(repository Repository, storage MetadataReader) (*Service, error) {
	if repository == nil || storage == nil {
		return nil, fmt.Errorf("content repository and object metadata reader are required")
	}
	return &Service{repository: repository, storage: storage, now: time.Now}, nil
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
	if input.ExpectedVersion < 1 {
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
		return target.File, nil
	}
	metadata, err := service.storage.Head(ctx, target.ObjectKey)
	if err != nil {
		return File{}, fmt.Errorf("%w: read object metadata", ErrStorageUnavailable)
	}
	mediaType, err := normalizeObservedMediaType(metadata.ContentType)
	if err != nil || metadata.ContentLength != target.File.ExpectedSizeBytes ||
		mediaType != target.File.DeclaredMediaType ||
		len(metadata.ChecksumSHA256) != sha256.Size ||
		!bytes.Equal(metadata.ChecksumSHA256, target.ChecksumSHA256) ||
		strings.TrimSpace(metadata.ETag) == "" || strings.TrimSpace(metadata.VersionID) == "" {
		return File{}, ErrStorageMismatch
	}
	item, err := service.repository.CommitFinalize(ctx, access, fileID, FinalizeProof{
		ExpectedVersion: input.ExpectedVersion,
		ContentLength:   metadata.ContentLength, ContentType: mediaType,
		ChecksumSHA256: append([]byte(nil), metadata.ChecksumSHA256...),
		ETag:           strings.TrimSpace(metadata.ETag), VersionID: strings.TrimSpace(metadata.VersionID),
		FinalizedAt: now,
	})
	return item, normalizeRepositoryError(err)
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
		ErrVersionConflict, ErrUnavailable,
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
