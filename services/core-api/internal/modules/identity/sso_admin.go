package identity

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/policy"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var (
	ErrSSOInvalid        = errors.New("invalid school SSO configuration")
	ErrSSOForbidden      = errors.New("school SSO administration requires tenant administrator")
	ErrSSONotFound       = errors.New("school SSO resource not found")
	ErrSSOConflict       = errors.New("school SSO configuration conflict")
	ErrSSOLockout        = errors.New("sign in through the target school SSO configuration first")
	ErrSSODNSMismatch    = errors.New("school SSO DNS verification proof unavailable")
	ErrSSODNSUnavailable = errors.New("school SSO DNS lookup unavailable")
)

type SSOConnection struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	AllowedOrgIDs []string  `json:"allowed_org_ids"`
	EnforceSSO    bool      `json:"enforce_sso"`
	AutoJoinRole  *string   `json:"auto_join_role"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SSODomain struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	ConnectionID uuid.UUID  `json:"connection_id"`
	Domain       string     `json:"domain"`
	Status       string     `json:"status"`
	VerifiedAt   *time.Time `json:"verified_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
type SSOConnectionPatch struct {
	Status          *string
	AllowedOrgIDs   *[]string
	EnforceSSO      *bool
	AutoJoinRole    *string
	SetAutoJoinRole bool
}
type SSOChallenge struct {
	Domain   SSODomain `json:"domain"`
	TXTName  string    `json:"txt_name"`
	TXTValue string    `json:"txt_value"`
}

func (SSOChallenge) String() string { return "[redacted school verification challenge]" }

type DNSTXTResolver interface {
	LookupTXT(context.Context, string) ([]string, error)
}
type SchoolSSOService struct {
	native                          *NativeAuthService
	resolver                        DNSTXTResolver
	dnsSlots                        chan struct{}
	googleEnabled, microsoftEnabled bool
}

func NewSchoolSSOService(native *NativeAuthService, resolver DNSTXTResolver, googleEnabled, microsoftEnabled bool) *SchoolSSOService {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &SchoolSSOService{native: native, resolver: resolver, dnsSlots: make(chan struct{}, 32), googleEnabled: googleEnabled, microsoftEnabled: microsoftEnabled}
}

// Accept DNS names without a trailing dot; return their canonical A-label form.
func NormalizeSSODomain(value string) (string, error) {
	if value == "" || len(value) > 1024 || value != strings.TrimSpace(value) {
		return "", ErrSSOInvalid
	}
	domain, err := idna.Lookup.ToASCII(value)
	domain = strings.ToLower(domain)
	if err != nil || len(domain) > 253 || net.ParseIP(domain) != nil || !strings.Contains(domain, ".") || strings.Trim(domain, "0123456789.") == "" {
		return "", ErrSSOInvalid
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrSSOInvalid
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", ErrSSOInvalid
			}
		}
	}
	// Re-encode decoded A-labels to reject bogus or noncanonical punycode.
	unicodeName, err := idna.Lookup.ToUnicode(domain)
	if err != nil {
		return "", ErrSSOInvalid
	}
	roundTrip, err := idna.Lookup.ToASCII(unicodeName)
	if err != nil || roundTrip != domain {
		return "", ErrSSOInvalid
	}
	switch domain {
	case "gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "live.com", "msn.com", "yahoo.com", "icloud.com":
		return "", ErrSSOInvalid
	}
	if _, err := publicsuffix.EffectiveTLDPlusOne(domain); err != nil {
		return "", ErrSSOInvalid
	}
	return domain, nil
}

func normalizeSSOOrgIDs(kind string, ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 64 || (kind != "google_workspace" && kind != "entra") {
		return nil, ErrSSOInvalid
	}
	result := make([]string, 0, len(ids))
	for _, v := range ids {
		if kind == "google_workspace" {
			var err error
			v, err = NormalizeSSODomain(v)
			if err != nil {
				return nil, err
			}
		} else {
			id, err := uuid.Parse(v)
			if err != nil || id == uuid.Nil || len(v) != 36 || id.String() != strings.ToLower(v) || id.String() == microsoftPersonalTenant {
				return nil, ErrSSOInvalid
			}
			v = id.String()
		}
		if slices.Contains(result, v) {
			return nil, ErrSSOInvalid
		}
		result = append(result, v)
	}
	return result, nil
}

type ssoAdminChange func(context.Context, pgx.Tx, lockedSecuritySession) error

