//go:build integration

package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresAvailabilityPollOwnershipQuotaIsolationAndCapabilityLifecycle(t *testing.T) {
	migrationURL := requireCalendarEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireCalendarEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply availability poll migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read availability poll migration version: %v", err)
	}
	if version.Number < 23 || version.Dirty {
		t.Fatalf("unexpected availability poll migration version: %+v", version)
	}

	runtimePool := openCalendarPool(t, ctx, runtimeURL)
	defer runtimePool.Close()
	outer, err := runtimePool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin availability poll integration transaction: %v", err)
	}
	defer func() { _ = outer.Rollback(context.Background()) }()
	fixture := seedAvailabilityPollIntegrationFixture(t, ctx, outer)

	clock := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	protector, err := protecteddata.New(protecteddata.Config{
		Key: bytes.Repeat([]byte{0x73}, 32), KeyVersion: 23,
	})
	if err != nil {
		t.Fatalf("create availability poll protector: %v", err)
	}
	controls, err := featurecontrol.NewPostgresRepository(
		outer,
		15*time.Second,
		policy.NewEngine(),
		featurecontrol.NewDefaultCatalog(),
	)
	if err != nil {
		t.Fatalf("create availability poll feature controls: %v", err)
	}
	repository, err := NewPostgresAvailabilityPollRepository(
		outer,
		15*time.Second,
		policy.NewEngine(),
		controls,
		protector,
		unavailablePollOutcomeWriter{},
		"https://calendar.example.test",
		func() time.Time { return clock },
	)
	if err != nil {
		t.Fatalf("create availability poll repository: %v", err)
	}
	service, err := NewAvailabilityPollService(repository, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("create availability poll service: %v", err)
	}
	ownerScope := mustCalendarScope(t, fixture.tenantID, fixture.ownerID)

	createInput := CreateAvailabilityPollInput{
		Title:                  "Disposable availability gate",
		Description:            "Verifies PostgreSQL authorization and capability lifecycle.",
		Timezone:               "Asia/Ho_Chi_Minh",
		RangeStart:             "2026-08-03",
		RangeEnd:               "2026-08-03",
		WorkingDayStart:        "08:00",
		WorkingDayEnd:          "18:00",
		DurationMinutes:        60,
		SlotGranularityMinutes: 30,
		DeadlineAt:             clock.Add(12 * time.Hour),
		ShareMode:              PollShareAnyoneWithLink,
		Slots: []AvailabilityPollSlotInput{{
			StartsAt: time.Date(2026, time.August, 3, 2, 0, 0, 0, time.UTC),
			EndsAt:   time.Date(2026, time.August, 3, 3, 0, 0, 0, time.UTC),
		}},
		Participants:   []AvailabilityPollParticipantInput{},
		IdempotencyKey: uuid.NewString(),
	}
	poll, err := service.CreatePoll(ctx, ownerScope, createInput)
	if err != nil {
		t.Fatalf("create owner poll: %v", err)
	}
	replayed, err := service.CreatePoll(ctx, ownerScope, createInput)
	if err != nil || replayed.ID != poll.ID || replayed.Version != poll.Version {
		t.Fatalf("replay owner poll: poll=%+v err=%v", replayed, err)
	}
	secondInput := createInput
	secondInput.Title = "Quota must reject this poll"
	secondInput.IdempotencyKey = uuid.NewString()
	if _, err := service.CreatePoll(ctx, ownerScope, secondInput); !errors.Is(
		err, featurecontrol.ErrQuotaExceeded,
	) {
		t.Fatalf("second active poll error = %v, want quota exceeded", err)
	}

	memberScope := mustCalendarScope(t, fixture.tenantID, fixture.memberID)
	if _, err := service.GetPoll(ctx, memberScope, poll.ID); !errors.Is(
		err, ErrAvailabilityPollNotFound,
	) {
		t.Fatalf("uninvited member read error = %v, want concealed not found", err)
	}
	memberPolls, err := service.ListPolls(ctx, memberScope, ListAvailabilityPollsInput{})
	if err != nil || len(memberPolls) != 0 {
		t.Fatalf("uninvited member list = %+v, err=%v, want empty", memberPolls, err)
	}
	foreignScope := mustCalendarScope(t, fixture.foreignTenantID, fixture.foreignOwnerID)
	if _, err := service.GetPoll(ctx, foreignScope, poll.ID); !errors.Is(
		err, ErrAvailabilityPollNotFound,
	) {
		t.Fatalf("foreign tenant read error = %v, want concealed not found", err)
	}
	foreignPolls, err := service.ListPolls(ctx, foreignScope, ListAvailabilityPollsInput{})
	if err != nil || len(foreignPolls) != 0 {
		t.Fatalf("foreign tenant list = %+v, err=%v, want empty", foreignPolls, err)
	}

	updateInput := UpdateAvailabilityPollInput{
		CreateAvailabilityPollInput: createInput,
		ExpectedVersion:             poll.Version,
	}
	updateInput.Title = "Updated disposable availability gate"
	previousVersion := poll.Version
	poll, err = service.UpdatePoll(ctx, ownerScope, poll.ID, updateInput)
	if err != nil || poll.Title != updateInput.Title || poll.Version != previousVersion+1 {
		t.Fatalf("update poll: poll=%+v err=%v", poll, err)
	}
	previousVersion = poll.Version
	poll, err = service.OpenPoll(ctx, ownerScope, poll.ID, poll.Version)
	if err != nil || poll.Status != PollStatusOpen || poll.Version != previousVersion+1 {
		t.Fatalf("open poll: poll=%+v err=%v", poll, err)
	}
	previousVersion = poll.Version
	firstCapability, err := service.CreateCapability(
		ctx,
		ownerScope,
		poll.ID,
		CreateAvailabilityPollCapabilityInput{
			Scope: PollCapabilityPublic, ExpiresAt: clock.Add(2 * time.Hour),
			ExpectedVersion: poll.Version,
		},
	)
	if err != nil {
		t.Fatalf("create first public capability: %v", err)
	}
	assertPersistedPollCapabilityDigest(
		t, ctx, outer, protector, firstCapability,
	)
	firstExchange, err := service.ResolvePublic(
		ctx, poll.PublicID, firstCapability.RawToken,
	)
	if err != nil || firstExchange.Poll.PublicID != poll.PublicID ||
		firstExchange.Poll.Status != PollStatusOpen {
		t.Fatalf("resolve first public capability: exchange=%+v err=%v", firstExchange, err)
	}
	assertPublicPollProjectionPrivacy(t, firstExchange, firstCapability.RawToken)

	poll, err = service.GetPoll(ctx, ownerScope, poll.ID)
	if err != nil || poll.Version != previousVersion+1 {
		t.Fatalf("reload poll after first capability: poll=%+v err=%v", poll, err)
	}
	previousVersion = poll.Version
	secondCapability, err := service.CreateCapability(
		ctx,
		ownerScope,
		poll.ID,
		CreateAvailabilityPollCapabilityInput{
			Scope: PollCapabilityPublic, ExpiresAt: clock.Add(3 * time.Hour),
			ExpectedVersion: poll.Version,
		},
	)
	if err != nil {
		t.Fatalf("create second public capability: %v", err)
	}
	poll, err = service.GetPoll(ctx, ownerScope, poll.ID)
	if err != nil || poll.Version != previousVersion+1 {
		t.Fatalf("reload poll after second capability: poll=%+v err=%v", poll, err)
	}
	if _, err := service.CreateCapability(
		ctx,
		ownerScope,
		poll.ID,
		CreateAvailabilityPollCapabilityInput{
			Scope: PollCapabilityPublic, ExpiresAt: clock.Add(4 * time.Hour),
			ExpectedVersion: poll.Version,
		},
	); !errors.Is(err, featurecontrol.ErrQuotaExceeded) {
		t.Fatalf("third capability error = %v, want rate quota exceeded", err)
	}

	revoked, err := service.RevokeCapability(
		ctx,
		ownerScope,
		poll.ID,
		firstCapability.Capability.ID,
		poll.Version,
		"Disposable lifecycle gate",
	)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("revoke first capability: capability=%+v err=%v", revoked, err)
	}
	if _, err := service.ResolvePublic(
		ctx, poll.PublicID, firstCapability.RawToken,
	); !errors.Is(err, ErrAvailabilityPollCapabilityUnavailable) {
		t.Fatalf("revoked capability error = %v, want uniformly unavailable", err)
	}
	if _, err := service.ResolvePublic(
		ctx, poll.PublicID, "v23.invalid-capability-token",
	); !errors.Is(err, ErrAvailabilityPollCapabilityUnavailable) {
		t.Fatalf("invalid capability error = %v, want uniformly unavailable", err)
	}
	clock = clock.Add(4 * time.Hour)
	if _, err := service.ResolvePublic(
		ctx, poll.PublicID, secondCapability.RawToken,
	); !errors.Is(err, ErrAvailabilityPollCapabilityUnavailable) {
		t.Fatalf("expired capability error = %v, want uniformly unavailable", err)
	}
	poll, err = service.GetPoll(ctx, ownerScope, poll.ID)
	if err != nil {
		t.Fatalf("reload poll before cancellation: %v", err)
	}
	previousVersion = poll.Version
	poll, err = service.CancelPoll(
		ctx, ownerScope, poll.ID, poll.Version, "Disposable lifecycle gate cancellation",
	)
	if err != nil || poll.Status != PollStatusCancelled || poll.Version != previousVersion+1 {
		t.Fatalf("cancel poll: poll=%+v err=%v", poll, err)
	}
}

