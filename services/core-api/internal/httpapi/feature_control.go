package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	tenantCapabilitiesPattern    = "/api/v1/tenants/{tenant_id}/capabilities"
	tenantFeatureControlsPattern = "/api/v1/tenants/{tenant_id}/feature-controls"
)

type featureControlHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service featurecontrol.ServiceAPI
}

type featureCapabilityResponse struct {
	Enabled           bool  `json:"enabled"`
	ConfiguredEnabled *bool `json:"configured_enabled,omitempty"`
}

type quotaCapabilityResponse struct {
	Limit           int64      `json:"limit"`
	ConfiguredLimit *int64     `json:"configured_limit,omitempty"`
	Used            int64      `json:"used"`
	Remaining       int64      `json:"remaining"`
	ResetAt         *time.Time `json:"reset_at,omitempty"`
}

type operationCapabilityResponse struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type tenantFeatureCapabilitiesResponse struct {
	MembershipInvitations featureCapabilityResponse `json:"membership_invitations"`
	ClassManagement       featureCapabilityResponse `json:"class_management"`
	ClassInviteLinks      featureCapabilityResponse `json:"class_invite_links"`
}

type tenantQuotaCapabilitiesResponse struct {
	Members                quotaCapabilityResponse `json:"members"`
	ActiveClasses          quotaCapabilityResponse `json:"active_classes"`
	InviteCreationsPerHour quotaCapabilityResponse `json:"invite_creations_per_hour"`
}

type tenantOperationCapabilitiesResponse struct {
	CreateMembershipInvitation operationCapabilityResponse `json:"create_membership_invitation"`
	AcceptMembershipInvitation operationCapabilityResponse `json:"accept_membership_invitation"`
	CreateClass                operationCapabilityResponse `json:"create_class"`
	ActivateClass              operationCapabilityResponse `json:"activate_class"`
	RestoreActiveClass         operationCapabilityResponse `json:"restore_active_class"`
	CreateClassInviteLink      operationCapabilityResponse `json:"create_class_invite_link"`
	JoinClassInviteLink        operationCapabilityResponse `json:"join_class_invite_link"`
}

type tenantCapabilitiesResponse struct {
	TenantID           uuid.UUID                           `json:"tenant_id"`
	Version            int64                               `json:"version"`
	CanManageOverrides bool                                `json:"can_manage_overrides"`
	Features           tenantFeatureCapabilitiesResponse   `json:"features"`
	Quotas             tenantQuotaCapabilitiesResponse     `json:"quotas"`
	Operations         tenantOperationCapabilitiesResponse `json:"operations"`
}

type updateTenantFeatureControlsRequest struct {
	ExpectedVersion *int64                                   `json:"expected_version"`
	Features        *updateTenantFeatureControlValuesRequest `json:"features"`
	Quotas          *updateTenantQuotaControlValuesRequest   `json:"quotas"`
}

type updateTenantFeatureControlValuesRequest struct {
	MembershipInvitations *bool `json:"membership_invitations"`
	ClassManagement       *bool `json:"class_management"`
	ClassInviteLinks      *bool `json:"class_invite_links"`
}

type updateTenantQuotaControlValuesRequest struct {
	Members                *int64 `json:"members"`
	ActiveClasses          *int64 `json:"active_classes"`
	InviteCreationsPerHour *int64 `json:"invite_creations_per_hour"`
}

func (request updateTenantFeatureControlsRequest) complete() bool {
	return request.ExpectedVersion != nil && request.Features != nil &&
		request.Features.MembershipInvitations != nil &&
		request.Features.ClassManagement != nil &&
		request.Features.ClassInviteLinks != nil &&
		request.Quotas != nil && request.Quotas.Members != nil &&
		request.Quotas.ActiveClasses != nil &&
		request.Quotas.InviteCreationsPerHour != nil
}

func newFeatureControlHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service featurecontrol.ServiceAPI,
) featureControlHandlers {
	return featureControlHandlers{logger: logger, auth: auth, service: service}
}

func featureControlResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Add("Vary", "Cookie")
		next.ServeHTTP(w, r)
	})
}

func (handlers featureControlHandlers) capabilities(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	tenantContext, ok := activeTenantContext(principal, r.PathValue("tenant_id"))
	if !ok {
		handlers.writeProblem(w, r, featurecontrol.ErrTenantNotFound)
		return
	}
	capabilities, err := handlers.service.GetCapabilities(r.Context(), tenantContext)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	response, err := mapTenantCapabilities(capabilities)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, response)
}

