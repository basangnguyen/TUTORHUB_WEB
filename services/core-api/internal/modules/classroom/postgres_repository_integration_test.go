//go:build integration

package classroom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresRepositoryClassLifecycleAndTenantIsolation(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 9 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}

	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create integration pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping integration database: %v", err)
	}

	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin integration transaction: %v", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	tenantA, ownerA := seedTenantOwner(t, ctx, transaction, "a")
	tenantB, ownerB := seedTenantOwner(t, ctx, transaction, "b")
	nextOwner := seedTenantMember(t, ctx, transaction, tenantA, "next-owner", "teacher")
	ineligibleOwner := seedTenantMember(t, ctx, transaction, tenantA, "student-owner", "student")
	enrolledStudent := seedTenantMember(t, ctx, transaction, tenantA, "enrolled-student", "student")
	unenrolledStudent := seedTenantMember(t, ctx, transaction, tenantA, "unenrolled-student", "student")
	contextA := mustTenantContext(t, tenantA, ownerA)
	contextB := mustTenantContext(t, tenantB, ownerB)
	nextOwnerContext := mustTenantContext(t, tenantA, nextOwner)
	repository := NewPostgresRepository(
		transaction,
		10*time.Second,
		policy.NewEngine(),
	)
	classCode := "SEC" + strings.ToUpper(uuid.NewString()[:8])

	created, err := repository.Create(ctx, contextA, CreateClassParams{
		OwnerUserID: ownerA,
		Code:        classCode,
		Title:       "Kiến trúc an toàn thông tin",
		Description: "Integration fixture",
	})
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	if created.TenantID != tenantA ||
		created.Code != classCode ||
		created.Timezone != "Asia/Ho_Chi_Minh" ||
		created.Status != ClassStatusDraft ||
		created.Version != 1 {
		t.Fatalf("unexpected class: %+v", created)
	}

	active := ClassStatusActive
	updatedTitle := "Kiến trúc an toàn nâng cao"
	updated, err := repository.Update(
		ctx,
		contextA,
		created.ID,
		UpdateClassParams{
			Title:           &updatedTitle,
			Status:          &active,
			ExpectedVersion: 1,
		},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("activate class: %v", err)
	}
	if updated.Status != ClassStatusActive ||
		updated.Title != updatedTitle ||
		updated.Version != 2 {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := repository.Update(
		ctx,
		contextA,
		created.ID,
		UpdateClassParams{Title: &updatedTitle, ExpectedVersion: 1},
		time.Now().UTC(),
	); !errors.Is(err, ErrClassVersionConflict) {
		t.Fatalf("stale update must conflict, got %v", err)
	}

	archived, err := repository.Archive(
		ctx,
		contextA,
		created.ID,
		2,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("archive class: %v", err)
	}
	if archived.Status != ClassStatusArchived ||
		archived.ArchivedAt == nil ||
		archived.Version != 3 {
		t.Fatalf("unexpected archived class: %+v", archived)
	}

	transferred, err := repository.TransferOwnership(
		ctx,
		contextA,
		created.ID,
		TransferClassOwnershipParams{
			NewOwnerUserID:  nextOwner,
			ExpectedVersion: 3,
		},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("transfer archived class ownership: %v", err)
	}
	if transferred.OwnerUserID != nextOwner ||
		transferred.Status != ClassStatusArchived ||
		transferred.Version != 4 {
		t.Fatalf("unexpected ownership transfer: %+v", transferred)
	}

	restored, err := repository.Restore(
		ctx,
		nextOwnerContext,
		created.ID,
		4,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("restore active class: %v", err)
	}
	if restored.Status != ClassStatusActive ||
		restored.ArchivedAt != nil ||
		restored.Version != 5 {
		t.Fatalf("unexpected restored class: %+v", restored)
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships
SET role = 'student', updated_at = now()
WHERE tenant_id = $1 AND user_id = $2`,
		tenantA,
		nextOwner,
	); err != nil {
		t.Fatalf("demote current owner organization role: %v", err)
	}
	unchanged, err := repository.TransferOwnership(
		ctx,
		nextOwnerContext,
		created.ID,
		TransferClassOwnershipParams{
			NewOwnerUserID:  nextOwner,
			ExpectedVersion: 5,
		},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("same-owner transfer: %v", err)
	}
	if unchanged.Version != 5 || unchanged.OwnerUserID != nextOwner {
		t.Fatalf("same-owner transfer must be a no-op: %+v", unchanged)
	}
	if _, err := repository.TransferOwnership(
		ctx,
		nextOwnerContext,
		created.ID,
		TransferClassOwnershipParams{
			NewOwnerUserID:  ineligibleOwner,
			ExpectedVersion: 5,
		},
		time.Now().UTC(),
	); !errors.Is(err, ErrClassOwnerUnavailable) {
		t.Fatalf("member without class-create permission must be unavailable, got %v", err)
	}

	if _, err := repository.Archive(
		ctx,
		contextA,
		created.ID,
		5,
		time.Now().UTC(),
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("non-owner teacher must not archive, got %v", err)
	}
	if _, err := repository.TransferOwnership(
		ctx,
		nextOwnerContext,
		created.ID,
		TransferClassOwnershipParams{
			NewOwnerUserID:  ownerB,
			ExpectedVersion: 5,
		},
		time.Now().UTC(),
	); !errors.Is(err, ErrClassOwnerUnavailable) {
		t.Fatalf("cross-tenant owner target must be unavailable, got %v", err)
	}

	draftCode := "DRF" + strings.ToUpper(uuid.NewString()[:8])
	draftClass, err := repository.Create(ctx, contextA, CreateClassParams{
		OwnerUserID: ownerA,
		Code:        draftCode,
		Title:       "Draft class",
	})
	if err != nil {
		t.Fatalf("create draft class: %v", err)
	}
	draftArchived, err := repository.Archive(
		ctx,
		contextA,
		draftClass.ID,
		1,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("archive draft class: %v", err)
	}
	draftRestored, err := repository.Restore(
		ctx,
		contextA,
		draftClass.ID,
		draftArchived.Version,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("restore draft class: %v", err)
	}
	if draftRestored.Status != ClassStatusDraft || draftRestored.Version != 3 {
		t.Fatalf("draft restore must preserve prior state: %+v", draftRestored)
	}

	secondActiveCode := "ACT" + strings.ToUpper(uuid.NewString()[:8])
	secondActive, err := repository.Create(ctx, contextA, CreateClassParams{
		OwnerUserID: ownerA,
		Code:        secondActiveCode,
		Title:       "Second active class",
	})
	if err != nil {
		t.Fatalf("create second active class: %v", err)
	}
	secondActive, err = repository.Update(
		ctx,
		contextA,
		secondActive.ID,
		UpdateClassParams{Status: &active, ExpectedVersion: 1},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("activate second class: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		tenantA,
		created.ID,
		enrolledStudent,
		ownerA,
	); err != nil {
		t.Fatalf("insert active student enrollment: %v", err)
	}
	classService, err := NewService(repository, policy.NewEngine())
	if err != nil {
		t.Fatalf("create integrated classroom service: %v", err)
	}
	enrolledAccess := accessForOrganizationRole(
		tenantA,
		enrolledStudent,
		policy.OrganizationRoleStudent,
	)
	enrolledClass, err := classService.Get(ctx, enrolledAccess, created.ID)
	if err != nil {
		t.Fatalf("active enrollment must grant class detail: %v", err)
	}
	if enrolledClass.ViewerAccess.ClassRole == nil ||
		*enrolledClass.ViewerAccess.ClassRole != policy.ClassRoleStudent ||
		!enrolledClass.ViewerAccess.CanJoinRoom ||
		!enrolledClass.ViewerAccess.CanPublishMedia ||
		!enrolledClass.ViewerAccess.CanLeave {
		t.Fatalf("unexpected integrated viewer access: %+v", enrolledClass.ViewerAccess)
	}
	unenrolledAccess := accessForOrganizationRole(
		tenantA,
		unenrolledStudent,
		policy.OrganizationRoleStudent,
	)
	if _, err := classService.Get(ctx, unenrolledAccess, created.ID); !errors.Is(
		err,
		ErrClassAccessDenied,
	) {
		t.Fatalf("unenrolled student class detail must be denied, got %v", err)
	}
	enrolledPage, err := classService.List(ctx, enrolledAccess, ListClassesInput{Limit: 10})
	if err != nil {
		t.Fatalf("list enrolled student's classes: %v", err)
	}
	if len(enrolledPage.Items) != 1 || enrolledPage.Items[0].ID != created.ID {
		t.Fatalf("student list must contain only active enrollments: %+v", enrolledPage)
	}
	unenrolledPage, err := classService.List(ctx, unenrolledAccess, ListClassesInput{Limit: 10})
	if err != nil {
		t.Fatalf("list unenrolled student's classes: %v", err)
	}
	if len(unenrolledPage.Items) != 0 {
		t.Fatalf("unenrolled student list must be empty: %+v", unenrolledPage)
	}

	activeFilter := ClassStatusActive
	firstPage, err := repository.List(ctx, contextA, ListClassesParams{
		Status: &activeFilter,
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("list first active page: %v", err)
	}
	if len(firstPage.Items) != 1 || !firstPage.HasMore {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}
	secondPage, err := repository.List(ctx, contextA, ListClassesParams{
		Status: &activeFilter,
		Limit:  1,
		After: &ClassCursor{
			CreatedAt: firstPage.Items[0].CreatedAt,
			ID:        firstPage.Items[0].ID,
		},
	})
	if err != nil {
		t.Fatalf("list second active page: %v", err)
	}
	if len(secondPage.Items) != 1 ||
		secondPage.Items[0].ID == firstPage.Items[0].ID ||
		secondPage.Items[0].Status != ClassStatusActive {
		t.Fatalf("keyset pagination repeated or lost an item: %+v", secondPage)
	}

	loaded, err := repository.Get(ctx, contextA, created.ID)
	if err != nil || loaded.ID != created.ID {
		t.Fatalf("get class in tenant: class=%+v error=%v", loaded, err)
	}
	if _, err := repository.Get(ctx, contextB, created.ID); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant get must be hidden as not found, got %v", err)
	}
	crossTenantTitle := "Cross-tenant mutation"
	if _, err := repository.Update(
		ctx,
		contextB,
		created.ID,
		UpdateClassParams{
			Title:           &crossTenantTitle,
			ExpectedVersion: restored.Version,
		},
		time.Now().UTC(),
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant update must be hidden as not found, got %v", err)
	}
	if _, err := repository.Archive(
		ctx,
		contextB,
		created.ID,
		restored.Version,
		time.Now().UTC(),
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant archive must be hidden as not found, got %v", err)
	}
	if _, err := repository.TransferOwnership(
		ctx,
		contextB,
		created.ID,
		TransferClassOwnershipParams{
			NewOwnerUserID:  ownerB,
			ExpectedVersion: restored.Version,
		},
		time.Now().UTC(),
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant transfer must be hidden as not found, got %v", err)
	}
	classesB, err := repository.List(ctx, contextB, ListClassesParams{Limit: 10})
	if err != nil || len(classesB.Items) != 0 {
		t.Fatalf("tenant B must not see tenant A classes: classes=%+v error=%v", classesB, err)
	}

	var transferEvents, lifecycleEvents int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FILTER (WHERE event_type = 'class.ownership_transferred'),
                count(*) FILTER (
                    WHERE event_type IN ('class.updated', 'class.archived', 'class.restored')
                )
FROM tutorhub.outbox_events
WHERE aggregate_id = $1`,
		created.ID,
	).Scan(&transferEvents, &lifecycleEvents); err != nil {
		t.Fatalf("count class lifecycle events: %v", err)
	}
	if transferEvents != 1 || lifecycleEvents != 3 {
		t.Fatalf(
			"unexpected class events: transfers=%d lifecycle=%d",
			transferEvents,
			lifecycleEvents,
		)
	}

	err = runInSavepoint(t, ctx, transaction, "duplicate_code", func() error {
		_, createErr := repository.Create(ctx, contextA, CreateClassParams{
			OwnerUserID: ownerA,
			Code:        classCode,
			Title:       "Duplicate code",
		})
		return createErr
	})
	if !errors.Is(err, ErrDuplicateClassCode) {
		t.Fatalf("expected duplicate class code error, got %v", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships
SET status = 'suspended', updated_at = now()
WHERE tenant_id = $1 AND user_id = $2`,
		tenantA,
		ownerA,
	); err != nil {
		t.Fatalf("suspend mutation actor: %v", err)
	}
	inactiveActorTitle := "Must not be written"
	if _, err := repository.Update(
		ctx,
		contextA,
		secondActive.ID,
		UpdateClassParams{
			Title:           &inactiveActorTitle,
			ExpectedVersion: secondActive.Version,
		},
		time.Now().UTC(),
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("inactive membership snapshot must be re-authorized, got %v", err)
	}
}

func TestPostgresRepositoryConcurrentUpdateUsesOptimisticVersion(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin concurrent fixture: %v", err)
	}
	defer func() {
		_ = setup.Rollback(context.Background())
	}()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "concurrent")
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID)

	repository := NewPostgresRepository(pool, 20*time.Second, policy.NewEngine())
	tenantContext := mustTenantContext(t, tenantID, ownerID)
	created, err := repository.Create(ctx, tenantContext, CreateClassParams{
		OwnerUserID: ownerID,
		Code:        "CON" + strings.ToUpper(uuid.NewString()[:8]),
		Title:       "Concurrent class",
	})
	if err != nil {
		t.Fatalf("create concurrent class: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		attempt := attempt
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			title := fmt.Sprintf("Concurrent update %d", attempt)
			_, updateErr := repository.Update(
				ctx,
				tenantContext,
				created.ID,
				UpdateClassParams{Title: &title, ExpectedVersion: 1},
				time.Now().UTC(),
			)
			results <- updateErr
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var successes, conflicts int
	for updateErr := range results {
		switch {
		case updateErr == nil:
			successes++
		case errors.Is(updateErr, ErrClassVersionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent update error: %v", updateErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf(
			"concurrent update must have one winner: successes=%d conflicts=%d",
			successes,
			conflicts,
		)
	}

	loaded, err := repository.Get(ctx, tenantContext, created.ID)
	if err != nil {
		t.Fatalf("load concurrent class: %v", err)
	}
	if loaded.Version != 2 {
		t.Fatalf("concurrent update silently overwrote version: %+v", loaded)
	}
	var updateEvents int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'class.updated'`,
		created.ID,
	).Scan(&updateEvents); err != nil {
		t.Fatalf("count concurrent update events: %v", err)
	}
	if updateEvents != 1 {
		t.Fatalf("concurrent update duplicated event: %d", updateEvents)
	}
}

func runInSavepoint(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	name string,
	operation func() error,
) error {
	t.Helper()

	if _, err := transaction.Exec(ctx, "SAVEPOINT "+name); err != nil {
		t.Fatalf("create savepoint %s: %v", name, err)
	}
	operationErr := operation()
	if _, err := transaction.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name); err != nil {
		t.Fatalf("rollback savepoint %s: %v", name, err)
	}
	if _, err := transaction.Exec(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		t.Fatalf("release savepoint %s: %v", name, err)
	}

	return operationErr
}

