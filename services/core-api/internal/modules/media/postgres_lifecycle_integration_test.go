//go:build integration

package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresMediaLifecycleRuntimeExactACL(t *testing.T) {
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply media lifecycle migrations: %v", err)
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)

	var migrationRole, runtimeRole string
	if err := migrationPool.QueryRow(ctx, "SELECT current_user").Scan(&migrationRole); err != nil {
		t.Fatalf("read migration database identity: %v", err)
	}
	if err := runtimePool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatalf("read runtime database identity: %v", err)
	}
	if migrationRole == runtimeRole {
		t.Fatal("exact media lifecycle ACL requires distinct migration and runtime roles")
	}

	var schemaUsage, schemaCreate bool
	if err := runtimePool.QueryRow(ctx, `SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    has_schema_privilege(current_user, 'tutorhub', 'CREATE')`).Scan(
		&schemaUsage,
		&schemaCreate,
	); err != nil {
		t.Fatalf("inspect media lifecycle schema ACL: %v", err)
	}
	if !schemaUsage || schemaCreate {
		t.Fatalf("runtime schema ACL = usage:%t create:%t, want true/false", schemaUsage, schemaCreate)
	}

	for _, expectation := range []mediaACLExpectation{
		{
			relation: "tutorhub.media_spaces",
			selectColumns: []string{
				"class_id", "create_idempotency_key", "create_request_fingerprint",
				"created_at", "created_by", "id", "source_class_session_id", "source_kind",
				"source_occurrence_key", "source_series_id", "source_study_meeting_id",
				"status", "tenant_id", "updated_at", "version",
			},
			insertColumns: []string{
				"class_id", "create_idempotency_key", "create_request_fingerprint",
				"created_at", "created_by", "id", "source_class_session_id", "source_kind",
				"source_occurrence_key", "source_series_id", "source_study_meeting_id",
				"tenant_id", "updated_at", "updated_by",
			},
			updateColumns: []string{
				"cancelled_at", "cancelled_by", "ended_at", "ended_by", "locked", "opened_at",
				"opened_by", "status", "updated_at", "updated_by", "version",
			},
		},
		{
			relation: "tutorhub.media_room_instances",
			selectColumns: []string{
				"created_at", "id", "space_id", "status", "tenant_id", "updated_at", "version",
			},
			insertColumns: []string{
				"created_at", "created_by", "id", "provider_room_name", "space_id", "tenant_id",
				"updated_at", "updated_by", "attempt_number",
			},
			updateColumns: []string{
				"closing_at", "ended_at", "failed_at", "failure_code", "status", "updated_at",
				"updated_by", "version",
			},
		},
		{
			relation:      "tutorhub.media_space_members",
			selectColumns: []string{"space_id", "status", "tenant_id", "user_id"},
		},
		{relation: "tutorhub.media_admission_requests"},
		{
			relation:      "tutorhub.media_participant_sessions",
			selectColumns: []string{"status", "tenant_id"},
		},
		{
			relation: "tutorhub.media_space_mutation_receipts",
			selectColumns: []string{
				"actor_user_id", "idempotency_key", "operation", "request_fingerprint", "space_id",
				"tenant_id",
			},
			insertColumns: []string{
				"actor_user_id", "created_at", "idempotency_key", "operation", "request_fingerprint",
				"result_room_instance_id", "result_space_version", "space_id", "tenant_id",
			},
		},
	} {
		assertExactMediaACL(t, ctx, runtimePool, expectation)
	}

	mediaTables := []string{
		"media_spaces", "media_room_instances", "media_space_members",
		"media_admission_requests", "media_participant_sessions",
		"media_space_mutation_receipts",
	}
	var publicTableGrants, publicColumnGrants int
	if err := migrationPool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM information_schema.table_privileges
     WHERE table_schema = 'tutorhub' AND table_name = ANY($1::text[]) AND grantee = 'PUBLIC'),
    (SELECT count(*) FROM information_schema.column_privileges
     WHERE table_schema = 'tutorhub' AND table_name = ANY($1::text[]) AND grantee = 'PUBLIC')`,
		mediaTables,
	).Scan(&publicTableGrants, &publicColumnGrants); err != nil {
		t.Fatalf("inspect PUBLIC media lifecycle grants: %v", err)
	}
	if publicTableGrants != 0 || publicColumnGrants != 0 {
		t.Fatalf(
			"PUBLIC retained media lifecycle grants: table=%d column=%d",
			publicTableGrants,
			publicColumnGrants,
		)
	}

	var safeRole, noMigrationMembership, noTableOwnership bool
	if err := migrationPool.QueryRow(ctx, `SELECT
    NOT runtime.rolsuper AND NOT runtime.rolcreaterole AND NOT runtime.rolcreatedb
        AND NOT runtime.rolreplication AND NOT runtime.rolbypassrls,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER'),
    NOT EXISTS (
        SELECT 1
        FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname = ANY($3::text[])
          AND relation.relowner = runtime.oid
    )
