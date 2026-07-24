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
)

func TestClassSessionHandlersEnforceCSRFAndExposeVersionedContract(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	sessionID := uuid.New()
	service := &fakeSessionService{
		session: classroom.ClassSession{
			ID: sessionID, TenantID: tenantID, ClassID: classID,
			Title: "Buổi học thử", Description: "Mô tả",
			StartsAt: fixedTime, EndsAt: fixedTime.Add(time.Hour),
			Timezone: "Asia/Ho_Chi_Minh", Status: classroom.SessionStatusScheduled,
			Version: 4, CreatedBy: userID, UpdatedBy: userID,
			CreatedAt: fixedTime, UpdatedAt: fixedTime,
			ViewerAccess: classroom.SessionViewerAccess{CanUpdate: true, CanCancel: true},
		},
	}
	handler := NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock: func() time.Time { return fixedTime },
			Identity: classIdentityService(tenantID, userID, []string{
				"class.view", "class.update", "class.create",
			}),
			ClassSessions: service,
		},
	)

	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/classes/"+classID.String()+"/sessions"+
			"?range_start=2026-07-24T00:00:00%2B07:00&range_end=2026-07-25T00:00:00%2B07:00",
		nil,
	)
	addSessionCookie(listRequest)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK ||
		listResponse.Header().Get("Cache-Control") != "no-store" ||
		listResponse.Header().Get("Vary") != "Cookie" {
		t.Fatalf("list response: status=%d headers=%v body=%s",
			listResponse.Code, listResponse.Header(), listResponse.Body.String())
	}
	var listBody classSessionListResponse
	if err := json.NewDecoder(listResponse.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listBody.Items) != 1 || listBody.Items[0].ID != sessionID ||
		service.listInput.From == "" || service.listInput.To == "" {
		t.Fatalf("unexpected list response/service: %+v / %+v", listBody, service)
	}

	body := `{"title":"Buổi học mới","description":"","starts_at":"2026-07-24T10:00:00+07:00","ends_at":"2026-07-24T11:00:00+07:00","timezone":"Asia/Ho_Chi_Minh"}`
	missingCSRF := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/classes/"+classID.String()+"/sessions",
		strings.NewReader(body),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.createCalled {
		t.Fatalf("missing CSRF must be denied: status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/classes/"+classID.String()+"/sessions",
		strings.NewReader(body),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set(csrfHeader, "csrf-token")
	addSessionCookie(createRequest)
	createRequest.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated ||
		!strings.HasSuffix(createResponse.Header().Get("Location"), sessionID.String()) ||
		!service.createCalled ||
		service.createInput.Title != "Buổi học mới" {
		t.Fatalf("create response/service: status=%d location=%q body=%s service=%+v",
			createResponse.Code, createResponse.Header().Get("Location"),
			createResponse.Body.String(), service)
	}

	updateResponse := performClassMutation(
		handler,
		http.MethodPatch,
		"/api/v1/classes/"+classID.String()+"/sessions/"+sessionID.String(),
		`{"expected_version":4,"title":"Đã cập nhật"}`,
	)
	if updateResponse.Code != http.StatusOK ||
		service.updateInput.ExpectedVersion != 4 ||
		service.updateInput.Title == nil ||
		*service.updateInput.Title != "Đã cập nhật" {
		t.Fatalf("update response/service: status=%d body=%s service=%+v",
			updateResponse.Code, updateResponse.Body.String(), service)
	}

	cancelResponse := performClassMutation(
		handler,
		http.MethodPost,
		"/api/v1/classes/"+classID.String()+"/sessions/"+sessionID.String()+"/cancel",
		`{"expected_version":5}`,
	)
	if cancelResponse.Code != http.StatusOK || service.cancelVersion != 5 {
		t.Fatalf("cancel response/service: status=%d body=%s service=%+v",
			cancelResponse.Code, cancelResponse.Body.String(), service)
	}
}

type fakeSessionService struct {
	session       classroom.ClassSession
	listInput     classroom.ListSessionsInput
	createInput   classroom.CreateSessionInput
	updateInput   classroom.UpdateSessionInput
	cancelVersion int64
	createCalled  bool
	requestError  error
}

func (service *fakeSessionService) CreateSession(
	_ context.Context,
	_ classroom.AccessContext,
	_ uuid.UUID,
	input classroom.CreateSessionInput,
) (classroom.ClassSession, error) {
	service.createCalled = true
	service.createInput = input
	if service.requestError != nil {
		return classroom.ClassSession{}, service.requestError
	}
	return service.session, nil
}

func (service *fakeSessionService) GetSession(
	_ context.Context,
	_ classroom.AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
) (classroom.ClassSession, error) {
	if service.requestError != nil {
		return classroom.ClassSession{}, service.requestError
	}
	return service.session, nil
}

func (service *fakeSessionService) ListSessions(
	_ context.Context,
	_ classroom.AccessContext,
	_ uuid.UUID,
	input classroom.ListSessionsInput,
) (classroom.SessionPage, error) {
	service.listInput = input
	if service.requestError != nil {
		return classroom.SessionPage{}, service.requestError
	}
	return classroom.SessionPage{Items: []classroom.ClassSession{service.session}}, nil
}

func (service *fakeSessionService) UpdateSession(
	_ context.Context,
	_ classroom.AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
	input classroom.UpdateSessionInput,
) (classroom.ClassSession, error) {
	service.updateInput = input
	if service.requestError != nil {
		return classroom.ClassSession{}, service.requestError
	}
	return service.session, nil
}

func (service *fakeSessionService) CancelSession(
	_ context.Context,
	_ classroom.AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
	expectedVersion int64,
) (classroom.ClassSession, error) {
	service.cancelVersion = expectedVersion
	if service.requestError != nil {
		return classroom.ClassSession{}, service.requestError
	}
	return service.session, nil
}
