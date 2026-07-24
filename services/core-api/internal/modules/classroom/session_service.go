package classroom

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type CreateSessionInput struct {
	Title       string
	Description string
	StartsAt    string
	EndsAt      string
	Timezone    string
}

type UpdateSessionInput struct {
	Title           *string
	Description     *string
	StartsAt        *string
	EndsAt          *string
	Timezone        *string
	ExpectedVersion int64
}

type ListSessionsInput struct {
	From   string
	To     string
	Limit  int
	Cursor string
}

type SessionPage struct {
	Items      []ClassSession
	NextCursor string
}

type SessionServiceAPI interface {
	CreateSession(
		context.Context,
		AccessContext,
		uuid.UUID,
		CreateSessionInput,
	) (ClassSession, error)
	GetSession(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
	) (ClassSession, error)
	ListSessions(
		context.Context,
		AccessContext,
		uuid.UUID,
		ListSessionsInput,
	) (SessionPage, error)
	UpdateSession(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		UpdateSessionInput,
	) (ClassSession, error)
	CancelSession(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		int64,
	) (ClassSession, error)
}

type SessionServiceConfig struct {
	Clock func() time.Time
}

type SessionService struct {
	repository      SessionRepository
	classAuthorizer ClassActionAuthorizer
	clock           func() time.Time
}

func NewSessionService(
	repository SessionRepository,
	classAuthorizer ClassActionAuthorizer,
	configurations ...SessionServiceConfig,
) (*SessionService, error) {
	if repository == nil || classAuthorizer == nil {
		return nil, fmt.Errorf("session repository and class authorizer are required")
	}
	if len(configurations) > 1 {
		return nil, fmt.Errorf("only one session service configuration is supported")
	}
	config := SessionServiceConfig{}
	if len(configurations) == 1 {
		config = configurations[0]
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &SessionService{
		repository: repository, classAuthorizer: classAuthorizer, clock: config.Clock,
	}, nil
}

func (service *SessionService) CreateSession(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input CreateSessionInput,
) (ClassSession, error) {
	class, tenantContext, err := service.authorize(
		ctx, access, classID, policy.ActionSessionSchedule,
	)
	if err != nil {
		return ClassSession{}, err
	}
	startsAt, err := parseSessionTimestamp(input.StartsAt, input.Timezone)
	if err != nil {
		return ClassSession{}, err
	}
	endsAt, err := parseSessionTimestamp(input.EndsAt, input.Timezone)
	if err != nil {
		return ClassSession{}, err
	}
	params, err := (CreateSessionParams{
		Title: input.Title, Description: input.Description,
		StartsAt: startsAt, EndsAt: endsAt, Timezone: input.Timezone,
		CreatedBy: access.ActorID,
	}).normalized()
	if err != nil {
		return ClassSession{}, err
	}
	session, err := service.repository.CreateSession(
		ctx, tenantContext, classID, params, service.clock().UTC(),
	)
	if err != nil {
		return ClassSession{}, err
	}
	return projectSessionViewerAccess(session, class), nil
}

func (service *SessionService) GetSession(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (ClassSession, error) {
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionClassView)
	if err != nil {
		return ClassSession{}, err
	}
	if sessionID == uuid.Nil {
		return ClassSession{}, ErrSessionNotFound
	}
	session, err := service.repository.GetSession(ctx, tenantContext, classID, sessionID)
	if err != nil {
		return ClassSession{}, err
	}
	return projectSessionViewerAccess(session, class), nil
}

func (service *SessionService) ListSessions(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input ListSessionsInput,
) (SessionPage, error) {
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionClassView)
	if err != nil {
		return SessionPage{}, err
	}
	params, err := normalizeListSessionsInput(input, tenantContext.TenantID, classID)
	if err != nil {
		return SessionPage{}, err
	}
	result, err := service.repository.ListSessions(ctx, tenantContext, classID, params)
	if err != nil {
		return SessionPage{}, err
	}
	page := SessionPage{Items: result.Items}
	for index := range page.Items {
		page.Items[index] = projectSessionViewerAccess(page.Items[index], class)
	}
	if result.HasMore && len(result.Items) > 0 {
		last := result.Items[len(result.Items)-1]
		page.NextCursor, err = encodeSessionCursor(
			SessionCursor{StartsAt: last.StartsAt, ID: last.ID},
			tenantContext.TenantID,
			classID,
			params.From,
			params.To,
		)
		if err != nil {
			return SessionPage{}, fmt.Errorf("encode next session cursor: %w", err)
		}
	}
	return page, nil
}

