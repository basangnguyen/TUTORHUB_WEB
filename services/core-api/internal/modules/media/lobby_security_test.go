package media

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestP404IntegrationGateCannotSilentlySkipACLOrDatabaseSlice(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Join("..", "..", "..", "..", "..")
	paths := []string{
		filepath.Join(repositoryRoot, "scripts", "require-media-disposable-confirm.mjs"),
		filepath.Join(repositoryRoot, ".github", "workflows", "ci.yml"),
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(contents)
		for _, required := range []string{
			"P4_02_DISPOSABLE_CONFIRM",
			"I_UNDERSTAND_P4_02_DISPOSABLE_ONLY",
			"P4_04_DISPOSABLE_CONFIRM",
			"I_UNDERSTAND_P4_04_DISPOSABLE_ONLY",
			"P4_04_ACL_PROVISION_CONFIRM",
			"I_UNDERSTAND_P4_04_ACL_PROVISION_DISPOSABLE_ONLY",
		} {
			if !strings.Contains(source, required) {
				t.Fatalf("P4-04 integration gate %s is missing %s", path, required)
			}
		}
	}

	packageContents, err := os.ReadFile(filepath.Join(repositoryRoot, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	packageSource := string(packageContents)
	if !strings.Contains(packageSource, "node scripts/require-media-disposable-confirm.mjs") {
		t.Fatal("P4-04 package integration sequence bypasses the disposable confirmation guard")
	}
	p404DatabaseGate := strings.Index(
		packageSource,
		`-run \"^TestPostgresMediaLobbyAdmissionInviteRaceAndRestoreBarrier$\"`,
	)
	aclGate := strings.Index(
		packageSource,
		`-run \"^TestProvisionPostgresMediaLifecycleRuntimeExactACL$\"`,
	)
	if aclGate < 0 || p404DatabaseGate < aclGate {
		t.Fatal("P4-04 package integration sequence does not provision ACL before its database gate")
	}

	workflowContents, err := os.ReadFile(filepath.Join(repositoryRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflowSource := string(workflowContents)
	fixtureBoundary := strings.Index(
		workflowSource,
		`DATABASE_POOL_URL="$DATABASE_MEDIA_FIXTURE_TEST_URL" go test -count=1 -tags=integration`,
	)
	workflowLobbyGate := strings.Index(
		workflowSource,
		`-run '^TestPostgresMediaLobbyAdmissionInviteRaceAndRestoreBarrier$'`,
	)
	if fixtureBoundary < 0 || workflowLobbyGate < fixtureBoundary {
		t.Fatal("P4-04 CI lobby database gate does not use the isolated non-superuser fixture role")
	}
}

func TestP404ACLProvisioningIsVersionPinnedAndFreshlyOptedIn(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("postgres_acl_provision_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		`P4_04_ACL_PROVISION_CONFIRM`,
		`I_UNDERSTAND_P4_04_ACL_PROVISION_DISPOSABLE_ONLY`,
		`P4_04_SHARED_ACL_PROVISION_CONFIRM`,
		`I_UNDERSTAND_P4_04_ACL_PROVISION_SHARED_STAGING_ONLY`,
		`P4_04_SHARED_CONFIRM`,
		`p404SharedPreflightConfirmation`,
		`shared ACL provisioning refuses disposable confirmations`,
		`requireP404SharedIntegrationEnvironment`,
		`requireP404NeonURLBoundary`,
		`if version != 31 || dirty`,
		`P4-04 ACL provisioning requires ledger 31 false`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-04 ACL provisioning is missing safety boundary %q", required)
		}
	}
	for _, stale := range []string{
		`P4_02_ACL_PROVISION_CONFIRM`,
		`P4_02_SHARED_ACL_PROVISION_CONFIRM`,
		`P4_02_SHARED_CONFIRM`,
		`if version != 30 || dirty`,
	} {
		if strings.Contains(source, stale) {
			t.Fatalf("P4-04 ACL provisioning still accepts stale boundary %q", stale)
		}
	}
}

func TestP404SharedACLProbeIsReadOnlyAndFailClosed(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("postgres_lifecycle_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		`TestPostgresMediaLifecycleRuntimeExactACLSharedReadOnly`,
		`P4_04_SHARED_CONFIRM`,
		`P4_04_SHARED_ACL_PROVISION_CONFIRM`,
		`shared read-only ACL probe refuses disposable confirmations`,
		`runPostgresMediaLifecycleRuntimeExactACL(t, false)`,
		`if applyMigrations`,
		`AccessMode: pgx.ReadOnly`,
		`SHOW transaction_read_only`,
		`exact media lifecycle ACL requires ledger 31 false`,
		`pg_default_acl`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-04 shared read-only ACL probe is missing safety boundary %q", required)
		}
	}
}

func TestP404SharedSnapshotIsReadOnlyMinimalAndFailClosed(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("p404_shared_snapshot_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		`TestPostgresP404SharedReadOnlySnapshot`,
		`P4_04_SHARED_CONFIRM`,
		`P4_04_SHARED_SNAPSHOT_CONFIRM`,
		`I_UNDERSTAND_P4_04_SHARED_READ_ONLY_SNAPSHOT`,
		`p404AnyConflictingSharedConfirmationIsSet`,
		`AccessMode: pgx.ReadOnly`,
		`IsoLevel:   pgx.RepeatableRead`,
		`SHOW transaction_read_only`,
		`P4-04 shared read-only snapshot requires ledger 31 false`,
		`P4-04 shared read-only snapshot found enabled media feature overrides`,
		`media_outbox`,
		`media_audit`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-04 shared snapshot is missing safety boundary %q", required)
		}
	}
	for _, forbidden := range []string{
		`migrationrunner`,
		`INSERT INTO`,
		`UPDATE tutorhub`,
		`DELETE FROM`,
		`TRUNCATE`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("P4-04 shared snapshot contains mutation boundary %q", forbidden)
		}
	}
}

