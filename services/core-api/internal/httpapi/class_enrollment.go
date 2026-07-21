package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	maximumClassEnrollmentRequestBytes = 16 * 1024

	classEnrollmentsPattern       = "/api/v1/classes/{class_id}/enrollments"
	classEnrollmentSuspendPattern = "/api/v1/classes/{class_id}/enrollments/{user_id}/suspend"
	classEnrollmentRemovePattern  = "/api/v1/classes/{class_id}/enrollments/{user_id}/remove"
	classInviteCodesPattern       = "/api/v1/classes/{class_id}/invite-codes"
	classInviteCodeRevokePattern  = "/api/v1/classes/{class_id}/invite-codes/{code_id}/revoke"
	classInvitationJoinPath       = "/api/v1/class-invitations/join"
	classLeavePattern             = "/api/v1/classes/{class_id}/leave"
)

type classEnrollmentHandlers struct {
	auth        authHandlers
	logger      *slog.Logger
	enrollments classroom.EnrollmentServiceAPI
	webOrigin   string
	rateLimiter InvitationRateLimiter
	clock       func() time.Time
	audit       audit.ServiceAPI
}

type directClassEnrollmentRequest struct {
	MemberEmail string `json:"member_email"`
}

type createClassInviteCodeRequest struct {
	ExpiresInSeconds int `json:"expires_in_seconds"`
	UsageLimit       int `json:"usage_limit"`
}

type classInvitationTokenRequest struct {
	Token string `json:"token"`
}

