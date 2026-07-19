package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type fakeAuditService struct {
	page                   audit.Page
	listError              error
	listCalls              int
	listedTenantContext    tenancy.Context
	listedTenantID         uuid.UUID
	listedFilter           audit.Filter
	recordError            error
	recordCalls            int
	recordedDrafts         []audit.Draft
	fallbackError          error
	fallbackAttempts       int
	recordedFallbackDrafts []audit.Draft
	fallbackSeen           map[string]struct{}
	itemFallbackError      error
	itemFallbackAttempts   int
	recordedItemDrafts     []audit.Draft
	itemFallbackSeen       map[string]struct{}
}

func (service *fakeAuditService) List(
	_ context.Context,
	tenantContext tenancy.Context,
	tenantID uuid.UUID,
	filter audit.Filter,
) (audit.Page, error) {
	service.listCalls++
	service.listedTenantContext = tenantContext
	service.listedTenantID = tenantID
	service.listedFilter = filter
	return service.page, service.listError
}

func (service *fakeAuditService) Record(_ context.Context, draft audit.Draft) error {
	service.recordCalls++
	service.recordedDrafts = append(service.recordedDrafts, draft)
	return service.recordError
}

func (service *fakeAuditService) RecordFallback(ctx context.Context, draft audit.Draft) error {
	service.fallbackAttempts++
	if service.fallbackError != nil {
		return service.fallbackError
	}
	if service.fallbackSeen == nil {
		service.fallbackSeen = make(map[string]struct{})
	}
	request := requestmeta.SnapshotFromContext(ctx)
	key := request.RequestInstance.String() + "\x00" + string(draft.Action)
	if _, exists := service.fallbackSeen[key]; exists {
		return nil
	}
	service.fallbackSeen[key] = struct{}{}
	service.recordedFallbackDrafts = append(service.recordedFallbackDrafts, draft)
	return nil
}

func (service *fakeAuditService) RecordItemFallback(
	ctx context.Context,
	targetUserID uuid.UUID,
	draft audit.Draft,
) error {
	service.itemFallbackAttempts++
	if service.itemFallbackError != nil {
		return service.itemFallbackError
	}
	if service.itemFallbackSeen == nil {
		service.itemFallbackSeen = make(map[string]struct{})
	}
	request := requestmeta.SnapshotFromContext(ctx)
	key := request.RequestInstance.String() + "\x00" + string(draft.Action) + "\x00" + targetUserID.String()
	if _, exists := service.itemFallbackSeen[key]; exists {
		return nil
	}
	service.itemFallbackSeen[key] = struct{}{}
	metadata := make(audit.Metadata, len(draft.Metadata)+1)
	for metadataKey, value := range draft.Metadata {
		metadata[metadataKey] = value
	}
	metadata[audit.MetadataKeyTargetUserID] = targetUserID.String()
	draft.Metadata = metadata
	service.recordedItemDrafts = append(service.recordedItemDrafts, draft)
	return nil
}