type availabilityPollIntegrationFixture struct {
	tenantID        uuid.UUID
	ownerID         uuid.UUID
	memberID        uuid.UUID
	foreignTenantID uuid.UUID
	foreignOwnerID  uuid.UUID
}

func seedAvailabilityPollIntegrationFixture(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
) availabilityPollIntegrationFixture {
	t.Helper()
	fixture := availabilityPollIntegrationFixture{
		tenantID: uuid.New(), ownerID: uuid.New(), memberID: uuid.New(),
		foreignTenantID: uuid.New(), foreignOwnerID: uuid.New(),
	}
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	for index, user := range []struct {
		id    uuid.UUID
		label string
	}{
		{id: fixture.ownerID, label: "owner"},
		{id: fixture.memberID, label: "member"},
		{id: fixture.foreignOwnerID, label: "foreign"},
	} {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.users (id, email, display_name, locale, timezone)
VALUES ($1, $2, $3, 'vi', 'Asia/Ho_Chi_Minh')`,
			user.id,
			fmt.Sprintf("poll-gate-%d-%s@example.test", index, unique),
			"Poll gate "+user.label,
		); err != nil {
			t.Fatalf("insert availability poll user %s: %v", user.label, err)
		}
	}
	for _, tenant := range []struct {
		id    uuid.UUID
		label string
	}{
		{id: fixture.tenantID, label: "primary"},
		{id: fixture.foreignTenantID, label: "foreign"},
	} {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, $3)`,
			tenant.id,
			"poll-gate-"+tenant.label+"-"+unique,
			"Poll gate "+tenant.label,
		); err != nil {
			t.Fatalf("insert availability poll tenant %s: %v", tenant.label, err)
		}
	}
	for _, membership := range []struct {
		tenantID uuid.UUID
		userID   uuid.UUID
		role     string
	}{
		{tenantID: fixture.tenantID, userID: fixture.ownerID, role: "teacher"},
		{tenantID: fixture.tenantID, userID: fixture.memberID, role: "student"},
		{tenantID: fixture.foreignTenantID, userID: fixture.foreignOwnerID, role: "teacher"},
	} {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, $3, 'active', $4)`,
			membership.tenantID,
			membership.userID,
			membership.role,
			time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		); err != nil {
			t.Fatalf("insert availability poll membership: %v", err)
		}
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_feature_control_revisions (
    tenant_id, version, updated_by, updated_at
) VALUES ($1, 1, $2, $3)`,
		fixture.tenantID,
		fixture.ownerID,
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed availability poll feature-control revision: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_quota_overrides (
    tenant_id, quota_key, limit_value, updated_by, created_at, updated_at
) VALUES
    ($1, $4, 1, $2, $3, $3),
    ($1, $5, 2, $2, $3, $3)`,
		fixture.tenantID,
		fixture.ownerID,
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		featurecontrol.QuotaActiveAvailabilityPolls,
		featurecontrol.QuotaAvailabilityPollCapabilityCreationsPerHour,
	); err != nil {
		t.Fatalf("seed availability poll quota overrides: %v", err)
	}
	return fixture
}

func assertPersistedPollCapabilityDigest(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	protector *protecteddata.Protector,
	secret AvailabilityPollCapabilitySecret,
) {
	t.Helper()
	wantVersion, wantDigest, err := digestPollCapabilityToken(protector, secret.RawToken)
	if err != nil {
		t.Fatalf("digest issued poll capability: %v", err)
	}
	var storedVersion int16
	var storedDigest []byte
	if err := transaction.QueryRow(
		ctx,
		`SELECT token_version, token_digest
