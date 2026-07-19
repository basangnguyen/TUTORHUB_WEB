package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestClassRosterListMapsFiltersAndKeepsOwnerOutsidePagination(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	ownerID := uuid.New()
	userID := uuid.New()
	status := classroom.EnrollmentStatusActive
	joinedAt := fixedTime
	service := &fakeClassEnrollmentService{listRosterResult: classroom.RosterPage{
		Owner: classroom.RosterOwner{User: classroom.RosterUser{
			ID: ownerID, DisplayName: "Owner", Email: "owner@example.test",
		}},
		Items: []classroom.RosterMember{{
			User: classroom.RosterUser{
				ID: userID, DisplayName: "Student", Email: "student@example.test",
			},
			Enrollment: classroom.Enrollment{
				ID: uuid.New(), TenantID: tenantID, ClassID: classID,
				UserID: userID, ClassRole: policy.ClassRoleStudent,
				Status: status, EnrolledBy: ownerID, JoinedAt: &joinedAt,
				CreatedAt: fixedTime, UpdatedAt: fixedTime,
			},
			Actions: classroom.RosterMemberActions{
				AssignableRoles: []policy.ClassRole{policy.ClassRoleTeachingAssistant},
				CanSuspend:      true, CanRemove: true,
			},
		}},
		NextCursor: "thro1_next",
	}}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(tenantID, actorID, []string{"enrollment.manage"}),
		service,
		nil,
	)
	request := httptest.NewRequest(
		http.MethodGet,
		classRosterPath(classID)+"?search=%20Student%20%20One%20&status=active&limit=15&cursor=thro1_current",
		nil,
	)
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("list class roster: status=%d body=%s", response.Code, response.Body.String())
	}
	assertClassAccess(t, service.access, tenantID, actorID)
	if service.listRosterClassID != classID ||
		service.listRosterInput.Search != " Student  One " ||
		service.listRosterInput.Status == nil ||
		*service.listRosterInput.Status != classroom.EnrollmentStatusActive ||
		service.listRosterInput.Limit != 15 ||
		service.listRosterInput.Cursor != "thro1_current" {
		t.Fatalf("unexpected roster list input: %+v", service.listRosterInput)
	}
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing roster privacy headers: %v", response.Header())
	}
	var body classRosterPageResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode roster response: %v", err)
	}
	if body.Owner.User.ID != ownerID || body.Owner.ClassRole != "owner" ||
		len(body.Items) != 1 || body.Items[0].User.ID != userID ||
		body.Items[0].Enrollment.ClassRole != "student" ||
		len(body.Items[0].Actions.AssignableRoles) != 1 ||
		body.NextCursor == nil || *body.NextCursor != "thro1_next" {
		t.Fatalf("unexpected roster response: %+v", body)
	}
	serializedBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("re-marshal roster response: %v", err)
	}
	serialized := string(serializedBytes)
	if strings.Contains(serialized, "tenant_id") || strings.Contains(serialized, "actor_user_id") {
		t.Fatalf("roster response exposed internal scope: %s", serialized)
	}
}

func TestClassRosterRolePatchRequiresCSRFAndReturnsIdempotentOutcome(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	userID := uuid.New()
	enrollment := classroom.Enrollment{
		ID: uuid.New(), TenantID: tenantID, ClassID: classID, UserID: userID,
		ClassRole: policy.ClassRoleTeachingAssistant,
		Status:    classroom.EnrollmentStatusActive, EnrolledBy: actorID,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}
	service := &fakeClassEnrollmentService{updateRosterResult: classroom.EnrollmentMutationResult{
		Enrollment: enrollment,
	}}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(tenantID, actorID, []string{"enrollment.manage"}),
		service,
		nil,
	)
	path := classRosterPath(classID) + "/" + userID.String()

	missingCSRF := httptest.NewRequest(
		http.MethodPatch, path, strings.NewReader(`{"class_role":"teaching_assistant"}`),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	addSessionCookie(missingCSRF)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, missingCSRF)
	if denied.Code != http.StatusForbidden || service.updateRosterUserID != uuid.Nil {
		t.Fatalf("missing CSRF must be denied: status=%d service=%+v", denied.Code, service)
	}

	request := httptest.NewRequest(
		http.MethodPatch, path, strings.NewReader(`{"class_role":"teaching_assistant"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("patch class roster: status=%d body=%s", response.Code, response.Body.String())
	}
	if service.updateRosterClassID != classID || service.updateRosterUserID != userID ||
		service.updateRosterInput.ClassRole != policy.ClassRoleTeachingAssistant {
		t.Fatalf("unexpected role update input: %+v", service)
	}
	var body classRosterMutationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode role response: %v", err)
	}
	if body.Outcome != "unchanged" || body.Enrollment.ClassRole != "teaching_assistant" {
		t.Fatalf("unexpected idempotent role response: %+v", body)
	}
}

func TestClassRosterBulkPreservesOrderAndSerializesPartialFailures(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	first := uuid.New()
	second := uuid.New()
	enrollment := classroom.Enrollment{
		ID: uuid.New(), TenantID: tenantID, ClassID: classID, UserID: first,
		ClassRole: policy.ClassRoleStudent, Status: classroom.EnrollmentStatusSuspended,
		EnrolledBy: actorID, CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}
	service := &fakeClassEnrollmentService{bulkRosterResult: classroom.BulkRosterResult{
		Action: classroom.RosterBulkActionSuspend,
		Items: []classroom.RosterBulkItemResult{
			{UserID: first, Enrollment: &enrollment, Changed: true},
			{UserID: second, Failure: &classroom.RosterBulkFailure{
				Code: classroom.RosterBulkFailureConflict,
			}},
		},
		SucceededCount: 1, FailedCount: 1,
	}}
	handler := newClassEnrollmentTestHandler(
		classIdentityService(tenantID, actorID, []string{"enrollment.manage"}),
		service,
		nil,
	)
	request := newClassEnrollmentMutationRequest(
		classRosterPath(classID)+"/bulk",
		`{"action":"suspend","user_ids":["`+first.String()+`","`+second.String()+`"]}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("bulk class roster: status=%d body=%s", response.Code, response.Body.String())
	}
	if service.bulkRosterClassID != classID ||
		service.bulkRosterInput.Action != classroom.RosterBulkActionSuspend ||
		len(service.bulkRosterInput.UserIDs) != 2 ||
		service.bulkRosterInput.UserIDs[0] != first || service.bulkRosterInput.UserIDs[1] != second {
		t.Fatalf("unexpected bulk roster input: %+v", service.bulkRosterInput)
	}
	var body classRosterBulkResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode bulk roster response: %v", err)
	}
	if body.RequestedCount != 2 || body.UpdatedCount != 1 ||
		body.UnchangedCount != 0 || body.FailedCount != 1 ||
		len(body.Items) != 2 || body.Items[0].UserID != first ||
		body.Items[0].Outcome != "updated" || body.Items[1].UserID != second ||
		body.Items[1].Outcome != "failed" || body.Items[1].Failure == nil ||
		body.Items[1].Failure.Code != "conflict" {
		t.Fatalf("unexpected ordered bulk response: %+v", body)
	}
}

