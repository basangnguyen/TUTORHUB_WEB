package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	mediaJoinAttemptResourcePattern = "/api/v1/media/spaces/{space_id}/join-attempts/{join_attempt_id}"
	mediaJoinAttemptCancelPattern   = "/api/v1/media/spaces/{space_id}/join-attempts/{join_attempt_id}/cancel"
	mediaAdmissionsCollectionPath   = "/api/v1/media/spaces/{space_id}/admissions"
	mediaAdmissionAdmitPattern      = "/api/v1/media/spaces/{space_id}/admissions/{admission_id}/admit"
	mediaAdmissionDenyPattern       = "/api/v1/media/spaces/{space_id}/admissions/{admission_id}/deny"
	mediaAdmissionRestorePattern    = "/api/v1/media/spaces/{space_id}/admissions/{admission_id}/restore"
	mediaSpaceMembersPattern        = "/api/v1/media/spaces/{space_id}/members"
	mediaSpaceMemberRevokePattern   = "/api/v1/media/spaces/{space_id}/members/{user_id}/revoke"
	mediaSpaceMemberRestorePattern  = "/api/v1/media/spaces/{space_id}/members/{user_id}/restore"
	maximumMediaLobbyRequestBytes   = 8 * 1024
)

type mediaLobbyHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service media.LobbyServiceAPI
}

type mediaAdmissionMutationRequest struct {
	ExpectedSpaceVersion        *int64     `json:"expected_space_version"`
	ExpectedRoomInstanceID      *uuid.UUID `json:"expected_room_instance_id"`
	ExpectedRoomInstanceVersion *int64     `json:"expected_room_instance_version"`
	ExpectedAdmissionVersion    *int64     `json:"expected_admission_version"`
	IdempotencyKey              *string    `json:"idempotency_key"`
	ReasonCode                  *string    `json:"reason_code"`
}

type mediaSpaceMemberInviteRequest struct {
	TargetMemberEmail    *string `json:"target_member_email"`
	ExpectedSpaceVersion *int64  `json:"expected_space_version"`
	IdempotencyKey       *string `json:"idempotency_key"`
}

type mediaSpaceMemberMutationRequest struct {
	ExpectedSpaceVersion  *int64  `json:"expected_space_version"`
	ExpectedMemberVersion *int64  `json:"expected_member_version"`
	IdempotencyKey        *string `json:"idempotency_key"`
	ReasonCode            *string `json:"reason_code"`
}

func newMediaLobbyHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service media.LobbyServiceAPI,
) mediaLobbyHandlers {
	return mediaLobbyHandlers{logger: logger, auth: auth, service: service}
}

func (handlers mediaLobbyHandlers) joinAttempt(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, false)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	joinAttemptID, attemptOK := parseResourceUUID(r.PathValue("join_attempt_id"))
	input, inputOK := parseMediaAdmissionListInput(r)
	if !spaceOK || !attemptOK || !inputOK {
		handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
		return
	}
	item, err := handlers.service.GetJoinAttempt(
		r.Context(), mediaAccess(principal), spaceID, joinAttemptID, input,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers mediaLobbyHandlers) cancelJoinAttempt(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, true)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	joinAttemptID, attemptOK := parseResourceUUID(r.PathValue("join_attempt_id"))
	input, inputOK := decodeMediaAdmissionMutation(w, r)
	if !spaceOK || !attemptOK || !inputOK {
		handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
		return
	}
	item, err := handlers.service.CancelJoinAttempt(
		r.Context(), mediaAccess(principal), spaceID, joinAttemptID, input,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers mediaLobbyHandlers) admissions(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, false)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	input, inputOK := parseMediaAdmissionListInput(r)
	if !spaceOK || !inputOK {
		handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
		return
	}
	page, err := handlers.service.ListAdmissions(
		r.Context(), mediaAccess(principal), spaceID, input,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, page)
}

func (handlers mediaLobbyHandlers) mutateAdmission(
	operation string,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := handlers.principal(w, r, true)
		if !ok {
			return
		}
		spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
		admissionID, admissionOK := parseResourceUUID(r.PathValue("admission_id"))
		input, inputOK := decodeMediaAdmissionMutation(w, r)
		if !spaceOK || !admissionOK || !inputOK {
			handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
			return
		}
		var item media.LobbyAdmission
		var err error
		switch operation {
		case "admit":
			item, err = handlers.service.Admit(
				r.Context(), mediaAccess(principal), spaceID, admissionID, input,
			)
		case "deny":
			item, err = handlers.service.Deny(
				r.Context(), mediaAccess(principal), spaceID, admissionID, input,
			)
		case "restore":
			item, err = handlers.service.RestoreAdmission(
				r.Context(), mediaAccess(principal), spaceID, admissionID, input,
			)
		default:
			err = media.ErrInvalidLobbyRequest
		}
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, item)
	})
}