func (service *SessionService) UpdateSession(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
	input UpdateSessionInput,
) (ClassSession, error) {
	class, tenantContext, err := service.authorize(
		ctx, access, classID, policy.ActionSessionSchedule,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if sessionID == uuid.Nil {
		return ClassSession{}, ErrSessionNotFound
	}
	params := UpdateSessionParams{
		Title: input.Title, Description: input.Description,
		ExpectedVersion: input.ExpectedVersion,
	}
	hasTimeChange := input.StartsAt != nil || input.EndsAt != nil || input.Timezone != nil
	if hasTimeChange {
		if input.StartsAt == nil || input.EndsAt == nil || input.Timezone == nil {
			return ClassSession{}, fmt.Errorf(
				"%w: starts_at, ends_at, and timezone must change together",
				ErrInvalidSessionInput,
			)
		}
		startsAt, parseErr := parseSessionTimestamp(*input.StartsAt, *input.Timezone)
		if parseErr != nil {
			return ClassSession{}, parseErr
		}
		endsAt, parseErr := parseSessionTimestamp(*input.EndsAt, *input.Timezone)
		if parseErr != nil {
			return ClassSession{}, parseErr
		}
		params.StartsAt, params.EndsAt, params.Timezone = &startsAt, &endsAt, input.Timezone
	}
	params, err = params.normalized()
	if err != nil {
		return ClassSession{}, err
	}
	session, err := service.repository.UpdateSession(
		ctx, tenantContext, classID, sessionID, params, service.clock().UTC(),
	)
	if err != nil {
		return ClassSession{}, err
	}
	return projectSessionViewerAccess(session, class), nil
}

func (service *SessionService) CancelSession(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
	expectedVersion int64,
) (ClassSession, error) {
	class, tenantContext, err := service.authorize(
		ctx, access, classID, policy.ActionSessionSchedule,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if sessionID == uuid.Nil {
		return ClassSession{}, ErrSessionNotFound
	}
	params, err := (CancelSessionParams{ExpectedVersion: expectedVersion}).normalized()
	if err != nil {
		return ClassSession{}, err
	}
	session, err := service.repository.CancelSession(
		ctx, tenantContext, classID, sessionID, params, service.clock().UTC(),
	)
	if err != nil {
		return ClassSession{}, err
	}
	return projectSessionViewerAccess(session, class), nil
}

func (service *SessionService) authorize(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (Class, tenancy.Context, error) {
	if classID == uuid.Nil {
		return Class{}, tenancy.Context{}, ErrClassNotFound
	}
	class, err := service.classAuthorizer.AuthorizeClass(ctx, access, classID, action)
	if err != nil {
		return Class{}, tenancy.Context{}, err
	}
	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return Class{}, tenancy.Context{}, ErrSessionAccessDenied
	}
	return class, tenantContext, nil
}

func projectSessionViewerAccess(session ClassSession, class Class) ClassSession {
	canMutate := class.ViewerAccess.CanScheduleSessions &&
		session.Status == SessionStatusScheduled
	session.ViewerAccess = SessionViewerAccess{
		CanUpdate: canMutate,
		CanCancel: canMutate,
	}
	return session
}

type sessionCursorPayload struct {
	StartsAt string `json:"starts_at"`
	ID       string `json:"id"`
	Scope    string `json:"scope"`
}

func normalizeListSessionsInput(
	input ListSessionsInput,
	tenantID uuid.UUID,
	classID uuid.UUID,
) (ListSessionsParams, error) {
	from, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.From))
	if err != nil {
		return ListSessionsParams{}, ErrInvalidSessionRange
	}
	to, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.To))
	if err != nil {
		return ListSessionsParams{}, ErrInvalidSessionRange
	}
	from, to = from.UTC(), to.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) ||
		to.Sub(from) > maximumSessionQueryRange {
		return ListSessionsParams{}, ErrInvalidSessionRange
	}
	if input.Limit == 0 {
		input.Limit = defaultSessionListLimit
	}
	if input.Limit < 1 || input.Limit > maximumSessionListLimit {
		return ListSessionsParams{}, ErrInvalidSessionListLimit
	}
	after, err := decodeSessionCursor(
		strings.TrimSpace(input.Cursor), tenantID, classID, from, to,
	)
	if err != nil {
		return ListSessionsParams{}, err
	}
	return ListSessionsParams{From: from, To: to, Limit: input.Limit, After: after}, nil
}

func encodeSessionCursor(
	cursor SessionCursor,
	tenantID uuid.UUID,
	classID uuid.UUID,
	from time.Time,
	to time.Time,
) (string, error) {
	contents, err := json.Marshal(sessionCursorPayload{
		StartsAt: cursor.StartsAt.UTC().Format(time.RFC3339Nano),
		ID:       cursor.ID.String(),
		Scope:    sessionListScopeHash(tenantID, classID, from, to),
	})
	if err != nil {
		return "", err
	}
	return classSessionCursorPrefix +
		base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeSessionCursor(
	value string,
	tenantID uuid.UUID,
	classID uuid.UUID,
	from time.Time,
	to time.Time,
) (*SessionCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maximumSessionCursorLength ||
		!strings.HasPrefix(value, classSessionCursorPrefix) {
		return nil, ErrInvalidSessionCursor
	}
	contents, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(value, classSessionCursorPrefix),
	)
	if err != nil {
		return nil, ErrInvalidSessionCursor
	}
	var payload sessionCursorPayload
	if err := decodeStrictCursorJSON(contents, &payload); err != nil ||
		payload.Scope != sessionListScopeHash(tenantID, classID, from, to) {
		return nil, ErrInvalidSessionCursor
	}
	startsAt, err := time.Parse(time.RFC3339Nano, payload.StartsAt)
	if err != nil || startsAt.IsZero() {
		return nil, ErrInvalidSessionCursor
	}
	sessionID, err := uuid.Parse(payload.ID)
	if err != nil || sessionID == uuid.Nil {
		return nil, ErrInvalidSessionCursor
	}
	return &SessionCursor{StartsAt: startsAt.UTC(), ID: sessionID}, nil
}

func sessionListScopeHash(
	tenantID uuid.UUID,
	classID uuid.UUID,
	from time.Time,
	to time.Time,
) string {
	digest := sha256.Sum256([]byte(
		tenantID.String() + "\x00" + classID.String() + "\x00" +
			from.UTC().Format(time.RFC3339Nano) + "\x00" +
			to.UTC().Format(time.RFC3339Nano),
	))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