func seedTenantOwner(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	suffix string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	tenantID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	email := fmt.Sprintf("integration-%s-%s@example.test", suffix, unique)
	// Keep the unique tenant slug independent from the human-readable fixture
	// label so a descriptive suffix cannot exceed the schema's 63-char limit.
	slug := "integration-" + unique

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name) VALUES ($1, $2, $3)`,
		userID,
		email,
		"Integration Owner "+suffix,
	); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID,
		slug,
		"Integration Tenant "+suffix,
	); err != nil {
		t.Fatalf("insert integration tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
)
VALUES ($1, $2, 'teacher', 'active', now())`,
		tenantID,
		userID,
	); err != nil {
		t.Fatalf("insert integration membership: %v", err)
	}

	return tenantID, userID
}

func seedTenantMember(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	suffix string,
	role string,
) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	email := fmt.Sprintf("integration-member-%s-%s@example.test", suffix, unique)
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name) VALUES ($1, $2, $3)`,
		userID,
		email,
		"Integration Member "+suffix,
	); err != nil {
		t.Fatalf("insert integration member: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
)
VALUES ($1, $2, $3, 'active', now())`,
		tenantID,
		userID,
		role,
	); err != nil {
		t.Fatalf("insert integration member membership: %v", err)
	}
	return userID
}

func cleanupClassIntegrationFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	userIDs ...uuid.UUID,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var retainedAudit bool
	if err := pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM tutorhub.audit_events WHERE tenant_id = $1)`,
		tenantID,
	).Scan(&retainedAudit); err != nil {
		t.Errorf("inspect retained class audit fixture: %v", err)
		return
	}
	// P2-07 intentionally prevents runtime deletion of tenant/user rows that own
	// audit history. CI databases are ephemeral and fixture identifiers are unique,
	// so an exercised audit fixture remains available for inspection.
	if retainedAudit {
		return
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id = $1`, tenantID); err != nil {
		t.Errorf("delete class integration tenant: %v", err)
	}
	if len(userIDs) > 0 {
		if _, err := pool.Exec(
			ctx,
			`DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])`,
			userIDs,
		); err != nil {
			t.Errorf("delete class integration users: %v", err)
		}
	}
}

func mustTenantContext(
	t *testing.T,
	tenantID uuid.UUID,
	actorID uuid.UUID,
) tenancy.Context {
	t.Helper()

	context, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create tenant context: %v", err)
	}
	return context
}

func requireEnvironment(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for integration tests", key)
	}
	return value
}
