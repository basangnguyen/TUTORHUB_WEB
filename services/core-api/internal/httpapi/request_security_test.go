package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestDecodeJSONRequestRejectsMassAssignmentAcrossMutationSchemas(t *testing.T) {
	t.Parallel()

	const identifier = "4ab9249b-d5a7-4ab7-9be6-f98f63e03fd7"
	tests := []struct {
		name        string
		body        string
		destination func() any
	}{
		{
			name:        "create tenant cannot assign tenant id",
			body:        `{"name":"Workspace","slug":"workspace","tenant_id":"` + identifier + `"}`,
			destination: func() any { return &createTenantRequest{} },
		},
		{
			name:        "switch tenant cannot assign role",
			body:        `{"tenant_id":"` + identifier + `","role":"org_admin"}`,
			destination: func() any { return &switchActiveTenantRequest{} },
		},
		{
			name:        "update tenant cannot assign owner",
			body:        `{"expected_version":1,"owner_user_id":"` + identifier + `"}`,
			destination: func() any { return &updateTenantRequest{} },
		},
		{
			name:        "archive tenant cannot assign state",
			body:        `{"expected_version":1,"status":"active"}`,
			destination: func() any { return &archiveTenantRequest{} },
		},
		{
			name:        "profile cannot assign user id",
			body:        `{"display_name":"Learner","user_id":"` + identifier + `"}`,
			destination: func() any { return &identity.ProfilePatch{} },
		},
		{
			name:        "membership invitation cannot assign tenant",
			body:        `{"email":"student@example.test","intended_role":"student","tenant_id":"` + identifier + `"}`,
			destination: func() any { return &createMembershipInvitationRequest{} },
		},
		{
			name:        "membership token cannot assign email",
			body:        `{"token":"thinv1_value","email":"student@example.test"}`,
			destination: func() any { return &membershipInvitationTokenRequest{} },
		},
		{
			name:        "create class cannot assign tenant",
			body:        `{"code":"SEC101","title":"Security","description":"","tenant_id":"` + identifier + `"}`,
			destination: func() any { return &createClassRequest{} },
		},
		{
			name:        "update class cannot assign owner",
			body:        `{"title":"Security","expected_version":1,"owner_user_id":"` + identifier + `"}`,
			destination: func() any { return &updateClassRequest{} },
		},
		{
			name:        "class version cannot assign actor",
			body:        `{"expected_version":1,"actor_id":"` + identifier + `"}`,
			destination: func() any { return &classVersionRequest{} },
		},
		{
			name:        "ownership transfer cannot assign tenant",
			body:        `{"expected_version":1,"new_owner_user_id":"` + identifier + `","tenant_id":"` + identifier + `"}`,
			destination: func() any { return &transferClassOwnershipRequest{} },
		},
		{
			name:        "direct enrollment cannot assign user",
			body:        `{"member_email":"student@example.test","user_id":"` + identifier + `"}`,
			destination: func() any { return &directClassEnrollmentRequest{} },
		},
		{
			name:        "class invite cannot assign creator",
			body:        `{"expires_in_seconds":3600,"usage_limit":10,"created_by":"` + identifier + `"}`,
			destination: func() any { return &createClassInviteCodeRequest{} },
		},
		{
			name:        "class token cannot assign class",
			body:        `{"token":"thciv1_value","class_id":"` + identifier + `"}`,
			destination: func() any { return &classInvitationTokenRequest{} },
		},
		{
			name:        "roster role cannot assign actor",
			body:        `{"class_role":"student","actor_id":"` + identifier + `"}`,
			destination: func() any { return &updateClassRosterRoleRequest{} },
		},
		{
			name:        "bulk roster cannot assign tenant",
			body:        `{"action":"remove","user_ids":["` + identifier + `"],"tenant_id":"` + identifier + `"}`,
			destination: func() any { return &classRosterBulkRequest{} },
		},
		{
			name:        "feature control cannot assign source",
			body:        `{"expected_version":1,"features":{"membership_invitations":true,"class_management":true,"class_invite_links":true},"quotas":{"members":100,"active_classes":10,"invite_creations_per_hour":20},"feature_source":"deployment"}`,
			destination: func() any { return &updateTenantFeatureControlsRequest{} },
		},
		{
			name:        "nested feature control cannot assign usage",
			body:        `{"expected_version":1,"features":{"membership_invitations":true,"class_management":true,"class_invite_links":true},"quotas":{"members":100,"active_classes":10,"invite_creations_per_hour":20,"used":99}}`,
			destination: func() any { return &updateTenantFeatureControlsRequest{} },
		},
		{
			name:        "media telemetry cannot assign tenant",
			body:        `{"attempt_id":"` + identifier + `","stage":"join","outcome":"success","duration_ms":1,"tenant_id":"` + identifier + `"}`,
			destination: func() any { return &mediaEventRequest{} },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			if err := decodeJSONRequest(
				response,
				request,
				test.destination(),
				64*1024,
			); err == nil {
				t.Fatal("mass-assignment field was accepted")
			}
		})
	}
}

func TestDecodeJSONRequestRejectsAmbiguousOrNonObjectPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		contentType string
		maximum     int64
	}{
		{name: "null", body: `null`, contentType: "application/json", maximum: 1024},
		{name: "array", body: `[]`, contentType: "application/json", maximum: 1024},
		{name: "scalar", body: `true`, contentType: "application/json", maximum: 1024},
		{name: "duplicate top-level", body: `{"name":"one","name":"two"}`, contentType: "application/json", maximum: 1024},
		{name: "duplicate case-folded", body: `{"name":"one","NAME":"two"}`, contentType: "application/json", maximum: 1024},
		{name: "duplicate nested", body: `{"nested":{"value":1,"value":2}}`, contentType: "application/json", maximum: 1024},
		{name: "second object", body: `{} {}`, contentType: "application/json", maximum: 1024},
		{name: "wrong content type", body: `{}`, contentType: "text/plain", maximum: 1024},
		{name: "oversized", body: `{"value":"` + strings.Repeat("x", 128) + `"}`, contentType: "application/json", maximum: 32},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if err := decodeJSONRequest(
				httptest.NewRecorder(),
				request,
				&struct{}{},
				test.maximum,
			); err == nil {
				t.Fatal("ambiguous or non-object request was accepted")
			}
		})
	}
}

func TestDecodeJSONRequestAcceptsOneStrictObject(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"TutorHub"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	var destination struct {
		Name string `json:"name"`
	}
	if err := decodeJSONRequest(
		httptest.NewRecorder(), request, &destination, 1024,
	); err != nil {
		t.Fatalf("decode strict object: %v", err)
	}
	if destination.Name != "TutorHub" {
		t.Fatalf("unexpected decoded object: %+v", destination)
	}
}

func FuzzDecodeJSONRequestSecurityBoundary(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`null`,
		`[]`,
		`{"value":1}`,
		`{"value":1,"value":2}`,
		`{"value":1,"VALUE":2}`,
		`{} {}`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, body string) {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		_ = decodeJSONRequest(
			httptest.NewRecorder(),
			request,
			&struct{}{},
			4*1024,
		)
	})
}