func TestClassRosterBulkInfrastructureFailureAuditsEveryAbortedTarget(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	first := uuid.New()
	second := uuid.New()
	third := uuid.New()
	service := &fakeClassEnrollmentService{
		bulkRosterResult: classroom.BulkRosterResult{
			Action: classroom.RosterBulkActionSuspend,
			Items: []classroom.RosterBulkItemResult{
				{UserID: first, Changed: true},
				{UserID: second, Failure: &classroom.RosterBulkFailure{
					Code: classroom.RosterBulkFailureInternal,
				}},
				{UserID: third, Failure: &classroom.RosterBulkFailure{
					Code: classroom.RosterBulkFailureNotAttempted,
				}},
			},
			SucceededCount: 1,
			FailedCount:    2,
		},
		bulkRosterError: errors.New("database unavailable"),
	}
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
			Clock:      func() time.Time { return fixedTime },
			Identity:   classIdentityService(tenantID, actorID, []string{"enrollment.manage"}),
			Enrollment: service,
			Audit:      auditService,
		},
	)
	request := newClassEnrollmentMutationRequest(
		classRosterPath(classID)+"/bulk",
		`{"action":"suspend","user_ids":["`+first.String()+`","`+
			second.String()+`","`+third.String()+`"]}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected infrastructure failure response: status=%d body=%s", response.Code, response.Body.String())
	}
	if auditService.recordCalls != 0 || auditService.itemFallbackAttempts != 2 ||
		len(auditService.recordedItemDrafts) != 2 {
		t.Fatalf(
			"aborted targets must each have an item fallback: records=%d attempts=%d drafts=%#v",
			auditService.recordCalls,
			auditService.itemFallbackAttempts,
			auditService.recordedItemDrafts,
		)
	}
	wants := []struct {
		resourceID uuid.UUID
		reason     string
	}{
		{resourceID: second, reason: "internal_failure"},
		{resourceID: third, reason: "not_attempted"},
	}
	for index, want := range wants {
		draft := auditService.recordedItemDrafts[index]
		if draft.Action != audit.ActionClassEnrollmentSuspend ||
			draft.ResourceType != "class_member" || draft.ResourceID != want.resourceID ||
			draft.Outcome != audit.OutcomeFailed ||
			draft.Metadata["reason_code"] != want.reason ||
			draft.Metadata["bulk_action"] != "suspend" ||
			draft.Metadata["class_id"] != classID.String() ||
			draft.Metadata[audit.MetadataKeyTargetUserID] != want.resourceID.String() {
			t.Fatalf("unexpected aborted-target audit draft %d: %#v", index, draft)
		}
	}
	if auditService.fallbackAttempts != 1 || len(auditService.recordedFallbackDrafts) != 1 {
		t.Fatalf(
			"bulk infrastructure failure needs one overall fallback audit: attempts=%d drafts=%#v",
			auditService.fallbackAttempts,
			auditService.recordedFallbackDrafts,
		)
	}
	overall := auditService.recordedFallbackDrafts[0]
	if overall.Action != audit.ActionClassRosterBulk || overall.ResourceType != "class" ||
		overall.ResourceID != classID || overall.Outcome != audit.OutcomeFailed ||
		overall.Metadata["reason_code"] != "internal_failure" {
		t.Fatalf("unexpected overall bulk audit draft: %#v", overall)
	}
}

func classRosterPath(classID uuid.UUID) string {
	return "/api/v1/classes/" + classID.String() + "/roster"
}
