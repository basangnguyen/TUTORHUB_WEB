package requestmeta

import (
	"context"
	"crypto/sha256"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const internalRequestID = "internal"

type contextKey struct{}

// Metadata carries server-derived request correlation and privacy-reduced source
// hints across HTTP, service, and repository boundaries. The actor/tenant fields
// are populated only after authentication has resolved the authoritative principal.
type Metadata struct {
	mu sync.RWMutex

	requestID       string
	requestInstance uuid.UUID
	startedAt       time.Time
	sourceIPPrefix  string
	userAgentHash   []byte
	actorID         uuid.UUID
	tenantID        uuid.UUID
	auditTenantID   uuid.UUID
	auditTenantSet  bool
}

type Snapshot struct {
	RequestID           string
	RequestInstance     uuid.UUID
	StartedAt           time.Time
	SourceIPPrefix      string
	UserAgentHash       []byte
	ActorID             uuid.UUID
	TenantID            uuid.UUID
	AuditTenantID       uuid.UUID
	AuditTenantResolved bool
}

func New(
	ctx context.Context,
	requestID string,
	remoteAddress string,
	userAgent string,
	startedAt time.Time,
) (context.Context, *Metadata) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = internalRequestID
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	metadata := &Metadata{
		requestID:       requestID,
		requestInstance: uuid.New(),
		startedAt:       startedAt.UTC(),
		sourceIPPrefix:  IPPrefix(remoteAddress),
		userAgentHash:   UserAgentHash(userAgent),
	}
	return context.WithValue(ctx, contextKey{}, metadata), metadata
}

func FromContext(ctx context.Context) (*Metadata, bool) {
	if ctx == nil {
		return nil, false
	}
	metadata, ok := ctx.Value(contextKey{}).(*Metadata)
	return metadata, ok && metadata != nil
}

func SetPrincipal(ctx context.Context, actorID uuid.UUID, tenantID uuid.UUID) {
	metadata, ok := FromContext(ctx)
	if !ok {
		return
	}
	metadata.mu.Lock()
	metadata.actorID = actorID
	metadata.tenantID = tenantID
	metadata.mu.Unlock()
}

// SetAuditTenant records a server-resolved target tenant for sensitive
// mutations whose resource can belong to a tenant other than the caller's
// active workspace. Callers must only pass a tenant derived from authoritative
// server state, never a client-supplied tenant identifier. The first resolved
// tenant is immutable for the lifetime of the request.
func SetAuditTenant(ctx context.Context, tenantID uuid.UUID) {
	if tenantID == uuid.Nil {
		return
	}
	metadata, ok := FromContext(ctx)
	if !ok {
		return
	}
	metadata.mu.Lock()
	if !metadata.auditTenantSet {
		metadata.auditTenantID = tenantID
		metadata.auditTenantSet = true
	}
	metadata.mu.Unlock()
}

func SnapshotFromContext(ctx context.Context) Snapshot {
	metadata, ok := FromContext(ctx)
	if !ok {
		return Snapshot{
			RequestID:       internalRequestID,
			RequestInstance: uuid.New(),
			StartedAt:       time.Now().UTC(),
		}
	}
	metadata.mu.RLock()
	defer metadata.mu.RUnlock()
	return Snapshot{
		RequestID:           metadata.requestID,
		RequestInstance:     metadata.requestInstance,
		StartedAt:           metadata.startedAt,
		SourceIPPrefix:      metadata.sourceIPPrefix,
		UserAgentHash:       append([]byte(nil), metadata.userAgentHash...),
		ActorID:             metadata.actorID,
		TenantID:            metadata.tenantID,
		AuditTenantID:       metadata.auditTenantID,
		AuditTenantResolved: metadata.auditTenantSet,
	}
}

func RequestID(ctx context.Context) string {
	return SnapshotFromContext(ctx).RequestID
}

// IPPrefix reduces IPv4 addresses to /24 and IPv6 addresses to /56 before
// persistence. Invalid or non-IP remote addresses are deliberately discarded.
func IPPrefix(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		mask := net.CIDRMask(24, 32)
		return (&net.IPNet{IP: ipv4.Mask(mask), Mask: mask}).String()
	}
	mask := net.CIDRMask(56, 128)
	return (&net.IPNet{IP: ip.Mask(mask), Mask: mask}).String()
}

// UserAgentHash keeps a stable device hint without retaining the raw header.
// It is not an authentication credential or a globally reusable fingerprint.
func UserAgentHash(userAgent string) []byte {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return nil
	}
	digest := sha256.Sum256([]byte("tutorhub-audit-user-agent-v1\x00" + userAgent))
	return digest[:]
}