func TestP405SharedReadOnlyHarnessIsMinimalAndFailClosed(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("p405_shared_read_only_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		`TestPostgresP405SharedReadOnlySnapshot`,
		`TestPostgresMediaLifecycleRuntimeExactACLP405SharedReadOnly`,
		`I_UNDERSTAND_P4_05_SHARED_READ_ONLY`,
		`I_UNDERSTAND_P4_05_SHARED_READ_ONLY_SNAPSHOT`,
		`p405SharedConflictingConfirmations`,
		`"P4_05_DISPOSABLE_CONFIRM"`,
		`"P4_05_PROVIDER_CONFIRM"`,
		`"P4_05_BROWSER_PROVIDER_CONFIRM"`,
		`"P4_05_ACL_PROVISION_CONFIRM"`,
		`"P4_05_SHARED_ACL_PROVISION_CONFIRM"`,
		`beginP405ReadOnlyTransaction`,
		`requireP404NeonURLBoundary`,
		`runPostgresMediaLifecycleRuntimeExactACL(t, false)`,
		`if version != 31 || dirty`,
		`if counts[10] != 0`,
		`ValueSourceDeploymentGuardrail`,
		`P4_05_SHARED_SNAPSHOT`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-05 shared read-only harness is missing safety boundary %q", required)
		}
	}
	for _, forbidden := range []string{
		`migrationrunner`,
		`runProvisionPostgres`,
		`.Exec(`,
		`INSERT INTO`,
		`UPDATE tutorhub`,
		`DELETE FROM`,
		`TRUNCATE`,
		`NewLiveKitRoomProvider`,
		`LIVEKIT_URL`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("P4-05 shared read-only harness contains forbidden boundary %q", forbidden)
		}
	}
}

func TestP404DisposableOwnerPreflightIsPinnedAndFreshlyOptedIn(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("p404_disposable_preflight_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		`P4_04_DISPOSABLE_CONFIRM`,
		`p404DisposableConfirmation`,
		`P4_04_OWNER_PREFLIGHT`,
		`I_UNDERSTAND_P4_04_OWNER_PREFLIGHT_ONLY`,
		`P4_04_SHARED_CONFIRM`,
		`I_UNDERSTAND_P4_04_SHARED_STAGING_ONLY`,
		`TestPostgresP404SharedOwnerPreflight`,
		`shared owner preflight refuses disposable confirmations`,
		`p404AnyConflictingSharedConfirmationIsSet`,
		`requireP404SharedIntegrationEnvironment`,
		`P4_02_SHARED_CONFIRM`,
		`if version != 30 || dirty`,
		`P4-04 owner preflight requires ledger 30 false`,
		`migrationPooled || !runtimePooled`,
		`migrationEndpoint != runtimeEndpoint`,
		`pg_default_acl`,
		`defaults.defaclobjtype = 'r'`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-04 owner preflight is missing safety boundary %q", required)
		}
	}
	for _, stale := range []string{
		`P4_02_OWNER_PREFLIGHT`,
		`I_UNDERSTAND_P4_02_OWNER_PREFLIGHT_ONLY`,
		`if version != 29 || dirty`,
	} {
		if strings.Contains(source, stale) {
			t.Fatalf("P4-04 owner preflight still accepts stale boundary %q", stale)
		}
	}
}

