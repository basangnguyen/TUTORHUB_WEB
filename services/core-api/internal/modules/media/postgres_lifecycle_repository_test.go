package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestClassSourceMutationConcealsInvisibleSpace(t *testing.T) {
	t.Parallel()

	tenantID, actorID, sessionID := uuid.New(), uuid.New(), uuid.New()
	scope, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create tenant scope: %v", err)
	}
	authority := &mutationConcealmentClassSources{
		errorsByAction: map[policy.Action]error{
			policy.ActionClassView: classroom.ErrClassAccessDenied,
		},
	}
	repository := &PostgresLifecycleRepository{classSources: authority}
	_, err = repository.authorizeSource(
		context.Background(), nil, AccessContext{}, scope,
		spaceRow{Source: SourceReference{Kind: SourceClassSession, ClassSessionID: &sessionID}},
		policy.ActionSessionStart, true, false,
	)
	if !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("invisible mutation error = %v, want concealed space not found", err)
	}
	if len(authority.actions) != 1 || authority.actions[0] != policy.ActionClassView {
		t.Fatalf("invisible mutation actions = %v, want visibility precheck only", authority.actions)
	}
}

func TestClassSourceMutationPreservesVisiblePermissionDenial(t *testing.T) {
	t.Parallel()

	tenantID, actorID, sessionID := uuid.New(), uuid.New(), uuid.New()
	scope, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create tenant scope: %v", err)
	}
	authority := &mutationConcealmentClassSources{
		errorsByAction: map[policy.Action]error{
			policy.ActionSessionStart: classroom.ErrClassAccessDenied,
		},
	}
	repository := &PostgresLifecycleRepository{classSources: authority}
	_, err = repository.authorizeSource(
		context.Background(), nil, AccessContext{}, scope,
		spaceRow{Source: SourceReference{Kind: SourceClassSession, ClassSessionID: &sessionID}},
		policy.ActionSessionStart, true, false,
	)
	if !errors.Is(err, ErrSpaceAccessDenied) {
		t.Fatalf("visible unauthorized mutation error = %v, want access denied", err)
	}
	want := []policy.Action{policy.ActionClassView, policy.ActionSessionStart}
	if len(authority.actions) != len(want) {
		t.Fatalf("visible mutation actions = %v, want %v", authority.actions, want)
	}
	for index := range want {
		if authority.actions[index] != want[index] {
			t.Fatalf("visible mutation actions = %v, want %v", authority.actions, want)
		}
	}
}

type mutationConcealmentClassSources struct {
	errorsByAction map[policy.Action]error
	actions        []policy.Action
}

func (*mutationConcealmentClassSources) AuthorizeMediaClass(
	context.Context,
	pgx.Tx,
	tenancy.Context,
	uuid.UUID,
	policy.Action,
) (classroom.ClassStatus, error) {
	return classroom.ClassStatusActive, nil
}

func (authority *mutationConcealmentClassSources) ResolveMediaSource(
	_ context.Context,
	_ pgx.Tx,
	_ tenancy.Context,
	reference classroom.MediaSourceReference,
	action policy.Action,
) (classroom.MediaSourceSnapshot, error) {
	authority.actions = append(authority.actions, action)
	if err := authority.errorsByAction[action]; err != nil {
		return classroom.MediaSourceSnapshot{}, err
	}
	return classroom.MediaSourceSnapshot{
		Kind: reference.Kind, ClassSessionID: reference.ClassSessionID,
		Status: string(classroom.SessionStatusScheduled),
	}, nil
}

func (*mutationConcealmentClassSources) TransitionMediaSession(
	context.Context,
	pgx.Tx,
	tenancy.Context,
	uuid.UUID,
	classroom.SessionStatus,
	classroom.SessionStatus,
	time.Time,
) error {
	return nil
}
