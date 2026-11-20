package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type SchoolSSOAdmin interface {
	ListConnections(context.Context, identity.Principal, uuid.UUID) ([]identity.SSOConnection, error)
	CreateConnection(context.Context, identity.Principal, uuid.UUID, string, []string) (identity.SSOConnection, error)
	UpdateConnection(context.Context, identity.Principal, uuid.UUID, uuid.UUID, identity.SSOConnectionPatch) (identity.SSOConnection, error)
	ListDomains(context.Context, identity.Principal, uuid.UUID) ([]identity.SSODomain, error)
	CreateDomain(context.Context, identity.Principal, uuid.UUID, uuid.UUID, string) (identity.SSOChallenge, error)
	RotateDomain(context.Context, identity.Principal, uuid.UUID, uuid.UUID) (identity.SSOChallenge, error)
	VerifyDomain(context.Context, identity.Principal, uuid.UUID, uuid.UUID) (identity.SSODomain, error)
	DisableDomain(context.Context, identity.Principal, uuid.UUID, uuid.UUID) (identity.SSODomain, error)
}

type schoolSSOHandlers struct {
	auth    authHandlers
	service SchoolSSOAdmin
}

// The session establishes the audit tenant before the role and assurance checks.
// Foreign path assertions never select the tenant that receives an audit row.
func (h schoolSSOHandlers) register(mux *http.ServeMux, recorder audit.ServiceAPI) {
	const base = "/api/v1/tenants/{tenant_id}/"
	type endpoint func(context.Context, http.ResponseWriter, *http.Request, identity.Principal, uuid.UUID, uuid.UUID) (any, error)
	bind := func(method, path, idParam string, action audit.Action, resource string, status int, run endpoint) {
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.auth.available(w, r) {
				return
			}
			var p identity.Principal
			var ok bool
			if method == http.MethodGet {
				p, ok = h.auth.authenticatedPrincipal(w, r)
			} else {
				token, present := h.auth.sessionToken(w, r)
				if !present {
					return
				}
				p, ok = h.auth.csrfPrincipal(w, r, token)
			}
			if !ok {
				return
			}
			tenant, valid := parseResourceUUID(r.PathValue("tenant_id"))
			if !valid || p.ActiveTenant == nil || p.ActiveTenant.ID != tenant {
				h.writeError(w, r, identity.ErrSSONotFound)
				return
			}
			requestmeta.SetAuditTenant(r.Context(), tenant)
			if p.ActiveTenant.Role != string(policy.OrganizationRoleAdmin) || !slices.Contains(p.Permissions, string(policy.PermissionTenantManage)) {
				h.writeError(w, r, identity.ErrSSOForbidden)
				return
			}
			if h.auth.adminMFARequired(p) {
				h.auth.writeIdentityProblem(w, r, identity.ErrAdminMFARequired)
				return
			}
			var id uuid.UUID
			if idParam != "" {
				id, valid = parseResourceUUID(r.PathValue(idParam))
				if !valid {
					h.writeError(w, r, identity.ErrSSONotFound)
					return
				}
			}
			if h.service == nil {
				h.writeError(w, r, identity.ErrAuthFoundationStorage)
				return
			}
			value, err := run(r.Context(), w, r, p, tenant, id)
			if err != nil {
				h.writeError(w, r, err)
				return
			}
			writeJSON(h.auth.logger, w, status, value)
		})
		if method != http.MethodGet {
			handler = preAuthGuard(h.auth.config.WebOrigin, handler)
			handler = auditResolvedTenantMutationMiddleware(h.auth.logger, recorder, staticAuditMutation(method, action, resource, pathValueAuditResource(idParam)), handler)
		}
		// Register matching routes even when off so the generic tenant handler cannot intercept them.
		if !h.auth.config.Authentication.SSODomainsEnabled {
			handler = notFoundHandler()
		}
		mux.Handle(method+" "+base+path, authFoundationResponseHeaders(handler))
	}
	const conns = "sso-connections"
	const domains = "sso-domains"
	const connResource = "tenant_sso_connection"
	const domainResource = "tenant_sso_domain"
	bind("GET", conns, "", "", "", 200, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, p identity.Principal, t, _ uuid.UUID) (any, error) {
		items, err := h.service.ListConnections(ctx, p, t)
		return struct {
			Items []identity.SSOConnection `json:"items"`
		}{items}, err
	})
	bind("POST", conns, "", audit.ActionTenantSSOConnectionCreate, connResource, 201, func(ctx context.Context, w http.ResponseWriter, r *http.Request, p identity.Principal, t, _ uuid.UUID) (any, error) {
		var input struct {
			Kind          string   `json:"kind"`
			AllowedOrgIDs []string `json:"allowed_org_ids"`
		}
		if decodeJSONRequest(w, r, &input, 32768) != nil {
			return nil, identity.ErrSSOInvalid
		}
		return h.service.CreateConnection(ctx, p, t, input.Kind, input.AllowedOrgIDs)
	})
	bind("PATCH", conns+"/{connection_id}", "connection_id", audit.ActionTenantSSOConnectionUpdate, connResource, 200, func(ctx context.Context, w http.ResponseWriter, r *http.Request, p identity.Principal, t, id uuid.UUID) (any, error) {
		patch, err := decodeSSOConnectionPatch(w, r)
		if err != nil {
			return nil, err
		}
		return h.service.UpdateConnection(ctx, p, t, id, patch)
	})
	bind("GET", domains, "", "", "", 200, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, p identity.Principal, t, _ uuid.UUID) (any, error) {
		items, err := h.service.ListDomains(ctx, p, t)
		return struct {
			Items []identity.SSODomain `json:"items"`
		}{items}, err
	})
	bind("POST", domains, "", audit.ActionTenantSSODomainCreate, domainResource, 201, func(ctx context.Context, w http.ResponseWriter, r *http.Request, p identity.Principal, t, _ uuid.UUID) (any, error) {
		var input struct {
			ConnectionID uuid.UUID `json:"connection_id"`
			Domain       string    `json:"domain"`
		}
		if decodeJSONRequest(w, r, &input, 2048) != nil || input.ConnectionID == uuid.Nil {
			return nil, identity.ErrSSOInvalid
		}
		return h.service.CreateDomain(ctx, p, t, input.ConnectionID, input.Domain)
	})
	for _, mutation := range []struct {
		verb   string
		action audit.Action
	}{
		{"rotate", audit.ActionTenantSSODomainRotate}, {"verify", audit.ActionTenantSSODomainVerify}, {"disable", audit.ActionTenantSSODomainDisable},
	} {
		bind("POST", domains+"/{domain_id}/"+mutation.verb, "domain_id", mutation.action, domainResource, 200, func(ctx context.Context, w http.ResponseWriter, r *http.Request, p identity.Principal, t, id uuid.UUID) (any, error) {
			if decodeJSONRequest(w, r, &struct{}{}, 256) != nil {
				return nil, identity.ErrSSOInvalid
			}
			switch mutation.action {
			case audit.ActionTenantSSODomainRotate:
				return h.service.RotateDomain(ctx, p, t, id)
			case audit.ActionTenantSSODomainVerify:
				return h.service.VerifyDomain(ctx, p, t, id)
			default:
				return h.service.DisableDomain(ctx, p, t, id)
			}
		})
	}
}

