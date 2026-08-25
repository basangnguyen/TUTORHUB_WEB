package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const tenantPrivateAlphaEnrollmentPattern = `/api/v1/tenants/{tenant_id}/private-alpha-enrollment`

type updatePrivateAlphaEnrollmentRequest struct {
	Enrolled         bool
	ExpectedRevision int64
	NoticeVersion    string
}

func decodeUpdatePrivateAlphaEnrollmentRequest(
	w http.ResponseWriter,
	r *http.Request,
) (updatePrivateAlphaEnrollmentRequest, bool) {
	var fields map[string]json.RawMessage
	if err := decodeJSONRequest(w, r, &fields, 4<<10); err != nil || len(fields) != 3 {
		return updatePrivateAlphaEnrollmentRequest{}, false
	}
	enrolled, enrolledOK := fields[`enrolled`]
	expectedRevision, revisionOK := fields[`expected_revision`]
	noticeVersion, noticeOK := fields[`notice_version`]
	if !enrolledOK || !revisionOK || !noticeOK {
		return updatePrivateAlphaEnrollmentRequest{}, false
	}
	var request updatePrivateAlphaEnrollmentRequest
	if json.Unmarshal(enrolled, &request.Enrolled) != nil ||
		json.Unmarshal(expectedRevision, &request.ExpectedRevision) != nil ||
		json.Unmarshal(noticeVersion, &request.NoticeVersion) != nil ||
		request.ExpectedRevision < 0 ||
		request.NoticeVersion != featurecontrol.PrivateAlphaNoticeVersion {
		return updatePrivateAlphaEnrollmentRequest{}, false
	}
	return request, true
}

func (handlers featureControlHandlers) privateAlphaEnrollment(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.getPrivateAlphaEnrollment(w, r)
	case http.MethodPut:
		handlers.updatePrivateAlphaEnrollment(w, r)
	default:
		w.Header().Set(`Allow`, `GET, HEAD, PUT`)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			`Method not allowed`,
			`Private alpha enrollment supports GET and PUT requests.`,
		)
	}
}

func (handlers featureControlHandlers) getPrivateAlphaEnrollment(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	tenantContext, ok := activeTenantContext(principal, r.PathValue(`tenant_id`))
	if !ok {
		handlers.writeProblem(w, r, featurecontrol.ErrTenantNotFound)
		return
	}
	enrollment, err := handlers.service.GetPrivateAlphaEnrollment(r.Context(), tenantContext)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, mapPrivateAlphaEnrollment(enrollment))
}

func (handlers featureControlHandlers) updatePrivateAlphaEnrollment(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return
	}
	tenantContext, ok := activeTenantContext(principal, r.PathValue(`tenant_id`))
	if !ok {
		handlers.writeProblem(w, r, featurecontrol.ErrTenantNotFound)
		return
	}
	request, ok := decodeUpdatePrivateAlphaEnrollmentRequest(w, r)
	if !ok {
		handlers.writeProblem(w, r, featurecontrol.ErrInvalidControl)
		return
	}
	enrollment, err := handlers.service.UpdatePrivateAlphaEnrollment(
		r.Context(),
		tenantContext,
		featurecontrol.UpdatePrivateAlphaEnrollmentInput{
			Enrolled:         request.Enrolled,
			ExpectedRevision: request.ExpectedRevision,
			NoticeVersion:    request.NoticeVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, mapPrivateAlphaEnrollment(enrollment))
}

func mapPrivateAlphaEnrollment(enrollment featurecontrol.PrivateAlphaEnrollment) map[string]any {
	return map[string]any{
		`tenant_id`:      enrollment.TenantID,
		`program`:        enrollment.Program,
		`status`:         enrollment.Status,
		`revision`:       enrollment.Revision,
		`notice_version`: featurecontrol.PrivateAlphaNoticeVersion,
		`accepted_at`:    enrollment.AcceptedAt,
		`withdrawn_at`:   enrollment.WithdrawnAt,
		`allowed_actions`: map[string]bool{
			`manage_enrollment`: enrollment.AllowedActions.ManageEnrollment,
		},
	}
}
