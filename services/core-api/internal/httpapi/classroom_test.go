package httpapi

import (
	"context"
	"encoding/json"
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
)

func TestClassHandlersListAndGetUseActiveWorkspace(t *testing.T) {
	t.Parallel()

	classID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	identityService := classIdentityService(tenantID, userID, []string{"class.view"})
	classService := &fakeClassroomService{classes: []classroom.Class{{
		ID: classID, TenantID: tenantID, OwnerUserID: userID, Code: "SEC101",
		Title: "An toàn thông tin", Timezone: "Asia/Ho_Chi_Minh",
		Status: classroom.ClassStatusDraft, Version: 3,
		CreatedAt: fixedTime, UpdatedAt: fixedTime, ArchivedAt: nil,
	}}, nextCursor: "next-page"}
	handler := newClassTestHandler(identityService, classService)

	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/classes?status=draft&limit=20&cursor=current-page",
		nil,
	)
	addSessionCookie(listRequest)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list classes: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listBody classListResponse
	if err := json.NewDecoder(listResponse.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode class list: %v", err)
	}
	if len(listBody.Items) != 1 ||
		listBody.Items[0].ID != classID ||
		listBody.Items[0].Timezone != "Asia/Ho_Chi_Minh" ||
		listBody.Items[0].Version != 3 ||
		listBody.NextCursor == nil ||
		*listBody.NextCursor != "next-page" ||
		classService.listInput.Limit != 20 ||
		classService.listInput.Cursor != "current-page" ||
		classService.listInput.Status == nil ||
		*classService.listInput.Status != classroom.ClassStatusDraft {
		t.Fatalf("unexpected class list: body=%+v service=%+v", listBody, classService)
	}
	assertClassAccess(t, classService.access, tenantID, userID)

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/classes/"+classID.String(), nil)
	addSessionCookie(detailRequest)
	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK || classService.classID != classID {
		t.Fatalf("get class: status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
}

func TestClassHandlersCreateRequiresCSRFAndUsesPrincipalOwner(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	identityService := classIdentityService(
		tenantID,
		userID,
		[]string{"class.view", "class.create"},
	)
	classService := &fakeClassroomService{}
	handler := newClassTestHandler(identityService, classService)
	body := `{"code":"sec-201","title":"Mạng máy tính","description":"Học kỳ 1","timezone":"Asia/Ho_Chi_Minh"}`

	missingCSRF := httptest.NewRequest(http.MethodPost, classesCollectionPath, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || classService.createCalled {
		t.Fatalf("missing CSRF must be denied: status=%d", missingCSRFResponse.Code)
	}

	request := httptest.NewRequest(http.MethodPost, classesCollectionPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create class: status=%d body=%s", response.Code, response.Body.String())
	}
	if !classService.createCalled || classService.createInput.Code != "sec-201" ||
		classService.createInput.Title != "Mạng máy tính" ||
		classService.createInput.Timezone == nil ||
		*classService.createInput.Timezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("unexpected create request: %+v", classService)
	}
	assertClassAccess(t, classService.access, tenantID, userID)
	if !strings.HasPrefix(response.Header().Get("Location"), classesResourcePathPrefix) {
		t.Fatalf("missing class location: %q", response.Header().Get("Location"))
	}
}

func TestClassHandlersForwardVersionedLifecycleAndOwnershipMutations(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	newOwnerID := uuid.New()
	identityService := classIdentityService(
		tenantID,
		userID,
		[]string{"class.view", "class.update"},
	)
	classService := &fakeClassroomService{}
	handler := newClassTestHandler(identityService, classService)

	missingCSRF := httptest.NewRequest(
		http.MethodPatch,
		classesResourcePathPrefix+classID.String(),
		strings.NewReader(`{"expected_version":3,"title":"Updated"}`),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || classService.lastMutation != "" {
		t.Fatalf(
			"missing mutation CSRF must be denied: status=%d mutation=%q",
			missingCSRFResponse.Code,
			classService.lastMutation,
		)
	}

	updateResponse := performClassMutation(
		handler,
		http.MethodPatch,
		classesResourcePathPrefix+classID.String(),
		`{"expected_version":3,"description":"","status":"active"}`,
	)
	if updateResponse.Code != http.StatusOK ||
		classService.lastMutation != "update" ||
		classService.classID != classID ||
		classService.updateInput.ExpectedVersion != 3 ||
		classService.updateInput.Description == nil ||
		*classService.updateInput.Description != "" ||
		classService.updateInput.Status == nil ||
		*classService.updateInput.Status != classroom.ClassStatusActive {
		t.Fatalf(
			"unexpected update: status=%d body=%s service=%+v",
			updateResponse.Code,
			updateResponse.Body.String(),
			classService,
		)
	}

	archiveResponse := performClassMutation(
		handler,
		http.MethodPost,
		classesResourcePathPrefix+classID.String()+"/archive",
		`{"expected_version":4}`,
	)
	if archiveResponse.Code != http.StatusOK ||
		classService.lastMutation != "archive" ||
		classService.expectedVersion != 4 {
		t.Fatalf(
			"unexpected archive: status=%d body=%s service=%+v",
			archiveResponse.Code,
			archiveResponse.Body.String(),
			classService,
		)
	}

	restoreResponse := performClassMutation(
		handler,
		http.MethodPost,
		classesResourcePathPrefix+classID.String()+"/restore",
		`{"expected_version":5}`,
	)
	if restoreResponse.Code != http.StatusOK ||
		classService.lastMutation != "restore" ||
		classService.expectedVersion != 5 {
		t.Fatalf(
			"unexpected restore: status=%d body=%s service=%+v",
			restoreResponse.Code,
			restoreResponse.Body.String(),
			classService,
		)
	}

	transferResponse := performClassMutation(
		handler,
		http.MethodPost,
		classesResourcePathPrefix+classID.String()+"/transfer-ownership",
		`{"expected_version":6,"new_owner_user_id":"`+newOwnerID.String()+`"}`,
	)
	if transferResponse.Code != http.StatusOK ||
		classService.lastMutation != "transfer" ||
		classService.transferInput.ExpectedVersion != 6 ||
		classService.transferInput.NewOwnerUserID != newOwnerID {
		t.Fatalf(
			"unexpected transfer: status=%d body=%s service=%+v",
			transferResponse.Code,
			transferResponse.Body.String(),
			classService,
		)
	}
	assertClassAccess(t, classService.access, tenantID, userID)
}

func TestClassHandlersReturnStructuredAuthorizationAndValidationErrors(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	identityService := classIdentityService(tenantID, userID, []string{"class.view"})
	classService := &fakeClassroomService{requestError: classroom.ErrClassAccessDenied}
	handler := newClassTestHandler(identityService, classService)

	for _, testCase := range []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "invalid limit", method: http.MethodGet, path: "/api/v1/classes?limit=500", status: http.StatusBadRequest},
		{name: "invalid status", method: http.MethodGet, path: "/api/v1/classes?status=unknown", status: http.StatusBadRequest},
		{name: "invalid cursor", method: http.MethodGet, path: "/api/v1/classes?cursor=", status: http.StatusBadRequest},
		{name: "invalid class id", method: http.MethodGet, path: "/api/v1/classes/not-a-uuid", status: http.StatusNotFound},
		{name: "forbidden list", method: http.MethodGet, path: "/api/v1/classes", status: http.StatusForbidden},
		{name: "lifecycle method", method: http.MethodGet, path: "/api/v1/classes/" + uuid.NewString() + "/archive", status: http.StatusMethodNotAllowed},
		{name: "method", method: http.MethodDelete, path: "/api/v1/classes", status: http.StatusMethodNotAllowed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			addSessionCookie(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, testCase.status, response.Body.String())
			}
		})
	}
}