FROM pg_roles AS runtime
JOIN pg_roles AS migration ON migration.rolname = $2
WHERE runtime.rolname = $1`, runtimeRole, migrationRole, mediaTables).Scan(
		&safeRole,
		&noMigrationMembership,
		&noTableOwnership,
	); err != nil {
		t.Fatalf("inspect media lifecycle runtime role attributes: %v", err)
	}
	if !safeRole || !noMigrationMembership || !noTableOwnership {
		t.Fatalf(
			"unsafe runtime role: attributes=%t migration_membership_absent=%t ownership_absent=%t",
			safeRole,
			noMigrationMembership,
			noTableOwnership,
		)
	}
}

type mediaACLExpectation struct {
	relation      string
	selectColumns []string
	insertColumns []string
	updateColumns []string
}

func assertExactMediaACL(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	expectation mediaACLExpectation,
) {
	t.Helper()
	var inserted, selected, updated, deleted, truncated, referenced, triggered bool
	if err := pool.QueryRow(ctx, `SELECT
    has_table_privilege(current_user, $1, 'INSERT'),
    has_table_privilege(current_user, $1, 'SELECT'),
    has_table_privilege(current_user, $1, 'UPDATE'),
    has_table_privilege(current_user, $1, 'DELETE'),
    has_table_privilege(current_user, $1, 'TRUNCATE'),
    has_table_privilege(current_user, $1, 'REFERENCES'),
    has_table_privilege(current_user, $1, 'TRIGGER')`, expectation.relation).Scan(
		&inserted,
		&selected,
		&updated,
		&deleted,
		&truncated,
		&referenced,
		&triggered,
	); err != nil {
		t.Fatalf("inspect table ACL for %s: %v", expectation.relation, err)
	}
	if inserted || selected || updated || deleted || truncated || referenced || triggered {
		t.Fatalf(
			"runtime has broad table privilege on %s: insert=%t select=%t update=%t delete=%t truncate=%t references=%t trigger=%t",
			expectation.relation,
			inserted,
			selected,
			updated,
			deleted,
			truncated,
			referenced,
			triggered,
		)
	}
	assertExactMediaColumns(t, ctx, pool, expectation.relation, "SELECT", expectation.selectColumns)
	assertExactMediaColumns(t, ctx, pool, expectation.relation, "INSERT", expectation.insertColumns)
	assertExactMediaColumns(t, ctx, pool, expectation.relation, "UPDATE", expectation.updateColumns)
}

func assertExactMediaColumns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	relation string,
	privilege string,
	expected []string,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT column_name
FROM information_schema.columns
WHERE table_schema = split_part($1, '.', 1)
  AND table_name = split_part($1, '.', 2)
  AND has_column_privilege(current_user, $1, column_name, $2)
ORDER BY column_name`, relation, privilege)
	if err != nil {
		t.Fatalf("query runtime %s columns for %s: %v", privilege, relation, err)
	}
	defer rows.Close()
	actual := make([]string, 0, len(expected))
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan runtime %s column for %s: %v", privilege, relation, err)
		}
		actual = append(actual, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime %s columns for %s: %v", privilege, relation, err)
	}
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if strings.Join(actual, ",") != strings.Join(want, ",") {
		t.Fatalf("runtime %s columns for %s = %v, want %v", privilege, relation, actual, want)
	}
}

