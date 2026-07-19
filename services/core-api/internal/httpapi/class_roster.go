package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	classRosterPattern     = "/api/v1/classes/{class_id}/roster"
	classRosterUserPattern = "/api/v1/classes/{class_id}/roster/{user_id}"
	classRosterBulkPattern = "/api/v1/classes/{class_id}/roster/bulk"
)

type updateClassRosterRoleRequest struct {
	ClassRole string `json:"class_role"`
}

type classRosterMemberActionsResponse struct {
	AssignableRoles []string `json:"assignable_roles"`
	CanSuspend      bool     `json:"can_suspend"`
	CanRemove       bool     `json:"can_remove"`
}

type classRosterUserResponse struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
}

type classRosterOwnerResponse struct {
	User      classRosterUserResponse `json:"user"`
	ClassRole string                  `json:"class_role"`
}

type classRosterMemberResponse struct {
	User       classRosterUserResponse          `json:"user"`
	Enrollment classEnrollmentResponse          `json:"enrollment"`
	Actions    classRosterMemberActionsResponse `json:"actions"`
}

type classRosterPageResponse struct {
	Owner      classRosterOwnerResponse    `json:"class_owner"`
	Items      []classRosterMemberResponse `json:"items"`
	NextCursor *string                     `json:"next_cursor"`
}

type classRosterBulkRequest struct {
	Action    string      `json:"action"`
	ClassRole *string     `json:"class_role"`
	UserIDs   []uuid.UUID `json:"user_ids"`
}

type classRosterBulkFailureResponse struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type classRosterBulkItemResponse struct {
	UserID     uuid.UUID                       `json:"user_id"`
	Outcome    string                          `json:"outcome"`
	Enrollment *classEnrollmentResponse        `json:"enrollment"`
	Failure    *classRosterBulkFailureResponse `json:"failure"`
}

type classRosterMutationResponse struct {
	Outcome    string                  `json:"outcome"`
	Enrollment classEnrollmentResponse `json:"enrollment"`
}

type classRosterBulkResponse struct {
	Action         string                        `json:"action"`
	Items          []classRosterBulkItemResponse `json:"items"`
	RequestedCount int                           `json:"requested_count"`
	UpdatedCount   int                           `json:"updated_count"`
	UnchangedCount int                           `json:"unchanged_count"`
	FailedCount    int                           `json:"failed_count"`
}

func (handlers classEnrollmentHandlers) rosterCollection(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class roster collection accepts GET requests.",
		)
		return
	}
	if !handlers.available(w, r) {
		return
	}
	classID, ok := parseInvitationPathUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound, classEnrollmentAdminProblem)
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}

	input := classroom.ListRosterInput{
		Search: r.URL.Query().Get("search"),
		Cursor: r.URL.Query().Get("cursor"),
	}
	if value := strings.TrimSpace(r.URL.Query().Get("status")); value != "" {
		status := classroom.EnrollmentStatus(value)
		input.Status = &status
	}
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			handlers.writeProblem(w, r, classroom.ErrInvalidEnrollmentInput, classEnrollmentAdminProblem)
			return
		}
		input.Limit = limit
	}

	page, err := handlers.enrollments.ListRoster(
		r.Context(), classAccess(principal), classID, input,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	items := make([]classRosterMemberResponse, 0, len(page.Items))
	for _, member := range page.Items {
		items = append(items, newClassRosterMemberResponse(member))
	}
	var nextCursor *string
	if page.NextCursor != "" {
		value := page.NextCursor
		nextCursor = &value
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		classRosterPageResponse{
			Owner:      newClassRosterOwnerResponse(page.Owner),
			Items:      items,
			NextCursor: nextCursor,
		},
	)
}

func (handlers classEnrollmentHandlers) rosterUser(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPatch {
		w.Header().Set("Allow", http.MethodPatch)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class roster role mutation accepts PATCH requests.",
		)
		return
	}
	classID, userID, ok := parseClassRosterResource(r)
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrEnrollmentNotFound, classEnrollmentAdminProblem)
		return
	}
	principal, ok := handlers.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var request updateClassRosterRoleRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassEnrollmentRequestBytes,
	); err != nil {
		handlers.writeProblem(w, r, classroom.ErrInvalidEnrollmentInput, classEnrollmentAdminProblem)
		return
	}
	result, err := handlers.enrollments.UpdateRosterRole(
		r.Context(),
		classAccess(principal),
		classID,
		userID,
		classroom.UpdateRosterRoleInput{ClassRole: policy.ClassRole(request.ClassRole)},
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		classRosterMutationResponse{
			Outcome: func() string {
				if result.Changed {
					return "updated"
				}
				return "unchanged"
			}(),
			Enrollment: newClassEnrollmentResponse(result.Enrollment),
		},
	)
}