func TestClassHandlersMapLifecycleConflictsAndRecentAuthentication(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	newOwnerID := uuid.New()
	for _, testCase := range []struct {
		name   string
		path   string
		body   string
		err    error
		status int
	}{
		{
			name: "version conflict",
			path: classesResourcePathPrefix + classID.String() + "/archive",
			body: `{"expected_version":2}`, err: classroom.ErrClassVersionConflict,
			status: http.StatusConflict,
		},
		{
			name: "invalid transition",
			path: classesResourcePathPrefix + classID.String() + "/restore",
			body: `{"expected_version":2}`, err: classroom.ErrInvalidClassTransition,
			status: http.StatusConflict,
		},
		{
			name: "owner unavailable",
			path: classesResourcePathPrefix + classID.String() + "/transfer-ownership",
			body: `{"expected_version":2,"new_owner_user_id":"` + newOwnerID.String() + `"}`,
			err:  classroom.ErrClassOwnerUnavailable, status: http.StatusConflict,
		},
		{
			name: "recent authentication",
			path: classesResourcePathPrefix + classID.String() + "/transfer-ownership",
			body: `{"expected_version":2,"new_owner_user_id":"` + newOwnerID.String() + `"}`,
			err:  classroom.ErrRecentAuthenticationRequired, status: http.StatusForbidden,
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			handler := newClassTestHandler(
				classIdentityService(tenantID, userID, []string{"class.update"}),
				&fakeClassroomService{requestError: testCase.err},
			)
			response := performClassMutation(
				handler,
				http.MethodPost,
				testCase.path,
				testCase.body,
			)
			if response.Code != testCase.status {
				t.Fatalf(
					"status=%d want=%d body=%s",
					response.Code,
					testCase.status,
					response.Body.String(),
				)
			}
		})
	}
}

func newClassTestHandler(
	identityService identity.ServiceAPI,
	classService classroom.ServiceAPI,
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
		Options{Clock: func() time.Time { return fixedTime }, Identity: identityService, Classroom: classService},
	)
}