func TestPostgresMediaLifecycleAuthorityConcurrencyQuotaAndPrivacy(t *testing.T) {
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply media lifecycle migrations: %v", err)
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	fixture := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, fixture) })
	lifecycle, classes := newMediaIntegrationServices(t, runtimePool)

	ownerAccess := mediaIntegrationAccess(
		fixture.tenantID,
		fixture.ownerID,
		policy.OrganizationRoleStudent,
	)
	disabledInput := officialMediaCreateInput(
		fixture.sessionIDs["disabled"],
		mediaIntegrationKey("feature-off"),
	)
	if _, err := lifecycle.CreateSpace(ctx, ownerAccess, disabledInput); !errors.Is(
		err,
		featurecontrol.ErrFeatureDisabled,
	) {
		t.Fatalf("feature-off create error = %v, want feature disabled", err)
	}
	assertMediaSourceRowCount(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.sessionIDs["disabled"],
		0,
	)

	setMediaFeatureOverrides(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.adminID,
		map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
			featurecontrol.FeatureInstantStudyRooms:   true,
		},
	)
	setMediaQuotaOverrides(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.adminID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaActiveMediaSpaces:            100,
			featurecontrol.QuotaMediaSpaceStartsPerHour:      200,
			featurecontrol.QuotaActiveStudyMeetings:          200,
			featurecontrol.QuotaStudyMeetingCreationsPerHour: 200,
			featurecontrol.QuotaMediaParticipantsPerSpace:    50,
			featurecontrol.QuotaActiveMediaParticipants:      500,
		},
	)

	var ownerSpace MediaSpace
	officialSpaces := make(map[string]MediaSpace)
	for _, test := range []struct {
		name      string
		actorID   uuid.UUID
		role      policy.OrganizationRole
		session   string
		wantAllow bool
	}{
		{name: "implicit owner", actorID: fixture.ownerID, role: policy.OrganizationRoleStudent, session: "owner", wantAllow: true},
		{name: "organization teacher", actorID: fixture.teacherID, role: policy.OrganizationRoleTeacher, session: "teacher", wantAllow: true},
		{name: "organization admin", actorID: fixture.adminID, role: policy.OrganizationRoleAdmin, session: "admin", wantAllow: true},
		{name: "active co-teacher", actorID: fixture.coTeacherID, role: policy.OrganizationRoleStudent, session: "co-teacher", wantAllow: true},
		{name: "active student", actorID: fixture.studentID, role: policy.OrganizationRoleStudent, session: "student", wantAllow: false},
	} {
		t.Run("official authority "+test.name, func(t *testing.T) {
			result, err := lifecycle.CreateSpace(
				ctx,
				mediaIntegrationAccess(fixture.tenantID, test.actorID, test.role),
				officialMediaCreateInput(
					fixture.sessionIDs[test.session],
					mediaIntegrationKey("authority-"+test.session),
				),
			)
			if !test.wantAllow {
				if !errors.Is(err, ErrSpaceAccessDenied) {
					t.Fatalf("student official create error = %v, want access denied", err)
				}
				assertMediaSourceRowCount(
					t,
					ctx,
					migrationPool,
					fixture.tenantID,
					fixture.sessionIDs[test.session],
					0,
				)
				return
			}
			if err != nil {
				t.Fatalf("authorized official create: %v", err)
			}
			if !result.Created || result.Space.Status != SpaceStatusScheduled ||
				!result.Space.ViewerOperations.CanStart {
				t.Fatalf("unexpected authorized official space: %+v", result)
			}
			if test.session == "owner" {
				ownerSpace = result.Space
			}
			officialSpaces[test.session] = result.Space
		})
	}
	sharedActorKey := mediaIntegrationKey("actor-scoped-transition")
	if _, err := lifecycle.CancelSpace(
		ctx,
		ownerAccess,
		officialSpaces["owner"].ID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: sharedActorKey},
	); err != nil {
		t.Fatalf("owner consumes actor-scoped transition key: %v", err)
	}
	if _, err := lifecycle.CancelSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.teacherID,
			policy.OrganizationRoleTeacher,
		),
		officialSpaces["teacher"].ID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: sharedActorKey},
	); err != nil {
		t.Fatalf("second actor reuses transition key safely: %v", err)
	}
	assertActorScopedMediaReceipts(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		sharedActorKey,
		[]uuid.UUID{fixture.ownerID, fixture.teacherID},
	)

	studentProjection, err := lifecycle.GetSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.studentID,
			policy.OrganizationRoleStudent,
		),
		ownerSpace.ID,
	)
	if err != nil {
		t.Fatalf("active enrolled student reads official media space: %v", err)
	}
	if studentProjection.ViewerOperations.CanStart || studentProjection.ViewerOperations.CanEnd ||
		studentProjection.ViewerOperations.CanCancel {
		t.Fatalf("student received elevated official operations: %+v", studentProjection.ViewerOperations)
	}
	if _, err := lifecycle.StartSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.studentID,
			policy.OrganizationRoleStudent,
		),
		ownerSpace.ID,
		TransitionInput{
			ExpectedVersion: 1,
			IdempotencyKey:  mediaIntegrationKey("student-start-denied"),
		},
	); !errors.Is(err, ErrSpaceAccessDenied) {
		t.Fatalf("student official start error = %v, want access denied", err)
	}
	if _, err := lifecycle.GetSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.outsiderID,
			policy.OrganizationRoleStudent,
		),
		ownerSpace.ID,
	); !isConcealedMediaError(err) {
		t.Fatalf("same-tenant nonmember read error = %v, want concealed not found", err)
	}
	if _, err := lifecycle.StartSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.outsiderID,
			policy.OrganizationRoleStudent,
		),
		ownerSpace.ID,
		TransitionInput{
			ExpectedVersion: 1,
			IdempotencyKey:  sharedActorKey,
		},
	); !isConcealedMediaError(err) {
		t.Fatalf("same-tenant actor must not observe another receipt: %v", err)
	}
	if _, err := lifecycle.GetSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.foreignTenantID,
			fixture.foreignOwnerID,
			policy.OrganizationRoleTeacher,
		),
		ownerSpace.ID,
	); !isConcealedMediaError(err) {
		t.Fatalf("foreign-tenant media read error = %v, want concealed not found", err)
	}
	if _, err := lifecycle.StartSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.foreignTenantID,
			fixture.foreignOwnerID,
			policy.OrganizationRoleTeacher,
		),
		ownerSpace.ID,
		TransitionInput{
			ExpectedVersion: 1,
			IdempotencyKey:  mediaIntegrationKey("foreign-start-concealed"),
		},
	); !isConcealedMediaError(err) {
		t.Fatalf("foreign-tenant mutation error = %v, want concealed not found", err)
	}

	if _, err := lifecycle.CreateSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.outsiderID,
			policy.OrganizationRoleStudent,
		),
		CreateSpaceInput{
			Source: CreateSourceInput{
				Kind:           SourceStudyMeeting,
				StudyMeetingID: fixture.studyMeetingID,
			},
			IdempotencyKey: mediaIntegrationKey("meeting-foreign-owner"),
		},
	); !errors.Is(err, ErrSpaceAccessDenied) {
		t.Fatalf("nonowner StudyMeeting create error = %v, want access denied", err)
	}
	meetingResult, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		CreateSpaceInput{
			Source: CreateSourceInput{
				Kind:           SourceStudyMeeting,
				StudyMeetingID: fixture.studyMeetingID,
			},
			IdempotencyKey: mediaIntegrationKey("meeting-owner"),
		},
	)
	if err != nil {
		t.Fatalf("StudyMeeting owner creates media space: %v", err)
	}
	if _, err := lifecycle.GetSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.outsiderID,
			policy.OrganizationRoleStudent,
		),
		meetingResult.Space.ID,
	); !isConcealedMediaError(err) {
		t.Fatalf("uninvited StudyMeeting read error = %v, want concealed not found", err)
	}
	insertMediaSpaceMember(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		meetingResult.Space.ID,
		fixture.outsiderID,
		fixture.ownerID,
	)
	if _, err := lifecycle.GetSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.outsiderID,
			policy.OrganizationRoleStudent,
		),
		meetingResult.Space.ID,
	); err != nil {
		t.Fatalf("explicit same-tenant StudyMeeting member read: %v", err)
	}
	if _, err := lifecycle.StartSpace(
		ctx,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.outsiderID,
			policy.OrganizationRoleStudent,
		),
		meetingResult.Space.ID,
		TransitionInput{
			ExpectedVersion: 1,
			IdempotencyKey:  mediaIntegrationKey("meeting-member-start"),
		},
	); !errors.Is(err, ErrSpaceAccessDenied) {
		t.Fatalf("StudyMeeting member start error = %v, want owner-only denial", err)
	}
	meetingOpen, err := lifecycle.StartSpace(
		ctx,
		ownerAccess,
		meetingResult.Space.ID,
		TransitionInput{
			ExpectedVersion: 1,
			IdempotencyKey:  mediaIntegrationKey("meeting-owner-start"),
		},
	)
	if err != nil {
		t.Fatalf("StudyMeeting owner starts media space: %v", err)
	}
	adminAccess := mediaIntegrationAccess(
		fixture.tenantID,
		fixture.adminID,
		policy.OrganizationRoleAdmin,
	)
	if _, err := lifecycle.EndSpace(
		ctx,
		adminAccess,
		meetingOpen.ID,
		TransitionInput{
			ExpectedVersion: meetingOpen.Version,
			IdempotencyKey:  mediaIntegrationKey("admin-end-no-reason"),
		},
	); !errors.Is(err, ErrInvalidSpaceRequest) {
		t.Fatalf("admin safety end without reason = %v, want invalid request", err)
	}
	if _, err := lifecycle.EndSpace(
		ctx,
		adminAccess,
		meetingOpen.ID,
		TransitionInput{
			ExpectedVersion: meetingOpen.Version,
			IdempotencyKey:  mediaIntegrationKey("admin-safety-end"),
			ReasonCode:      "safety_policy",
		},
	); err != nil {
		t.Fatalf("admin safety end with reason: %v", err)
	}

	instantKey := mediaIntegrationKey("instant-concurrent")
	instantInput := CreateSpaceInput{
		Source: CreateSourceInput{
			Kind: SourceInstant,
			Instant: InstantSourceInput{
				Title:           fixture.privateTitle,
				DurationMinutes: 45,
				Timezone:        "Asia/Ho_Chi_Minh",
			},
		},
		IdempotencyKey: instantKey,
	}
	instantOutcomes := runConcurrentMediaCreates(
		ctx,
		lifecycle,
		mediaIntegrationAccess(
			fixture.tenantID,
			fixture.studentID,
			policy.OrganizationRoleStudent,
		),
		instantInput,
	)
	assertConcurrentCreateReplay(t, instantOutcomes)
	instantSpace := instantOutcomes[0].result.Space
	if instantSpace.Source.Kind != SourceStudyMeeting || instantSpace.Source.StudyMeetingID == nil {
		t.Fatalf("instant command did not bind an owned StudyMeeting: %+v", instantSpace.Source)
	}
	var instantMeetings int
	if err := migrationPool.QueryRow(ctx, `SELECT count(*)
FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND create_idempotency_key = $2`, fixture.tenantID, instantKey).Scan(
		&instantMeetings,
	); err != nil {
		t.Fatalf("count instant StudyMeetings: %v", err)
	}
	var instantOwner uuid.UUID
	var instantTitle string
	if err := migrationPool.QueryRow(ctx, `SELECT owner_user_id, title
FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND create_idempotency_key = $2`, fixture.tenantID, instantKey).Scan(
		&instantOwner,
		&instantTitle,
	); err != nil {
		t.Fatalf("inspect instant StudyMeeting: %v", err)
	}
	if instantMeetings != 1 || instantOwner != fixture.studentID || instantTitle != fixture.privateTitle {
		t.Fatalf(
			"instant StudyMeeting = count:%d owner:%s title-match:%t",
			instantMeetings,
			instantOwner,
			instantTitle == fixture.privateTitle,
		)
	}

	concurrentCreate, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.sessionIDs["concurrent"],
			mediaIntegrationKey("concurrent-source-create"),
		),
	)
	if err != nil {
		t.Fatalf("create concurrent-start media space: %v", err)
	}
	startKey := mediaIntegrationKey("concurrent-start")
	startOutcomes := runConcurrentMediaTransitions(func() (MediaSpace, error) {
		return lifecycle.StartSpace(
			ctx,
			ownerAccess,
			concurrentCreate.Space.ID,
			TransitionInput{ExpectedVersion: 1, IdempotencyKey: startKey},
		)
	})
	assertConcurrentTransitionReplay(t, startOutcomes, SpaceStatusOpen)
	if startOutcomes[0].space.ActiveRoomInstance == nil ||
		startOutcomes[1].space.ActiveRoomInstance == nil ||
		startOutcomes[0].space.ActiveRoomInstance.ID != startOutcomes[1].space.ActiveRoomInstance.ID {
		t.Fatalf("same-key start did not replay one room instance: %+v", startOutcomes)
	}
	assertMediaInstanceAndReceiptCount(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		concurrentCreate.Space.ID,
		startKey,
		1,
		1,
	)

	raceCreate, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.sessionIDs["end-cancel-race"],
			mediaIntegrationKey("end-cancel-create"),
		),
	)
	if err != nil {
		t.Fatalf("create end/cancel race space: %v", err)
	}
	raceOpen, err := lifecycle.StartSpace(
		ctx,
		ownerAccess,
		raceCreate.Space.ID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: mediaIntegrationKey("end-cancel-start")},
	)
	if err != nil {
		t.Fatalf("start end/cancel race space: %v", err)
	}
	raceOutcomes := runConcurrentMediaTransitions(
		func() (MediaSpace, error) {
			return lifecycle.EndSpace(
				ctx,
				ownerAccess,
				raceOpen.ID,
				TransitionInput{
					ExpectedVersion: raceOpen.Version,
					IdempotencyKey:  mediaIntegrationKey("race-end"),
				},
			)
		},
		func() (MediaSpace, error) {
			return lifecycle.CancelSpace(
				ctx,
				ownerAccess,
				raceOpen.ID,
				TransitionInput{
					ExpectedVersion: raceOpen.Version,
					IdempotencyKey:  mediaIntegrationKey("race-cancel"),
				},
			)
		},
	)
	if transitionSuccessCount(raceOutcomes) != 1 {
		t.Fatalf("end/cancel race successes = %d, want 1: %+v", transitionSuccessCount(raceOutcomes), raceOutcomes)
	}
	ended, err := lifecycle.GetSpace(ctx, ownerAccess, raceOpen.ID)
	if err != nil || ended.Status != SpaceStatusEnded || ended.Version != raceOpen.Version+1 {
		t.Fatalf("end/cancel final space = %+v, error %v", ended, err)
	}

	barrierCreate, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.sessionIDs["source-race"],
			mediaIntegrationKey("source-race-create"),
		),
	)
	if err != nil {
		t.Fatalf("create source barrier space: %v", err)
	}
	barrierStart := make(chan struct{})
	var barrierWait sync.WaitGroup
	barrierWait.Add(2)
	var mediaStartErr, sourceCancelErr error
	go func() {
		defer barrierWait.Done()
		<-barrierStart
		_, mediaStartErr = lifecycle.StartSpace(
			ctx,
			ownerAccess,
			barrierCreate.Space.ID,
			TransitionInput{ExpectedVersion: 1, IdempotencyKey: mediaIntegrationKey("source-race-start")},
		)
	}()
	go func() {
		defer barrierWait.Done()
		<-barrierStart
		_, sourceCancelErr = classes.CancelSession(
			ctx,
			tenancy.Context{TenantID: fixture.tenantID, ActorID: fixture.ownerID},
			fixture.classID,
			fixture.sessionIDs["source-race"],
			classroom.CancelSessionParams{ExpectedVersion: 1},
			time.Now().UTC(),
		)
	}()
	close(barrierStart)
	barrierWait.Wait()
	if (mediaStartErr == nil) == (sourceCancelErr == nil) {
		t.Fatalf(
			"source/start race must have one winner: media=%v source=%v",
			mediaStartErr,
			sourceCancelErr,
		)
	}
	assertMediaSourceRaceConsistency(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		barrierCreate.Space.ID,
		fixture.sessionIDs["source-race"],
	)

	archiveCreate, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.archiveSessionID,
			mediaIntegrationKey("archive-create"),
		),
	)
	if err != nil {
		t.Fatalf("create archive barrier media space: %v", err)
	}
	archiveOpen, err := lifecycle.StartSpace(
		ctx,
		ownerAccess,
		archiveCreate.Space.ID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: mediaIntegrationKey("archive-start")},
	)
	if err != nil {
		t.Fatalf("start archive barrier media space: %v", err)
	}
	if _, err := classes.Archive(
		ctx,
		tenancy.Context{TenantID: fixture.tenantID, ActorID: fixture.ownerID},
		fixture.archiveClassID,
		1,
		time.Now().UTC(),
	); !errors.Is(err, classroom.ErrInvalidClassTransition) {
		t.Fatalf("archive with open media error = %v, want invalid class transition", err)
	}
	if _, err := lifecycle.EndSpace(
		ctx,
		ownerAccess,
		archiveOpen.ID,
		TransitionInput{
			ExpectedVersion: archiveOpen.Version,
			IdempotencyKey:  mediaIntegrationKey("archive-end"),
		},
	); err != nil {
		t.Fatalf("end archive barrier media space: %v", err)
	}
	if _, err := classes.Archive(
		ctx,
		tenancy.Context{TenantID: fixture.tenantID, ActorID: fixture.ownerID},
		fixture.archiveClassID,
		1,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("archive after media end: %v", err)
	}

	setMediaFeatureOverrides(
		t,
		ctx,
		migrationPool,
		fixture.quotaTenantID,
		fixture.quotaOwnerID,
		map[featurecontrol.FeatureKey]bool{featurecontrol.FeatureClassroomMediaRooms: true},
	)
	setMediaQuotaOverrides(
		t,
		ctx,
		migrationPool,
		fixture.quotaTenantID,
		fixture.quotaOwnerID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaActiveMediaSpaces:       2,
			featurecontrol.QuotaMediaSpaceStartsPerHour: 1,
		},
	)
	quotaAccess := mediaIntegrationAccess(
		fixture.quotaTenantID,
		fixture.quotaOwnerID,
		policy.OrganizationRoleTeacher,
	)
	quotaFirst, err := lifecycle.CreateSpace(
		ctx,
		quotaAccess,
		officialMediaCreateInput(fixture.quotaSessionIDs[0], mediaIntegrationKey("quota-first")),
	)
	if err != nil {
		t.Fatalf("create first quota media space: %v", err)
	}
	quotaSecond, err := lifecycle.CreateSpace(
		ctx,
		quotaAccess,
		officialMediaCreateInput(fixture.quotaSessionIDs[1], mediaIntegrationKey("quota-second")),
	)
	if err != nil {
		t.Fatalf("create second quota media space: %v", err)
	}
	if _, err := lifecycle.CreateSpace(
		ctx,
		quotaAccess,
		officialMediaCreateInput(fixture.quotaSessionIDs[2], mediaIntegrationKey("quota-third")),
	); !errors.Is(err, featurecontrol.ErrQuotaExceeded) {
		t.Fatalf("active-space quota error = %v, want quota exceeded", err)
	}
	assertMediaSourceRowCount(
		t,
		ctx,
		migrationPool,
		fixture.quotaTenantID,
		fixture.quotaSessionIDs[2],
		0,
	)
	if _, err := lifecycle.StartSpace(
		ctx,
		quotaAccess,
		quotaFirst.Space.ID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: mediaIntegrationKey("quota-start-first")},
	); err != nil {
		t.Fatalf("consume first media start quota: %v", err)
	}
	if _, err := lifecycle.StartSpace(
		ctx,
		quotaAccess,
		quotaSecond.Space.ID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: mediaIntegrationKey("quota-start-second")},
	); !errors.Is(err, featurecontrol.ErrQuotaExceeded) {
		t.Fatalf("media start rate error = %v, want quota exceeded", err)
	}
	assertMediaInstanceAndReceiptCount(
		t,
		ctx,
		migrationPool,
		fixture.quotaTenantID,
		quotaSecond.Space.ID,
		mediaIntegrationKey("quota-start-second"),
		0,
		0,
	)

	assertMediaPrivacyAndProviderIsolation(t, ctx, migrationPool, fixture)
}

