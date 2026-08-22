package collaboration

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type DocumentStatus string

const (
	DocumentCreated   DocumentStatus = "created"
	DocumentOpen      DocumentStatus = "open"
	DocumentSuspended DocumentStatus = "suspended"
	DocumentClosed    DocumentStatus = "closed"
)

type Capability string

const (
	CapabilityView    Capability = "view"
	CapabilityEdit    Capability = "edit"
	CapabilityPresent Capability = "present"
)

type Audience string

const (
	AudienceOrganizationAdmin Audience = "organization_admin"
	AudienceHost              Audience = "host"
	AudienceCoHost            Audience = "co_host"
	AudienceTeachingAssistant Audience = Audience(policy.ClassRoleTeachingAssistant)
	AudienceAttendee          Audience = "attendee"
)

var (
	ErrUnavailable         = errors.New("whiteboard control plane unavailable")
	ErrInvalidRequest      = errors.New("invalid whiteboard request")
	ErrNotFound            = errors.New("whiteboard not found")
	ErrVersionConflict     = errors.New("whiteboard version conflict")
	ErrIdempotencyConflict = errors.New("whiteboard idempotency conflict")
	ErrTransitionConflict  = errors.New("whiteboard transition conflict")
	ErrArtifactUnavailable = errors.New("whiteboard artifact workflow unavailable")
	ErrGrantUnavailable    = errors.New("whiteboard grant exchange unavailable")
	ErrGrantDenied         = errors.New("whiteboard grant denied")
	ErrGrantRateLimited    = errors.New("whiteboard grant rate limited")
	ErrQuotaExceeded       = errors.New("whiteboard quota exceeded")
	ErrReadOnly            = errors.New("whiteboard runtime is read only")
)

type RuntimeMode string

const (
	RuntimeModeEnabled  RuntimeMode = "enabled"
	RuntimeModeReadOnly RuntimeMode = "read_only"
	RuntimeModeOff      RuntimeMode = "off"
)

type RuntimeLimits struct {
	MaxConnectionsPerTenant  int64 `json:"max_connections_per_tenant"`
	MaxOperationsPerMinute   int64 `json:"max_operations_per_minute"`
	MaxStorageBytesPerTenant int64 `json:"max_storage_bytes_per_tenant"`
}

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	SessionID         uuid.UUID
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
}

type ViewerCapabilities struct {
	Capability        Capability `json:"capability"`
	CanOpen           bool       `json:"can_open"`
	CanSuspend        bool       `json:"can_suspend"`
	CanResume         bool       `json:"can_resume"`
	CanClose          bool       `json:"can_close"`
	CanCreateSnapshot bool       `json:"can_create_snapshot"`
	CanExport         bool       `json:"can_export"`
	CanRestore        bool       `json:"can_restore"`
	CanExchangeGrant  bool       `json:"can_exchange_grant"`
}