func (handlers mediaLobbyHandlers) members(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeProblem(
			w, r, http.StatusMethodNotAllowed, "Method not allowed",
			"Media-space members support GET and POST requests.",
		)
		return
	}
	mutating := r.Method == http.MethodPost
	principal, ok := handlers.principal(w, r, mutating)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	if !spaceOK {
		handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
		return
	}
	if r.Method == http.MethodGet {
		expectedVersion, versionOK := positiveInt64Query(r, "expected_space_version")
		limit, limitOK := optionalPositiveIntQuery(r, "limit")
		if !versionOK || !limitOK {
			handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
			return
		}
		page, err := handlers.service.ListMembers(
			r.Context(), mediaAccess(principal), spaceID,
			media.ListLobbyMembersInput{ExpectedSpaceVersion: expectedVersion, Limit: limit},
		)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, page)
		return
	}
	var request mediaSpaceMemberInviteRequest
	if err := decodeJSONRequest(w, r, &request, maximumMediaLobbyRequestBytes); err != nil ||
		request.TargetMemberEmail == nil || request.ExpectedSpaceVersion == nil ||
		request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
		return
	}
	item, err := handlers.service.InviteMember(
		r.Context(), mediaAccess(principal), spaceID,
		media.InviteLobbyMemberInput{
			Email: *request.TargetMemberEmail, ExpectedSpaceVersion: *request.ExpectedSpaceVersion,
			IdempotencyKey: *request.IdempotencyKey,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers mediaLobbyHandlers) mutateMember(operation string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := handlers.principal(w, r, true)
		if !ok {
			return
		}
		spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
		userID, userOK := parseResourceUUID(r.PathValue("user_id"))
		var request mediaSpaceMemberMutationRequest
		if !spaceOK || !userOK || decodeJSONRequest(
			w, r, &request, maximumMediaLobbyRequestBytes,
		) != nil || request.ExpectedSpaceVersion == nil ||
			request.ExpectedMemberVersion == nil || request.IdempotencyKey == nil {
			handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
			return
		}
		input := media.MemberMutationInput{
			ExpectedSpaceVersion:  *request.ExpectedSpaceVersion,
			ExpectedMemberVersion: *request.ExpectedMemberVersion,
			IdempotencyKey:        *request.IdempotencyKey,
		}
		if request.ReasonCode != nil {
			input.ReasonCode = *request.ReasonCode
		}
		var item media.LobbyMember
		var err error
		if operation == "revoke" {
			item, err = handlers.service.RevokeMember(
				r.Context(), mediaAccess(principal), spaceID, userID, input,
			)
		} else {
			item, err = handlers.service.RestoreMember(
				r.Context(), mediaAccess(principal), spaceID, userID, input,
			)
		}
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, item)
	})
}

func (handlers mediaLobbyHandlers) principal(
	w http.ResponseWriter,
	r *http.Request,
	mutating bool,
) (identity.Principal, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, media.ErrLobbyUnavailable)
		return identity.Principal{}, false
	}
	var principal identity.Principal
	var ok bool
	if mutating {
		sessionToken, tokenOK := handlers.auth.sessionToken(w, r)
		if !tokenOK {
			return identity.Principal{}, false
		}
		principal, ok = handlers.auth.csrfPrincipal(w, r, sessionToken)
	} else {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
	}
	if !ok {
		return identity.Principal{}, false
	}
	if principal.ActiveTenant == nil || !principal.ActiveTenant.IsActive {
		handlers.writeProblem(w, r, media.ErrSpaceAccessDenied)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(mediaSpaceTenantHeader)),
	)
	if !valid {
		handlers.writeProblem(w, r, media.ErrInvalidLobbyRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errMediaSpaceScopeChanged)
		return identity.Principal{}, false
	}
	return principal, true
}

