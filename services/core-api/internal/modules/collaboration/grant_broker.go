package collaboration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	defaultGrantTTL          = 30 * time.Second
	maximumGrantTTL          = 60 * time.Second
	defaultGrantRateWindow   = 10 * time.Second
	defaultGrantRateLimit    = 8
	defaultMaximumGrants     = 4096
	defaultMaximumLeases     = 10_000
	defaultLeaseIdleTTL      = 2 * time.Minute
	grantCredentialByteCount = 32
)

type MemoryGrantBrokerConfig struct {
	Clock         func() time.Time
	GrantTTL      time.Duration
	LeaseIdleTTL  time.Duration
	MaximumGrants int
	MaximumLeases int
	ProviderURL   string
	Random        io.Reader
	RateLimit     int
	RateWindow    time.Duration
}

type pendingGrant struct {
	access     AccessContext
	authority  GrantAuthority
	capability Capability
	expiresAt  time.Time
	origin     string
}

type activeGrantLease struct {
	access    AccessContext
	expiresAt time.Time
	origin    string
	scope     GrantScope
}

type issueRateKey struct {
	tenantID  uuid.UUID
	actorID   uuid.UUID
	sessionID uuid.UUID
}

// MemoryGrantBroker is intentionally process-local for the accepted one-Core-
// API private-alpha profile. A restart invalidates every outstanding grant and
// lease, which is the safe failure mode. Multi-instance rollout requires a
// shared atomic store and is outside P5-COLLAB-04.
type MemoryGrantBroker struct {
	mu            sync.Mutex
	clock         func() time.Time
	grantTTL      time.Duration
	leaseIdleTTL  time.Duration
	maximumGrants int
	maximumLeases int
	providerURL   string
	random        io.Reader
	rateLimit     int
	rateWindow    time.Duration
	grants        map[[sha256.Size]byte]pendingGrant
	leases        map[[sha256.Size]byte]activeGrantLease
	issueRates    map[issueRateKey][]time.Time
}

func NewMemoryGrantBroker(config MemoryGrantBrokerConfig) (*MemoryGrantBroker, error) {
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.GrantTTL == 0 {
		config.GrantTTL = defaultGrantTTL
	}
	if config.LeaseIdleTTL == 0 {
		config.LeaseIdleTTL = defaultLeaseIdleTTL
	}
	if config.MaximumGrants == 0 {
		config.MaximumGrants = defaultMaximumGrants
	}
	if config.MaximumLeases == 0 {
		config.MaximumLeases = defaultMaximumLeases
	}
	if config.RateLimit == 0 {
		config.RateLimit = defaultGrantRateLimit
	}
	if config.RateWindow == 0 {
		config.RateWindow = defaultGrantRateWindow
	}
	if config.ProviderURL == "" || config.GrantTTL < time.Second ||
		config.GrantTTL > maximumGrantTTL || config.LeaseIdleTTL < time.Second ||
		config.MaximumGrants < 1 || config.MaximumLeases < 1 ||
		config.RateLimit < 1 || config.RateWindow < time.Second {
		return nil, fmt.Errorf("invalid whiteboard grant broker configuration")
	}
	return &MemoryGrantBroker{
		clock: config.Clock, grantTTL: config.GrantTTL, leaseIdleTTL: config.LeaseIdleTTL,
		maximumGrants: config.MaximumGrants, maximumLeases: config.MaximumLeases,
		providerURL: config.ProviderURL, random: config.Random, rateLimit: config.RateLimit,
		rateWindow: config.RateWindow, grants: make(map[[sha256.Size]byte]pendingGrant),
		leases:     make(map[[sha256.Size]byte]activeGrantLease),
		issueRates: make(map[issueRateKey][]time.Time),
	}, nil
}

func (broker *MemoryGrantBroker) Issue(
	_ context.Context,
	access AccessContext,
	authority GrantAuthority,
	capability Capability,
	input GrantExchangeInput,
) (GrantCredential, error) {
	if broker == nil || !validAccess(access) || !validCapability(capability) ||
		authority.Document.ID == uuid.Nil || authority.ProviderDocumentName == "" ||
		authority.WriterFence < 1 || input.Origin == "" {
		return GrantCredential{}, ErrGrantDenied
	}
	now := broker.clock().UTC()
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.pruneLocked(now)
	if len(broker.grants) >= broker.maximumGrants || !broker.allowIssueLocked(access, now) {
		return GrantCredential{}, ErrGrantRateLimited
	}
	credential, digest, err := broker.randomSecretLocked()
	if err != nil {
		return GrantCredential{}, ErrGrantUnavailable
	}
	expiresAt := now.Add(broker.grantTTL)
	broker.grants[digest] = pendingGrant{
		access: cloneAccess(access), authority: authority, capability: capability,
		expiresAt: expiresAt, origin: input.Origin,
	}
	return GrantCredential{
		Credential: credential, ProviderURL: broker.providerURL,
		DocumentID: authority.Document.ID, Generation: authority.Document.CurrentGeneration,
		RevokeGeneration: authority.Document.RevokeGeneration,
		Capability:       capability, ExpiresAt: expiresAt,
	}, nil
}

