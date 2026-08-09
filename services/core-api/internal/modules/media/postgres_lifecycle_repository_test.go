package media

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresLifecycleTenantControlLockPrecedesScopeRows(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("postgres_lifecycle_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{
			name:  "create",
			start: "func (repository *PostgresLifecycleRepository) CreateSpace(",
			end:   "func (repository *PostgresLifecycleRepository) GetSpace(",
		},
		{
			name:  "read",
			start: "func (repository *PostgresLifecycleRepository) GetSpace(",
			end:   "func (repository *PostgresLifecycleRepository) TransitionSpace(",
		},
		{
			name:  "transition",
			start: "func (repository *PostgresLifecycleRepository) TransitionSpace(",
			end:   "func (repository *PostgresLifecycleRepository) authorizeTransition(",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := strings.Index(source, test.start)
			if start < 0 {
				t.Fatalf("missing repository section %q", test.start)
			}
			endOffset := strings.Index(source[start:], test.end)
			if endOffset < 0 {
				t.Fatalf("missing repository section boundary %q", test.end)
			}
			section := source[start : start+endOffset]
			controlLock := strings.Index(section, "repository.acquireTenantControlLock(")
			activeScope := strings.Index(section, "repository.requireActiveScope(")
			if controlLock < 0 || activeScope < 0 || controlLock > activeScope {
				t.Fatal("tenant control lock must precede active membership and row locks")
			}
		})
	}
}

func TestPostgresLifecycleUnavailableRedactsDatabaseCause(t *testing.T) {
	t.Parallel()

	const sensitiveDetail = "relation tenant_private_table role tenant_private_role provider_room_sid RM_internal"
	repository := &PostgresLifecycleRepository{}
	err := repository.unavailable(
		"load active room instance",
		errors.New(sensitiveDetail),
	)
	if !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("unavailable error = %v, want lifecycle-unavailable classification", err)
	}
	if strings.Contains(err.Error(), sensitiveDetail) {
		t.Fatal("unavailable error exposed the underlying database cause")
	}
	if err.Error() != "load active room instance: media lifecycle unavailable" {
		t.Fatalf("unavailable error = %q, want stable redacted operation", err)
	}
}

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