func (handlers classEnrollmentHandlers) rosterBulk(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class roster bulk mutation accepts POST requests.",
		)
		return
	}
	classID, ok := parseInvitationPathUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound, classEnrollmentAdminProblem)
		return
	}
	principal, ok := handlers.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var request classRosterBulkRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassEnrollmentRequestBytes,
	); err != nil {
		handlers.writeProblem(w, r, classroom.ErrInvalidEnrollmentInput, classEnrollmentAdminProblem)
		return
	}
	var role *policy.ClassRole
	if request.ClassRole != nil {
		value := policy.ClassRole(*request.ClassRole)
		role = &value
	}
	input := classroom.BulkRosterInput{
		Action:    classroom.RosterBulkAction(request.Action),
		ClassRole: role,
		UserIDs:   append([]uuid.UUID(nil), request.UserIDs...),
	}
	result, err := handlers.enrollments.BulkMutateRoster(
		r.Context(), classAccess(principal), classID, input,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}

	items := make([]classRosterBulkItemResponse, 0, len(result.Items))
	for _, item := range result.Items {
		response := classRosterBulkItemResponse{
			UserID:  item.UserID,
			Outcome: "updated",
		}
		if item.Enrollment != nil {
			enrollment := newClassEnrollmentResponse(*item.Enrollment)
			response.Enrollment = &enrollment
			if !item.Changed {
				response.Outcome = "unchanged"
			}
		}
		if item.Failure != nil {
			response.Outcome = "failed"
			response.Failure = newClassRosterBulkFailureResponse(item.Failure.Code)
		}
		items = append(items, response)
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		classRosterBulkResponse{
			Action:         string(result.Action),
			Items:          items,
			RequestedCount: len(result.Items),
			UpdatedCount:   result.SucceededCount - result.UnchangedCount,
			UnchangedCount: result.UnchangedCount,
			FailedCount:    result.FailedCount,
		},
	)
}

func parseClassRosterResource(r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	classID, classOK := parseInvitationPathUUID(r.PathValue("class_id"))
	userID, userOK := parseInvitationPathUUID(r.PathValue("user_id"))
	return classID, userID, classOK && userOK
}

func newClassRosterMemberResponse(member classroom.RosterMember) classRosterMemberResponse {
	roles := make([]string, 0, len(member.Actions.AssignableRoles))
	for _, role := range member.Actions.AssignableRoles {
		roles = append(roles, string(role))
	}
	return classRosterMemberResponse{
		User: classRosterUserResponse{
			ID:          member.User.ID,
			DisplayName: member.User.DisplayName,
			Email:       member.User.Email,
		},
		Enrollment: newClassEnrollmentResponse(member.Enrollment),
		Actions: classRosterMemberActionsResponse{
			AssignableRoles: roles,
			CanSuspend:      member.Actions.CanSuspend,
			CanRemove:       member.Actions.CanRemove,
		},
	}
}

func newClassRosterOwnerResponse(owner classroom.RosterOwner) classRosterOwnerResponse {
	return classRosterOwnerResponse{
		User: classRosterUserResponse{
			ID:          owner.User.ID,
			DisplayName: owner.User.DisplayName,
			Email:       owner.User.Email,
		},
		ClassRole: string(policy.ClassRoleOwner),
	}
}

func newClassRosterBulkFailureResponse(
	code classroom.RosterBulkFailureCode,
) *classRosterBulkFailureResponse {
	detail := "The roster operation could not be completed."
	switch code {
	case classroom.RosterBulkFailureInvalid:
		detail = "The roster operation is invalid."
	case classroom.RosterBulkFailureAccessDenied:
		detail = "The current actor cannot apply this roster operation."
	case classroom.RosterBulkFailureNotFound:
		detail = "The target is not available in the active class scope."
	case classroom.RosterBulkFailureConflict:
		detail = "The target role or enrollment state conflicts with this operation."
	}
	return &classRosterBulkFailureResponse{Code: string(code), Detail: detail}
}