func parseMediaAdmissionListInput(r *http.Request) (media.ListLobbyAdmissionsInput, bool) {
	spaceVersion, spaceOK := positiveInt64Query(r, "expected_space_version")
	roomVersion, roomVersionOK := positiveInt64Query(r, "expected_room_instance_version")
	roomID, roomOK := parseResourceUUID(strings.TrimSpace(r.URL.Query().Get("room_instance_id")))
	limit, limitOK := optionalPositiveIntQuery(r, "limit")
	return media.ListLobbyAdmissionsInput{
		ExpectedSpaceVersion: spaceVersion, ExpectedRoomInstanceID: roomID,
		ExpectedRoomInstanceVersion: roomVersion, Limit: limit,
	}, spaceOK && roomVersionOK && roomOK && limitOK
}

func decodeMediaAdmissionMutation(
	w http.ResponseWriter,
	r *http.Request,
) (media.AdmissionMutationInput, bool) {
	var request mediaAdmissionMutationRequest
	if err := decodeJSONRequest(w, r, &request, maximumMediaLobbyRequestBytes); err != nil ||
		request.ExpectedSpaceVersion == nil || request.ExpectedRoomInstanceID == nil ||
		request.ExpectedRoomInstanceVersion == nil || request.ExpectedAdmissionVersion == nil ||
		request.IdempotencyKey == nil {
		return media.AdmissionMutationInput{}, false
	}
	input := media.AdmissionMutationInput{
		ExpectedSpaceVersion:        *request.ExpectedSpaceVersion,
		ExpectedRoomInstanceID:      *request.ExpectedRoomInstanceID,
		ExpectedRoomInstanceVersion: *request.ExpectedRoomInstanceVersion,
		ExpectedAdmissionVersion:    *request.ExpectedAdmissionVersion,
		IdempotencyKey:              *request.IdempotencyKey,
	}
	if request.ReasonCode != nil {
		input.ReasonCode = *request.ReasonCode
	}
	return input, true
}

func positiveInt64Query(r *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	return value, err == nil && value > 0
}

func optionalPositiveIntQuery(r *http.Request, name string) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value > 0
}

func (handlers mediaLobbyHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "media_lobby_unavailable"
	title, detail := "Media lobby unavailable", "The media lobby request could not be completed safely."
	switch {
	case errors.Is(err, media.ErrInvalidLobbyRequest):
		status, code = http.StatusBadRequest, "media_lobby_invalid"
		title, detail = "Invalid media lobby request", "Review the current space, room, optimistic versions, and idempotency key."
	case errors.Is(err, media.ErrSpaceAccessDenied):
		status, code = http.StatusForbidden, "media_lobby_forbidden"
		title, detail = "Media lobby access denied", "The active workspace cannot authorize this lobby action."
	case errors.Is(err, media.ErrSpaceNotFound), errors.Is(err, media.ErrSourceUnavailable),
		errors.Is(err, media.ErrAdmissionNotFound), errors.Is(err, media.ErrLobbyMemberNotFound):
		status, code = http.StatusNotFound, "media_lobby_not_found"
		title, detail = "Media lobby unavailable", "The requested media lobby resource is unavailable in the active workspace."
	case errors.Is(err, media.ErrSpaceVersionConflict),
		errors.Is(err, media.ErrAdmissionVersionConflict),
		errors.Is(err, media.ErrLobbyMemberVersionConflict),
		errors.Is(err, media.ErrJoinAttemptStale):
		status, code = http.StatusConflict, "stale_version"
		title, detail = "Media lobby changed", "Reload the latest media-space and lobby projection before retrying."
	case errors.Is(err, media.ErrSpaceIdempotency):
		status, code = http.StatusConflict, "media_lobby_idempotency_conflict"
		title, detail = "Media lobby retry conflict", "Use a new idempotency key after changing the requested action."
	case errors.Is(err, media.ErrAdmissionTransition),
		errors.Is(err, media.ErrParticipantConflict), errors.Is(err, media.ErrRoomNotOpen),
		errors.Is(err, media.ErrRoomLocked):
		status, code = http.StatusConflict, "media_lobby_transition_conflict"
		title, detail = "Media lobby transition unavailable", "Reload the current room and lobby state before retrying."
	case errors.Is(err, errMediaSpaceScopeChanged):
		status, code = http.StatusConflict, "media_space_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	case errors.Is(err, media.ErrMediaProviderUnavailable):
		status, code = http.StatusServiceUnavailable, "media_provider_unavailable"
		title, detail = "Media provider unavailable", "Try the lobby action again later."
	case errors.Is(err, media.ErrLobbyUnavailable), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, featurecontrol.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "media_lobby_unavailable"
		title, detail = "Media lobby unavailable", "Try again later."
	default:
		handlers.logger.Error(
			"media lobby request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