func (handlers featureControlHandlers) update(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return
	}
	tenantContext, ok := activeTenantContext(principal, r.PathValue("tenant_id"))
	if !ok {
		handlers.writeProblem(w, r, featurecontrol.ErrTenantNotFound)
		return
	}
	var request updateTenantFeatureControlsRequest
	if err := decodeJSONRequest(w, r, &request, 16<<10); err != nil {
		handlers.writeProblem(w, r, featurecontrol.ErrInvalidControl)
		return
	}
	if !request.complete() {
		handlers.writeProblem(w, r, featurecontrol.ErrInvalidControl)
		return
	}
	capabilities, err := handlers.service.PutOverrides(
		r.Context(),
		tenantContext,
		featurecontrol.PutOverridesInput{
			ExpectedVersion: *request.ExpectedVersion,
			FeatureOverrides: []featurecontrol.FeatureOverride{
				{Key: featurecontrol.FeatureMembershipInvitations, Enabled: *request.Features.MembershipInvitations},
				{Key: featurecontrol.FeatureClassManagement, Enabled: *request.Features.ClassManagement},
				{Key: featurecontrol.FeatureClassInviteLinks, Enabled: *request.Features.ClassInviteLinks},
			},
			QuotaOverrides: []featurecontrol.QuotaOverride{
				{Key: featurecontrol.QuotaMembers, Limit: *request.Quotas.Members},
				{Key: featurecontrol.QuotaActiveClasses, Limit: *request.Quotas.ActiveClasses},
				{Key: featurecontrol.QuotaInviteCreationsPerHour, Limit: *request.Quotas.InviteCreationsPerHour},
			},
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	response, err := mapTenantCapabilities(capabilities)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, response)
}

func (handlers featureControlHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service == nil {
		writeCodedProblem(
			w, r, http.StatusServiceUnavailable, "feature_control_unavailable",
			"Feature controls unavailable",
			"Tenant capabilities are not configured for this environment.",
		)
		return false
	}
	return true
}

func activeTenantContext(
	principal identity.Principal,
	rawTenantID string,
) (tenancy.Context, bool) {
	tenantID, ok := parseResourceUUID(rawTenantID)
	if !ok || principal.ActiveTenant == nil || principal.ActiveTenant.ID != tenantID {
		return tenancy.Context{}, false
	}
	tenantContext, err := tenancy.New(tenantID, principal.User.ID)
	return tenantContext, err == nil
}