type mediaCreateOutcome struct {
	result CreateSpaceResult
	err    error
}

func runConcurrentMediaCreates(
	ctx context.Context,
	service LifecycleServiceAPI,
	access AccessContext,
	input CreateSpaceInput,
) []mediaCreateOutcome {
	start := make(chan struct{})
	outcomes := make([]mediaCreateOutcome, 2)
	var wait sync.WaitGroup
	wait.Add(len(outcomes))
	for index := range outcomes {
		go func(index int) {
			defer wait.Done()
			<-start
			outcomes[index].result, outcomes[index].err = service.CreateSpace(ctx, access, input)
		}(index)
	}
	close(start)
	wait.Wait()
	return outcomes
}

func assertConcurrentCreateReplay(t *testing.T, outcomes []mediaCreateOutcome) {
	t.Helper()
	if len(outcomes) != 2 || outcomes[0].err != nil || outcomes[1].err != nil {
		t.Fatalf("concurrent create outcomes: %+v", outcomes)
	}
	if outcomes[0].result.Space.ID != outcomes[1].result.Space.ID ||
		outcomes[0].result.Space.Version != outcomes[1].result.Space.Version {
		t.Fatalf("concurrent create did not replay canonical space: %+v", outcomes)
	}
	created := 0
	for _, outcome := range outcomes {
		if outcome.result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("concurrent create Created=true count = %d, want 1", created)
	}
}