func (broker *MemoryGrantBroker) Consume(
	_ context.Context,
	input GrantConsumeInput,
) (GrantResolution, error) {
	if broker == nil || len(input.Credential) < 20 || len(input.Credential) > 1024 {
		return GrantResolution{}, ErrGrantDenied
	}
	now := broker.clock().UTC()
	digest := sha256.Sum256([]byte(input.Credential))
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.pruneLocked(now)
	grant, ok := broker.grants[digest]
	delete(broker.grants, digest)
	if !ok || !now.Before(grant.expiresAt) || input.Origin != grant.origin ||
		input.ProviderDocumentName != grant.authority.ProviderDocumentName {
		return GrantResolution{}, ErrGrantDenied
	}
	if len(broker.leases) >= broker.maximumLeases {
		return GrantResolution{}, ErrGrantRateLimited
	}
	lease, leaseDigest, err := broker.randomSecretLocked()
	if err != nil {
		return GrantResolution{}, ErrGrantUnavailable
	}
	scope := GrantScope{
		AuthorityLease: lease, ActorID: grant.access.ActorID, Capability: grant.capability,
		DocumentID:           grant.authority.Document.ID,
		Generation:           grant.authority.Document.CurrentGeneration,
		ProviderDocumentName: grant.authority.ProviderDocumentName,
		SessionID:            grant.access.SessionID, TenantID: grant.access.TenantID,
		WriterFence: grant.authority.WriterFence,
	}
	broker.leases[leaseDigest] = activeGrantLease{
		access: grant.access, expiresAt: now.Add(broker.leaseIdleTTL),
		origin: grant.origin, scope: scope,
	}
	return GrantResolution{Access: cloneAccess(grant.access), Scope: scope}, nil
}

func (broker *MemoryGrantBroker) Validate(
	_ context.Context,
	input GrantValidationInput,
) (GrantResolution, error) {
	if broker == nil || len(input.Scope.AuthorityLease) < 20 ||
		len(input.Scope.AuthorityLease) > 1024 {
		return GrantResolution{}, ErrGrantDenied
	}
	now := broker.clock().UTC()
	digest := sha256.Sum256([]byte(input.Scope.AuthorityLease))
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.pruneLocked(now)
	lease, ok := broker.leases[digest]
	if !ok || !now.Before(lease.expiresAt) || input.Origin != lease.origin ||
		!sameGrantScope(input.Scope, lease.scope) {
		delete(broker.leases, digest)
		return GrantResolution{}, ErrGrantDenied
	}
	lease.expiresAt = now.Add(broker.leaseIdleTTL)
	broker.leases[digest] = lease
	return GrantResolution{Access: cloneAccess(lease.access), Scope: lease.scope}, nil
}

func (broker *MemoryGrantBroker) InvalidateLease(lease string) {
	if broker == nil || lease == "" {
		return
	}
	digest := sha256.Sum256([]byte(lease))
	broker.mu.Lock()
	delete(broker.leases, digest)
	broker.mu.Unlock()
}

func (broker *MemoryGrantBroker) Revoke(documentID uuid.UUID) {
	if broker == nil || documentID == uuid.Nil {
		return
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	for digest, grant := range broker.grants {
		if grant.authority.Document.ID == documentID {
			delete(broker.grants, digest)
		}
	}
	for digest, lease := range broker.leases {
		if lease.scope.DocumentID == documentID {
			delete(broker.leases, digest)
		}
	}
}

func (broker *MemoryGrantBroker) allowIssueLocked(access AccessContext, now time.Time) bool {
	key := issueRateKey{tenantID: access.TenantID, actorID: access.ActorID, sessionID: access.SessionID}
	cutoff := now.Add(-broker.rateWindow)
	values := broker.issueRates[key]
	first := 0
	for first < len(values) && !values[first].After(cutoff) {
		first++
	}
	values = append(values[first:], now)
	broker.issueRates[key] = values
	return len(values) <= broker.rateLimit
}

func (broker *MemoryGrantBroker) pruneLocked(now time.Time) {
	for digest, grant := range broker.grants {
		if !now.Before(grant.expiresAt) {
			delete(broker.grants, digest)
		}
	}
	for digest, lease := range broker.leases {
		if !now.Before(lease.expiresAt) {
			delete(broker.leases, digest)
		}
	}
	cutoff := now.Add(-broker.rateWindow)
	for key, values := range broker.issueRates {
		first := 0
		for first < len(values) && !values[first].After(cutoff) {
			first++
		}
		if first == len(values) {
			delete(broker.issueRates, key)
		} else if first > 0 {
			broker.issueRates[key] = append([]time.Time(nil), values[first:]...)
		}
	}
}

func (broker *MemoryGrantBroker) randomSecretLocked() (string, [sha256.Size]byte, error) {
	var raw [grantCredentialByteCount]byte
	if _, err := io.ReadFull(broker.random, raw[:]); err != nil {
		return "", [sha256.Size]byte{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw[:])
	return encoded, sha256.Sum256([]byte(encoded)), nil
}

func cloneAccess(access AccessContext) AccessContext {
	access.OrganizationRoles = append([]policy.OrganizationRole(nil), access.OrganizationRoles...)
	return access
}

func sameGrantScope(left, right GrantScope) bool {
	return left.AuthorityLease == right.AuthorityLease && left.ActorID == right.ActorID &&
		left.Capability == right.Capability && left.DocumentID == right.DocumentID &&
		left.Generation == right.Generation &&
		left.ProviderDocumentName == right.ProviderDocumentName &&
		left.SessionID == right.SessionID && left.TenantID == right.TenantID &&
		left.WriterFence == right.WriterFence
}