func TestAuditMutationMiddlewareRecordsOutcomeAndSafeFailureReason(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	resourceID := uuid.New()
	tests := []struct {
		name          string
		status        int
		wantOutcome   audit.Outcome
		wantReason    string
		wantEffect    string
		writeExplicit bool
	}{
		{name: "implicit success", status: http.StatusOK, wantOutcome: audit.OutcomeSucceeded, wantEffect: "unchanged"},
		{name: "created success", status: http.StatusCreated, wantOutcome: audit.OutcomeSucceeded, wantEffect: "unchanged", writeExplicit: true},
		{name: "invalid request", status: http.StatusBadRequest, wantOutcome: audit.OutcomeFailed, wantReason: "invalid_request", writeExplicit: true},
		{name: "forbidden", status: http.StatusForbidden, wantOutcome: audit.OutcomeDenied, wantReason: "forbidden", writeExplicit: true},
		{name: "concealed unavailable", status: http.StatusNotFound, wantOutcome: audit.OutcomeDenied, wantReason: "resource_unavailable", writeExplicit: true},
		{name: "conflict", status: http.StatusConflict, wantOutcome: audit.OutcomeFailed, wantReason: "conflict", writeExplicit: true},
		{name: "rate limited", status: http.StatusTooManyRequests, wantOutcome: audit.OutcomeFailed, wantReason: "rate_limited", writeExplicit: true},
		{name: "internal failure", status: http.StatusInternalServerError, wantOutcome: audit.OutcomeFailed, wantReason: "internal_failure", writeExplicit: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeAuditService{}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.writeExplicit {
					w.WriteHeader(test.status)
				}
			})
			handler := auditMutationMiddleware(
				discardLogger(),
				service,
				staticAuditMutation(
					http.MethodPatch,
					audit.ActionClassUpdate,
					"class",
					func(*http.Request) uuid.UUID { return resourceID },
				),
				next,
			)
			request := requestWithAuditPrincipal(
				t, http.MethodPatch, "/api/v1/classes/"+resourceID.String(), actorID, tenantID,
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("unexpected response status: got %d want %d", response.Code, test.status)
			}
			if service.recordCalls != 0 || service.fallbackAttempts != 1 ||
				len(service.recordedFallbackDrafts) != 1 {
				t.Fatalf(
					"middleware must use fallback recorder once: record=%d fallback=%d rows=%d",
					service.recordCalls,
					service.fallbackAttempts,
					len(service.recordedFallbackDrafts),
				)
			}
			draft := service.recordedFallbackDrafts[0]
			if draft.TenantID != tenantID || draft.ActorID != actorID ||
				draft.Action != audit.ActionClassUpdate || draft.ResourceType != "class" ||
				draft.ResourceID != resourceID || draft.Outcome != test.wantOutcome {
				t.Fatalf("unexpected audit draft: %#v", draft)
			}
			if draft.Metadata["http_status"] != strconv.Itoa(test.status) ||
				draft.Metadata["reason_code"] != test.wantReason ||
				draft.Metadata["effect"] != test.wantEffect {
				t.Fatalf("unexpected audit metadata: %#v", draft.Metadata)
			}
		})
	}
}

func TestAuditMutationMiddlewareRecordsAndPropagatesPanic(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	tenantID := uuid.New()
	resourceID := uuid.New()
	service := &fakeAuditService{}
	handler := auditMutationMiddleware(
		discardLogger(),
		service,
		staticAuditMutation(
			http.MethodPatch,
			audit.ActionClassUpdate,
			"class",
			func(*http.Request) uuid.UUID { return resourceID },
		),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("sensitive mutation panic")
		}),
	)
	request := requestWithAuditPrincipal(
		t, http.MethodPatch, "/api/v1/classes/"+resourceID.String(), actorID, tenantID,
	)
	response := httptest.NewRecorder()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(response, request)
	}()

	if recovered == nil {
		t.Fatal("middleware must propagate the panic to the outer recovery boundary")
	}
	if service.fallbackAttempts != 1 || len(service.recordedFallbackDrafts) != 1 {
		t.Fatalf(
			"panic must produce one fallback audit attempt: attempts=%d rows=%d",
			service.fallbackAttempts,
			len(service.recordedFallbackDrafts),
		)
	}
	draft := service.recordedFallbackDrafts[0]
	if draft.Outcome != audit.OutcomeFailed ||
		draft.Metadata["http_status"] != "500" ||
		draft.Metadata["reason_code"] != "internal_failure" {
		t.Fatalf("unexpected panic audit draft: %#v", draft)
	}
}

func TestAuditMutationMiddlewareFallbackDedupeUsesRequestInstanceAndAction(t *testing.T) {
	t.Parallel()

	service := &fakeAuditService{}
	tenantID := uuid.New()
	actorID := uuid.New()
	resourceID := uuid.New()
	resolver := staticAuditMutation(
		http.MethodPatch,
		audit.ActionTenantUpdate,
		"tenant",
		func(*http.Request) uuid.UUID { return resourceID },
	)
	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	inner := auditMutationMiddleware(discardLogger(), service, resolver, terminal)
	handler := auditMutationMiddleware(discardLogger(), service, resolver, inner)
	request := requestWithAuditPrincipal(
		t, http.MethodPatch, "/api/v1/tenants/"+resourceID.String(), actorID, tenantID,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected response status: %d", response.Code)
	}
	if service.fallbackAttempts != 2 || len(service.recordedFallbackDrafts) != 1 {
		t.Fatalf(
			"same request/action fallback was not deduplicated: attempts=%d rows=%d",
			service.fallbackAttempts,
			len(service.recordedFallbackDrafts),
		)
	}
}