func classIdentityService(
	tenantID uuid.UUID,
	userID uuid.UUID,
	permissions []string,
) *fakeIdentityService {
	activeTenant := identity.Tenant{
		ID: tenantID, Slug: "security-lab", Name: "Security Lab", Role: "teacher", IsActive: true,
	}
	return &fakeIdentityService{principal: identity.Principal{
		SessionID:       uuid.New(),
		AuthenticatedAt: fixedTime,
		User: identity.User{
			ID: userID, Email: "teacher@example.test", DisplayName: "Nguyễn Minh Anh", Locale: "vi",
		},
		ActiveTenant: &activeTenant,
		Memberships:  []identity.Tenant{activeTenant},
		Permissions:  permissions,
	}}
}

func addSessionCookie(request *http.Request) {
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
}

func performClassMutation(
	handler http.Handler,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertClassAccess(
	t *testing.T,
	access classroom.AccessContext,
	tenantID uuid.UUID,
	userID uuid.UUID,
) {
	t.Helper()
	if access.TenantID != tenantID || access.ActorID != userID || !access.MembershipActive ||
		!access.AuthenticatedAt.Equal(fixedTime) ||
		len(access.OrganizationRoles) != 1 || string(access.OrganizationRoles[0]) != "teacher" {
		t.Fatalf("unexpected classroom access context: %+v", access)
	}
}

type fakeClassroomService struct {
	access          classroom.AccessContext
	classes         []classroom.Class
	nextCursor      string
	classID         uuid.UUID
	listInput       classroom.ListClassesInput
	createCalled    bool
	createInput     classroom.CreateClassInput
	updateInput     classroom.UpdateClassInput
	expectedVersion int64
	transferInput   classroom.TransferClassOwnershipInput
	lastMutation    string
	requestError    error
}

func (service *fakeClassroomService) Create(
	_ context.Context,
	access classroom.AccessContext,
	input classroom.CreateClassInput,
) (classroom.Class, error) {
	service.access = access
	service.createCalled = true
	service.createInput = input
	if service.requestError != nil {
		return classroom.Class{}, service.requestError
	}
	timezone := "Asia/Ho_Chi_Minh"
	if input.Timezone != nil {
		timezone = *input.Timezone
	}
	return classroom.Class{
		ID: uuid.New(), TenantID: access.TenantID, OwnerUserID: access.ActorID,
		Code: strings.ToUpper(input.Code), Title: input.Title, Description: input.Description,
		Timezone: timezone, Status: classroom.ClassStatusDraft, Version: 1,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}, nil
}

func (service *fakeClassroomService) Get(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
) (classroom.Class, error) {
	service.access = access
	service.classID = classID
	if service.requestError != nil {
		return classroom.Class{}, service.requestError
	}
	for _, class := range service.classes {
		if class.ID == classID {
			return class, nil
		}
	}
	return classroom.Class{}, classroom.ErrClassNotFound
}

func (service *fakeClassroomService) List(
	_ context.Context,
	access classroom.AccessContext,
	input classroom.ListClassesInput,
) (classroom.ClassPage, error) {
	service.access = access
	service.listInput = input
	if service.requestError != nil {
		return classroom.ClassPage{}, service.requestError
	}
	return classroom.ClassPage{
		Items:      append([]classroom.Class(nil), service.classes...),
		NextCursor: service.nextCursor,
	}, nil
}

func (service *fakeClassroomService) Update(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	input classroom.UpdateClassInput,
) (classroom.Class, error) {
	service.access = access
	service.classID = classID
	service.updateInput = input
	service.lastMutation = "update"
	return service.mutationResult(classID)
}

func (service *fakeClassroomService) Archive(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	expectedVersion int64,
) (classroom.Class, error) {
	service.access = access
	service.classID = classID
	service.expectedVersion = expectedVersion
	service.lastMutation = "archive"
	return service.mutationResult(classID)
}

func (service *fakeClassroomService) Restore(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	expectedVersion int64,
) (classroom.Class, error) {
	service.access = access
	service.classID = classID
	service.expectedVersion = expectedVersion
	service.lastMutation = "restore"
	return service.mutationResult(classID)
}

func (service *fakeClassroomService) TransferOwnership(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	input classroom.TransferClassOwnershipInput,
) (classroom.Class, error) {
	service.access = access
	service.classID = classID
	service.transferInput = input
	service.lastMutation = "transfer"
	return service.mutationResult(classID)
}

func (service *fakeClassroomService) mutationResult(
	classID uuid.UUID,
) (classroom.Class, error) {
	if service.requestError != nil {
		return classroom.Class{}, service.requestError
	}
	return classroom.Class{
		ID: classID, TenantID: service.access.TenantID, OwnerUserID: service.access.ActorID,
		Code: "SEC101", Title: "An toàn thông tin", Timezone: "Asia/Ho_Chi_Minh",
		Status: classroom.ClassStatusActive, Version: 4,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}, nil
}
