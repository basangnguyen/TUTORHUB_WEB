package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrSSORequired = errors.New("this email domain requires school SSO")

type SSORealmConfig struct {
	Enabled, GoogleEnabled, MicrosoftEnabled bool
}

func (c SSORealmConfig) available() bool {
	return c.Enabled && (c.GoogleEnabled || c.MicrosoftEnabled)
}

type SSORoute struct {
	Required  bool   `json:"required"`
	Available bool   `json:"available,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Domain    string `json:"domain,omitempty"`
}

type SSORealmDiscoverer interface {
	DiscoverSSORealm(ctx context.Context, domain string) (SSORoute, error)
}

type schoolRealm struct {
	SSORoute
	TenantID, ConnectionID uuid.UUID
	AllowedOrgIDs          []string
	AutoJoinRole           *string
}

// SSOEmailDomain accepts a syntactically validated address. A quoted local
// part may contain '@', so only the final separator identifies its domain.
func SSOEmailDomain(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

func (s *NativeAuthService) DiscoverSSORealm(ctx context.Context, domain string) (SSORoute, error) {
	if !s.config.SSO.available() {
		return SSORoute{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout())
	defer cancel()
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return SSORoute{}, ErrAuthFoundationStorage
	}
	defer rollback(tx)
	realm, err := s.ssoRealm(ctx, tx, domain, false)
	if err != nil || realm == nil {
		return SSORoute{}, err
	}
	return realm.SSORoute, nil
}

// At a commit boundary, hold a shared policy lock through commit. This also
// covers an absent row becoming a verified route during a login or recovery.
func (s *NativeAuthService) ssoRealm(ctx context.Context, tx pgx.Tx, domain string, lock bool) (*schoolRealm, error) {
	cfg := s.config.SSO
	if !cfg.available() {
		return nil, nil
	}
	domain, err := NormalizeSSODomain(domain)
	if err != nil {
		return nil, nil
	}
	if lock {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('tutorhub-school-sso-policy-v1',0))`); err != nil {
			return nil, ErrAuthFoundationStorage
		}
	}
	var realm schoolRealm
	err = tx.QueryRow(ctx, `SELECT c.tenant_id,c.id,c.enforce_sso,c.allowed_org_ids,c.auto_join_role,
	    CASE c.kind WHEN 'google_workspace' THEN 'google' WHEN 'entra' THEN 'microsoft' END
	    FROM tutorhub.auth_sso_domains d
	    JOIN tutorhub.auth_sso_connections c ON c.tenant_id=d.tenant_id AND c.id=d.connection_id
	    JOIN tutorhub.tenants t ON t.id=d.tenant_id AND t.status='active'
	    WHERE d.domain=$1 AND d.status='verified' AND c.status='enabled'
	    AND ((c.kind='google_workspace' AND $2) OR (c.kind='entra' AND $3))`,
		domain, cfg.GoogleEnabled, cfg.MicrosoftEnabled).Scan(&realm.TenantID, &realm.ConnectionID, &realm.Required, &realm.AllowedOrgIDs, &realm.AutoJoinRole, &realm.Provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrAuthFoundationStorage
	}
	realm.Domain, realm.Available = domain, true
	return &realm, nil
}

func (s *NativeAuthService) allowLocalEmail(ctx context.Context, tx pgx.Tx, email string, lock bool) error {
	realm, err := s.ssoRealm(ctx, tx, SSOEmailDomain(email), lock)
	if err == nil && realm != nil && realm.Required {
		return ErrSSORequired
	}
	return err
}