func TestAuditMutationMiddlewareSkipsUnauthenticatedOrInsensitiveRequests(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	tests := []struct {
		name      string
		method    string
		principal bool
	}{
		{name: "method is not sensitive", method: http.MethodGet, principal: true},
		{name: "principal unavailable", method: http.MethodPatch, principal: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeAuditService{}
			handler := auditMutationMiddleware(
				discardLogger(),
				service,
				staticAuditMutation(http.MethodPatch, audit.ActionTenantUpdate, "tenant", nil),
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			request := requestWithAuditPrincipal(
				t, test.method, "/api/v1/tenants/"+tenantID.String(), uuid.Nil, uuid.Nil,
			)
			if test.principal {
				requestmeta.SetPrincipal(request.Context(), actorID, tenantID)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || service.fallbackAttempts != 0 {
				t.Fatalf("request unexpectedly audited: status=%d attempts=%d", response.Code, service.fallbackAttempts)
			}
		})
	}
}

func TestResolvedTenantAuditMutationMiddlewareUsesOnlyAuthoritativeTarget(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	activeTenantID := uuid.New()
	targetTenantID := uuid.New()
	for _, test := range []struct {
		name           string
		activeTenantID uuid.UUID
		resolveTarget  bool
		wantAttempts   int
	}{
		{
			name:           "resolved target replaces unrelated active tenant",
			activeTenantID: activeTenantID, resolveTarget: true, wantAttempts: 1,
		},
		{
			name:           "resolved target works without active workspace",
			activeTenantID: uuid.Nil, resolveTarget: true, wantAttempts: 1,
		},
		{
			name:           "unresolved target does not fall back to active tenant",
			activeTenantID: activeTenantID, resolveTarget: false, wantAttempts: 0,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeAuditService{}
			handler := auditResolvedTenantMutationMiddleware(
				discardLogger(),
				service,
				staticAuditMutation(
					http.MethodPost,
					audit.ActionMembershipInvitationAccept,
					"membership_invitation",
					nil,
				),
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestmeta.SetPrincipal(r.Context(), actorID, test.activeTenantID)
					if test.resolveTarget {
						requestmeta.SetAuditTenant(r.Context(), targetTenantID)
					}
					w.WriteHeader(http.StatusForbidden)
				}),
			)
			request := requestWithAuditPrincipal(
				t, http.MethodPost, membershipInvitationAcceptPath, uuid.Nil, uuid.Nil,
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden ||
				service.fallbackAttempts != test.wantAttempts ||
				len(service.recordedFallbackDrafts) != test.wantAttempts {
				t.Fatalf(
					"unexpected resolved-target audit result: status=%d attempts=%d rows=%d",
					response.Code,
					service.fallbackAttempts,
					len(service.recordedFallbackDrafts),
				)
			}
			if test.wantAttempts == 1 {
				draft := service.recordedFallbackDrafts[0]
				if draft.TenantID != targetTenantID || draft.TenantID == activeTenantID ||
					draft.ActorID != actorID || draft.Outcome != audit.OutcomeDenied {
					t.Fatalf("resolved target or authenticated actor was lost: %#v", draft)
				}
			}
		})
	}
}

func TestMembershipInvitationAcceptAuditUsesResolvedInvitationTenant(t *testing.T) {
	t.Parallel()

	principal := membershipInvitationPrincipal()
	if principal.ActiveTenant == nil || principal.ActiveTenant.ID == membershipInvitationTenantID {
		t.Fatal("test fixture must use an unrelated active tenant")
	}
	acceptedInvitation := membershipInvitationFixture(identity.MembershipInvitationAccepted)
	acceptedAt := fixedTime
	acceptedInvitation.AcceptedAt = &acceptedAt

	for _, test := range []struct {
		name              string
		service           *fakeIdentityService
		wantStatus        int
		wantAuditAttempts int
		wantOutcome       audit.Outcome
	}{
		{
			name: "successful idempotent result is scoped to invitation tenant",
			service: &fakeIdentityService{
				principal: principal,
				acceptInvitationResult: identity.AcceptMembershipInvitationResult{
					Invitation: acceptedInvitation,
					Principal:  principal,
				},
			},
			wantStatus: http.StatusOK, wantAuditAttempts: 1, wantOutcome: audit.OutcomeSucceeded,
		},
		{
			name: "post-resolution failure is scoped to invitation tenant",
			service: &fakeIdentityService{
				principal:                     principal,
				acceptInvitationError:         identity.ErrMembershipInvitationIdentityMismatch,
				acceptInvitationAuditTenantID: membershipInvitationTenantID,
			},
			wantStatus: http.StatusForbidden, wantAuditAttempts: 1, wantOutcome: audit.OutcomeDenied,
		},
		{
			name: "unresolved token does not audit unrelated active tenant",
			service: &fakeIdentityService{
				principal:             principal,
				acceptInvitationError: identity.ErrMembershipInvitationUnavailable,
			},
			wantStatus: http.StatusNotFound,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
					Clock:    func() time.Time { return fixedTime },
					Identity: test.service,
					Audit:    auditService,
				},
			)
			response := performMembershipInvitationRequest(
				handler,
				http.MethodPost,
				membershipInvitationAcceptPath,
				`{"token":"`+membershipInvitationToken+`"}`,
				true,
				true,
				"203.0.113.129:443",
			)

			if response.Code != test.wantStatus ||
				auditService.fallbackAttempts != test.wantAuditAttempts ||
				len(auditService.recordedFallbackDrafts) != test.wantAuditAttempts {
				t.Fatalf(
					"unexpected invitation audit result: status=%d body=%s attempts=%d rows=%d",
					response.Code,
					response.Body.String(),
					auditService.fallbackAttempts,
					len(auditService.recordedFallbackDrafts),
				)
			}
			if test.wantAuditAttempts == 1 {
				draft := auditService.recordedFallbackDrafts[0]
				if draft.TenantID != membershipInvitationTenantID ||
					draft.TenantID == principal.ActiveTenant.ID ||
					draft.ActorID != principal.User.ID ||
					draft.Action != audit.ActionMembershipInvitationAccept ||
					draft.Outcome != test.wantOutcome {
					t.Fatalf("invitation audit used the wrong scope: %#v", draft)
				}
			}
		})
	}
}

