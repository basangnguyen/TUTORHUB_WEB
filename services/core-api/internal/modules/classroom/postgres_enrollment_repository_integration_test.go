//go:build integration

package classroom

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresEnrollmentInviteUsageIsAtomicAndIdempotent(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 10 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create enrollment integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin enrollment fixture: %v", err)
	}
	defer func() {
		_ = setup.Rollback(context.Background())
	}()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "enrollment-race")
	sameUserID := seedTenantMember(t, ctx, setup, tenantID, "same", "student")
	firstUserID := seedTenantMember(t, ctx, setup, tenantID, "first", "student")
	secondUserID := seedTenantMember(t, ctx, setup, tenantID, "second", "student")
	revokedUserID := seedTenantMember(t, ctx, setup, tenantID, "revoked", "student")
	expiredUserID := seedTenantMember(t, ctx, setup, tenantID, "expired", "student")
	classID := uuid.New()
	if _, err := setup.Exec(
		ctx,
		`INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Enrollment race class', 'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"ER"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert active enrollment class: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit enrollment fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(
		t,
		pool,
		tenantID,
		ownerID,
		sameUserID,
		firstUserID,
		secondUserID,
		revokedUserID,
		expiredUserID,
	)

	repository := NewPostgresRepository(pool, 30*time.Second, policy.NewEngine())
	ownerContext := mustEnrollmentTenantContext(t, tenantID, ownerID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	tooShortHash := sha256.Sum256([]byte("too-short-" + uuid.NewString()))
	_, err = pool.Exec(
		ctx,
		`INSERT INTO tutorhub.class_invite_codes (
    tenant_id, class_id, code_hash, expires_at, usage_limit,
    created_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, 1, $5, $6, $6)`,
		tenantID,
		classID,
		tooShortHash[:],
		now.Add(time.Second),
		ownerID,
		now,
	)
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) ||
		constraintError.ConstraintName != "class_invite_codes_expiry_valid" {
		t.Fatalf("short invite TTL must fail the database constraint, got %v", err)
	}
	sameHash := sha256.Sum256([]byte("same-" + uuid.NewString()))
	sameCode, err := repository.CreateInviteCode(
		ctx,
		ownerContext,
		classID,
		CreateInviteCodeParams{
			CodeHash: sameHash[:], ExpiresAt: now.Add(time.Hour),
			UsageLimit: 2, CreatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("create same-user invite code: %v", err)
	}

	type joinResult struct {
		result JoinClassInvitationResult
		err    error
	}
	runConcurrentJoins := func(
		userIDs []uuid.UUID,
		hash []byte,
	) []joinResult {
		t.Helper()
		start := make(chan struct{})
		results := make(chan joinResult, len(userIDs))
		var waitGroup sync.WaitGroup
		for _, userID := range userIDs {
			userID := userID
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start
				tenantContext, contextErr := tenancy.New(tenantID, userID)
				if contextErr != nil {
					results <- joinResult{err: contextErr}
					return
				}
				result, joinErr := repository.JoinByInviteCode(
					ctx,
					tenantContext,
					hash,
					now.Add(time.Minute),
				)
				results <- joinResult{result: result, err: joinErr}
			}()
		}
		close(start)
		waitGroup.Wait()
		close(results)
		collected := make([]joinResult, 0, len(userIDs))
		for result := range results {
			collected = append(collected, result)
		}
		return collected
	}

	sameResults := runConcurrentJoins(
		[]uuid.UUID{sameUserID, sameUserID},
		sameHash[:],
	)
	var sameJoined int
	for _, result := range sameResults {
		if result.err != nil {
			t.Fatalf("same-user join must converge: %v", result.err)
		}
		if result.result.Joined {
			sameJoined++
		}
	}
	var sameUsage, sameEnrollments, sameJoinEvents int
	if err := pool.QueryRow(
		ctx,
		`SELECT usage_count FROM tutorhub.class_invite_codes WHERE id = $1`,
		sameCode.ID,
	).Scan(&sameUsage); err != nil {
		t.Fatalf("read same-user usage: %v", err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		tenantID,
		classID,
		sameUserID,
	).Scan(&sameEnrollments); err != nil {
		t.Fatalf("count same-user enrollment: %v", err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.outbox_events
WHERE aggregate_type = 'class_enrollment'
  AND event_type = 'class.enrollment.joined'
  AND payload ->> 'user_id' = $1`,
		sameUserID.String(),
	).Scan(&sameJoinEvents); err != nil {
		t.Fatalf("count same-user join events: %v", err)
	}
	if sameJoined != 1 || sameUsage != 1 || sameEnrollments != 1 || sameJoinEvents != 1 {
		t.Fatalf(
			"same-user join was not idempotent: joined=%d usage=%d enrollments=%d events=%d",
			sameJoined,
			sameUsage,
			sameEnrollments,
			sameJoinEvents,
		)
	}

	tracingHash := sha256.Sum256([]byte("limited-" + uuid.NewString()))
	limitedCode, err := repository.CreateInviteCode(
		ctx,
		ownerContext,
		classID,
		CreateInviteCodeParams{
			CodeHash: tracingHash[:], ExpiresAt: now.Add(time.Hour),
			UsageLimit: 1, CreatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("create limited invite code: %v", err)
	}
	limitedResults := runConcurrentJoins(
		[]uuid.UUID{firstUserID, secondUserID},
		tracingHash[:],
	)
	var limitedSuccesses, limitedUnavailable int
	for _, result := range limitedResults {
		switch {
		case result.err == nil && result.result.Joined:
			limitedSuccesses++
		case errors.Is(result.err, ErrClassInviteCodeUnavailable):
			limitedUnavailable++
		default:
			t.Fatalf("unexpected limited join result: %+v", result)
		}
	}
	var limitedUsage, limitedEnrollments, exhaustedEvents int
	var limitedStatus ClassInviteCodeStatus
	if err := pool.QueryRow(
		ctx,
		`SELECT usage_count, status FROM tutorhub.class_invite_codes WHERE id = $1`,
		limitedCode.ID,
	).Scan(&limitedUsage, &limitedStatus); err != nil {
		t.Fatalf("read limited usage: %v", err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2
  AND user_id = ANY($3::uuid[]) AND status = 'active'`,
		tenantID,
		classID,
		[]uuid.UUID{firstUserID, secondUserID},
	).Scan(&limitedEnrollments); err != nil {
		t.Fatalf("count limited enrollments: %v", err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'class.invite_code.exhausted'`,
		limitedCode.ID,
	).Scan(&exhaustedEvents); err != nil {
		t.Fatalf("count exhausted events: %v", err)
	}
	if limitedSuccesses != 1 || limitedUnavailable != 1 || limitedUsage != 1 ||
		limitedStatus != ClassInviteCodeStatusExhausted || limitedEnrollments != 1 ||
		exhaustedEvents != 1 {
		t.Fatalf(
			"usage limit was not atomic: success=%d unavailable=%d usage=%d status=%s enrollments=%d events=%d",
			limitedSuccesses,
			limitedUnavailable,
			limitedUsage,
			limitedStatus,
			limitedEnrollments,
			exhaustedEvents,
		)
	}

	revokedHash := sha256.Sum256([]byte("revoked-" + uuid.NewString()))
	revokedCode, err := repository.CreateInviteCode(
		ctx,
		ownerContext,
		classID,
		CreateInviteCodeParams{
			CodeHash: revokedHash[:], ExpiresAt: now.Add(time.Hour),
			UsageLimit: 2, CreatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("create revoked invite code: %v", err)
	}
	if _, err := repository.RevokeInviteCode(
		ctx,
		ownerContext,
		classID,
		revokedCode.ID,
		now.Add(time.Minute),
	); err != nil {
		t.Fatalf("revoke invite code: %v", err)
	}
	if _, err := repository.JoinByInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, revokedUserID),
		revokedHash[:],
		now.Add(2*time.Minute),
	); !errors.Is(err, ErrClassInviteCodeUnavailable) {
		t.Fatalf("revoked invite code must be unavailable, got %v", err)
	}

	expiredHash := sha256.Sum256([]byte("expired-" + uuid.NewString()))
	expiredCode, err := repository.CreateInviteCode(
		ctx,
		ownerContext,
		classID,
		CreateInviteCodeParams{
			CodeHash: expiredHash[:], ExpiresAt: now.Add(15 * time.Minute),
			UsageLimit: 2, CreatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("create expiring invite code: %v", err)
	}
	if _, err := repository.JoinByInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, expiredUserID),
		expiredHash[:],
		now.Add(16*time.Minute),
	); !errors.Is(err, ErrClassInviteCodeUnavailable) {
		t.Fatalf("expired invite code must be unavailable, got %v", err)
	}
	var expiredStatus ClassInviteCodeStatus
	var expiredEvents int
	if err := pool.QueryRow(
		ctx,
		`SELECT status FROM tutorhub.class_invite_codes WHERE id = $1`,
		expiredCode.ID,
	).Scan(&expiredStatus); err != nil {
		t.Fatalf("read expired invite status: %v", err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'class.invite_code.expired'`,
		expiredCode.ID,
	).Scan(&expiredEvents); err != nil {
		t.Fatalf("count expired invite events: %v", err)
	}
	if expiredStatus != ClassInviteCodeStatusExpired || expiredEvents != 1 {
		t.Fatalf(
			"expired invite transition mismatch: status=%s events=%d",
			expiredStatus,
			expiredEvents,
		)
	}
	var outboxPayloadViolations int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.outbox_events
WHERE tenant_id = $1
  AND (
      (
          aggregate_type = 'class_enrollment'
          AND payload - ARRAY[
              'class_id', 'user_id', 'actor_user_id',
              'class_role', 'status', 'source'
          ] <> '{}'::jsonb
      )
      OR (
          aggregate_type = 'class_invite_code'
          AND payload - ARRAY[
              'class_id', 'actor_user_id', 'status', 'expires_at',
              'usage_limit', 'usage_count'
          ] <> '{}'::jsonb
      )
  )`,
		tenantID,
	).Scan(&outboxPayloadViolations); err != nil {
		t.Fatalf("inspect class enrollment outbox payload allowlist: %v", err)
	}
	if outboxPayloadViolations != 0 {
		t.Fatalf("class enrollment outbox contains %d non-allowlisted payloads", outboxPayloadViolations)
	}
}

