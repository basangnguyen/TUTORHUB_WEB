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
		Title: "An toàn thông tin", Status: classroom.ClassStatusDraft,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}}}
	handler := newClassTestHandler(identityService, classService)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/classes?limit=20", nil)
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
	if len(listBody.Items) != 1 || listBody.Items[0].ID != classID || classService.listLimit != 20 {
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
	body := `{"code":"sec-201","title":"Mạng máy tính","description":"Học kỳ 1"}`

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
		classService.createInput.Title != "Mạng máy tính" {
		t.Fatalf("unexpected create request: %+v", classService)
	}
	assertClassAccess(t, classService.access, tenantID, userID)
	if !strings.HasPrefix(response.Header().Get("Location"), classesResourcePathPrefix) {
		t.Fatalf("missing class location: %q", response.Header().Get("Location"))
	}
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
		{name: "invalid class id", method: http.MethodGet, path: "/api/v1/classes/not-a-uuid", status: http.StatusNotFound},
		{name: "forbidden list", method: http.MethodGet, path: "/api/v1/classes", status: http.StatusForbidden},
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
		SessionID: uuid.New(),
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

func assertClassAccess(
	t *testing.T,
	access classroom.AccessContext,
	tenantID uuid.UUID,
	userID uuid.UUID,
) {
	t.Helper()
	if access.TenantID != tenantID || access.ActorID != userID || !access.MembershipActive ||
		len(access.OrganizationRoles) != 1 || string(access.OrganizationRoles[0]) != "teacher" {
		t.Fatalf("unexpected classroom access context: %+v", access)
	}
}

type fakeClassroomService struct {
	access       classroom.AccessContext
	classes      []classroom.Class
	classID      uuid.UUID
	listLimit    int
	createCalled bool
	createInput  classroom.CreateClassInput
	requestError error
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
	return classroom.Class{
		ID: uuid.New(), TenantID: access.TenantID, OwnerUserID: access.ActorID,
		Code: strings.ToUpper(input.Code), Title: input.Title, Description: input.Description,
		Status: classroom.ClassStatusDraft, CreatedAt: fixedTime, UpdatedAt: fixedTime,
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
	limit int,
) ([]classroom.Class, error) {
	service.access = access
	service.listLimit = limit
	if service.requestError != nil {
		return nil, service.requestError
	}
	return append([]classroom.Class(nil), service.classes...), nil
}