type mediaTransitionOutcome struct {
	space MediaSpace
	err   error
}

func runConcurrentMediaTransitions(
	operations ...func() (MediaSpace, error),
) []mediaTransitionOutcome {
	start := make(chan struct{})
	outcomes := make([]mediaTransitionOutcome, len(operations))
	var wait sync.WaitGroup
	wait.Add(len(operations))
	for index, operation := range operations {
		go func(index int, operation func() (MediaSpace, error)) {
			defer wait.Done()
			<-start
			outcomes[index].space, outcomes[index].err = operation()
		}(index, operation)
	}
	close(start)
	wait.Wait()
	return outcomes
}

func assertConcurrentTransitionReplay(
	t *testing.T,
	outcomes []mediaTransitionOutcome,
	wantStatus SpaceStatus,
) {
	t.Helper()
	if len(outcomes) != 2 || outcomes[0].err != nil || outcomes[1].err != nil {
		t.Fatalf("concurrent transition outcomes: %+v", outcomes)
	}
	if outcomes[0].space.ID != outcomes[1].space.ID ||
		outcomes[0].space.Version != outcomes[1].space.Version ||
		outcomes[0].space.Status != wantStatus || outcomes[1].space.Status != wantStatus {
		t.Fatalf("concurrent transition did not replay canonical space: %+v", outcomes)
	}
}