func TestPostgresInviteScopeAndArchivedLifecycle(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create archived lifecycle pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin archived fixture: %v", err)
	}
	defer func() {
		_ = setup.Rollback(context.Background())
	}()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "archived-invite")
	studentID := seedTenantMember(t, ctx, setup, tenantID, "archived-student", "student")
	otherTenantID, otherOwnerID := seedTenantOwner(t, ctx, setup, "other-invite")
	classID := uuid.New()
	if _, err := setup.Exec(
		ctx,
		`INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Archived invite class', 'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"AR"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert archived lifecycle class: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit archived lifecycle fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID, studentID)
	defer cleanupClassIntegrationFixture(t, pool, otherTenantID, otherOwnerID)

	repository := NewPostgresRepository(pool, 20*time.Second, policy.NewEngine())
	now := time.Now().UTC().Truncate(time.Microsecond)
	hash := sha256.Sum256([]byte("archived-" + uuid.NewString()))
	code, err := repository.CreateInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, ownerID),
		classID,
		CreateInviteCodeParams{
			CodeHash: hash[:], ExpiresAt: now.Add(time.Hour),
			UsageLimit: 2, CreatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("create archived lifecycle code: %v", err)
	}
	archiveGuardHash := sha256.Sum256([]byte("archive-guard-" + uuid.NewString()))
	archiveGuardCode, err := repository.CreateInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, ownerID),
		classID,
		CreateInviteCodeParams{
			CodeHash: archiveGuardHash[:], ExpiresAt: now.Add(time.Hour),
			UsageLimit: 2, CreatedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("create active archived-join guard code: %v", err)
	}
	joined, err := repository.JoinByInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, studentID),
		hash[:],
		now.Add(time.Minute),
	)
	if err != nil || !joined.Joined || joined.Enrollment == nil {
		t.Fatalf("join before archive: result=%+v error=%v", joined, err)
	}
	if _, err := repository.JoinByInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, otherTenantID, otherOwnerID),
		hash[:],
		now.Add(time.Minute),
	); !errors.Is(err, ErrClassInviteCodeUnavailable) {
		t.Fatalf("cross-tenant token must be unavailable, got %v", err)
	}
	var usageAfterCrossTenant, crossTenantEnrollments, crossTenantEvents int
	if err := pool.QueryRow(
		ctx,
		`SELECT
    (
        SELECT usage_count
        FROM tutorhub.class_invite_codes
        WHERE tenant_id = $1 AND id = $2
    ),
    (
        SELECT count(*)
        FROM tutorhub.class_enrollments
        WHERE tenant_id = $3 AND class_id = $4
    ),
    (
        SELECT count(*)
        FROM tutorhub.outbox_events
        WHERE tenant_id = $3
          AND aggregate_type IN ('class_enrollment', 'class_invite_code')
    )`,
		tenantID,
		code.ID,
		otherTenantID,
		classID,
	).Scan(
		&usageAfterCrossTenant,
		&crossTenantEnrollments,
		&crossTenantEvents,
	); err != nil {
		t.Fatalf("inspect cross-tenant invite invariants: %v", err)
	}
	if usageAfterCrossTenant != 1 || crossTenantEnrollments != 0 || crossTenantEvents != 0 {
		t.Fatalf(
			"cross-tenant invite mutated state: usage=%d enrollments=%d events=%d",
			usageAfterCrossTenant,
			crossTenantEnrollments,
			crossTenantEvents,
		)
	}
	if _, err := pool.Exec(
		ctx,
		`UPDATE tutorhub.classes
SET status = 'archived', archived_from_status = 'active',
    archived_at = $3, updated_at = $3, version = version + 1
WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		classID,
		now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("archive invite lifecycle class: %v", err)
	}
	listed, err := repository.ListInviteCodes(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, ownerID),
		classID,
		now.Add(3*time.Minute),
	)
	listedIDs := make(map[uuid.UUID]struct{}, len(listed))
	for _, listedCode := range listed {
		listedIDs[listedCode.ID] = struct{}{}
	}
	_, hasOriginalCode := listedIDs[code.ID]
	_, hasArchiveGuardCode := listedIDs[archiveGuardCode.ID]
	if err != nil || len(listed) != 2 || !hasOriginalCode || !hasArchiveGuardCode {
		t.Fatalf("list archived invite codes: codes=%+v error=%v", listed, err)
	}
	revoked, err := repository.RevokeInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, ownerID),
		classID,
		code.ID,
		now.Add(4*time.Minute),
	)
	if err != nil || revoked.Status != ClassInviteCodeStatusRevoked {
		t.Fatalf("revoke archived invite code: code=%+v error=%v", revoked, err)
	}
	left, err := repository.LeaveClass(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, studentID),
		classID,
		now.Add(5*time.Minute),
	)
	if err != nil || !left.Changed || left.Enrollment.Status != EnrollmentStatusLeft {
		t.Fatalf("leave archived class: result=%+v error=%v", left, err)
	}
	if _, err := repository.JoinByInviteCode(
		ctx,
		mustEnrollmentTenantContext(t, tenantID, studentID),
		archiveGuardHash[:],
		now.Add(6*time.Minute),
	); !errors.Is(err, ErrClassInviteCodeUnavailable) {
		t.Fatalf("archived class must reject new join, got %v", err)
	}
}

func mustEnrollmentTenantContext(
	t *testing.T,
	tenantID uuid.UUID,
	actorID uuid.UUID,
) tenancy.Context {
	t.Helper()
	tenantContext, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create enrollment tenant context: %v", err)
	}
	return tenantContext
}
