package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

// Only verified ID-token claims enter this type. In particular, a mailbox
// established by a later TutorHub email challenge cannot create this proof.
type schoolFederationProof struct {
	Provider string
	Domain   string
	OrgID    string
}

type schoolLoginContext struct {
	ClaimedDomain   string                 `json:",omitempty"`
	RequestedDomain string                 `json:",omitempty"`
	Proof           *schoolFederationProof `json:",omitempty"`
}

func schoolProof(slug string, claims ProviderClaims) *schoolFederationProof {
	if !claims.EmailVerified || claims.ProviderOrgID == "" {
		return nil
	}
	domain, err := NormalizeSSODomain(SSOEmailDomain(claims.Email))
	if err != nil {
		return nil
	}
	org := strings.ToLower(claims.ProviderOrgID)
	switch slug {
	case "google":
		normalized, err := NormalizeSSODomain(org)
		if err != nil || normalized != org {
			return nil
		}
	case "microsoft":
		if !microsoftGUID(org) || org == microsoftPersonalTenant || claims.Issuer != microsoftIssuer(org) {
			return nil
		}
	default:
		return nil
	}
	return &schoolFederationProof{Provider: slug, Domain: domain, OrgID: org}
}

func (p *schoolFederationProof) matches(provider, domain string, allowed []string) bool {
	return p != nil && p.Provider == provider && p.Domain == domain && slices.Contains(allowed, p.OrgID)
}

func (s *Service) schoolOAuthOptions(ctx context.Context, provider, intent string, hints OAuthStartHints) (url.Values, error) {
	if hints == (OAuthStartHints{}) {
		return nil, nil
	}
	if intent != flowPurposeLogin || (provider != "google" && provider != "microsoft") {
		return nil, ErrInvalidAuthFlow
	}
	options := url.Values{}
	var mailboxDomain string
	if hints.LoginHint != "" {
		if strings.TrimSpace(hints.LoginHint) != hints.LoginHint || strings.IndexFunc(hints.LoginHint, unicode.IsControl) >= 0 {
			return nil, ErrInvalidAuthFlow
		}
		email, err := NormalizeSignupEmail(hints.LoginHint)
		if err != nil {
			return nil, ErrInvalidAuthFlow
		}
		options.Set("login_hint", email)
		mailboxDomain = SSOEmailDomain(email)
		if normalized, err := NormalizeSSODomain(mailboxDomain); err == nil {
			mailboxDomain = normalized
		}
	}
	domain := mailboxDomain
	if hints.Domain != "" {
		var err error
		domain, err = NormalizeSSODomain(hints.Domain)
		if err != nil || (mailboxDomain != "" && mailboxDomain != domain) {
			return nil, ErrInvalidAuthFlow
		}
	}
	var route SSORoute
	if s.federation.SchoolDomains != nil {
		var err error
		route, err = s.federation.SchoolDomains.DiscoverSSORealm(ctx, domain)
		if err != nil {
			return nil, err
		}
	}
	if route.Available && route.Provider == provider {
		if provider == "google" {
			options.Set("hd", route.Domain)
		}
	} else if hints.Domain != "" {
		return nil, ErrInvalidAuthFlow
	}
	return options, nil
}

// ConfigureSSOMemberCapacity supplies the same tenant quota enforcer used by
// invitation acceptance. Configure before serving authentication requests.
func (s *NativeAuthService) ConfigureSSOMemberCapacity(controls featurecontrol.Enforcer) {
	s.ssoMemberCapacity = controls
}

// Evaluate current policy for both the signed mailbox and the local account's
// canonical mailbox, which can differ for an explicitly linked identity.
func (s *NativeAuthService) checkSchoolLogin(ctx context.Context, tx pgx.Tx, userID uuid.UUID, school schoolLoginContext) (*schoolRealm, error) {
	if s == nil || !s.config.SSO.Enabled {
		if school.RequestedDomain != "" {
			return nil, ErrSSORequired
		}
		return nil, nil
	}
	claimedDomain, proof := school.ClaimedDomain, school.Proof
	var candidate *schoolRealm
	if claimedDomain != "" {
		realm, err := s.ssoRealm(ctx, tx, claimedDomain, true)
		if err != nil {
			return nil, err
		}
		if realm != nil {
			if proof.matches(realm.Provider, realm.Domain, realm.AllowedOrgIDs) {
				candidate = realm
			} else if realm.Required {
				return nil, ErrSSORequired
			}
		}
	}
	if school.RequestedDomain != "" && (candidate == nil || candidate.Domain != school.RequestedDomain) {
		return nil, ErrSSORequired
	}
	if userID != uuid.Nil {
		var canonical string
		if err := tx.QueryRow(ctx, "SELECT email FROM tutorhub.users WHERE id=$1", userID).Scan(&canonical); err != nil {
			return nil, ErrAuthFoundationStorage
		}
		if domain := SSOEmailDomain(canonical); domain != claimedDomain {
			realm, err := s.ssoRealm(ctx, tx, domain, true)
			if err != nil {
				return nil, err
			}
			if realm != nil && realm.Required && !proof.matches(realm.Provider, realm.Domain, realm.AllowedOrgIDs) {
				return nil, ErrSSORequired
			}
		}
	}
	return candidate, nil
}

