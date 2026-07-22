package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
)

type auditMutationResolver func(*http.Request) (audit.Draft, bool)

func auditMutationMiddleware(
	logger *slog.Logger,
	service audit.ServiceAPI,
	resolve auditMutationResolver,
	next http.Handler,
) http.Handler {
	return auditMutationMiddlewareWithScope(logger, service, resolve, false, next)
}

// auditResolvedTenantMutationMiddleware is reserved for sensitive mutations
// whose tenant can only be established after authoritative resource lookup.
// If lookup never resolves a tenant, no durable row is written against the
// caller's unrelated active workspace.
func auditResolvedTenantMutationMiddleware(
	logger *slog.Logger,
	service audit.ServiceAPI,
	resolve auditMutationResolver,
	next http.Handler,
) http.Handler {
	return auditMutationMiddlewareWithScope(logger, service, resolve, true, next)
}

func auditMutationMiddlewareWithScope(
	logger *slog.Logger,
	service audit.ServiceAPI,
	resolve auditMutationResolver,
	requireResolvedTenant bool,
	next http.Handler,
) http.Handler {
	if service == nil || resolve == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		draft, sensitive := resolve(r)
		if !sensitive {
			next.ServeHTTP(w, r)
			return
		}

		recorder := &statusRecorder{ResponseWriter: w}
		defer func() {
			recovered := recover()
			status := recorder.status
			if !recorder.wroteHeader {
				status = http.StatusOK
				if recovered != nil {
					status = http.StatusInternalServerError
				}
			}
			request := requestmeta.SnapshotFromContext(r.Context())
			tenantID := request.TenantID
			tenantResolved := !requireResolvedTenant || request.AuditTenantResolved
			if request.AuditTenantResolved {
				tenantID = request.AuditTenantID
			}
			if tenantResolved && request.ActorID != uuid.Nil && tenantID != uuid.Nil {
				draft.TenantID = tenantID
				draft.ActorID = request.ActorID
				draft.Metadata = audit.Metadata{"http_status": strconv.Itoa(status)}
				switch {
				case recovered != nil:
					draft.Outcome = audit.OutcomeFailed
					draft.Metadata["reason_code"] = "internal_failure"
				case status >= http.StatusOK && status < http.StatusMultipleChoices:
					draft.Outcome = audit.OutcomeSucceeded
					draft.Metadata["effect"] = "unchanged"
				case status == http.StatusForbidden || status == http.StatusNotFound:
					draft.Outcome = audit.OutcomeDenied
					draft.Metadata["reason_code"] = auditFailureReason(status)
				default:
					draft.Outcome = audit.OutcomeFailed
					draft.Metadata["reason_code"] = auditFailureReason(status)
				}
				if err := service.RecordFallback(
					context.WithoutCancel(r.Context()), draft,
				); err != nil {
					logger.Error(
						"audit_write_failed",
						"request_id", request.RequestID,
						"action", draft.Action,
						"status", status,
						"reason", "persistence_failed",
					)
				}
			}
			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(recorder, r)
	})
}

func auditFailureReason(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "resource_unavailable"
	case http.StatusConflict, http.StatusPreconditionFailed:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		if status >= http.StatusInternalServerError {
			return "internal_failure"
		}
		return "request_failed"
	}
}

func staticAuditMutation(
	method string,
	action audit.Action,
	resourceType string,
	resourceID func(*http.Request) uuid.UUID,
) auditMutationResolver {
	return func(r *http.Request) (audit.Draft, bool) {
		if r.Method != method {
			return audit.Draft{}, false
		}
		draft := audit.Draft{Action: action, ResourceType: resourceType}
		if resourceID != nil {
			draft.ResourceID = resourceID(r)
		}
		return draft, true
	}
}

func pathValueAuditResource(name string) func(*http.Request) uuid.UUID {
	return func(r *http.Request) uuid.UUID {
		value, _ := parseResourceUUID(r.PathValue(name))
		return value
	}
}

func tenantResourceAuditMutation(r *http.Request) (audit.Draft, bool) {
	tenantID, archive, ok := parseTenantResourcePath(r.URL.Path)
	if !ok {
		return audit.Draft{}, false
	}
	switch {
	case !archive && r.Method == http.MethodPatch:
		return audit.Draft{
			Action: audit.ActionTenantUpdate, ResourceType: "tenant", ResourceID: tenantID,
		}, true
	case archive && r.Method == http.MethodPost:
		return audit.Draft{
			Action: audit.ActionTenantArchive, ResourceType: "tenant", ResourceID: tenantID,
		}, true
	default:
		return audit.Draft{}, false
	}
}

func classResourceUpdateAuditMutation(r *http.Request) (audit.Draft, bool) {
	route, ok := parseClassRoute(r.URL.Path)
	if !ok || route.Action != "" || r.Method != http.MethodPatch {
		return audit.Draft{}, false
	}
	return audit.Draft{
		Action: audit.ActionClassUpdate, ResourceType: "class", ResourceID: route.ID,
	}, true
}