func transitionSuccessCount(outcomes []mediaTransitionOutcome) int {
	count := 0
	for _, outcome := range outcomes {
		if outcome.err == nil {
			count++
		}
	}
	return count
}

func isConcealedMediaError(err error) bool {
	return errors.Is(err, ErrSpaceNotFound) || errors.Is(err, ErrSourceUnavailable)
}

type mediaIntegrationFixture struct {
	tenantID         uuid.UUID
	foreignTenantID  uuid.UUID
	quotaTenantID    uuid.UUID
	adminID          uuid.UUID
	teacherID        uuid.UUID
	ownerID          uuid.UUID
	coTeacherID      uuid.UUID
	studentID        uuid.UUID
	outsiderID       uuid.UUID
	foreignOwnerID   uuid.UUID
	quotaOwnerID     uuid.UUID
	classID          uuid.UUID
	archiveClassID   uuid.UUID
	foreignClassID   uuid.UUID
	quotaClassID     uuid.UUID
	archiveSessionID uuid.UUID
	studyMeetingID   uuid.UUID
	sessionIDs       map[string]uuid.UUID
	quotaSessionIDs  []uuid.UUID
	userIDs          []uuid.UUID
	privateEmail     string
	privateTitle     string
}

func seedMediaIntegrationFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) mediaIntegrationFixture {
	t.Helper()
	fixture := mediaIntegrationFixture{
		tenantID:         uuid.New(),
		foreignTenantID:  uuid.New(),
		quotaTenantID:    uuid.New(),
		adminID:          uuid.New(),
		teacherID:        uuid.New(),
		ownerID:          uuid.New(),
		coTeacherID:      uuid.New(),
		studentID:        uuid.New(),
		outsiderID:       uuid.New(),
		foreignOwnerID:   uuid.New(),
		quotaOwnerID:     uuid.New(),
		classID:          uuid.New(),
		archiveClassID:   uuid.New(),
		foreignClassID:   uuid.New(),
		quotaClassID:     uuid.New(),
		archiveSessionID: uuid.New(),
		studyMeetingID:   uuid.New(),
		sessionIDs:       make(map[string]uuid.UUID),
		quotaSessionIDs:  []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
		privateTitle:     "P4-01 private instant " + uuid.NewString(),
	}
	fixture.userIDs = []uuid.UUID{
		fixture.adminID,
		fixture.teacherID,
		fixture.ownerID,
		fixture.coTeacherID,
		fixture.studentID,
		fixture.outsiderID,
		fixture.foreignOwnerID,
		fixture.quotaOwnerID,
	}
	for _, name := range []string{
		"disabled",
		"owner",
		"teacher",
		"admin",
		"co-teacher",
		"student",
		"concurrent",
		"end-cancel-race",
		"source-race",
	} {
		fixture.sessionIDs[name] = uuid.New()
	}

	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin media integration fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	for index, userID := range fixture.userIDs {
		email := fmt.Sprintf(
			"p401-%d-%s@example.test",
			index,
			strings.ReplaceAll(uuid.NewString(), "-", ""),
		)
		if index == 4 {
			fixture.privateEmail = email
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`, userID, email, fmt.Sprintf("P4-01 user %d", index)); err != nil {
			t.Fatalf("insert media integration user: %v", err)
		}
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'P4-01 media integration'),
       ($3, $4, 'P4-01 foreign integration'),
       ($5, $6, 'P4-01 quota integration')`,
		fixture.tenantID,
		mediaIntegrationSlug("p401-media"),
		fixture.foreignTenantID,
		mediaIntegrationSlug("p401-foreign"),
		fixture.quotaTenantID,
		mediaIntegrationSlug("p401-quota"),
	); err != nil {
		t.Fatalf("insert media integration tenants: %v", err)
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
) VALUES
    ($1, $2, 'org_admin', 'active', now()),
    ($1, $3, 'teacher', 'active', now()),
    ($1, $4, 'student', 'active', now()),
    ($1, $5, 'student', 'active', now()),
    ($1, $6, 'student', 'active', now()),
    ($1, $7, 'student', 'active', now()),
    ($8, $9, 'teacher', 'active', now()),
    ($10, $11, 'teacher', 'active', now())`,
		fixture.tenantID,
		fixture.adminID,
		fixture.teacherID,
		fixture.ownerID,
		fixture.coTeacherID,
		fixture.studentID,
		fixture.outsiderID,
		fixture.foreignTenantID,
		fixture.foreignOwnerID,
		fixture.quotaTenantID,
		fixture.quotaOwnerID,
	); err != nil {
		t.Fatalf("insert media integration memberships: %v", err)
	}
	for _, class := range []struct {
		id       uuid.UUID
		tenantID uuid.UUID
		ownerID  uuid.UUID
		title    string
	}{
		{fixture.classID, fixture.tenantID, fixture.ownerID, "P4-01 authority class"},
		{fixture.archiveClassID, fixture.tenantID, fixture.ownerID, "P4-01 archive class"},
		{fixture.foreignClassID, fixture.foreignTenantID, fixture.foreignOwnerID, "P4-01 foreign class"},
		{fixture.quotaClassID, fixture.quotaTenantID, fixture.quotaOwnerID, "P4-01 quota class"},
	} {
		if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
) VALUES ($1, $2, $3, $4, $5, 'Asia/Ho_Chi_Minh', 'active')`,
			class.id,
			class.tenantID,
			class.ownerID,
			mediaIntegrationClassCode(),
			class.title,
		); err != nil {
			t.Fatalf("insert media integration class: %v", err)
		}
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
) VALUES
    ($1, $2, $3, 'co_teacher', 'active', $4, now()),
    ($1, $2, $5, 'student', 'active', $4, now())`,
		fixture.tenantID,
		fixture.classID,
		fixture.coTeacherID,
		fixture.ownerID,
		fixture.studentID,
	); err != nil {
		t.Fatalf("insert media integration class enrollments: %v", err)
	}

	startsAt := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Microsecond)
	index := 0
	insertSession := func(sessionID, tenantID, classID, actorID uuid.UUID, title string) {
		t.Helper()
		start := startsAt.Add(time.Duration(index*2) * time.Hour)
		index++
		if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.class_sessions (
    id, tenant_id, class_id, title, starts_at, ends_at, timezone, status,
    created_by, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, 'Asia/Ho_Chi_Minh', 'scheduled', $7, $7)`,
			sessionID,
			tenantID,
			classID,
			title,
			start,
			start.Add(time.Hour),
			actorID,
		); err != nil {
			t.Fatalf("insert media integration class session: %v", err)
		}
	}
	for _, name := range []string{
		"disabled",
		"owner",
		"teacher",
		"admin",
		"co-teacher",
		"student",
		"concurrent",
		"end-cancel-race",
		"source-race",
	} {
		insertSession(
			fixture.sessionIDs[name],
			fixture.tenantID,
			fixture.classID,
			fixture.ownerID,
			"P4-01 "+name,
		)
	}
	insertSession(
		fixture.archiveSessionID,
		fixture.tenantID,
		fixture.archiveClassID,
		fixture.ownerID,
		"P4-01 archive barrier",
	)
	for quotaIndex, sessionID := range fixture.quotaSessionIDs {
		insertSession(
			sessionID,
			fixture.quotaTenantID,
			fixture.quotaClassID,
			fixture.quotaOwnerID,
			fmt.Sprintf("P4-01 quota %d", quotaIndex),
		)
	}
	meetingStart := startsAt.Add(72 * time.Hour)
	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.study_meetings (
    id, tenant_id, owner_user_id, title, starts_at, ends_at, timezone,
    create_idempotency_key, create_request_fingerprint
) VALUES ($1, $2, $3, 'P4-01 private StudyMeeting', $4, $5,
          'Asia/Ho_Chi_Minh', $6, $7)`,
		fixture.studyMeetingID,
		fixture.tenantID,
		fixture.ownerID,
		meetingStart,
		meetingStart.Add(time.Hour),
		mediaIntegrationKey("seed-meeting"),
		make([]byte, 32),
	); err != nil {
		t.Fatalf("insert media integration StudyMeeting: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit media integration fixture: %v", err)
	}
	return fixture
}

func newMediaIntegrationServices(
	t *testing.T,
	pool *pgxpool.Pool,
) (*LifecycleService, *classroom.PostgresRepository) {
	t.Helper()
	authorizer := policy.NewEngine()
	controls, err := featurecontrol.NewPostgresRepository(
		pool,
		20*time.Second,
		authorizer,
		featurecontrol.NewDefaultCatalog(),
	)
	if err != nil {
		t.Fatalf("create media integration feature enforcer: %v", err)
	}
	classes := classroom.NewPostgresRepository(pool, 20*time.Second, authorizer, controls)
	repository, err := NewPostgresLifecycleRepository(
		pool,
		20*time.Second,
		authorizer,
		controls,
		classes,
	)
	if err != nil {
		t.Fatalf("create media lifecycle PostgreSQL repository: %v", err)
	}
	service, err := NewLifecycleService(repository)
	if err != nil {
		t.Fatalf("create media lifecycle service: %v", err)
	}
	return service, classes
}

func mediaIntegrationAccess(
	tenantID uuid.UUID,
	actorID uuid.UUID,
	role policy.OrganizationRole,
) AccessContext {
	return AccessContext{
		TenantID: tenantID, ActorID: actorID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{role},
	}
}

func officialMediaCreateInput(sessionID uuid.UUID, key string) CreateSpaceInput {
	return CreateSpaceInput{
		Source:         CreateSourceInput{Kind: SourceClassSession, ClassSessionID: sessionID},
		IdempotencyKey: key,
	}
}

func setMediaFeatureOverrides(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	overrides map[featurecontrol.FeatureKey]bool,
) {
	t.Helper()
	for key, enabled := range overrides {
		if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.tenant_feature_overrides (
    tenant_id, feature_key, enabled, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, now(), now())
ON CONFLICT (tenant_id, feature_key) DO UPDATE
SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			tenantID,
			key,
			enabled,
			actorID,
		); err != nil {
			t.Fatalf("set media integration feature %s: %v", key, err)
		}
	}
}

func setMediaQuotaOverrides(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	overrides map[featurecontrol.QuotaKey]int64,
) {
	t.Helper()
	for key, limit := range overrides {
		if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.tenant_quota_overrides (
    tenant_id, quota_key, limit_value, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, now(), now())
ON CONFLICT (tenant_id, quota_key) DO UPDATE
SET limit_value = EXCLUDED.limit_value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			tenantID,
			key,
			limit,
			actorID,
		); err != nil {
			t.Fatalf("set media integration quota %s: %v", key, err)
		}
	}
}

func insertMediaSpaceMember(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	userID uuid.UUID,
	inviterID uuid.UUID,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_space_members (
    tenant_id, space_id, user_id, status, invited_by
) VALUES ($1, $2, $3, 'active', $4)`, tenantID, spaceID, userID, inviterID); err != nil {
		t.Fatalf("insert explicit media space member: %v", err)
	}
}

func assertMediaSourceRowCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	sessionID uuid.UUID,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*)
FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND source_class_session_id = $2`, tenantID, sessionID).Scan(&count); err != nil {
		t.Fatalf("count media spaces for source: %v", err)
	}
	if count != want {
		t.Fatalf("media spaces for source = %d, want %d", count, want)
	}
}

func assertMediaInstanceAndReceiptCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	idempotencyKey string,
	wantInstances int,
	wantReceipts int,
) {
	t.Helper()
	var instances, receipts int
	if err := pool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_room_instances
     WHERE tenant_id = $1 AND space_id = $2),
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts
     WHERE tenant_id = $1 AND space_id = $2 AND idempotency_key = $3)`,
		tenantID,
		spaceID,
		idempotencyKey,
	).Scan(&instances, &receipts); err != nil {
		t.Fatalf("count media instances and receipts: %v", err)
	}
	if instances != wantInstances || receipts != wantReceipts {
		t.Fatalf(
			"media instance/receipt counts = %d/%d, want %d/%d",
			instances,
			receipts,
			wantInstances,
			wantReceipts,
		)
	}
}

func assertActorScopedMediaReceipts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	idempotencyKey string,
	actorIDs []uuid.UUID,
) {
	t.Helper()
	var count, distinctActors int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT actor_user_id)