func joinSchoolTenant(ctx context.Context, tx pgx.Tx, realm *schoolRealm, userID uuid.UUID, controls featurecontrol.Enforcer, now time.Time) error {
	if realm == nil || realm.AutoJoinRole == nil || *realm.AutoJoinRole != string(policy.OrganizationRoleStudent) {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM tutorhub.memberships WHERE tenant_id=$1 AND user_id=$2)", realm.TenantID, userID).Scan(&exists); err != nil {
		return ErrAuthFoundationStorage
	}
	if exists {
		return nil
	}
	if controls != nil {
		if err := controls.RequireMemberCapacity(ctx, tx, realm.TenantID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status,joined_at,created_at,updated_at)
		VALUES ($1,$2,$3,'active',$4,$4,$4) ON CONFLICT (tenant_id,user_id) DO NOTHING`,
		realm.TenantID, userID, string(policy.OrganizationRoleStudent), now)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

// A completed login receipt links the fresh callback proof to the precise
// session. Expiry is bounded by the administration recent-auth window; no
// browser input can mint a receipt or select another session's proof.
func schoolReceiptBinding(crypto *Crypto, userID, sessionID uuid.UUID) []byte {
	return crypto.Digest("school-login-receipt-v1", userID.String()+":"+sessionID.String())
}

func (s *NativeAuthService) schoolSessionForToken(ctx context.Context, tx pgx.Tx, userID uuid.UUID, token string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT id FROM tutorhub.sessions WHERE user_id=$1 AND token_hash=$2",
		userID, s.crypto.Digest(sessionPurpose, token)).Scan(&id)
	if err != nil {
		return uuid.Nil, ErrAuthFoundationStorage
	}
	return id, nil
}

func saveSchoolReceipt(ctx context.Context, tx pgx.Tx, crypto *Crypto, userID, sessionID uuid.UUID, proof *schoolFederationProof, now time.Time) error {
	if proof == nil {
		return nil
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return ErrAuthFoundationStorage
	}
	sealed, err := crypto.Encrypt(string(raw))
	if err != nil {
		return ErrAuthFoundationStorage
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,user_id,session_id,payload_ciphertext,created_at,expires_at,completed_at)
		SELECT $1,'login','completed',$2,$3,$4,$5,$6,$5
		WHERE EXISTS(SELECT FROM tutorhub.memberships WHERE user_id=$2 AND role='org_admin' AND status='active')`, schoolReceiptBinding(crypto, userID, sessionID), userID, sessionID, sealed, now, now.Add(10*time.Minute))
	if err != nil {
		return ErrAuthFoundationStorage
	}
	return nil
}

func schoolSessionProvesOrg(ctx context.Context, tx pgx.Tx, sessionID, userID uuid.UUID, kind, emailDomain string, allowedOrgIDs []string, crypto *Crypto) (bool, error) {
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT t.payload_ciphertext FROM tutorhub.auth_transactions t
		JOIN tutorhub.sessions s ON s.id=t.session_id AND s.user_id=t.user_id
		JOIN tutorhub.identities i ON i.id=s.identity_id AND i.user_id=s.user_id AND i.status='active'
		WHERE t.binding_hash=$1 AND t.kind='login' AND t.state='completed' AND t.user_id=$2 AND t.session_id=$3
		AND t.expires_at>clock_timestamp() AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.absolute_expires_at>clock_timestamp()`,
		schoolReceiptBinding(crypto, userID, sessionID), userID, sessionID).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrAuthFoundationStorage
	}
	raw, err := crypto.Decrypt(sealed)
	var proof schoolFederationProof
	if err != nil || json.Unmarshal([]byte(raw), &proof) != nil {
		return false, ErrAuthFoundationStorage
	}
	switch kind {
	case "google_workspace":
		return proof.matches("google", emailDomain, allowedOrgIDs), nil
	case "entra":
		return proof.matches("microsoft", emailDomain, allowedOrgIDs), nil
	default:
		return false, nil
	}
}