func (s *SchoolSSOService) transaction(ctx context.Context, p Principal, tenant uuid.UUID, mutation bool, change ssoAdminChange) error {
	if s == nil || s.native == nil || !s.native.config.SSO.Enabled {
		return ErrSSONotFound
	}
	if p.ActiveTenant == nil || p.ActiveTenant.ID != tenant || tenant == uuid.Nil {
		return ErrSSOForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, s.native.queryTimeout())
	defer cancel()
	tx, err := s.native.database.Begin(ctx)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	defer rollback(tx)
	locked, err := s.native.lockNativeSecuritySession(ctx, tx, p)
	if err != nil {
		return err
	}
	if mutation {
		// Pair with the shared lock taken when an authentication decision commits.
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tutorhub-school-sso-policy-v1',0))`); err != nil {
			return ErrAuthFoundationStorage
		}
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT m.role FROM tutorhub.sessions s
        JOIN tutorhub.tenants t ON t.id=s.active_tenant_id AND t.status='active'
        JOIN tutorhub.memberships m ON m.tenant_id=t.id AND m.user_id=s.user_id AND m.status='active'
        WHERE s.id=$1 AND s.user_id=$2 AND s.active_tenant_id=$3 FOR SHARE OF t,m`, p.SessionID, p.User.ID, tenant).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSSOForbidden
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	subject := policy.Subject{ActorID: p.User.ID, ActiveTenantID: tenant, MembershipActive: true, OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRole(role)}}
	decision := policy.NewEngine().Authorize(policy.Input{Subject: subject, Action: policy.ActionTenantManage, Resource: policy.Resource{TenantID: tenant, State: policy.ResourceStateActive}})
	if role != string(policy.OrganizationRoleAdmin) || !decision.Allowed {
		return ErrSSOForbidden
	}
	if s.native.config.RequireAdminMFA && locked.AssuranceLevel < 2 {
		return ErrFederationAALRequired
	}
	if mutation {
		if err := s.native.requireRecoveryFactorAssurance(ctx, tx, p.User.ID, locked.AssuranceLevel); err != nil {
			return err
		}
		if err := nativePasswordRecentAuthentication(locked.AuthenticatedAt, s.native.now().UTC()); err != nil {
			return err
		}
	}
	if err = change(ctx, tx, locked); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func ssoAudit(ctx context.Context, tx pgx.Tx, tenant, actor, resource uuid.UUID, resourceType, event string, metadata audit.Metadata) error {
	err := audit.AppendDomainEvent(ctx, tx, audit.DomainEvent{TenantID: tenant, ActorID: actor, AggregateType: resourceType, AggregateID: resource, EventType: event, Metadata: metadata})
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}
func ssoReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSSONotFound
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

const ssoConnectionColumns = `id,tenant_id,kind,status,allowed_org_ids,enforce_sso,auto_join_role,created_at,updated_at`
const ssoDomainColumns = `id,tenant_id,connection_id,domain,status,verified_at,created_at,updated_at`

func scanSSOConnection(row pgx.Row) (c SSOConnection, err error) {
	err = ssoReadError(row.Scan(&c.ID, &c.TenantID, &c.Kind, &c.Status, &c.AllowedOrgIDs, &c.EnforceSSO, &c.AutoJoinRole, &c.CreatedAt, &c.UpdatedAt))
	return
}
func scanSSODomain(row pgx.Row) (d SSODomain, err error) {
	err = ssoReadError(row.Scan(&d.ID, &d.TenantID, &d.ConnectionID, &d.Domain, &d.Status, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt))
	return
}
func (s *SchoolSSOService) ListConnections(ctx context.Context, p Principal, tenant uuid.UUID) ([]SSOConnection, error) {
	result := []SSOConnection{}
	err := s.transaction(ctx, p, tenant, false, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		rows, err := tx.Query(ctx, `SELECT `+ssoConnectionColumns+` FROM tutorhub.auth_sso_connections WHERE tenant_id=$1 ORDER BY created_at,id`, tenant)
		if err != nil {
			return ErrAuthFoundationStorage
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanSSOConnection(rows)
			if err != nil {
				return err
			}
			result = append(result, c)
		}
		return ssoReadError(rows.Err())
	})
	return result, err
}
func (s *SchoolSSOService) ListDomains(ctx context.Context, p Principal, tenant uuid.UUID) ([]SSODomain, error) {
	result := []SSODomain{}
	err := s.transaction(ctx, p, tenant, false, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		rows, err := tx.Query(ctx, `SELECT `+ssoDomainColumns+` FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 ORDER BY created_at,id`, tenant)
		if err != nil {
			return ErrAuthFoundationStorage
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanSSODomain(rows)
			if err != nil {
				return err
			}
			result = append(result, d)
		}
		return ssoReadError(rows.Err())
	})
	return result, err
}