FROM tutorhub.media_space_mutation_receipts
WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, idempotencyKey).Scan(
		&count,
		&distinctActors,
	); err != nil {
		t.Fatalf("count actor-scoped media receipts: %v", err)
	}
	if count != len(actorIDs) || distinctActors != len(actorIDs) {
		t.Fatalf(
			"actor-scoped receipt counts = total:%d actors:%d, want %d/%d",
			count,
			distinctActors,
			len(actorIDs),
			len(actorIDs),
		)
	}
	for _, actorID := range actorIDs {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM tutorhub.media_space_mutation_receipts
    WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3
)`, tenantID, actorID, idempotencyKey).Scan(&exists); err != nil {
			t.Fatalf("inspect actor-scoped media receipt: %v", err)
		}
		if !exists {
			t.Fatalf("missing actor-scoped media receipt for actor %s", actorID)
		}
	}
}

func assertMediaSourceRaceConsistency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	sessionID uuid.UUID,
) {
	t.Helper()
	var spaceStatus, sourceStatus string
	if err := pool.QueryRow(ctx, `SELECT space.status, session.status
FROM tutorhub.media_spaces AS space
JOIN tutorhub.class_sessions AS session
  ON session.tenant_id = space.tenant_id
 AND session.id = space.source_class_session_id
WHERE space.tenant_id = $1 AND space.id = $2 AND session.id = $3`,
		tenantID,
		spaceID,
		sessionID,
	).Scan(&spaceStatus, &sourceStatus); err != nil {
		t.Fatalf("read media/source race state: %v", err)
	}
	consistent := (spaceStatus == string(SpaceStatusOpen) &&
		sourceStatus == string(classroom.SessionStatusLive)) ||
		(spaceStatus == string(SpaceStatusScheduled) &&
			sourceStatus == string(classroom.SessionStatusCancelled))
	if !consistent {
		t.Fatalf("inconsistent media/source race state: space=%s source=%s", spaceStatus, sourceStatus)
	}
}

func assertMediaPrivacyAndProviderIsolation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture mediaIntegrationFixture,
) {
	t.Helper()
	var outboxText, auditText string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(string_agg(payload::text, ' '), '')
FROM tutorhub.outbox_events
WHERE tenant_id = $1
  AND (event_type LIKE 'media_space.%' OR event_type = 'study_meeting.scheduled.v1')`,
		fixture.tenantID,
	).Scan(&outboxText); err != nil {
		t.Fatalf("read media lifecycle outbox facts: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(string_agg(metadata::text, ' '), '')
FROM tutorhub.audit_events
WHERE tenant_id = $1
  AND (action LIKE 'media_space.%' OR action = 'study_meeting.create')`,
		fixture.tenantID,
	).Scan(&auditText); err != nil {
		t.Fatalf("read media lifecycle audit facts: %v", err)
	}
	var providerRoomName string
	if err := pool.QueryRow(ctx, `SELECT provider_room_name
FROM tutorhub.media_room_instances
WHERE tenant_id = $1
ORDER BY created_at, id
LIMIT 1`, fixture.tenantID).Scan(&providerRoomName); err != nil {
		t.Fatalf("read opaque provider room binding: %v", err)
	}
	combined := strings.ToLower(outboxText + " " + auditText)
	for _, forbidden := range []string{
		strings.ToLower(fixture.privateTitle),
		strings.ToLower(fixture.privateEmail),
		strings.ToLower(providerRoomName),
		"provider_room_name",
		"provider_room_sid",
		"participant_email",
		"access_token",
		"refresh_token",
	} {
		if forbidden != "" && strings.Contains(combined, forbidden) {
			t.Fatalf("media audit/outbox leaked forbidden value or field %q", forbidden)
		}
	}
	if !strings.Contains(combined, "safety_action") ||
		!strings.Contains(combined, "safety_policy") {
		t.Fatal("admin safety end audit/outbox omitted bounded reason and safety marker")
	}

	var instances, providerSIDs, webhookEvents int
	if err := pool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_room_instances WHERE tenant_id = $1),
    (SELECT count(provider_room_sid) FROM tutorhub.media_room_instances WHERE tenant_id = $1),
    (SELECT count(*) FROM tutorhub.livekit_webhook_events WHERE tenant_id = $1)`,
		fixture.tenantID,
	).Scan(&instances, &providerSIDs, &webhookEvents); err != nil {
		t.Fatalf("inspect provider-side-effect evidence: %v", err)
	}
	if instances == 0 || providerSIDs != 0 || webhookEvents != 0 {
		t.Fatalf(
			"provider isolation = instances:%d provider_sids:%d webhooks:%d, want >0/0/0",
			instances,
			providerSIDs,
			webhookEvents,
		)
	}
}

func cleanupMediaIntegrationFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture mediaIntegrationFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.foreignTenantID, fixture.quotaTenantID},
	); err != nil {
		t.Errorf("delete media integration tenants: %v", err)
		return
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])`,
		fixture.userIDs,
	); err != nil {
		t.Errorf("delete media integration users: %v", err)
	}
}

func requireMediaIntegrationEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s is not configured; skipping PostgreSQL media lifecycle integration", key)
	}
	return value
}

func openMediaIntegrationPool(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("open media lifecycle integration database")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal("authenticate media lifecycle integration database")
	}
	return pool
}

func mediaIntegrationKey(prefix string) string {
	normalized := strings.NewReplacer(" ", "-", "/", "-", "_", "-").Replace(prefix)
	return normalized + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func mediaIntegrationSlug(prefix string) string {
	return prefix + "-" + strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
}

func mediaIntegrationClassCode() string {
	return "M4" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:10]
}
