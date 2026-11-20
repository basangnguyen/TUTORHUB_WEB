package identity

import (
	"context"
	"crypto/hmac"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
)

const ssoTXTPrefix = "tutorhub-site-verification="

func ssoTXTName(domain string) string { return "_tutorhub-challenge." + domain + "." }
func ssoTXTPurpose(tenant, id uuid.UUID) string {
	return "sso-txt-v1:" + tenant.String() + ":" + id.String()
}

func (s *SchoolSSOService) newChallenge(tenant, id uuid.UUID, domain string) (SSOChallenge, []byte, error) {
	value, err := s.native.crypto.RandomToken()
	if err != nil {
		return SSOChallenge{}, nil, ErrAuthFoundationStorage
	}
	challenge := SSOChallenge{TXTName: ssoTXTName(domain), TXTValue: ssoTXTPrefix + value}
	return challenge, s.native.crypto.Digest(ssoTXTPurpose(tenant, id), challenge.TXTValue), nil
}

func (s *SchoolSSOService) CreateDomain(ctx context.Context, p Principal, tenant, conn uuid.UUID, domain string) (SSOChallenge, error) {
	var result SSOChallenge
	err := s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		domain, err := NormalizeSSODomain(domain)
		if err != nil || len(ssoTXTName(domain)) > 254 {
			return ErrSSOInvalid
		}
		var kind string
		if err := tx.QueryRow(ctx, `SELECT kind FROM tutorhub.auth_sso_connections WHERE tenant_id=$1 AND id=$2`, tenant, conn).Scan(&kind); err != nil {
			return ssoReadError(err)
		}
		id := uuid.New()
		challenge, hash, err := s.newChallenge(tenant, id, domain)
		if err != nil {
			return err
		}
		challenge.Domain, err = scanSSODomain(tx.QueryRow(ctx, `INSERT INTO tutorhub.auth_sso_domains
            (id,tenant_id,connection_id,domain,verification_token_hash) VALUES($1,$2,$3,$4,$5) RETURNING `+ssoDomainColumns,
			id, tenant, conn, domain, hash))
		if err != nil {
			return err
		}
		result = challenge
		return ssoAudit(ctx, tx, tenant, p.User.ID, id, "tenant_sso_domain", "tenant.sso_domain.created", audit.Metadata{"status": "pending", "sso_kind": kind})
	})
	if err != nil {
		return SSOChallenge{}, err
	}
	return result, nil
}
func (s *SchoolSSOService) RotateDomain(ctx context.Context, p Principal, tenant, id uuid.UUID) (SSOChallenge, error) {
	var result SSOChallenge
	err := s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		var domain, status string
		if err := tx.QueryRow(ctx, `SELECT domain,status FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&domain, &status); err != nil {
			return ssoReadError(err)
		}
		if status != "pending" {
			return ErrSSOConflict
		}
		challenge, hash, err := s.newChallenge(tenant, id, domain)
		if err != nil {
			return err
		}
		challenge.Domain, err = scanSSODomain(tx.QueryRow(ctx, `UPDATE tutorhub.auth_sso_domains SET verification_token_hash=$3,updated_at=clock_timestamp()
            WHERE tenant_id=$1 AND id=$2 RETURNING `+ssoDomainColumns, tenant, id, hash))
		if err != nil {
			return err
		}
		result = challenge
		return ssoAudit(ctx, tx, tenant, p.User.ID, id, "tenant_sso_domain", "tenant.sso_domain.rotated", audit.Metadata{"status": "pending"})
	})
	if err != nil {
		return SSOChallenge{}, err
	}
	return result, nil
}

type ssoVerification struct {
	id             uuid.UUID
	domain, status string
	hash           []byte
}

func readSSOVerification(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (ssoVerification, error) {
	var state ssoVerification
	state.id = id
	err := tx.QueryRow(ctx, `SELECT domain,status,verification_token_hash FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).
		Scan(&state.domain, &state.status, &state.hash)
	return state, ssoReadError(err)
}