func decodeSSOConnectionPatch(w http.ResponseWriter, r *http.Request) (identity.SSOConnectionPatch, error) {
	var patch identity.SSOConnectionPatch
	var fields map[string]json.RawMessage
	if decodeJSONRequest(w, r, &fields, 32768) != nil || len(fields) == 0 {
		return patch, identity.ErrSSOInvalid
	}
	for key, value := range fields {
		var dst any
		switch key {
		case "auto_join_role":
			patch.SetAutoJoinRole = true
			dst = &patch.AutoJoinRole
		case "allowed_org_ids":
			dst = &patch.AllowedOrgIDs
		case "enforce_sso":
			dst = &patch.EnforceSSO
		case "status":
			dst = &patch.Status
		default:
			return patch, identity.ErrSSOInvalid
		}
		if (key != "auto_join_role" && bytes.Equal(value, []byte("null"))) || json.Unmarshal(value, dst) != nil {
			return patch, identity.ErrSSOInvalid
		}
	}
	return patch, nil
}

func (h schoolSSOHandlers) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrSSOInvalid):
		writeCodedProblem(w, r, 400, "auth_sso_invalid", "Invalid school SSO configuration", "Check the domain, connection and policy values.")
	case errors.Is(err, identity.ErrSSONotFound):
		notFoundHandler().ServeHTTP(w, r)
	case errors.Is(err, identity.ErrSSOForbidden):
		writeCodedProblem(w, r, 403, "auth_sso_forbidden", "Forbidden", "School SSO management requires an administrator of this workspace.")
	case errors.Is(err, identity.ErrSSOLockout):
		writeCodedProblem(w, r, 409, "auth_sso_lockout", "Administrator sign-in required", "Sign in through the school connection before applying this policy.")
	case errors.Is(err, identity.ErrSSODNSMismatch):
		writeCodedProblem(w, r, 409, "auth_sso_dns_mismatch", "DNS challenge pending", "Publish the current TXT record and retry verification.")
	case errors.Is(err, identity.ErrSSOConflict):
		writeCodedProblem(w, r, 409, "auth_sso_conflict", "School SSO conflict", "The requested change conflicts with the current domain or connection state.")
	case errors.Is(err, identity.ErrSSODNSUnavailable), errors.Is(err, identity.ErrAuthFoundationStorage):
		writeProblem(w, r, 503, "Service unavailable", "School SSO is temporarily unavailable.")
	default:
		h.auth.writeIdentityProblem(w, r, err)
	}
}
