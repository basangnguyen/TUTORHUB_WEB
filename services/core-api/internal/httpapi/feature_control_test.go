package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type fakeFeatureControlService struct {
	capabilities featurecontrol.Capabilities
	err          error
	getContext   tenancy.Context
	putContext   tenancy.Context
	putInput     featurecontrol.PutOverridesInput
}

func (service *fakeFeatureControlService) GetCapabilities(
	_ context.Context,
	tenantContext tenancy.Context,
) (featurecontrol.Capabilities, error) {
	service.getContext = tenantContext
	return service.capabilities, service.err
}

func (service *fakeFeatureControlService) PutOverrides(
	_ context.Context,
	tenantContext tenancy.Context,
	input featurecontrol.PutOverridesInput,
) (featurecontrol.Capabilities, error) {
	service.putContext = tenantContext
	service.putInput = input
	return service.capabilities, service.err
}

func TestFeatureControlCapabilitiesAreActiveTenantScopedAndContractShaped(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	identityService := &fakeIdentityService{principal: auditViewerPrincipal(actorID, tenantID)}
	service := &fakeFeatureControlService{capabilities: completeFeatureCapabilities(tenantID)}
	handler := featureControlTestHandler(identityService, service)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+tenantID.String()+"/capabilities",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.getContext != (tenancy.Context{TenantID: tenantID, ActorID: actorID}) {
		t.Fatalf("unexpected tenant context: %+v", service.getContext)
	}
	var payload tenantCapabilitiesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.CanManageOverrides || payload.TenantID != tenantID ||
		payload.Operations.CreateMembershipInvitation.Reason != "rate_limited" ||
		payload.Operations.AcceptMembershipInvitation.Reason != "quota_exhausted" ||
		!payload.Operations.CreateClass.Available {
		t.Fatalf("unexpected capability response: %+v", payload)
	}
	if payload.Features.MembershipInvitations.ConfiguredEnabled == nil ||
		!*payload.Features.MembershipInvitations.ConfiguredEnabled ||
		payload.Quotas.Members.ConfiguredLimit == nil ||
		*payload.Quotas.Members.ConfiguredLimit != 100 {
		t.Fatalf("manager response must include configured edit values: %+v", payload)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("capability response must not be cached: %v", response.Header())
	}
}

func TestFeatureControlCapabilitiesOmitConfiguredValuesForReadOnlyMembers(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	capabilities := completeFeatureCapabilities(tenantID)
	capabilities.AllowedAction.ManageControls = false
	identityService := &fakeIdentityService{
		principal: auditViewerPrincipal(uuid.New(), tenantID),
	}
	handler := featureControlTestHandler(
		identityService,
		&fakeFeatureControlService{capabilities: capabilities},
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+tenantID.String()+"/capabilities",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload tenantCapabilitiesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.CanManageOverrides ||
		payload.Features.MembershipInvitations.ConfiguredEnabled != nil ||
		payload.Quotas.Members.ConfiguredLimit != nil {
		t.Fatalf("read-only response exposed configured edit values: %+v", payload)
	}
}

func TestFeatureControlHandlersHideCrossTenantAndReplaceTypedAggregate(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	identityService := &fakeIdentityService{principal: auditViewerPrincipal(actorID, tenantID)}
	service := &fakeFeatureControlService{capabilities: completeFeatureCapabilities(tenantID)}
	handler := featureControlTestHandler(identityService, service)

	crossTenant := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+uuid.NewString()+"/capabilities",
		nil,
	)
	crossTenant.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	crossResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossResponse, crossTenant)
	if crossResponse.Code != http.StatusNotFound || service.getContext.TenantID != uuid.Nil {
		t.Fatalf("cross-tenant request must be hidden before service call: %d %+v", crossResponse.Code, service.getContext)
	}

	body := `{"expected_version":3,"features":{"membership_invitations":false,"class_management":true,"class_invite_links":false},"quotas":{"members":90,"active_classes":20,"invite_creations_per_hour":40}}`
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/tenants/"+tenantID.String()+"/feature-controls",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.putContext.TenantID != tenantID || service.putInput.ExpectedVersion != 3 ||
		len(service.putInput.FeatureOverrides) != 3 || len(service.putInput.QuotaOverrides) != 3 {
		t.Fatalf("unexpected typed override input: %+v %+v", service.putContext, service.putInput)
	}
}

func TestFeatureControlUpdateRejectsMissingAggregateFields(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	for name, body := range map[string]string{
		"expected version": `{"features":{"membership_invitations":true,"class_management":true,"class_invite_links":true},"quotas":{"members":100,"active_classes":25,"invite_creations_per_hour":60}}`,
		"features":         `{"expected_version":0,"quotas":{"members":100,"active_classes":25,"invite_creations_per_hour":60}}`,
		"feature value":    `{"expected_version":0,"features":{"membership_invitations":true,"class_management":true},"quotas":{"members":100,"active_classes":25,"invite_creations_per_hour":60}}`,
		"quotas":           `{"expected_version":0,"features":{"membership_invitations":true,"class_management":true,"class_invite_links":true}}`,
		"quota value":      `{"expected_version":0,"features":{"membership_invitations":true,"class_management":true,"class_invite_links":true},"quotas":{"members":100,"active_classes":25}}`,
		"null aggregate":   `{"expected_version":0,"features":null,"quotas":{"members":100,"active_classes":25,"invite_creations_per_hour":60}}`,
	} {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			identityService := &fakeIdentityService{
				principal: auditViewerPrincipal(uuid.New(), tenantID),
			}
			service := &fakeFeatureControlService{
				capabilities: completeFeatureCapabilities(tenantID),
			}
			handler := featureControlTestHandler(identityService, service)
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/v1/tenants/"+tenantID.String()+"/feature-controls",
				strings.NewReader(body),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(csrfHeader, "csrf-token")
			request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
			request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
			if service.putContext.TenantID != uuid.Nil {
				t.Fatalf("invalid aggregate reached service: %+v", service.putInput)
			}
		})
	}
}