// Some OS resolvers can keep a lookup in flight after cancellation. The
// caller's deadline stays independent and abandoned lookups consume a slot
// until the resolver returns, keeping the number of workers bounded.
func (s *SchoolSSOService) lookupTXT(ctx context.Context, name string) ([]string, error) {
	select {
	case s.dnsSlots <- struct{}{}:
	default:
		return nil, ErrSSODNSUnavailable
	}
	type response struct {
		records []string
		err     error
	}
	done := make(chan response, 1)
	resolver := s.resolver
	go func() {
		defer func() { <-s.dnsSlots }()
		records, err := resolver.LookupTXT(ctx, name)
		done <- response{records, err}
	}()
	select {
	case r := <-done:
		return r.records, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *SchoolSSOService) matchTXT(ctx context.Context, tenant uuid.UUID, state ssoVerification) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	records, err := s.lookupTXT(ctx, ssoTXTName(state.domain))
	if ctx.Err() != nil {
		return ErrSSODNSUnavailable
	}
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return ErrSSODNSMismatch
		}
		return ErrSSODNSUnavailable
	}
	if len(records) > 32 {
		return ErrSSODNSMismatch
	}
	found := false
	for _, value := range records {
		if len(value) > 512 {
			return ErrSSODNSMismatch
		}
		if !strings.HasPrefix(value, ssoTXTPrefix) {
			continue
		}
		if s.native.crypto.EqualDigest(state.hash, ssoTXTPurpose(tenant, state.id), value) {
			found = true
		}
	}
	if !found {
		return ErrSSODNSMismatch
	}
	return nil
}

func (s *SchoolSSOService) VerifyDomain(ctx context.Context, p Principal, tenant, id uuid.UUID) (SSODomain, error) {
	var snapshot ssoVerification
	var result SSODomain
	err := s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		var err error
		snapshot, err = readSSOVerification(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		switch snapshot.status {
		case "verified":
			result, err = scanSSODomain(tx.QueryRow(ctx, `SELECT `+ssoDomainColumns+` FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 AND id=$2`, tenant, id))
			return err
		case "pending":
			return nil
		default:
			return ErrSSOConflict
		}
	})
	if err != nil || snapshot.status == "verified" {
		return result, err
	}
	// Release session/tenant and policy locks while the upstream DNS resolver runs.
	if err = s.matchTXT(ctx, tenant, snapshot); err != nil {
		return result, err
	}
	err = s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, locked lockedSecuritySession) error {
		state, err := readSSOVerification(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		if state.status != "pending" || !hmac.Equal(state.hash, snapshot.hash) {
			return ErrSSOConflict
		}
		c, err := scanSSOConnection(tx.QueryRow(ctx, `SELECT `+ssoConnectionColumns+` FROM tutorhub.auth_sso_connections WHERE tenant_id=$1
            AND id=(SELECT connection_id FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 AND id=$2)`, tenant, id))
		if err != nil {
			return err
		}
		ownDomain, _ := NormalizeSSODomain(SSOEmailDomain(locked.Email))
		if c.EnforceSSO && ownDomain == state.domain {
			if err := s.preventAdminLockout(ctx, tx, p, c, ownDomain); err != nil {
				return err
			}
		}
		result, err = promoteVerifiedDomain(ctx, tx, tenant, id)
		if err != nil {
			return err
		}
		return ssoAudit(ctx, tx, tenant, p.User.ID, id, "tenant_sso_domain", "tenant.sso_domain.verified", audit.Metadata{"sso_kind": c.Kind, "status": "verified"})
	})
	return result, err
}

func promoteVerifiedDomain(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (SSODomain, error) {
	// A unique violation discloses no details of the other tenant.
	_, err := tx.Exec(ctx, `UPDATE tutorhub.auth_sso_domains SET status='verified',verification_token_hash=NULL,
        verified_at=statement_timestamp(),updated_at=statement_timestamp() WHERE tenant_id=$1 AND id=$2`, tenant, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "auth_sso_domains_verified_unique" {
		return SSODomain{}, ErrSSOConflict
	}
	if err != nil {
		return SSODomain{}, ErrAuthFoundationStorage
	}
	return scanSSODomain(tx.QueryRow(ctx, `SELECT `+ssoDomainColumns+` FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 AND id=$2`, tenant, id))
}

func (s *SchoolSSOService) DisableDomain(ctx context.Context, p Principal, tenant, id uuid.UUID) (SSODomain, error) {
	var result SSODomain
	err := s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		var err error
		result, err = scanSSODomain(tx.QueryRow(ctx, `SELECT `+ssoDomainColumns+` FROM tutorhub.auth_sso_domains WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return err
		}
		if result.Status == "disabled" {
			return nil
		}
		result, err = scanSSODomain(tx.QueryRow(ctx, `UPDATE tutorhub.auth_sso_domains SET status='disabled',verification_token_hash=NULL,
            verified_at=NULL,updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2 RETURNING `+ssoDomainColumns, tenant, id))
		if err != nil {
			return err
		}
		return ssoAudit(ctx, tx, tenant, p.User.ID, id, "tenant_sso_domain", "tenant.sso_domain.disabled", audit.Metadata{"status": "disabled"})
	})
	return result, err
}