type Document struct {
	ID                uuid.UUID          `json:"id"`
	MediaSpaceID      uuid.UUID          `json:"media_space_id"`
	Status            DocumentStatus     `json:"status"`
	Version           int64              `json:"version"`
	CurrentGeneration int64              `json:"current_generation"`
	RevokeGeneration  int64              `json:"revoke_generation"`
	Viewer            ViewerCapabilities `json:"viewer_capabilities"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

// ToolProjection is the server-authoritative discovery response used by the
// classroom shell. An authorized manager can see that a whiteboard may be
// created without the UI inferring that permission from a role label. The
// document remains nil until the control-plane record exists.
type ToolProjection struct {
	Document  *Document `json:"document"`
	CanCreate bool      `json:"can_create"`
}

type CreateInput struct {
	MediaSpaceID   uuid.UUID
	IdempotencyKey string
}

type CreateCommand struct {
	DocumentID           uuid.UUID
	MediaSpaceID         uuid.UUID
	ProviderDocumentName string
	IdempotencyKey       string
	Fingerprint          []byte
	OccurredAt           time.Time
}

type CreateResult struct {
	Document Document
	Created  bool
}

type TransitionInput struct {
	ExpectedVersion int64
	IdempotencyKey  string
}

type TransitionCommand struct {
	DocumentID      uuid.UUID
	Operation       string
	ExpectedVersion int64
	IdempotencyKey  string
	Fingerprint     []byte
	OccurredAt      time.Time
}

type RestoreInput struct {
	SnapshotID         uuid.UUID
	ExpectedVersion    int64
	ExpectedGeneration int64
	IdempotencyKey     string
}

type RestoreCommand struct {
	ArtifactCommandID    uuid.UUID
	DocumentID           uuid.UUID
	SnapshotID           uuid.UUID
	ExpectedVersion      int64
	ExpectedGeneration   int64
	ProviderDocumentName string
	IdempotencyKey       string
	Fingerprint          []byte
	OccurredAt           time.Time
}

type Snapshot struct {
	ID                    uuid.UUID `json:"id"`
	DocumentID            uuid.UUID `json:"document_id"`
	Generation            int64     `json:"generation"`
	Kind                  string    `json:"kind"`
	FormatVersion         string    `json:"format_version"`
	EngineVersion         string    `json:"engine_version"`
	AuthorityVersion      string    `json:"authority_version"`
	SchemaVersion         int       `json:"schema_version"`
	CausalWatermarkSHA256 string    `json:"causal_watermark_sha256"`
	ContentSHA256         string    `json:"content_sha256"`
	SizeBytes             int64     `json:"size_bytes"`
	CreatedAt             time.Time `json:"created_at"`
	RetentionUntil        time.Time `json:"retention_until"`
}

type SnapshotList struct {
	Items      []Snapshot `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

type SnapshotListInput struct {
	Limit  int
	Cursor string
}

type SnapshotPageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type ArtifactCommandStatus string

const ArtifactCommandAccepted ArtifactCommandStatus = "accepted"

type ArtifactCommand struct {
	ID                     uuid.UUID             `json:"id"`
	Kind                   string                `json:"kind"`
	DocumentID             uuid.UUID             `json:"document_id"`
	Generation             int64                 `json:"generation"`
	Status                 ArtifactCommandStatus `json:"status"`
	RequestedAt            time.Time             `json:"requested_at"`
	TargetProviderDocument string                `json:"-"`
}

type SnapshotCreateInput struct {
	ExpectedGeneration int64
	IdempotencyKey     string
}

type ExportInput struct {
	ExpectedGeneration int64
	IdempotencyKey     string
}

type ImportManifest struct {
	FormatVersion    string `json:"format_version"`
	EngineVersion    string `json:"engine_version"`
	AuthorityVersion string `json:"authority_version"`
	SchemaVersion    int    `json:"schema_version"`
	ContentSHA256    string `json:"content_sha256"`
	SizeBytes        int64  `json:"size_bytes"`
}

type ImportValidation struct {
	Valid    bool           `json:"valid"`
	Manifest ImportManifest `json:"manifest"`
	Problems []string       `json:"problems"`
}

type GrantExchangeInput struct {
	Capability               Capability
	ExpectedGeneration       int64
	ExpectedRevokeGeneration int64
	Origin                   string
}

type GrantCredential struct {
	Credential       string     `json:"credential"`
	ProviderURL      string     `json:"provider_url"`
	DocumentID       uuid.UUID  `json:"document_id"`
	Generation       int64      `json:"generation"`
	RevokeGeneration int64      `json:"revoke_generation"`
	Capability       Capability `json:"capability"`
	ExpiresAt        time.Time  `json:"expires_at"`
}

// GrantAuthority is server-only data that binds a logical document generation
// to its opaque provider document name. It must never be returned by public
// whiteboard lifecycle endpoints.
type GrantAuthority struct {
	Document             Document
	ProviderDocumentName string
	RuntimeLimits        RuntimeLimits
	WriterFence          int64
}

type GrantConsumeInput struct {
	Credential           string
	Origin               string
	ProviderDocumentName string
}

// GrantScope is returned only to the authenticated collaboration runtime. The
// authority lease remains memory-only and is used for bounded revalidation of
// an active provider connection.
type GrantScope struct {
	AuthorityLease           string     `json:"authority_lease"`
	ActorID                  uuid.UUID  `json:"actor_id"`
	Capability               Capability `json:"capability"`
	DocumentID               uuid.UUID  `json:"document_id"`
	Generation               int64      `json:"generation"`
	MaxConnectionsPerTenant  int64      `json:"max_connections_per_tenant"`
	MaxOperationsPerMinute   int64      `json:"max_operations_per_minute"`
	MaxStorageBytesPerTenant int64      `json:"max_storage_bytes_per_tenant"`
	ProviderDocumentName     string     `json:"provider_document_name"`
	SessionID                uuid.UUID  `json:"session_id"`
	TenantID                 uuid.UUID  `json:"tenant_id"`
	WriterFence              int64      `json:"writer_fence"`
}

type GrantValidationInput struct {
	Scope  GrantScope
	Origin string
}

type GrantResolution struct {
	Access AccessContext
	Scope  GrantScope
}

type Repository interface {
	Create(context.Context, AccessContext, CreateCommand) (CreateResult, error)
	Get(context.Context, AccessContext, uuid.UUID) (Document, error)
	GetByMediaSpace(context.Context, AccessContext, uuid.UUID) (Document, error)
	GrantAuthority(context.Context, AccessContext, uuid.UUID) (GrantAuthority, error)
	Transition(context.Context, AccessContext, TransitionCommand) (Document, error)
	CapabilityPolicies(context.Context, AccessContext, uuid.UUID) (map[Audience]Capability, error)
	ListSnapshots(context.Context, AccessContext, uuid.UUID, SnapshotPageCursor, int) ([]Snapshot, error)
	Restore(context.Context, AccessContext, RestoreCommand) (Document, error)
}

type SpaceAuthority interface {
	GetSpace(context.Context, media.AccessContext, uuid.UUID) (media.MediaSpace, error)
}

type GrantBroker interface {
	Issue(context.Context, AccessContext, GrantAuthority, Capability, GrantExchangeInput) (GrantCredential, error)
	Consume(context.Context, GrantConsumeInput) (GrantResolution, error)
	Validate(context.Context, GrantValidationInput) (GrantResolution, error)
	InvalidateLease(string)
	Revoke(uuid.UUID)
}

type ArtifactWorkflow interface {
	RequestSnapshot(context.Context, AccessContext, Document, SnapshotCreateInput) (ArtifactCommand, error)
	RequestExport(context.Context, AccessContext, Document, ExportInput) (ArtifactCommand, error)
	PrepareRestore(context.Context, AccessContext, Document, RestoreCommand) (ArtifactCommand, error)
}

type ServiceAPI interface {
	Create(context.Context, AccessContext, CreateInput) (CreateResult, error)
	Get(context.Context, AccessContext, uuid.UUID) (Document, error)
	Resolve(context.Context, AccessContext, uuid.UUID) (ToolProjection, error)
	Capabilities(context.Context, AccessContext, uuid.UUID) (ViewerCapabilities, error)
	Open(context.Context, AccessContext, uuid.UUID, TransitionInput) (Document, error)
	Suspend(context.Context, AccessContext, uuid.UUID, TransitionInput) (Document, error)
	Resume(context.Context, AccessContext, uuid.UUID, TransitionInput) (Document, error)
	Close(context.Context, AccessContext, uuid.UUID, TransitionInput) (Document, error)
	ExchangeGrant(context.Context, AccessContext, uuid.UUID, GrantExchangeInput) (GrantCredential, error)
	ListSnapshots(context.Context, AccessContext, uuid.UUID, SnapshotListInput) (SnapshotList, error)
	CreateSnapshot(context.Context, AccessContext, uuid.UUID, SnapshotCreateInput) (ArtifactCommand, error)
	Export(context.Context, AccessContext, uuid.UUID, ExportInput) (ArtifactCommand, error)
	ValidateImport(context.Context, ImportManifest) (ImportValidation, error)
	Restore(context.Context, AccessContext, uuid.UUID, RestoreInput) (Document, error)
}

type InternalServiceAPI interface {
	ConsumeGrant(context.Context, GrantConsumeInput) (GrantScope, error)
	ValidateGrant(context.Context, GrantValidationInput) (bool, error)
}
