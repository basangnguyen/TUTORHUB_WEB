package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestClassInviteCodeCreateReturnsCopyOnceFragmentLink(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	codeID := uuid.New()
	token := "thciv1_abcdefghijklmnopqrstuvwxyz0123456789ABCDE"
	service := &fakeClassEnrollmentService{
		createInviteCodeResult: classroom.CreateClassInviteCodeResult{
			InviteCode: classroom.ClassInviteCode{
				ID: codeID, TenantID: tenantID, ClassID: classID,
				Status:     classroom.ClassInviteCodeStatusActive,
				ExpiresAt:  fixedTime.Add(24 * time.Hour),
				UsageLimit: 12, CreatedBy: actorID,
				CreatedAt: fixedTime, UpdatedAt: fixedTime,
			},
			Token: token,
		},
	}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(tenantID, actorID, []string{"enrollment.manage"}),
		service,
		nil,
	)

	request := newClassEnrollmentMutationRequest(
		classInviteCodesPath(classID),
		`{"expires_in_seconds":86400,"usage_limit":12}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("create invite code: status=%d body=%s", response.Code, response.Body.String())
	}
	if service.createInviteCodeClassID != classID ||
		service.createInviteCodeInput.ExpiresInSeconds != 86400 ||
		service.createInviteCodeInput.UsageLimit != 12 {
		t.Fatalf("unexpected create invite input: %+v", service)
	}
	assertClassAccess(t, service.access, tenantID, actorID)
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing token-safe response headers: %v", response.Header())
	}

	var body struct {
		InviteCode classInviteCodeResponse `json:"invite_code"`
		JoinURL    string                  `json:"join_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode create invite response: %v", err)
	}
	if body.InviteCode.ID != codeID ||
		body.JoinURL != "http://localhost:5173/class-invite#token="+token {
		t.Fatalf("unexpected create invite response: %+v", body)
	}
	serialized, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal response for redaction assertion: %v", err)
	}
	value := string(serialized)
	if strings.Contains(value, "tenant_id") ||
		strings.Contains(value, "revoked_by") ||
		strings.Contains(value, `"token":`) {
		t.Fatalf("response exposed an internal invite field: %s", value)
	}
}

func TestClassInvitationJoinRequiresCSRFAndKeepsTokenOutOfRoute(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	enrollmentID := uuid.New()
	token := "thciv1_join-token-placeholder"
	joinedAt := fixedTime
	classRole := policy.ClassRoleStudent
	enrollmentStatus := classroom.EnrollmentStatusActive
	service := &fakeClassEnrollmentService{
		joinResult: classroom.JoinClassInvitationResult{
			Class: classroom.Class{
				ID: classID, TenantID: tenantID, OwnerUserID: uuid.New(),
				Code: "SEC101", Title: "Security", Timezone: "Asia/Ho_Chi_Minh",
				Status: classroom.ClassStatusActive, Version: 1,
				CreatedAt: fixedTime, UpdatedAt: fixedTime,
				ViewerAccess: classroom.ViewerAccess{
					ClassRole: &classRole, EnrollmentStatus: &enrollmentStatus,
					CanJoinRoom: true, CanPublishMedia: true, CanLeave: true,
				},
			},
			Enrollment: &classroom.Enrollment{
				ID: enrollmentID, TenantID: tenantID, ClassID: classID,
				UserID: actorID, ClassRole: classRole,
				Status: enrollmentStatus, EnrolledBy: actorID,
				JoinedAt: &joinedAt, CreatedAt: fixedTime, UpdatedAt: fixedTime,
			},
			Joined: true,
		},
	}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(tenantID, actorID, []string{"tenant.view"}),
		service,
		nil,
	)

	missingCSRF := httptest.NewRequest(
		http.MethodPost,
		classInvitationJoinPath,
		strings.NewReader(`{"token":"`+token+`"}`),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || service.joinCalled {
		t.Fatalf(
			"missing CSRF must be denied: status=%d called=%t",
			missingCSRFResponse.Code,
			service.joinCalled,
		)
	}

	request := newClassEnrollmentMutationRequest(
		classInvitationJoinPath,
		`{"token":"`+token+`"}`,
	)
	if strings.Contains(request.URL.RequestURI(), token) {
		t.Fatal("raw invitation token must not appear in the request target")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("join class invitation: status=%d body=%s", response.Code, response.Body.String())
	}
	if !service.joinCalled || service.joinToken != token {
		t.Fatalf("join token was not forwarded from the JSON body: %+v", service)
	}

	var body joinClassInvitationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode class join response: %v", err)
	}
	if !body.Joined || body.Classroom.ID != classID ||
		!body.Classroom.ViewerAccess.CanJoinRoom ||
		!body.Classroom.ViewerAccess.CanPublishMedia ||
		body.Enrollment == nil || body.Enrollment.ID != enrollmentID {
		t.Fatalf("unexpected class join response: %+v", body)
	}
}