func TestAuditQueryHandlerMapsFiltersAuthorizationAndPrivacyHeaders(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	resourceID := uuid.New()
	from := "2026-07-01T00:00:00Z"
	to := "2026-07-20T00:00:00Z"
	tests := []struct {
		name             string
		query            string
		serviceError     error
		pathTenantID     string
		wantStatus       int
		wantServiceCalls int
	}{
		{name: "invalid filter", query: "?occurred_from=not-a-time", pathTenantID: tenantID.String(), wantStatus: http.StatusBadRequest},
		{name: "access denied", serviceError: audit.ErrAccessDenied, pathTenantID: tenantID.String(), wantStatus: http.StatusForbidden, wantServiceCalls: 1},
		{name: "tenant concealed", serviceError: audit.ErrNotFound, pathTenantID: tenantID.String(), wantStatus: http.StatusNotFound, wantServiceCalls: 1},
		{name: "malformed tenant id", pathTenantID: "not-a-uuid", wantStatus: http.StatusNotFound},
		{
			name: "valid filtered query",
			query: "?occurred_from=" + from + "&occurred_to=" + to +
				"&action=" + string(audit.ActionClassUpdate) +
				"&resource_type=class&resource_id=" + resourceID.String() +
				"&outcome=" + string(audit.OutcomeSucceeded) + "&limit=10&cursor=opaque",
			pathTenantID: tenantID.String(), wantStatus: http.StatusOK, wantServiceCalls: 1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identityService := &fakeIdentityService{principal: auditViewerPrincipal(actorID, tenantID)}
			auditService := &fakeAuditService{
				page:      audit.Page{Items: []audit.Event{}},
				listError: test.serviceError,
			}
			handler := newAuditQueryTestHandler(identityService, auditService)
			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/tenants/"+test.pathTenantID+"/audit-events"+test.query,
				nil,
			)
			request.SetPathValue("tenantId", test.pathTenantID)
			request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("unexpected status: got %d want %d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if auditService.listCalls != test.wantServiceCalls {
				t.Fatalf("unexpected service calls: got %d want %d", auditService.listCalls, test.wantServiceCalls)
			}
			if response.Header().Get("Cache-Control") != "no-store" ||
				response.Header().Get("Referrer-Policy") != "no-referrer" ||
				!strings.Contains(response.Header().Get("Vary"), "Cookie") {
				t.Fatalf("missing audit privacy headers: %v", response.Header())
			}
			if test.name == "valid filtered query" {
				if auditService.listedTenantID != tenantID ||
					auditService.listedTenantContext.ActorID != actorID ||
					auditService.listedTenantContext.TenantID != tenantID {
					t.Fatalf("query lost tenant scope: context=%#v tenant=%s", auditService.listedTenantContext, auditService.listedTenantID)
				}
				filter := auditService.listedFilter
				if filter.Action != audit.ActionClassUpdate || filter.ResourceType != "class" ||
					filter.ResourceID != resourceID || filter.Outcome != audit.OutcomeSucceeded ||
					filter.Limit != 10 || filter.Cursor != "opaque" || filter.OccurredFrom == nil ||
					filter.OccurredTo == nil {
					t.Fatalf("unexpected parsed filter: %#v", filter)
				}
			}
		})
	}
}

