package classroom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type sessionServiceFakeRepository struct {
	created   ClassSession
	createErr error
}

func (fake *sessionServiceFakeRepository) CreateSession(
	_ context.Context, _ tenancy.Context, classID uuid.UUID,
	params CreateSessionParams, now time.Time,
) (ClassSession, error) {
	if fake.createErr != nil {
		return ClassSession{}, fake.createErr
	}
	fake.created = ClassSession{
		ID: uuid.New(), TenantID: uuid.New(), ClassID: classID,
		Title: params.Title, Description: params.Description,
		StartsAt: params.StartsAt, EndsAt: params.EndsAt, Timezone: params.Timezone,
		Status: SessionStatusScheduled, Version: 1, CreatedBy: params.CreatedBy,
		UpdatedBy: params.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	return fake.created, nil
}

func (*sessionServiceFakeRepository) GetSession(
	context.Context, tenancy.Context, uuid.UUID, uuid.UUID,
) (ClassSession, error) {
	return ClassSession{}, ErrSessionNotFound
}

func (*sessionServiceFakeRepository) ListSessions(
	context.Context, tenancy.Context, uuid.UUID, ListSessionsParams,
) (ListSessionsResult, error) {
	return ListSessionsResult{}, nil
}

func (*sessionServiceFakeRepository) UpdateSession(
	context.Context, tenancy.Context, uuid.UUID, uuid.UUID, UpdateSessionParams, time.Time,
) (ClassSession, error) {
	return ClassSession{}, ErrSessionNotFound
}

func (*sessionServiceFakeRepository) CancelSession(
	context.Context, tenancy.Context, uuid.UUID, uuid.UUID, CancelSessionParams, time.Time,
) (ClassSession, error) {
	return ClassSession{}, ErrSessionNotFound
}

type sessionServiceFakeClassAuthorizer struct {
	class Class
	err   error
}

func (fake sessionServiceFakeClassAuthorizer) AuthorizeClass(
	context.Context, AccessContext, uuid.UUID, policy.Action,
) (Class, error) {
	if fake.err != nil {
		return Class{}, fake.err
	}
	return fake.class, nil
}

func TestSessionServiceCreateProjectsSchedulingCapability(t *testing.T) {
	t.Parallel()
	tenantID, actorID, classID := uuid.New(), uuid.New(), uuid.New()
	service, err := NewSessionService(
		&sessionServiceFakeRepository{},
		sessionServiceFakeClassAuthorizer{class: Class{
			ID: classID, TenantID: tenantID, Status: ClassStatusActive,
			ViewerAccess: ViewerAccess{CanScheduleSessions: true},
		}},
		SessionServiceConfig{
			Clock: func() time.Time {
				return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
			},
		},
	)
	if err != nil {
		t.Fatalf("new session service: %v", err)
	}
	session, err := service.CreateSession(
		context.Background(),
		AccessContext{
			TenantID: tenantID, ActorID: actorID, MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleTeacher},
		},
		classID,
		CreateSessionInput{
			Title: "Math", StartsAt: "2026-07-23T10:00:00+07:00",
			EndsAt: "2026-07-23T11:00:00+07:00", Timezone: "Asia/Ho_Chi_Minh",
		},
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if !session.ViewerAccess.CanUpdate || !session.ViewerAccess.CanCancel {
		t.Fatalf("unexpected viewer access: %+v", session.ViewerAccess)
	}
}

func TestSessionServiceRejectsInvalidCursorScope(t *testing.T) {
	t.Parallel()
	from := "2026-07-23T00:00:00Z"
	to := "2026-07-24T00:00:00Z"
	tenantID, classID := uuid.New(), uuid.New()
	cursor, err := encodeSessionCursor(
		SessionCursor{
			StartsAt: time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC),
			ID:       uuid.New(),
		},
		tenantID, classID,
		time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if _, err := normalizeListSessionsInput(
		ListSessionsInput{From: from, To: to, Cursor: cursor},
		tenantID, uuid.New(),
	); !errors.Is(err, ErrInvalidSessionCursor) {
		t.Fatalf("scope error = %v", err)
	}
}