func TestClassInvitationJoinConcealsDomainFailures(t *testing.T) {
	t.Parallel()

	domainErrors := []error{
		classroom.ErrInvalidEnrollmentInput,
		classroom.ErrEnrollmentAccessDenied,
		classroom.ErrClassAccessDenied,
		classroom.ErrEnrollmentConflict,
		classroom.ErrClassInviteCodeConflict,
		classroom.ErrInvalidClassTransition,
		classroom.ErrClassInviteCodeUnavailable,
		classroom.ErrEnrollmentNotFound,
		classroom.ErrClassNotFound,
	}
	for _, domainError := range domainErrors {
		service := &fakeClassEnrollmentService{joinError: domainError}
		handler := newClassEnrollmentTestHandler(
			classIdentityService(uuid.New(), uuid.New(), []string{"tenant.view"}),
			service,
			nil,
		)
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response,
			newClassEnrollmentMutationRequest(
				classInvitationJoinPath,
				`{"token":"thciv1_unknown"}`,
			),
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf(
				"error %v: status=%d body=%s",
				domainError,
				response.Code,
				response.Body.String(),
			)
		}
		var problem struct {
			Status int    `json:"status"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		}
		if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
			t.Fatalf("decode concealed problem for %v: %v", domainError, err)
		}
		if problem.Status != http.StatusNotFound ||
			problem.Title != "Class invitation unavailable" ||
			problem.Detail != "The class invitation is invalid, unavailable, or no longer active." {
			t.Fatalf("domain failure was distinguishable: error=%v problem=%+v", domainError, problem)
		}
	}
}

func TestClassInvitationJoinRateLimitUsesOnlyActionAndIPPrefix(t *testing.T) {
	t.Parallel()

	service := &fakeClassEnrollmentService{}
	limiter := &classJoinRecordingLimiter{
		decision: InvitationRateLimitDecision{
			Allowed: false, RetryAfter: 17 * time.Second,
		},
	}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(uuid.New(), uuid.New(), []string{"tenant.view"}),
		service,
		limiter,
	)
	request := newClassEnrollmentMutationRequest(
		classInvitationJoinPath,
		`{"token":"thciv1_rate-limit-placeholder"}`,
	)
	request.RemoteAddr = "203.0.113.45:54321"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests ||
		response.Header().Get("Retry-After") != "17" ||
		service.joinCalled {
		t.Fatalf(
			"unexpected rate-limit response: status=%d retry=%q called=%t",
			response.Code,
			response.Header().Get("Retry-After"),
			service.joinCalled,
		)
	}
	if len(limiter.calls) != 1 ||
		limiter.calls[0].action != InvitationRateLimitClassJoin ||
		limiter.calls[0].clientPrefix != "203.0.113.0/24" {
		t.Fatalf("unexpected limiter key: %+v", limiter.calls)
	}
}

func TestClassInvitationJoinRateLimiterFailureIsUnavailable(t *testing.T) {
	t.Parallel()

	service := &fakeClassEnrollmentService{}
	limiter := &classJoinRecordingLimiter{
		decision: InvitationRateLimitDecision{Err: errors.New("database unavailable")},
	}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(uuid.New(), uuid.New(), []string{"tenant.view"}),
		service,
		limiter,
	)
	request := newClassEnrollmentMutationRequest(
		classInvitationJoinPath,
		`{"token":"thciv1_rate-limit-placeholder"}`,
	)
	request.RemoteAddr = "203.0.113.45:54321"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 response, got %d: %s", response.Code, response.Body.String())
	}
	var problem Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "rate_limit_unavailable" || service.joinCalled {
		t.Fatalf("unexpected limiter failure response: problem=%+v called=%t", problem, service.joinCalled)
	}
}

func classInviteCodesPath(classID uuid.UUID) string {
	return "/api/v1/classes/" + classID.String() + "/invite-codes"
}

func newClassEnrollmentMutationRequest(path string, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	return request
}

func newClassEnrollmentTestHandler(
	identityService identity.ServiceAPI,
	enrollmentService classroom.EnrollmentServiceAPI,
	limiter InvitationRateLimiter,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test",
			Port:        "8080",
			WebOrigin:   "http://localhost:5173",
			Authentication: config.AuthenticationConfig{
				SessionTTL: 8 * time.Hour,
			},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock:                 func() time.Time { return fixedTime },
			Identity:              identityService,
			Enrollment:            enrollmentService,
			InvitationRateLimiter: limiter,
		},
	)
}

type fakeClassEnrollmentService struct {
	access                  classroom.AccessContext
	listRosterClassID       uuid.UUID
	listRosterInput         classroom.ListRosterInput
	listRosterResult        classroom.RosterPage
	listRosterError         error
	updateRosterClassID     uuid.UUID
	updateRosterUserID      uuid.UUID
	updateRosterInput       classroom.UpdateRosterRoleInput
	updateRosterResult      classroom.EnrollmentMutationResult
	updateRosterError       error
	bulkRosterClassID       uuid.UUID
	bulkRosterInput         classroom.BulkRosterInput
	bulkRosterResult        classroom.BulkRosterResult
	bulkRosterError         error
	createInviteCodeClassID uuid.UUID
	createInviteCodeInput   classroom.CreateClassInviteCodeInput
	createInviteCodeResult  classroom.CreateClassInviteCodeResult
	joinCalled              bool
	joinToken               string
	joinResult              classroom.JoinClassInvitationResult
	joinError               error
}

func (service *fakeClassEnrollmentService) ListRoster(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	input classroom.ListRosterInput,
) (classroom.RosterPage, error) {
	service.access = access
	service.listRosterClassID = classID
	service.listRosterInput = input
	return service.listRosterResult, service.listRosterError
}

func (service *fakeClassEnrollmentService) UpdateRosterRole(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	userID uuid.UUID,
	input classroom.UpdateRosterRoleInput,
) (classroom.EnrollmentMutationResult, error) {
	service.access = access
	service.updateRosterClassID = classID
	service.updateRosterUserID = userID
	service.updateRosterInput = input
	return service.updateRosterResult, service.updateRosterError
}

func (service *fakeClassEnrollmentService) BulkMutateRoster(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	input classroom.BulkRosterInput,
) (classroom.BulkRosterResult, error) {
	service.access = access
	service.bulkRosterClassID = classID
	service.bulkRosterInput = input
	return service.bulkRosterResult, service.bulkRosterError
}

func (service *fakeClassEnrollmentService) DirectEnroll(
	_ context.Context,
	access classroom.AccessContext,
	_ uuid.UUID,
	_ classroom.DirectEnrollmentInput,
) (classroom.EnrollmentMutationResult, error) {
	service.access = access
	return classroom.EnrollmentMutationResult{}, nil
}

func (service *fakeClassEnrollmentService) SuspendEnrollment(
	_ context.Context,
	access classroom.AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
) (classroom.EnrollmentMutationResult, error) {
	service.access = access
	return classroom.EnrollmentMutationResult{}, nil
}

func (service *fakeClassEnrollmentService) RemoveEnrollment(
	_ context.Context,
	access classroom.AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
) (classroom.EnrollmentMutationResult, error) {
	service.access = access
	return classroom.EnrollmentMutationResult{}, nil
}

func (service *fakeClassEnrollmentService) CreateInviteCode(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	input classroom.CreateClassInviteCodeInput,
) (classroom.CreateClassInviteCodeResult, error) {
	service.access = access
	service.createInviteCodeClassID = classID
	service.createInviteCodeInput = input
	return service.createInviteCodeResult, nil
}

func (service *fakeClassEnrollmentService) ListInviteCodes(
	_ context.Context,
	access classroom.AccessContext,
	_ uuid.UUID,
) ([]classroom.ClassInviteCode, error) {
	service.access = access
	return []classroom.ClassInviteCode{}, nil
}

func (service *fakeClassEnrollmentService) RevokeInviteCode(
	_ context.Context,
	access classroom.AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
) (classroom.ClassInviteCode, error) {
	service.access = access
	return classroom.ClassInviteCode{}, nil
}

func (service *fakeClassEnrollmentService) JoinByInviteCode(
	_ context.Context,
	access classroom.AccessContext,
	token string,
) (classroom.JoinClassInvitationResult, error) {
	service.access = access
	service.joinCalled = true
	service.joinToken = token
	if service.joinError != nil {
		return classroom.JoinClassInvitationResult{}, service.joinError
	}
	return service.joinResult, nil
}

func (service *fakeClassEnrollmentService) LeaveClass(
	_ context.Context,
	access classroom.AccessContext,
	_ uuid.UUID,
) (classroom.EnrollmentMutationResult, error) {
	service.access = access
	return classroom.EnrollmentMutationResult{}, nil
}

type classJoinRateLimitCall struct {
	action       InvitationRateLimitAction
	clientPrefix string
}

type classJoinRecordingLimiter struct {
	decision InvitationRateLimitDecision
	calls    []classJoinRateLimitCall
}

func (limiter *classJoinRecordingLimiter) Allow(
	_ context.Context,
	action InvitationRateLimitAction,
	clientPrefix string,
	_ time.Time,
) InvitationRateLimitDecision {
	limiter.calls = append(limiter.calls, classJoinRateLimitCall{
		action: action, clientPrefix: clientPrefix,
	})
	return limiter.decision
}