func TestLobbyAdmissionStatusVocabularySeparatesStateFromRestoreEvent(t *testing.T) {
	t.Parallel()

	statuses := []LobbyAdmissionStatus{
		LobbyAdmissionWaiting,
		LobbyAdmissionAdmitted,
		LobbyAdmissionDenied,
		LobbyAdmissionCancelled,
		LobbyAdmissionMeetingEnded,
		LobbyAdmissionTimeout,
		LobbyAdmissionProviderUnavailable,
	}
	want := []string{
		"waiting", "admitted", "denied", "cancelled",
		"meeting_ended", "timeout", "provider_unavailable",
	}
	if len(statuses) != len(want) {
		t.Fatalf("lobby admission status count = %d, want %d", len(statuses), len(want))
	}
	seen := make(map[LobbyAdmissionStatus]struct{}, len(statuses))
	for index, status := range statuses {
		if string(status) != want[index] {
			t.Fatalf("lobby admission status %d = %q, want %q", index, status, want[index])
		}
		if status == "restored" {
			t.Fatal("restore is an event/command, not a public admission state")
		}
		if _, duplicated := seen[status]; duplicated {
			t.Fatalf("duplicate lobby admission status %q", status)
		}
		seen[status] = struct{}{}
	}
}

func TestLobbyPublicProjectionsExposeOnlyReviewedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		model  any
		fields []string
	}{
		{
			name:  "admission",
			model: LobbyAdmission{},
			fields: []string{
				"id", "status", "version", "display_name", "created_at", "expires_at",
			},
		},
		{
			name:  "member",
			model: LobbyMember{},
			fields: []string{
				"user_id", "display_name", "status", "version", "created_at", "updated_at",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			typeOf := reflect.TypeOf(test.model)
			got := make([]string, 0, typeOf.NumField())
			for index := 0; index < typeOf.NumField(); index++ {
				field := typeOf.Field(index)
				jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
				if jsonName == "" || jsonName == "-" {
					t.Fatalf("lobby %s field %s lacks an explicit public JSON name", test.name, field.Name)
				}
				got = append(got, jsonName)
			}
			if !reflect.DeepEqual(got, test.fields) {
				t.Fatalf("lobby %s projection fields = %v, want exact reviewed fields %v", test.name, got, test.fields)
			}
		})
	}
}

func TestLobbyProjectionJSONDoesNotLeakAdmissionInternals(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(struct {
		Admissions LobbyAdmissionPage `json:"admissions"`
		Members    LobbyMemberPage    `json:"members"`
	}{
		Admissions: LobbyAdmissionPage{Items: []LobbyAdmission{{
			ID:     uuid.MustParse("2145d3c6-1e70-451b-8bb6-4481a2172a4d"),
			Status: LobbyAdmissionWaiting, Version: 2, DisplayName: "Learner",
			CreatedAt: time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.August, 10, 8, 10, 0, 0, time.UTC),
		}}},
		Members: LobbyMemberPage{Items: []LobbyMember{{
			UserID:      uuid.MustParse("16fc6f01-b6d7-4aa8-925a-68cd74ca9195"),
			DisplayName: "Learner", Status: LobbyMemberActive, Version: 1,
			CreatedAt: time.Date(2026, time.August, 10, 7, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, time.August, 10, 7, 0, 0, 0, time.UTC),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	projection := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"email", "provider", "participant_session", "session_id", "join_attempt", "token",
	} {
		if strings.Contains(projection, forbidden) {
			t.Fatalf("lobby projection leaked forbidden field class %q", forbidden)
		}
	}
}
