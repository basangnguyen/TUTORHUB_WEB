package identity

import (
	"context"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func (s *SchoolSSOService) CreateConnection(ctx context.Context, p Principal, tenant uuid.UUID, kind string, orgIDs []string) (SSOConnection, error) {
	var result SSOConnection
	err := s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, _ lockedSecuritySession) error {
		ids, err := normalizeSSOOrgIDs(kind, orgIDs)
		if err != nil {
			return err
		}
		result, err = scanSSOConnection(tx.QueryRow(ctx, `INSERT INTO tutorhub.auth_sso_connections(tenant_id,kind,allowed_org_ids)
            VALUES($1,$2,$3) RETURNING `+ssoConnectionColumns, tenant, kind, ids))
		if err != nil {
			return err
		}
		return ssoAudit(ctx, tx, tenant, p.User.ID, result.ID, "tenant_sso_connection", "tenant.sso_connection.created", result.auditMetadata())
	})
	return result, err
}

func (c SSOConnection) auditMetadata() audit.Metadata {
	role := "none"
	if c.AutoJoinRole != nil {
		role = *c.AutoJoinRole
	}
	return audit.Metadata{"sso_kind": c.Kind, "status": c.Status, "enforce_sso": strconv.FormatBool(c.EnforceSSO), "auto_join_role": role, "org_id_count": strconv.Itoa(len(c.AllowedOrgIDs))}
}

func (p SSOConnectionPatch) apply(c SSOConnection) (SSOConnection, error) {
	if p.Status == nil && p.AllowedOrgIDs == nil && p.EnforceSSO == nil && !p.SetAutoJoinRole {
		return c, ErrSSOInvalid
	}
	if p.Status != nil {
		if *p.Status != "enabled" && *p.Status != "disabled" {
			return c, ErrSSOInvalid
		}
		c.Status = *p.Status
		if c.Status == "disabled" {
			c.EnforceSSO = false
		}
	}
	if p.EnforceSSO != nil {
		c.EnforceSSO = *p.EnforceSSO
	}
	if p.AllowedOrgIDs != nil {
		var err error
		if c.AllowedOrgIDs, err = normalizeSSOOrgIDs(c.Kind, *p.AllowedOrgIDs); err != nil {
			return c, err
		}
	}
	if p.SetAutoJoinRole {
		if p.AutoJoinRole != nil && *p.AutoJoinRole != string(policy.OrganizationRoleStudent) {
			return c, ErrSSOInvalid
		}
		c.AutoJoinRole = p.AutoJoinRole
	}
	return c, nil
}
func (s *SchoolSSOService) UpdateConnection(ctx context.Context, p Principal, tenant, id uuid.UUID, patch SSOConnectionPatch) (SSOConnection, error) {
	var result SSOConnection
	err := s.transaction(ctx, p, tenant, true, func(ctx context.Context, tx pgx.Tx, locked lockedSecuritySession) error {
		old, err := scanSSOConnection(tx.QueryRow(ctx, `SELECT `+ssoConnectionColumns+` FROM tutorhub.auth_sso_connections WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return err
		}
		c, err := patch.apply(old)
		if err != nil {
			return err
		}
		if c.EnforceSSO {
			if c.Status != "enabled" || (c.Kind == "google_workspace" && !s.googleEnabled) || (c.Kind == "entra" && !s.microsoftEnabled) {
				return ErrSSOConflict
			}
			domain, _ := NormalizeSSODomain(SSOEmailDomain(locked.Email))
			var anyVerified, ownVerified bool
			if err := tx.QueryRow(ctx, `SELECT count(*)>0,count(*) FILTER(WHERE domain=$3)>0 FROM tutorhub.auth_sso_domains
                WHERE tenant_id=$1 AND connection_id=$2 AND status='verified'`, tenant, id, domain).Scan(&anyVerified, &ownVerified); err != nil {
				return ErrAuthFoundationStorage
			}
			if !anyVerified {
				return ErrSSOConflict
			}
			if ownVerified && (!old.EnforceSSO || !slices.Equal(c.AllowedOrgIDs, old.AllowedOrgIDs)) {
				if err := s.preventAdminLockout(ctx, tx, p, c, domain); err != nil {
					return err
				}
			}
		}
		result, err = scanSSOConnection(tx.QueryRow(ctx, `UPDATE tutorhub.auth_sso_connections SET status=$3,allowed_org_ids=$4,
            enforce_sso=$5,auto_join_role=$6,updated_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2 RETURNING `+ssoConnectionColumns,
			tenant, id, c.Status, c.AllowedOrgIDs, c.EnforceSSO, c.AutoJoinRole))
		if err != nil {
			return err
		}
		return ssoAudit(ctx, tx, tenant, p.User.ID, id, "tenant_sso_connection", "tenant.sso_connection.updated", result.auditMetadata())
	})
	return result, err
}

func (s *SchoolSSOService) preventAdminLockout(ctx context.Context, tx pgx.Tx, p Principal, c SSOConnection, emailDomain string) error {
	proven, err := schoolSessionProvesOrg(ctx, tx, p.SessionID, p.User.ID, c.Kind, emailDomain, c.AllowedOrgIDs, s.native.crypto)
	if err != nil {
		return err
	}
	if !proven {
		return ErrSSOLockout
	}
	return nil
}