FROM tutorhub.availability_poll_capabilities
WHERE id = $1 AND purpose = 'poll_access'`,
		secret.Capability.ID,
	).Scan(&storedVersion, &storedDigest); err != nil {
		t.Fatalf("read persisted poll capability digest: %v", err)
	}
	if storedVersion != wantVersion || len(storedDigest) != 32 ||
		!bytes.Equal(storedDigest, wantDigest[:]) || bytes.Contains(storedDigest, []byte(secret.RawToken)) {
		t.Fatal("persisted poll capability is not the expected 32-byte versioned digest")
	}
	var rawTokenColumns int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*)
FROM information_schema.columns
WHERE table_schema = 'tutorhub'
  AND table_name = 'availability_poll_capabilities'
  AND column_name IN ('raw_token', 'token', 'secret')`,
	).Scan(&rawTokenColumns); err != nil {
		t.Fatalf("inspect poll capability columns: %v", err)
	}
	if rawTokenColumns != 0 {
		t.Fatalf("poll capability table exposes %d raw-token columns", rawTokenColumns)
	}
}

func assertPublicPollProjectionPrivacy(
	t *testing.T,
	exchange PublicAvailabilityPollExchange,
	accessToken string,
) {
	t.Helper()
	contents, err := json.Marshal(exchange)
	if err != nil {
		t.Fatalf("marshal public availability poll exchange: %v", err)
	}
	for _, forbidden := range [][]byte{
		[]byte(`"tenant_id"`),
		[]byte(`"owner_user_id"`),
		[]byte(`"individual_responses"`),
		[]byte(accessToken),
	} {
		if bytes.Contains(contents, forbidden) {
			t.Fatal("public availability poll exchange leaked a forbidden private field")
		}
	}
}

type unavailablePollOutcomeWriter struct{}

func (unavailablePollOutcomeWriter) CreatePollOutcomeInTransaction(
	context.Context,
	pgx.Tx,
	tenancy.Context,
	uuid.UUID,
	ClassSessionOutcomeInput,
) (uuid.UUID, error) {
	return uuid.Nil, errors.New("class-session outcome is outside this integration gate")
}

var _ ClassSessionOutcomeWriter = unavailablePollOutcomeWriter{}
