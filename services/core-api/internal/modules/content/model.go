package content

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/objectstorage"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	StatusPending    Status = "pending"
	StatusUploaded   Status = "uploaded"
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusRejected   Status = "rejected"
)

var (
	ErrInvalidInput        = errors.New("invalid file input")
	ErrAccessDenied        = errors.New("file access denied")
	ErrNotFound            = errors.New("file not found")
	ErrReadOnly            = errors.New("file class is read only")
	ErrIdempotencyConflict = errors.New("file upload intent idempotency conflict")
	ErrIntentExpired       = errors.New("file upload intent expired")
	ErrQuotaExceeded       = errors.New("file quota exceeded")
	ErrRateLimited         = errors.New("file upload intent rate limited")
	ErrStorageMismatch     = errors.New("stored file metadata mismatch")
	ErrStorageUnavailable  = errors.New("stored file metadata unavailable")
	ErrNotReady            = errors.New("file is not ready for download")
	ErrVersionConflict     = errors.New("file version conflict")
	ErrUnavailable         = errors.New("file metadata unavailable")
)

type Status string

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
}

type File struct {
	ID                     uuid.UUID  `json:"id"`
	ClassID                uuid.UUID  `json:"class_id"`
	CreatorUserID          uuid.UUID  `json:"creator_user_id"`
	DisplayName            string     `json:"display_name"`
	DeclaredMediaType      string     `json:"declared_media_type"`
	ExpectedSizeBytes      int64      `json:"expected_size_bytes"`
	ExpectedChecksumSHA256 string     `json:"expected_checksum_sha256"`
	Status                 Status     `json:"status"`
	Version                int64      `json:"version"`
	UploadExpiresAt        time.Time  `json:"upload_expires_at"`
	UploadedAt             *time.Time `json:"uploaded_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type CreateIntentInput struct {
	ClassID           uuid.UUID `json:"class_id"`
	DisplayName       string    `json:"display_name"`
	DeclaredMediaType string    `json:"declared_media_type"`
	ExpectedSizeBytes int64     `json:"expected_size_bytes"`
	ChecksumSHA256    string    `json:"checksum_sha256"`
	ClientRequestID   uuid.UUID `json:"client_request_id"`
}

type CreateIntentResult struct {
	File    File
	Created bool
}

type FinalizeInput struct {
	ExpectedVersion  int64  `json:"expected_version"`
	StorageVersionID string `json:"storage_version_id"`
}

type UploadCapabilityInput struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type UploadCapability struct {
	Method             string            `json:"method"`
	URL                string            `json:"url"`
	ExpiresAt          time.Time         `json:"expires_at"`
	ContentLengthBytes int64             `json:"content_length_bytes"`
	RequiredHeaders    map[string]string `json:"required_headers"`
}

type DownloadCapability struct {
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CreateCommand struct {
	ID                 uuid.UUID
	ClassID            uuid.UUID
	DisplayName        string
	DeclaredMediaType  string
	ExpectedSizeBytes  int64
	ChecksumSHA256     []byte
	ClientRequestID    uuid.UUID
	RequestFingerprint []byte
	ObjectKey          string
	CreatedAt          time.Time
	UploadExpiresAt    time.Time
}

type FinalizeTarget struct {
	File             File
	ObjectKey        string
	StorageVersionID string
}

type FinalizeProof struct {
	ExpectedVersion int64
	ContentLength   int64
	ContentType     string
	ETag            string
	VersionID       string
	FinalizedAt     time.Time
}

type UploadTarget struct {
	File      File
	ObjectKey string
}

type DownloadTarget struct {
	File            File
	ObjectKey       string
	VersionID       string
	StoredMediaType string
}

type Repository interface {
	CreateIntent(context.Context, AccessContext, CreateCommand) (CreateIntentResult, error)
	Get(context.Context, AccessContext, uuid.UUID) (File, error)
	PrepareUpload(context.Context, AccessContext, uuid.UUID, int64, time.Time) (UploadTarget, error)
	PrepareDownload(context.Context, AccessContext, uuid.UUID) (DownloadTarget, error)
	PrepareFinalize(context.Context, AccessContext, uuid.UUID, int64, time.Time) (FinalizeTarget, error)
	CommitFinalize(context.Context, AccessContext, uuid.UUID, FinalizeProof) (File, error)
}

type ServiceAPI interface {
	CreateIntent(context.Context, AccessContext, CreateIntentInput) (CreateIntentResult, error)
	Get(context.Context, AccessContext, uuid.UUID) (File, error)
	IssueUploadCapability(context.Context, AccessContext, uuid.UUID, UploadCapabilityInput) (UploadCapability, error)
	IssueDownloadCapability(context.Context, AccessContext, uuid.UUID) (DownloadCapability, error)
	Finalize(context.Context, AccessContext, uuid.UUID, FinalizeInput) (File, error)
}

type ObjectStorage interface {
	objectstorage.MetadataReader
	objectstorage.TransferPresigner
}