type classEnrollmentResponse struct {
	ID          uuid.UUID                  `json:"id"`
	ClassID     uuid.UUID                  `json:"class_id"`
	UserID      uuid.UUID                  `json:"user_id"`
	ClassRole   string                     `json:"class_role"`
	Status      classroom.EnrollmentStatus `json:"status"`
	EnrolledBy  uuid.UUID                  `json:"enrolled_by"`
	JoinedAt    *time.Time                 `json:"joined_at"`
	SuspendedAt *time.Time                 `json:"suspended_at"`
	LeftAt      *time.Time                 `json:"left_at"`
	RemovedAt   *time.Time                 `json:"removed_at"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

type classInviteCodeResponse struct {
	ID         uuid.UUID                       `json:"id"`
	ClassID    uuid.UUID                       `json:"class_id"`
	Status     classroom.ClassInviteCodeStatus `json:"status"`
	ExpiresAt  time.Time                       `json:"expires_at"`
	UsageLimit int                             `json:"usage_limit"`
	UsageCount int                             `json:"usage_count"`
	CreatedBy  uuid.UUID                       `json:"created_by"`
	CreatedAt  time.Time                       `json:"created_at"`
	UpdatedAt  time.Time                       `json:"updated_at"`
	RevokedAt  *time.Time                      `json:"revoked_at"`
}

type classInviteCodeListResponse struct {
	Items []classInviteCodeResponse `json:"items"`
}

type createClassInviteCodeResponse struct {
	InviteCode classInviteCodeResponse `json:"invite_code"`
	JoinURL    string                  `json:"join_url"`
}

type joinClassInvitationResponse struct {
	Classroom  classResponse            `json:"classroom"`
	Enrollment *classEnrollmentResponse `json:"enrollment"`
	Joined     bool                     `json:"joined"`
}

type classEnrollmentProblemScope int

const (
	classEnrollmentAdminProblem classEnrollmentProblemScope = iota
	classEnrollmentTokenProblem
)

func newClassEnrollmentHandlers(
	cfg config.Config,
	logger *slog.Logger,
	auth authHandlers,
	service classroom.EnrollmentServiceAPI,
	rateLimiter InvitationRateLimiter,
	clock func() time.Time,
	auditService audit.ServiceAPI,
) classEnrollmentHandlers {
	return classEnrollmentHandlers{
		auth:        auth,
		logger:      logger,
		enrollments: service,
		webOrigin:   strings.TrimRight(cfg.WebOrigin, "/"),
		rateLimiter: rateLimiter,
		clock:       clock,
		audit:       auditService,
	}
}

func classEnrollmentResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Add("Vary", "Cookie")
		next.ServeHTTP(w, r)
	})
}

func (handlers classEnrollmentHandlers) directEnrollment(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		handlers.methodNotAllowed(w, r, "Direct enrollment accepts POST requests.")
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
	var request directClassEnrollmentRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassEnrollmentRequestBytes,
	); err != nil {
		handlers.writeProblem(
			w,
			r,
			classroom.ErrInvalidEnrollmentInput,
			classEnrollmentAdminProblem,
		)
		return
	}
	result, err := handlers.enrollments.DirectEnroll(
		r.Context(),
		classAccess(principal),
		classID,
		classroom.DirectEnrollmentInput{MemberEmail: request.MemberEmail},
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusCreated,
		newClassEnrollmentResponse(result.Enrollment),
	)
}

func (handlers classEnrollmentHandlers) enrollmentStateMutation(
	action string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			handlers.methodNotAllowed(w, r, "Enrollment state changes accept POST requests.")
			return
		}
		classID, classOK := parseInvitationPathUUID(r.PathValue("class_id"))
		userID, userOK := parseInvitationPathUUID(r.PathValue("user_id"))
		if !classOK || !userOK {
			handlers.writeProblem(
				w,
				r,
				classroom.ErrEnrollmentNotFound,
				classEnrollmentAdminProblem,
			)
			return
		}
		principal, ok := handlers.mutationPrincipal(w, r)
		if !ok {
			return
		}

		var result classroom.EnrollmentMutationResult
		var err error
		switch action {
		case "suspend":
			result, err = handlers.enrollments.SuspendEnrollment(
				r.Context(),
				classAccess(principal),
				classID,
				userID,
			)
		case "remove":
			result, err = handlers.enrollments.RemoveEnrollment(
				r.Context(),
				classAccess(principal),
				classID,
				userID,
			)
		default:
			err = classroom.ErrEnrollmentNotFound
		}
		if err != nil {
			handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
			return
		}
		writeJSON(
			handlers.logger,
			w,
			http.StatusOK,
			newClassEnrollmentResponse(result.Enrollment),
		)
	}
}

func (handlers classEnrollmentHandlers) inviteCodeCollection(
	w http.ResponseWriter,
	r *http.Request,
) {
	classID, ok := parseInvitationPathUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound, classEnrollmentAdminProblem)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.listInviteCodes(w, r, classID)
	case http.MethodPost:
		handlers.createInviteCode(w, r, classID)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class invite codes support GET and POST requests.",
		)
	}
}

func (handlers classEnrollmentHandlers) listInviteCodes(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	codes, err := handlers.enrollments.ListInviteCodes(
		r.Context(),
		classAccess(principal),
		classID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	items := make([]classInviteCodeResponse, 0, len(codes))
	for _, code := range codes {
		items = append(items, newClassInviteCodeResponse(code))
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		classInviteCodeListResponse{Items: items},
	)
}

func (handlers classEnrollmentHandlers) createInviteCode(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
) {
	principal, ok := handlers.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var request createClassInviteCodeRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassEnrollmentRequestBytes,
	); err != nil {
		handlers.writeProblem(
			w,
			r,
			classroom.ErrInvalidEnrollmentInput,
			classEnrollmentAdminProblem,
		)
		return
	}
	result, err := handlers.enrollments.CreateInviteCode(
		r.Context(),
		classAccess(principal),
		classID,
		classroom.CreateClassInviteCodeInput{
			ExpiresInSeconds: request.ExpiresInSeconds,
			UsageLimit:       request.UsageLimit,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	writeJSON(handlers.logger, w, http.StatusCreated, createClassInviteCodeResponse{
		InviteCode: newClassInviteCodeResponse(result.InviteCode),
		JoinURL:    handlers.joinURL(result.Token),
	})
}

func (handlers classEnrollmentHandlers) revokeInviteCode(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		handlers.methodNotAllowed(w, r, "Class invite-code revoke accepts POST requests.")
		return
	}
	classID, classOK := parseInvitationPathUUID(r.PathValue("class_id"))
	codeID, codeOK := parseInvitationPathUUID(r.PathValue("code_id"))
	if !classOK || !codeOK {
		handlers.writeProblem(
			w,
			r,
			classroom.ErrClassInviteCodeUnavailable,
			classEnrollmentAdminProblem,
		)
		return
	}
	principal, ok := handlers.mutationPrincipal(w, r)
	if !ok {
		return
	}
	code, err := handlers.enrollments.RevokeInviteCode(
		r.Context(),
		classAccess(principal),
		classID,
		codeID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, newClassInviteCodeResponse(code))
}

func (handlers classEnrollmentHandlers) joinByInviteCode(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		handlers.methodNotAllowed(w, r, "Class invitation join accepts POST requests.")
		return
	}
	if !handlers.available(w, r) ||
		!handlers.allowClassJoin(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	var request classInvitationTokenRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassEnrollmentRequestBytes,
	); err != nil {
		handlers.writeProblem(
			w,
			r,
			classroom.ErrClassInviteCodeUnavailable,
			classEnrollmentTokenProblem,
		)
		return
	}
	result, err := handlers.enrollments.JoinByInviteCode(
		r.Context(),
		classAccess(principal),
		request.Token,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentTokenProblem)
		return
	}
	var enrollment *classEnrollmentResponse
	if result.Enrollment != nil {
		response := newClassEnrollmentResponse(*result.Enrollment)
		enrollment = &response
	}
	writeJSON(handlers.logger, w, http.StatusOK, joinClassInvitationResponse{
		Classroom:  newClassResponse(result.Class),
		Enrollment: enrollment,
		Joined:     result.Joined,
	})
}

func (handlers classEnrollmentHandlers) leaveClass(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		handlers.methodNotAllowed(w, r, "Leaving a class accepts POST requests.")
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
	result, err := handlers.enrollments.LeaveClass(
		r.Context(),
		classAccess(principal),
		classID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, classEnrollmentAdminProblem)
		return
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		newClassEnrollmentResponse(result.Enrollment),
	)
}

func (handlers classEnrollmentHandlers) mutationPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	if !handlers.available(w, r) {
		return identity.Principal{}, false
	}
	return handlers.csrfPrincipal(w, r)
}

func (handlers classEnrollmentHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers classEnrollmentHandlers) available(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.enrollments != nil {
		return true
	}
	writeProblem(
		w,
		r,
		http.StatusServiceUnavailable,
		"Class enrollment unavailable",
		"Class enrollment storage is not configured for this environment.",
	)
	return false
}

func (handlers classEnrollmentHandlers) allowClassJoin(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	clientPrefix := identity.IPPrefix(r.RemoteAddr)
	if clientPrefix == "" {
		clientPrefix = "unknown"
	}
	decision := handlers.rateLimiter.Allow(
		r.Context(),
		InvitationRateLimitClassJoin,
		clientPrefix,
		handlers.clock().UTC(),
	)
	if decision.Allowed {
		return true
	}
	if decision.Err != nil {
		handlers.logger.Error(
			"class invitation rate limiter unavailable",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error_class", "rate_limit_unavailable",
		)
		writeCodedProblem(
			w,
			r,
			http.StatusServiceUnavailable,
			"rate_limit_unavailable",
			"Class invitation rate limit unavailable",
			"The class invitation safety check is temporarily unavailable. Try again later.",
		)
		return false
	}
	w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
	writeCodedProblem(
		w,
		r,
		http.StatusTooManyRequests,
		"rate_limit_exceeded",
		"Too many class invitation requests",
		"Wait before trying to join a class again.",
	)
	return false
}

func (handlers classEnrollmentHandlers) methodNotAllowed(
	w http.ResponseWriter,
	r *http.Request,
	detail string,
) {
	w.Header().Set("Allow", http.MethodPost)
	writeProblem(
		w,
		r,
		http.StatusMethodNotAllowed,
		"Method not allowed",
		detail,
	)
}

func (handlers classEnrollmentHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	scope classEnrollmentProblemScope,
) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status := http.StatusInternalServerError
	title := "Class enrollment request failed"
	detail := "The class enrollment request could not be completed."

	if scope == classEnrollmentTokenProblem && isClassInvitationDomainError(err) {
		writeProblem(
			w,
			r,
			http.StatusNotFound,
			"Class invitation unavailable",
			"The class invitation is invalid, unavailable, or no longer active.",
		)
		return
	}

	switch {
	case errors.Is(err, classroom.ErrInvalidEnrollmentInput),
		errors.Is(err, classroom.ErrInvalidRosterCursor):
		status = http.StatusBadRequest
		title = "Invalid class enrollment request"
		detail = "Check the member email, invite-code lifetime, and usage limit."
	case errors.Is(err, classroom.ErrEnrollmentAccessDenied),
		errors.Is(err, classroom.ErrClassAccessDenied):
		status = http.StatusForbidden
		title = "Class enrollment access denied"
		detail = "Your active workspace and class access do not allow this action."
	case errors.Is(err, classroom.ErrEnrollmentConflict),
		errors.Is(err, classroom.ErrClassInviteCodeConflict),
		errors.Is(err, classroom.ErrInvalidClassTransition):
		status = http.StatusConflict
		title = "Class enrollment state conflict"
		detail = "The class, enrollment, or invite code is not in a compatible state."
	case errors.Is(err, classroom.ErrClassInviteCodeUnavailable):
		status = http.StatusNotFound
		title = "Class invitation unavailable"
		detail = "The class invitation is invalid, unavailable, or no longer active."
	case errors.Is(err, classroom.ErrEnrollmentNotFound):
		status = http.StatusNotFound
		title = "Class enrollment not found"
		detail = "The enrollment does not exist in the active class scope."
	case errors.Is(err, classroom.ErrClassNotFound):
		status = http.StatusNotFound
		if scope == classEnrollmentTokenProblem {
			title = "Class invitation unavailable"
			detail = "The class invitation is invalid, unavailable, or no longer active."
		} else {
			title = "Class not found"
			detail = "The class does not exist in the active workspace."
		}
	}

	if status >= http.StatusInternalServerError {
		handlers.logger.Error(
			"class enrollment request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error_class", classEnrollmentErrorClass(err),
		)
	}
	writeProblem(w, r, status, title, detail)
}

func isClassInvitationDomainError(err error) bool {
	return errors.Is(err, classroom.ErrInvalidEnrollmentInput) ||
		errors.Is(err, classroom.ErrEnrollmentAccessDenied) ||
		errors.Is(err, classroom.ErrClassAccessDenied) ||
		errors.Is(err, classroom.ErrEnrollmentConflict) ||
		errors.Is(err, classroom.ErrClassInviteCodeConflict) ||
		errors.Is(err, classroom.ErrInvalidClassTransition) ||
		errors.Is(err, classroom.ErrClassInviteCodeUnavailable) ||
		errors.Is(err, classroom.ErrEnrollmentNotFound) ||
		errors.Is(err, classroom.ErrClassNotFound)
}

func (handlers classEnrollmentHandlers) joinURL(token string) string {
	return handlers.webOrigin + "/class-invite#token=" + url.QueryEscape(token)
}

func newClassEnrollmentResponse(
	enrollment classroom.Enrollment,
) classEnrollmentResponse {
	return classEnrollmentResponse{
		ID:          enrollment.ID,
		ClassID:     enrollment.ClassID,
		UserID:      enrollment.UserID,
		ClassRole:   string(enrollment.ClassRole),
		Status:      enrollment.Status,
		EnrolledBy:  enrollment.EnrolledBy,
		JoinedAt:    enrollment.JoinedAt,
		SuspendedAt: enrollment.SuspendedAt,
		LeftAt:      enrollment.LeftAt,
		RemovedAt:   enrollment.RemovedAt,
		CreatedAt:   enrollment.CreatedAt,
		UpdatedAt:   enrollment.UpdatedAt,
	}
}

func newClassInviteCodeResponse(
	code classroom.ClassInviteCode,
) classInviteCodeResponse {
	return classInviteCodeResponse{
		ID:         code.ID,
		ClassID:    code.ClassID,
		Status:     code.Status,
		ExpiresAt:  code.ExpiresAt,
		UsageLimit: code.UsageLimit,
		UsageCount: code.UsageCount,
		CreatedBy:  code.CreatedBy,
		CreatedAt:  code.CreatedAt,
		UpdatedAt:  code.UpdatedAt,
		RevokedAt:  code.RevokedAt,
	}
}

func classEnrollmentErrorClass(err error) string {
	switch {
	case errors.Is(err, classroom.ErrInvalidEnrollmentInput):
		return "enrollment_invalid"
	case errors.Is(err, classroom.ErrEnrollmentAccessDenied),
		errors.Is(err, classroom.ErrClassAccessDenied):
		return "enrollment_access_denied"
	case errors.Is(err, classroom.ErrEnrollmentConflict):
		return "enrollment_conflict"
	case errors.Is(err, classroom.ErrClassInviteCodeConflict):
		return "class_invite_code_conflict"
	case errors.Is(err, classroom.ErrClassInviteCodeUnavailable):
		return "class_invite_code_unavailable"
	case errors.Is(err, classroom.ErrEnrollmentNotFound):
		return "enrollment_not_found"
	case errors.Is(err, classroom.ErrClassNotFound):
		return "class_not_found"
	default:
		return "internal"
	}
}