func (handlers featureControlHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusServiceUnavailable
	code := "feature_control_unavailable"
	title := "Feature control request failed"
	detail := "Tenant capabilities could not be evaluated safely."
	switch {
	case errors.Is(err, featurecontrol.ErrInvalidControl):
		status, code = http.StatusBadRequest, "feature_control_invalid"
		title, detail = "Invalid feature controls", "Review the feature and quota values."
	case errors.Is(err, featurecontrol.ErrAccessDenied):
		status, code = http.StatusForbidden, "feature_control_forbidden"
		title, detail = "Feature control access denied", "Only an organization administrator can change tenant controls."
	case errors.Is(err, featurecontrol.ErrTenantNotFound):
		status, code = http.StatusNotFound, "tenant_not_found"
		title, detail = "Workspace not found", "The workspace does not exist in the active tenant scope."
	case errors.Is(err, featurecontrol.ErrFeatureDisabled):
		status, code = http.StatusForbidden, "feature_disabled"
		title, detail = "Feature disabled", "This operation is disabled by the effective tenant controls."
	case errors.Is(err, featurecontrol.ErrVersionConflict):
		status, code = http.StatusConflict, "feature_control_conflict"
		title, detail = "Feature controls changed", "Reload the latest controls before saving again."
	case errors.Is(err, featurecontrol.ErrQuotaExceeded):
		status, code = http.StatusConflict, "quota_exceeded"
		title, detail = "Quota exceeded", "The operation would exceed the effective tenant quota."
		var quotaError *featurecontrol.QuotaExceededError
		if errors.As(err, &quotaError) && quotaError.RetryAfter > 0 {
			status = http.StatusTooManyRequests
			w.Header().Set("Retry-After", strconv.FormatInt(maxInt64(1, int64(quotaError.RetryAfter.Round(time.Second)/time.Second)), 10))
		}
	default:
		handlers.logger.Error("feature control request failed", "error", err)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func writeFeatureControlEnforcementProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) bool {
	status := 0
	code := ""
	title := ""
	detail := ""
	switch {
	case errors.Is(err, featurecontrol.ErrFeatureDisabled):
		status, code = http.StatusForbidden, "feature_disabled"
		title, detail = "Feature disabled", "This operation is disabled by the effective tenant controls."
	case errors.Is(err, featurecontrol.ErrQuotaExceeded):
		status, code = http.StatusConflict, "quota_exceeded"
		title, detail = "Quota exceeded", "The operation would exceed the effective tenant quota."
		var quotaError *featurecontrol.QuotaExceededError
		if errors.As(err, &quotaError) && quotaError.RetryAfter > 0 {
			status = http.StatusTooManyRequests
			seconds := int64(quotaError.RetryAfter.Round(time.Second) / time.Second)
			w.Header().Set("Retry-After", strconv.FormatInt(maxInt64(1, seconds), 10))
		}
	case errors.Is(err, featurecontrol.ErrAccessDenied):
		status, code = http.StatusForbidden, "feature_control_forbidden"
		title, detail = "Operation unavailable", "The effective tenant controls cannot authorize this operation."
	case errors.Is(err, featurecontrol.ErrTenantNotFound):
		status, code = http.StatusNotFound, "tenant_not_found"
		title, detail = "Workspace not found", "The workspace does not exist in the active tenant scope."
	case errors.Is(err, featurecontrol.ErrInvalidControl):
		status, code = http.StatusServiceUnavailable, "feature_control_unavailable"
		title, detail = "Feature controls unavailable", "Tenant capabilities could not be evaluated safely."
	case errors.Is(err, featurecontrol.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "feature_control_unavailable"
		title, detail = "Feature controls unavailable", "Tenant capabilities could not be evaluated safely."
	default:
		return false
	}
	writeCodedProblem(w, r, status, code, title, detail)
	return true
}

func mapTenantCapabilities(
	capabilities featurecontrol.Capabilities,
) (tenantCapabilitiesResponse, error) {
	features := make(map[featurecontrol.FeatureKey]featurecontrol.FeatureCapability, len(capabilities.Features))
	for _, capability := range capabilities.Features {
		features[capability.Key] = capability
	}
	quotas := make(map[featurecontrol.QuotaKey]featurecontrol.QuotaCapability, len(capabilities.Quotas))
	for _, capability := range capabilities.Quotas {
		quotas[capability.Key] = capability
	}
	membershipFeature, membershipOK := features[featurecontrol.FeatureMembershipInvitations]
	classFeature, classOK := features[featurecontrol.FeatureClassManagement]
	inviteFeature, inviteOK := features[featurecontrol.FeatureClassInviteLinks]
	membersQuota, membersOK := quotas[featurecontrol.QuotaMembers]
	classesQuota, classesOK := quotas[featurecontrol.QuotaActiveClasses]
	invitesQuota, invitesOK := quotas[featurecontrol.QuotaInviteCreationsPerHour]
	if !membershipOK || !classOK || !inviteOK || !membersOK || !classesOK || !invitesOK {
		return tenantCapabilitiesResponse{}, errors.New("feature control snapshot is incomplete")
	}
	response := tenantCapabilitiesResponse{
		TenantID:           capabilities.TenantID,
		Version:            capabilities.Version,
		CanManageOverrides: capabilities.AllowedAction.ManageControls,
		Features: tenantFeatureCapabilitiesResponse{
			MembershipInvitations: mapFeatureCapability(membershipFeature, capabilities.AllowedAction.ManageControls),
			ClassManagement:       mapFeatureCapability(classFeature, capabilities.AllowedAction.ManageControls),
			ClassInviteLinks:      mapFeatureCapability(inviteFeature, capabilities.AllowedAction.ManageControls),
		},
		Quotas: tenantQuotaCapabilitiesResponse{
			Members:                mapQuotaCapability(membersQuota, capabilities.AllowedAction.ManageControls),
			ActiveClasses:          mapQuotaCapability(classesQuota, capabilities.AllowedAction.ManageControls),
			InviteCreationsPerHour: mapQuotaCapability(invitesQuota, capabilities.AllowedAction.ManageControls),
		},
	}
	response.Operations = tenantOperationCapabilitiesResponse{
		CreateMembershipInvitation: combineOperation(membershipFeature.Enabled, invitesQuota, true),
		AcceptMembershipInvitation: combineOperation(membershipFeature.Enabled, membersQuota, false),
		CreateClass:                featureOperation(classFeature.Enabled),
		ActivateClass:              combineOperation(classFeature.Enabled, classesQuota, false),
		RestoreActiveClass:         combineOperation(classFeature.Enabled, classesQuota, false),
		CreateClassInviteLink:      combineOperation(inviteFeature.Enabled, invitesQuota, true),
		JoinClassInviteLink:        featureOperation(inviteFeature.Enabled),
	}
	return response, nil
}

func mapFeatureCapability(
	capability featurecontrol.FeatureCapability,
	includeConfigured bool,
) featureCapabilityResponse {
	response := featureCapabilityResponse{Enabled: capability.Enabled}
	if includeConfigured {
		configured := capability.ConfiguredEnabled
		response.ConfiguredEnabled = &configured
	}
	return response
}

func mapQuotaCapability(
	capability featurecontrol.QuotaCapability,
	includeConfigured bool,
) quotaCapabilityResponse {
	response := quotaCapabilityResponse{
		Limit: capability.Limit, Used: capability.Used, Remaining: capability.Remaining,
		ResetAt: capability.ResetAt,
	}
	if includeConfigured {
		configured := capability.ConfiguredLimit
		response.ConfiguredLimit = &configured
	}
	return response
}

func featureOperation(enabled bool) operationCapabilityResponse {
	if !enabled {
		return operationCapabilityResponse{Available: false, Reason: "feature_disabled"}
	}
	return operationCapabilityResponse{Available: true, Reason: "available"}
}

func combineOperation(
	enabled bool,
	quota featurecontrol.QuotaCapability,
	rateLimited bool,
) operationCapabilityResponse {
	if !enabled {
		return operationCapabilityResponse{Available: false, Reason: "feature_disabled"}
	}
	if quota.Remaining <= 0 {
		reason := "quota_exhausted"
		if rateLimited {
			reason = "rate_limited"
		}
		return operationCapabilityResponse{Available: false, Reason: reason}
	}
	return operationCapabilityResponse{Available: true, Reason: "available"}
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
