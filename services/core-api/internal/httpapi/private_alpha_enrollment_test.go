package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

func TestDecodeUpdatePrivateAlphaEnrollmentRequestAcceptsExactContract(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		`enrolled`:          true,
		`expected_revision`: int64(3),
		`notice_version`:    featurecontrol.PrivateAlphaNoticeVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		`/api/v1/tenants/00000000-0000-0000-0000-000000000001/private-alpha-enrollment`,
		bytes.NewReader(payload),
	)
	request.Header.Set(`Content-Type`, `application/json`)

	got, ok := decodeUpdatePrivateAlphaEnrollmentRequest(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal(`expected exact private alpha payload to decode`)
	}
	if !got.Enrolled || got.ExpectedRevision != 3 ||
		got.NoticeVersion != featurecontrol.PrivateAlphaNoticeVersion {
		t.Fatalf(`unexpected decoded request: %+v`, got)
	}
}

func TestDecodeUpdatePrivateAlphaEnrollmentRequestRejectsUnknownField(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		`enrolled`:          true,
		`expected_revision`: int64(0),
		`notice_version`:    featurecontrol.PrivateAlphaNoticeVersion,
		`tenant_id`:         `00000000-0000-0000-0000-000000000001`,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, `/`, bytes.NewReader(payload))
	request.Header.Set(`Content-Type`, `application/json`)

	if _, ok := decodeUpdatePrivateAlphaEnrollmentRequest(httptest.NewRecorder(), request); ok {
		t.Fatal(`expected payload with unknown field to be rejected`)
	}
}

func TestMapPrivateAlphaEnrollmentUsesServerOwnedNoticeVersion(t *testing.T) {
	response := mapPrivateAlphaEnrollment(featurecontrol.PrivateAlphaEnrollment{
		TenantID:      uuid.MustParse(`00000000-0000-0000-0000-000000000001`),
		Program:       featurecontrol.PrivateAlphaProgramClassroomWhiteboards,
		Status:        featurecontrol.PrivateAlphaEnrollmentActive,
		Revision:      4,
		NoticeVersion: `client-supplied-value-must-not-escape`,
		AllowedActions: featurecontrol.PrivateAlphaEnrollmentAllowedActions{
			ManageEnrollment: true,
		},
	})
	if response[`notice_version`] != featurecontrol.PrivateAlphaNoticeVersion {
		t.Fatalf(`notice version must be server-owned, got %v`, response[`notice_version`])
	}
}
