package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

func TestMediaLobbyListUsesExactModeratorProjection(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	spaceID, roomID, admissionID := uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaLobbyService{admissions: media.LobbyAdmissionPage{
		Items: []media.LobbyAdmission{{
			ID: admissionID, Status: media.LobbyAdmissionWaiting, Version: 2,
			DisplayName: "Learner", CreatedAt: fixedTime, ExpiresAt: fixedTime.Add(10 * time.Minute),
		}},
	}}
	handler := newMediaLobbyTestHandler(classIdentityService(tenantID, actorID, nil), service)
	path := "/api/v1/media/spaces/" + spaceID.String() + "/admissions" +
		"?room_instance_id=" + roomID.String() +
		"&expected_space_version=7&expected_room_instance_version=3&limit=25"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.listAdmissionsCalls != 1 ||
		service.spaceID != spaceID || service.listAdmissionsInput.ExpectedRoomInstanceID != roomID ||
		service.listAdmissionsInput.ExpectedSpaceVersion != 7 ||
		service.listAdmissionsInput.ExpectedRoomInstanceVersion != 3 ||
		service.listAdmissionsInput.Limit != 25 {
		t.Fatalf("unexpected lobby list result: status=%d service=%+v", response.Code, service)
	}
	if service.access.TenantID != tenantID || service.access.ActorID != actorID ||
		!service.access.MembershipActive {
		t.Fatalf("unexpected lobby access projection: %+v", service.access)
	}
	for _, header := range []string{"Cache-Control", "Referrer-Policy", "X-Content-Type-Options"} {
		if strings.TrimSpace(response.Header().Get(header)) == "" {
			t.Fatalf("missing privacy header %s", header)
		}
	}
	for _, forbidden := range []string{
		"email", "participant_session", "join_attempt", "provider", "access_token",
	} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("lobby response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestMediaLobbyDenyRequiresCSRFAndExactCASBody(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	spaceID, roomID, admissionID := uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaLobbyService{admission: media.LobbyAdmission{
		ID: admissionID, Status: media.LobbyAdmissionDenied, Version: 5,
		DisplayName: "Learner", CreatedAt: fixedTime, ExpiresAt: fixedTime.Add(10 * time.Minute),
	}}
	handler := newMediaLobbyTestHandler(classIdentityService(tenantID, actorID, nil), service)
	body := `{"expected_space_version":7,"expected_room_instance_id":"` + roomID.String() +
		`","expected_room_instance_version":3,"expected_admission_version":4,` +
		`"idempotency_key":"admission-deny-00001","reason_code":"host_denied"}`
	path := "/api/v1/media/spaces/" + spaceID.String() + "/admissions/" + admissionID.String() + "/deny"

	missingCSRF := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.denyCalls != 0 {
		t.Fatalf("missing CSRF must stop before lobby service: status=%d calls=%d", missingResponse.Code, service.denyCalls)
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.denyCalls != 1 ||
		service.spaceID != spaceID || service.admissionID != admissionID ||
		service.admissionInput.ExpectedRoomInstanceID != roomID ||
		service.admissionInput.ExpectedSpaceVersion != 7 ||
		service.admissionInput.ExpectedRoomInstanceVersion != 3 ||
		service.admissionInput.ExpectedAdmissionVersion != 4 ||
		service.admissionInput.IdempotencyKey != "admission-deny-00001" ||
		service.admissionInput.ReasonCode != "host_denied" {
		t.Fatalf("unexpected deny request: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
}

func TestMediaLobbyInviteEmailIsInputOnly(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	spaceID, memberID := uuid.New(), uuid.New()
	service := &fakeMediaLobbyService{member: media.LobbyMember{
		UserID: memberID, DisplayName: "Learner", Status: media.LobbyMemberActive,
		Version: 1, CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}}
	handler := newMediaLobbyTestHandler(classIdentityService(tenantID, actorID, nil), service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/media/spaces/"+spaceID.String()+"/members",
		strings.NewReader(`{"target_member_email":"learner@example.test","expected_space_version":7,"idempotency_key":"member-invite-00001"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.inviteCalls != 1 ||
		service.inviteInput.Email != "learner@example.test" ||
		service.inviteInput.ExpectedSpaceVersion != 7 {
		t.Fatalf("unexpected invite result: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "example.test") ||
		strings.Contains(strings.ToLower(response.Body.String()), "email") {
		t.Fatalf("invite response must not echo raw email: %s", response.Body.String())
	}
}

func newMediaLobbyTestHandler(
	identityService identity.ServiceAPI,
	service media.LobbyServiceAPI,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: fixedClock, Identity: identityService, MediaLobby: service},
	)
}

type fakeMediaLobbyService struct {
	access              media.AccessContext
	spaceID             uuid.UUID
	admissionID         uuid.UUID
	joinAttemptID       uuid.UUID
	listAdmissionsInput media.ListLobbyAdmissionsInput
	admissionInput      media.AdmissionMutationInput
	memberInput         media.MemberMutationInput
	inviteInput         media.InviteLobbyMemberInput
	admissions          media.LobbyAdmissionPage
	admission           media.LobbyAdmission
	joinAttempt         media.JoinAttempt
	members             media.LobbyMemberPage
	member              media.LobbyMember
	requestError        error
	listAdmissionsCalls int
	denyCalls           int
	inviteCalls         int
}

func (service *fakeMediaLobbyService) ListAdmissions(
	_ context.Context, access media.AccessContext, spaceID uuid.UUID,
	input media.ListLobbyAdmissionsInput,
) (media.LobbyAdmissionPage, error) {
	service.listAdmissionsCalls++
	service.access, service.spaceID, service.listAdmissionsInput = access, spaceID, input
	return service.admissions, service.requestError
}

func (service *fakeMediaLobbyService) GetJoinAttempt(
	_ context.Context, access media.AccessContext, spaceID, joinAttemptID uuid.UUID,
	input media.ListLobbyAdmissionsInput,
) (media.JoinAttempt, error) {
	service.access, service.spaceID, service.joinAttemptID = access, spaceID, joinAttemptID
	service.listAdmissionsInput = input
	return service.joinAttempt, service.requestError
}

func (service *fakeMediaLobbyService) Admit(
	_ context.Context, access media.AccessContext, spaceID, admissionID uuid.UUID,
	input media.AdmissionMutationInput,
) (media.LobbyAdmission, error) {
	service.access, service.spaceID, service.admissionID, service.admissionInput = access, spaceID, admissionID, input
	return service.admission, service.requestError
}

func (service *fakeMediaLobbyService) Deny(
	_ context.Context, access media.AccessContext, spaceID, admissionID uuid.UUID,
	input media.AdmissionMutationInput,
) (media.LobbyAdmission, error) {
	service.denyCalls++
	service.access, service.spaceID, service.admissionID, service.admissionInput = access, spaceID, admissionID, input
	return service.admission, service.requestError
}

func (service *fakeMediaLobbyService) CancelJoinAttempt(
	_ context.Context, access media.AccessContext, spaceID, joinAttemptID uuid.UUID,
	input media.AdmissionMutationInput,
) (media.JoinAttempt, error) {
	service.access, service.spaceID, service.joinAttemptID, service.admissionInput = access, spaceID, joinAttemptID, input
	return service.joinAttempt, service.requestError
}

func (service *fakeMediaLobbyService) RestoreAdmission(
	_ context.Context, access media.AccessContext, spaceID, admissionID uuid.UUID,
	input media.AdmissionMutationInput,
) (media.LobbyAdmission, error) {
	service.access, service.spaceID, service.admissionID, service.admissionInput = access, spaceID, admissionID, input
	return service.admission, service.requestError
}

func (service *fakeMediaLobbyService) ListMembers(
	_ context.Context, access media.AccessContext, spaceID uuid.UUID,
	_ media.ListLobbyMembersInput,
) (media.LobbyMemberPage, error) {
	service.access, service.spaceID = access, spaceID
	return service.members, service.requestError
}

func (service *fakeMediaLobbyService) InviteMember(
	_ context.Context, access media.AccessContext, spaceID uuid.UUID,
	input media.InviteLobbyMemberInput,
) (media.LobbyMember, error) {
	service.inviteCalls++
	service.access, service.spaceID, service.inviteInput = access, spaceID, input
	return service.member, service.requestError
}

func (service *fakeMediaLobbyService) RevokeMember(
	_ context.Context, access media.AccessContext, spaceID, _ uuid.UUID,
	input media.MemberMutationInput,
) (media.LobbyMember, error) {
	service.access, service.spaceID, service.memberInput = access, spaceID, input
	return service.member, service.requestError
}

func (service *fakeMediaLobbyService) RestoreMember(
	_ context.Context, access media.AccessContext, spaceID, _ uuid.UUID,
	input media.MemberMutationInput,
) (media.LobbyMember, error) {
	service.access, service.spaceID, service.memberInput = access, spaceID, input
	return service.member, service.requestError
}