func TestAuditQueryHandlerReturnsNoStoreJSONPage(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	eventID := uuid.New()
	auditService := &fakeAuditService{page: audit.Page{
		Items: []audit.Event{{
			ID: eventID, TenantID: tenantID,
			Actor:    audit.Actor{Type: audit.ActorTypeSystem},
			Action:   audit.ActionMembershipInvitationExpire,
			Resource: audit.Resource{Type: "membership_invitation"},
			Outcome:  audit.OutcomeSucceeded, RequestID: "audit-list-request",
			Metadata:   audit.Metadata{"effect": "expired"},
			OccurredAt: time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC),
		}},
		NextCursor: "next-page",
	}}
	handler := newAuditQueryTestHandler(
		&fakeIdentityService{principal: auditViewerPrincipal(actorID, tenantID)},
		auditService,
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+tenantID.String()+"/audit-events",
		nil,
	)
	request.SetPathValue("tenantId", tenantID.String())
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected audit page response: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	var page audit.Page
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != eventID || page.NextCursor != "next-page" {
		t.Fatalf("unexpected audit page: %#v", page)
	}
}

func newAuditQueryTestHandler(
	identityService identity.ServiceAPI,
	auditService audit.ServiceAPI,
) http.Handler {
	logger := discardLogger()
	auth := newAuthHandlers(
		config.Config{
			Environment: "test",
			Authentication: config.AuthenticationConfig{
				SessionTTL: 8 * time.Hour,
			},
		},
		logger,
		identityService,
		time.Now,
	)
	handlers := newAuditHandlers(logger, auth, auditService)
	return auditResponseHeaders(http.HandlerFunc(handlers.list))
}

func auditViewerPrincipal(actorID uuid.UUID, tenantID uuid.UUID) identity.Principal {
	tenant := identity.Tenant{
		ID: tenantID, Slug: "audit-tenant", Name: "Audit Tenant", Status: "active",
		Version: 1, Role: "org_admin", IsActive: true,
	}
	return identity.Principal{
		SessionID: uuid.New(),
		User: identity.User{
			ID: actorID, Email: "admin@example.test", DisplayName: "Audit Admin",
			Locale: "vi", Timezone: "Asia/Ho_Chi_Minh",
		},
		ActiveTenant: &tenant,
		Memberships:  []identity.Tenant{tenant},
		Permissions:  []string{"audit.view"},
	}
}

func requestWithAuditPrincipal(
	t *testing.T,
	method string,
	target string,
	actorID uuid.UUID,
	tenantID uuid.UUID,
) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	ctx, _ := requestmeta.New(
		request.Context(),
		"audit-middleware-request",
		"203.0.113.19:443",
		"TutorHub audit test",
		time.Now(),
	)
	request = request.WithContext(ctx)
	if actorID != uuid.Nil && tenantID != uuid.Nil {
		requestmeta.SetPrincipal(request.Context(), actorID, tenantID)
	}
	return request
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAuditFailureReasonCatalog(t *testing.T) {
	t.Parallel()

	tests := map[int]string{
		http.StatusBadRequest:          "invalid_request",
		http.StatusUnprocessableEntity: "invalid_request",
		http.StatusUnauthorized:        "forbidden",
		http.StatusForbidden:           "forbidden",
		http.StatusNotFound:            "resource_unavailable",
		http.StatusConflict:            "conflict",
		http.StatusPreconditionFailed:  "conflict",
		http.StatusTooManyRequests:     "rate_limited",
		http.StatusInternalServerError: "internal_failure",
		http.StatusTeapot:              "request_failed",
	}
	for status, expected := range tests {
		if actual := auditFailureReason(status); actual != expected {
			t.Fatalf("status %d mapped to %q, want %q", status, actual, expected)
		}
	}
}

func TestAuditQueryProblemBodiesDoNotEchoInternalErrors(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	secretError := errors.New("database password=must-not-leak")
	handler := newAuditQueryTestHandler(
		&fakeIdentityService{principal: auditViewerPrincipal(uuid.New(), tenantID)},
		&fakeAuditService{listError: secretError},
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+tenantID.String()+"/audit-events",
		nil,
	)
	request.SetPathValue("tenantId", tenantID.String())
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	if strings.Contains(response.Body.String(), secretError.Error()) || strings.Contains(response.Body.String(), "password") {
		t.Fatalf("audit problem leaked internal error: %s", response.Body.String())
	}
}