func TestFeatureControlUpdateFailureWritesFallbackAudit(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	auditService := &fakeAuditService{}
	handler := NewHandlerWithOptions(
		config.Config{
			Environment: "test",
			Port:        "8080",
			WebOrigin:   "http://localhost:5173",
			Authentication: config.AuthenticationConfig{
				SessionTTL: 8 * time.Hour,
			},
		},
		discardLogger(),
		Options{
			Identity: &fakeIdentityService{
				principal: auditViewerPrincipal(actorID, tenantID),
			},
			Audit:           auditService,
			FeatureControls: &fakeFeatureControlService{},
		},
	)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/tenants/"+tenantID.String()+"/feature-controls",
		strings.NewReader(`{"expected_version":0}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
	if auditService.fallbackAttempts != 1 || len(auditService.recordedFallbackDrafts) != 1 {
		t.Fatalf(
			"feature control failure must write one fallback audit: attempts=%d rows=%d",
			auditService.fallbackAttempts,
			len(auditService.recordedFallbackDrafts),
		)
	}
	draft := auditService.recordedFallbackDrafts[0]
	if draft.TenantID != tenantID || draft.ActorID != actorID ||
		draft.Action != audit.ActionTenantFeatureControlUpdate ||
		draft.ResourceType != "tenant_feature_control" || draft.ResourceID != tenantID ||
		draft.Outcome != audit.OutcomeFailed ||
		draft.Metadata["http_status"] != "400" ||
		draft.Metadata["reason_code"] != "invalid_request" {
		t.Fatalf("unexpected fallback audit draft: %#v", draft)
	}
}

func TestFeatureControlQuotaProblemUsesBoundedCodeAndRetryAfter(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	identityService := &fakeIdentityService{principal: auditViewerPrincipal(uuid.New(), tenantID)}
	service := &fakeFeatureControlService{err: &featurecontrol.QuotaExceededError{
		Quota: featurecontrol.QuotaInviteCreationsPerHour, RetryAfter: 31 * time.Second,
	}}
	handler := featureControlTestHandler(identityService, service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+tenantID.String()+"/capabilities",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "31" {
		t.Fatalf("unexpected rate rejection: %d %v", response.Code, response.Header())
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "quota_exceeded" || !errors.Is(service.err, featurecontrol.ErrQuotaExceeded) {
		t.Fatalf("unexpected problem: %+v", problem)
	}
}

func TestFeatureControlEnforcementUnavailableFailsClosed(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/classes", nil)
	response := httptest.NewRecorder()
	if !writeFeatureControlEnforcementProblem(
		response,
		request,
		fmt.Errorf("evaluate control: %w", featurecontrol.ErrUnavailable),
	) {
		t.Fatal("expected unavailable feature control error to be handled")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "feature_control_unavailable" {
		t.Fatalf("unexpected problem: %+v", problem)
	}
}

func featureControlTestHandler(
	identityService *fakeIdentityService,
	service featurecontrol.ServiceAPI,
) http.Handler {
	auth := newAuthHandlers(config.Config{}, discardLogger(), identityService, time.Now)
	handlers := newFeatureControlHandlers(discardLogger(), auth, service)
	mux := http.NewServeMux()
	mux.Handle(
		tenantCapabilitiesPattern,
		featureControlResponseHeaders(requireMethod(http.MethodGet, http.HandlerFunc(handlers.capabilities))),
	)
	mux.Handle(
		tenantFeatureControlsPattern,
		featureControlResponseHeaders(requireMethod(http.MethodPut, http.HandlerFunc(handlers.update))),
	)
	return mux
}

func completeFeatureCapabilities(tenantID uuid.UUID) featurecontrol.Capabilities {
	now := time.Now().UTC().Add(time.Hour)
	return featurecontrol.Capabilities{
		TenantID: tenantID,
		Version:  3,
		AllowedAction: featurecontrol.AllowedActions{
			ManageControls: true,
		},
		Features: []featurecontrol.FeatureCapability{
			{EffectiveFeature: featurecontrol.EffectiveFeature{Key: featurecontrol.FeatureMembershipInvitations, Enabled: true}, ConfiguredEnabled: true},
			{EffectiveFeature: featurecontrol.EffectiveFeature{Key: featurecontrol.FeatureClassManagement, Enabled: true}, ConfiguredEnabled: true},
			{EffectiveFeature: featurecontrol.EffectiveFeature{Key: featurecontrol.FeatureClassInviteLinks, Enabled: true}, ConfiguredEnabled: true},
		},
		Quotas: []featurecontrol.QuotaCapability{
			{EffectiveQuota: featurecontrol.EffectiveQuota{Key: featurecontrol.QuotaMembers, Limit: 100}, ConfiguredLimit: 100, Used: 100, Remaining: 0},
			{EffectiveQuota: featurecontrol.EffectiveQuota{Key: featurecontrol.QuotaActiveClasses, Limit: 25}, ConfiguredLimit: 25, Used: 4, Remaining: 21},
			{EffectiveQuota: featurecontrol.EffectiveQuota{Key: featurecontrol.QuotaInviteCreationsPerHour, Limit: 60}, ConfiguredLimit: 60, Used: 60, Remaining: 0, ResetAt: &now},
		},
	}
}
