package media

import (
	"database/sql"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresInstanceRepositoryCredentialLimiterUsesOpaqueBucket(t *testing.T) {
	t.Parallel()

	if mediaCredentialRatePurpose != "media.join_credential" {
		t.Fatalf("credential limiter purpose = %q", mediaCredentialRatePurpose)
	}
	if mediaCredentialRateLimit != 30 {
		t.Fatalf("credential limiter limit = %d, want 30", mediaCredentialRateLimit)
	}
	if mediaCredentialRateWindow != 10*time.Minute {
		t.Fatalf("credential limiter window = %s, want 10m", mediaCredentialRateWindow)
	}

	source := postgresInstanceRepositorySource(t)
	section := postgresInstanceRepositorySection(
		t,
		source,
		"func consumeMediaCredentialRateLimit(",
		"type providerRoomBinding struct",
	)
	for _, fragment := range []string{
		"sha256.Sum256([]byte(",
		"mediaCredentialRatePurpose + \"\\x00\" + tenantID.String() + \"\\x00\" +",
		"actorID.String() + \"\\x00\" + sessionID.String()",
		"purpose, bucket_hash, window_started_at, window_ends_at, used_count, updated_at",
		"ON CONFLICT (purpose, bucket_hash, window_started_at)",
		"WHERE tutorhub.rate_limit_windows.used_count < $6",
		"mediaCredentialRatePurpose, bucketHash[:], windowStartedAt, windowEndsAt",
		"now, mediaCredentialRateLimit",
	} {
		if !strings.Contains(section, fragment) {
			t.Fatalf("credential limiter is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"mediaCredentialRatePurpose + tenantID.String()",
		"mediaCredentialRatePurpose + actorID.String()",
		"mediaCredentialRatePurpose + sessionID.String()",
		"tenantID, bucketHash[:]",
		"actorID, bucketHash[:]",
		"sessionID, bucketHash[:]",
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("credential limiter risks persisting a raw identifier via %q", forbidden)
		}
	}
}

func TestPostgresInstanceIntegrationMigrationFailureDoesNotLogDriverError(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("postgres_instance_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	if !strings.Contains(source, `t.Fatal("apply room-instance binding migrations")`) {
		t.Fatal("room-instance migration failure must use a stable redacted message")
	}
	if strings.Contains(source, `apply room-instance binding migrations: %v`) {
		t.Fatal("room-instance migration failure must not render the driver error")
	}
}

func TestOpaqueParticipantIdentityUsesIndependentProviderIdentifier(t *testing.T) {
	t.Parallel()

	applicationID := uuid.MustParse("c74d5a18-7b36-4b98-9a14-cf1c4bb9f9cf")
	identity := opaqueParticipantIdentity(applicationID)
	if !regexp.MustCompile(`^p_[0-9a-f]{32}$`).MatchString(identity) {
		t.Fatalf("provider participant identity %q is not opaque provider format", identity)
	}
	if identity == applicationID.String() || strings.Contains(identity, "-") {
		t.Fatalf("provider participant identity exposes raw application UUID %q", identity)
	}

	source := postgresInstanceRepositorySource(t)
	creation := postgresInstanceRepositorySection(
		t,
		source,
		"participant := participantRow{",
		"if participant.ID == uuid.Nil",
	)
	if !strings.Contains(
		creation,
		"ProviderIdentity: opaqueParticipantIdentity(repository.newID())",
	) {
		t.Fatal("participant identity must be generated from an independent opaque ID")
	}
	for _, rawID := range []string{
		"scope.TenantID",
		"scope.ActorID",
		"access.ActorID",
		"space.ID",
		"room.ID",
		"joinAttemptID",
	} {
		if strings.Contains(creation, "opaqueParticipantIdentity("+rawID+")") {
			t.Fatalf("participant identity derives from application identifier %s", rawID)
		}
	}
}

func TestRoomActivationReauthorizesStartCapability(t *testing.T) {
	t.Parallel()

	source := postgresInstanceRepositorySource(t)
	activation := postgresInstanceRepositorySection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) ActivateRoomInstance(",
		"func (repository *PostgresInstanceRepository) ProviderRoomName(",
	)
	if !strings.Contains(
		activation,
		"space, policy.ActionSessionStart, true, false",
	) {
		t.Fatal("provider activation must reload the actor's session-start authority")
	}
	if strings.Contains(activation, "space, policy.ActionClassView, false, false") {
		t.Fatal("provider activation must not reconcile using view-only authority")
	}
}

func TestInstanceControlLockPrecedesRowsWhileSpaceConcealsFeature(t *testing.T) {
	t.Parallel()

	source := postgresInstanceRepositorySource(t)
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{
			name:  "provider activation",
			start: "func (repository *PostgresInstanceRepository) ActivateRoomInstance(",
			end:   "func (repository *PostgresInstanceRepository) ProviderRoomName(",
		},
		{
			name:  "join credential",
			start: "func (repository *PostgresInstanceRepository) PrepareCredential(",
			end:   "func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
		},
		{
			name:  "join attempt",
			start: "func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
			end:   "func (repository *PostgresInstanceRepository) requireParticipantCapacity(",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			section := postgresInstanceRepositorySection(t, source, test.start, test.end)
			controlLock := strings.Index(section, "repository.lifecycle.acquireTenantControlLock(")
			activeScope := strings.Index(section, "repository.lifecycle.requireActiveScope(")
			spaceLookup := strings.Index(section, "space, err := loadSpace(")
			featureGate := strings.Index(section, "controls.RequireFeature(")
			if controlLock < 0 || activeScope < 0 || spaceLookup < 0 || featureGate < 0 {
				t.Fatal("room-instance transaction is missing a required lock or feature boundary")
			}
			if controlLock > activeScope || controlLock > spaceLookup {
				t.Fatal("tenant feature-control lock must precede membership and space row locks")
			}
			if spaceLookup > featureGate {
				t.Fatal("exact tenant space lookup must conceal foreign spaces before feature evaluation")
			}
		})
	}
}

func TestJoinAttemptOwnsParticipantCreationBeforeCredentialMint(t *testing.T) {
	t.Parallel()

	source := postgresInstanceRepositorySource(t)
	credential := postgresInstanceRepositorySection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) PrepareCredential(",
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
	)
	if strings.Contains(credential, "INSERT INTO tutorhub.media_participant_sessions") {
		t.Fatal("credential issuance must not create a participant session")
	}
	for _, fragment := range []string{
		`existing.Status == string(JoinAttemptWaiting)`,
		`existing.Status != string(JoinAttemptAdmitted)`,
		`existing.Status != string(JoinAttemptJoining)`,
		"SET status = 'joining'",
		"joining_at = $6",
	} {
		if !strings.Contains(credential, fragment) {
			t.Fatalf("credential issuance is missing existing-attempt boundary %q", fragment)
		}
	}

	attempt := postgresInstanceRepositorySection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
		"func (repository *PostgresInstanceRepository) requireParticipantCapacity(",
	)
	for _, fragment := range []string{
		"space.Version != input.ExpectedSpaceVersion",
		"room.ID != input.ExpectedRoomInstanceID",
		"INSERT INTO tutorhub.media_admission_requests",
		"INSERT INTO tutorhub.media_participant_sessions",
		"space.LobbyEnabled && source.InstanceRole == InstanceRoleAttendee",
		"participant.Status = string(JoinAttemptWaiting)",
	} {
		if !strings.Contains(attempt, fragment) {
			t.Fatalf("join-attempt authority is missing %q", fragment)
		}
	}
}

func TestJoinAttemptRequiresExplicitRestoreAfterRemoval(t *testing.T) {
	t.Parallel()

	source := postgresInstanceRepositorySource(t)
	attempt := postgresInstanceRepositorySection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
		"func (repository *PostgresInstanceRepository) requireParticipantCapacity(",
	)
	activeCheck := strings.Index(attempt, "hasActiveParticipant(")
	restoreBarrier := strings.Index(attempt, "hasUnrestoredRemovalBarrier(")
	capacityLock := strings.Index(attempt, "media-participant-capacity:")
	admissionInsert := strings.Index(attempt, "INSERT INTO tutorhub.media_admission_requests")
	if activeCheck < 0 || restoreBarrier <= activeCheck || capacityLock <= restoreBarrier ||
		admissionInsert <= capacityLock {
		t.Fatal("join attempt does not enforce the removal restore barrier before capacity/admission creation")
	}
	barrier := postgresInstanceRepositorySection(
		t,
		source,
		"func hasUnrestoredRemovalBarrier(",
		"func activeParticipantStatus(",
	)
	for _, fragment := range []string{
		"tenant_id = $1 AND space_id = $2 AND room_instance_id = $3",
		"user_id = $4 AND status = 'removed'",
		"rejoin_restored_at IS NULL",
	} {
		if !strings.Contains(barrier, fragment) {
			t.Fatalf("restore barrier is missing %q", fragment)
		}
	}
}

func TestCredentialLockBlocksAdmittedAttemptBeforeMint(t *testing.T) {
	t.Parallel()

	credential := postgresInstanceRepositorySection(
		t,
		postgresInstanceRepositorySource(t),
		"func (repository *PostgresInstanceRepository) PrepareCredential(",
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
	)
	lockCheck := strings.Index(credential, "if space.Locked {")
	participantLookup := strings.Index(credential, "loadParticipantByAttempt(")
	rateLimit := strings.Index(credential, "consumeMediaCredentialRateLimit(")
	if lockCheck < 0 || participantLookup < 0 || rateLimit < 0 {
		t.Fatal("credential issuance is missing the lock, admitted-attempt, or rate-limit boundary")
	}
	if lockCheck > participantLookup || lockCheck > rateLimit {
		t.Fatal("space lock must reject every new credential before admitted-attempt lookup or mint rate consumption")
	}
}

func TestJoinBoundariesAuthorizeAndConcealSourceBeforeObservableState(t *testing.T) {
	t.Parallel()

	source := postgresInstanceRepositorySource(t)
	for _, test := range []struct {
		name       string
		start      string
		end        string
		observable []string
	}{
		{
			name:  "credential",
			start: "func (repository *PostgresInstanceRepository) PrepareCredential(",
			end:   "func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
			observable: []string{
				"controls.RequireFeature(", "if space.Status !=", "if space.Locked {",
			},
		},
		{
			name:  "join attempt",
			start: "func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
			end:   "func (repository *PostgresInstanceRepository) requireParticipantCapacity(",
			observable: []string{
				"controls.RequireFeature(", "if space.Version !=", "if space.Status !=", "if space.Locked {",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			section := postgresInstanceRepositorySection(t, source, test.start, test.end)
			authorization := strings.Index(section, "repository.lifecycle.authorizeSource(")
			concealment := strings.Index(section, "concealJoinSourceError(err)")
			if authorization < 0 || concealment < authorization {
				t.Fatal("join boundary must authorize and conceal the source before exposing state")
			}
			for _, fragment := range test.observable {
				observable := strings.Index(section, fragment)
				if observable < 0 || authorization > observable {
					t.Fatalf("source authorization must precede observable boundary %q", fragment)
				}
			}
		})
	}

	if !errors.Is(concealJoinSourceError(ErrSpaceAccessDenied), ErrSpaceNotFound) {
		t.Fatal("join source access denial must be concealed as not found")
	}
	if !errors.Is(concealJoinSourceError(ErrSourceUnavailable), ErrSourceUnavailable) {
		t.Fatal("join source unavailable error must retain its concealed class")
	}
}

func TestCredentialRechecksCurrentLobbyBypassAuthority(t *testing.T) {
	t.Parallel()

	credential := postgresInstanceRepositorySection(
		t,
		postgresInstanceRepositorySource(t),
		"func (repository *PostgresInstanceRepository) PrepareCredential(",
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
	)
	roleCheck := strings.Index(
		credential,
		"space.LobbyEnabled && source.InstanceRole == InstanceRoleAttendee",
	)
	admissionCheck := strings.Index(credential, "requireAdmittedAdmission(")
	rateLimit := strings.Index(credential, "consumeMediaCredentialRateLimit(")
	if roleCheck < 0 || admissionCheck < roleCheck || rateLimit < admissionCheck {
		t.Fatal("credential must recheck current lobby admission before rate-limit or mint")
	}
}

func TestProviderRoomNameControlLockPrecedesAuthorizationRows(t *testing.T) {
	t.Parallel()

	section := postgresInstanceRepositorySection(
		t,
		postgresInstanceRepositorySource(t),
		"func (repository *PostgresInstanceRepository) ProviderRoomName(",
		"func (repository *PostgresInstanceRepository) PrepareCredential(",
	)
	controlLock := strings.Index(section, "repository.lifecycle.acquireTenantControlLock(")
	activeScope := strings.Index(section, "repository.lifecycle.requireActiveScope(")
	spaceLookup := strings.Index(section, "space, err := loadSpace(")
	if controlLock < 0 || activeScope < 0 || spaceLookup < 0 ||
		controlLock > activeScope || controlLock > spaceLookup {
		t.Fatal("provider room lookup must acquire the tenant control lock before authorization rows")
	}
}

func TestProviderWebhookClassifierSupportsBoundLifecycleTransitions(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	createdAt := receivedAt.Add(-time.Hour)
	transitionAt := receivedAt.Add(-time.Minute)
	base := providerRoomBinding{
		TenantID:       uuid.MustParse("b16dd520-65d5-4ea0-a7ca-62e43a35bbfb"),
		SpaceID:        uuid.MustParse("f6808028-e785-41d3-8cb6-bf027018ba41"),
		RoomInstanceID: uuid.MustParse("e97d4164-a66c-49dc-9bf3-28d694f565e7"),
		ProviderName:   "rm_opaque_01HZXZ9QGJQXZJ6J2T7S",
		CreatedAt:      createdAt,
	}
	active := base
	active.Status = RoomInstanceActive
	active.ProviderSID = sql.NullString{String: "RM_provider_sid", Valid: true}
	active.ActivatedAt = sql.NullTime{Time: createdAt.Add(10 * time.Minute), Valid: true}
	provisioning := base
	provisioning.Status = RoomInstanceProvisioning
	joining := &participantRow{
		ID:        uuid.MustParse("cb94435d-e0c8-451a-bfe4-bb669525d836"),
		Status:    "joining",
		CreatedAt: createdAt.Add(20 * time.Minute),
	}
	connected := *joining
	connected.Status = "connected"
	connected.ConnectedAt = sql.NullTime{Time: transitionAt.Add(-time.Minute), Valid: true}
	staleConnected := connected
	staleConnected.ConnectedAt = sql.NullTime{Time: transitionAt.Add(time.Second), Valid: true}
	sameSecondJoining := *joining
	sameSecondJoining.CreatedAt = transitionAt.Add(750 * time.Millisecond)
	sameSecondConnected := connected
	sameSecondConnected.ConnectedAt = sql.NullTime{
		Time: transitionAt.Add(750 * time.Millisecond), Valid: true,
	}

	tests := []struct {
		name         string
		binding      providerRoomBinding
		participant  *participantRow
		event        WebhookEvent
		want         string
		wantMutation bool
	}{
		{
			name:    "room started activates provisioning instance",
			binding: provisioning,
			event: WebhookEvent{
				EventType: "room_started", RoomName: base.ProviderName,
				RoomSID: "RM_provider_sid", OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:    "room finished closes active instance",
			binding: active,
			event: WebhookEvent{
				EventType: "room_finished", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:    "room finished fails provisioning instance",
			binding: provisioning,
			event: WebhookEvent{
				EventType: "room_finished", RoomName: base.ProviderName,
				RoomSID: "RM_provider_sid", OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:        "participant joined connects joining participant",
			binding:     active,
			participant: joining,
			event: WebhookEvent{
				EventType: "participant_joined", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, ParticipantIdentity: "p_opaque",
				OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:        "participant left releases connected participant",
			binding:     active,
			participant: &connected,
			event: WebhookEvent{
				EventType: "participant_left", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, ParticipantIdentity: "p_opaque",
				OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:        "same-second participant joined respects provider precision",
			binding:     active,
			participant: &sameSecondJoining,
			event: WebhookEvent{
				EventType: "participant_joined", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, ParticipantIdentity: "p_opaque",
				OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:        "same-second participant left respects provider precision",
			binding:     active,
			participant: &sameSecondConnected,
			event: WebhookEvent{
				EventType: "participant_left", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, ParticipantIdentity: "p_opaque",
				OccurredAt: transitionAt,
			},
			want: "applied", wantMutation: true,
		},
		{
			name:        "out of order participant left is stale",
			binding:     active,
			participant: &staleConnected,
			event: WebhookEvent{
				EventType: "participant_left", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, ParticipantIdentity: "p_opaque",
				OccurredAt: transitionAt,
			},
			want: "ignored_stale",
		},
		{
			name:    "unknown participant is private no-op",
			binding: active,
			event: WebhookEvent{
				EventType: "participant_joined", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, ParticipantIdentity: "p_unknown",
				OccurredAt: transitionAt,
			},
			want: "ignored_unknown_participant",
		},
		{
			name:    "provider binding mismatch is ignored",
			binding: active,
			event: WebhookEvent{
				EventType: "room_finished", RoomName: "different_opaque_room",
				RoomSID: active.ProviderSID.String, OccurredAt: transitionAt,
			},
			want: "ignored_mismatch",
		},
		{
			name:    "unsupported provider event is receipt only",
			binding: active,
			event: WebhookEvent{
				EventType: "track_published", RoomName: base.ProviderName,
				RoomSID: active.ProviderSID.String, OccurredAt: transitionAt,
			},
			want: "ignored_unsupported_event",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			disposition, mutation := classifyProviderWebhook(
				test.binding,
				test.participant,
				test.event,
				receivedAt,
			)
			if disposition != test.want {
				t.Fatalf("disposition = %q, want %q", disposition, test.want)
			}
			if (mutation != nil) != test.wantMutation {
				t.Fatalf("mutation present = %t, want %t", mutation != nil, test.wantMutation)
			}
		})
	}
}

func TestProviderTransitionTimePreservesDatabaseLifecycleConstraints(t *testing.T) {
	t.Parallel()

	stateAt := time.Date(2026, time.August, 9, 10, 0, 0, int(750*time.Millisecond), time.UTC)
	providerSameSecond := stateAt.Truncate(time.Second)
	if actual := providerTransitionTime(providerSameSecond, stateAt); !actual.Equal(stateAt) {
		t.Fatalf("same-second provider transition = %s, want clamped %s", actual, stateAt)
	}
	providerLater := stateAt.Add(time.Second).Truncate(time.Second)
	if actual := providerTransitionTime(providerLater, stateAt); !actual.Equal(providerLater) {
		t.Fatalf("later provider transition = %s, want %s", actual, providerLater)
	}
}

func TestPostgresInstanceRepositoryWebhookSQLUsesExactOpaqueBindings(t *testing.T) {
	t.Parallel()

	source := postgresInstanceRepositorySource(t)
	if strings.Contains(source, "ParseRoomName") {
		t.Fatal("room-instance webhook repository must not derive authority by parsing a room name")
	}

	discovery := postgresInstanceRepositorySection(
		t,
		source,
		"func discoverProviderRoomTenant(",
		"func loadProviderRoomBinding(",
	)
	for _, fragment := range []string{
		"SELECT tenant_id",
		"WHERE provider_kind = 'livekit'",
		"($1 <> '' AND provider_room_sid = $1)",
		"($2 <> '' AND provider_room_name = $2)",
		"ORDER BY CASE WHEN provider_room_sid = $1 THEN 0 ELSE 1 END",
		"LIMIT 1",
		"roomSID, roomName",
	} {
		if !strings.Contains(discovery, fragment) {
			t.Fatalf("provider room tenant discovery is missing %q", fragment)
		}
	}
	if strings.Contains(discovery, "FOR UPDATE") {
		t.Fatal("provider tenant discovery must not lock a room before the tenant advisory lock")
	}

	binding := postgresInstanceRepositorySection(
		t,
		source,
		"func loadProviderRoomBinding(",
		"type webhookMutation func",
	)
	for _, fragment := range []string{
		"roomSID := strings.TrimSpace(event.RoomSID)",
		"roomName := strings.TrimSpace(event.RoomName)",
		"WHERE tenant_id = $1",
		"AND provider_kind = 'livekit'",
		"($2 <> '' AND provider_room_sid = $2)",
		"($3 <> '' AND provider_room_name = $3)",
		"ORDER BY CASE WHEN provider_room_sid = $2 THEN 0 ELSE 1 END",
		"LIMIT 1",
		"FOR UPDATE",
		"tenantID, roomSID, roomName",
	} {
		if !strings.Contains(binding, fragment) {
			t.Fatalf("provider room binding lookup is missing %q", fragment)
		}
	}

	record := postgresInstanceRepositorySection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) RecordProviderWebhook(",
		"type participantRow struct",
	)
	discoveryAt := strings.Index(record, "discoverProviderRoomTenant(")
	controlLockAt := strings.Index(record, "repository.lifecycle.acquireTenantControlLock(")
	bindingAt := strings.Index(record, "loadProviderRoomBinding(")
	if discoveryAt < 0 || controlLockAt < 0 || bindingAt < 0 ||
		discoveryAt > controlLockAt || controlLockAt > bindingAt {
		t.Fatal("provider webhook must discover tenant, acquire its advisory lock, then lock the exact binding")
	}

	participant := postgresInstanceRepositorySection(
		t,
		source,
		"func loadWebhookParticipant(",
		"func insertProviderWebhookReceipt(",
	)
	for _, fragment := range []string{
		"WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3",
		"AND provider_participant_identity = $4",
		"binding.TenantID, binding.SpaceID, binding.RoomInstanceID, identity",
	} {
		if !strings.Contains(participant, fragment) {
			t.Fatalf("provider participant binding lookup is missing %q", fragment)
		}
	}

	receipt := postgresInstanceRepositorySection(
		t,
		source,
		"func insertProviderWebhookReceipt(",
		"func validProviderIdentifier(",
	)
	for _, fragment := range []string{
		"provider_kind, event_id, tenant_id, space_id, room_instance_id",
		"participant_session_id, event_type, disposition, occurred_at",
		"received_at, retention_until",
		"VALUES ('livekit', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)",
		"ON CONFLICT (provider_kind, event_id) DO NOTHING",
		"receivedAt.Add(providerReceiptRetention)",
	} {
		if !strings.Contains(receipt, fragment) {
			t.Fatalf("provider receipt SQL is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"raw_payload",
		"payload",
		"room_name",
		"provider_room_name",
		"participant_identity",
		"provider_participant_identity",
		"participant_sid",
		"access_token",
		"api_key",
		"api_secret",
	} {
		if strings.Contains(strings.ToLower(receipt), forbidden) {
			t.Fatalf("provider receipt SQL persists forbidden field %q", forbidden)
		}
	}
}

func TestRoomTerminationReleasesParticipantCapacity(t *testing.T) {
	t.Parallel()

	instanceSource := postgresInstanceRepositorySource(t)
	roomFinished := postgresInstanceRepositorySection(
		t,
		instanceSource,
		`case "room_finished":`,
		`case "participant_joined", "participant_left":`,
	)
	if !strings.Contains(roomFinished, "return failRoomParticipants(") {
		t.Fatal("provider room-finished transition must fail participants and release capacity")
	}

	lifecycleContents, err := os.ReadFile("postgres_lifecycle_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	lifecycleSource := string(lifecycleContents)
	endSection := postgresInstanceRepositorySection(
		t,
		lifecycleSource,
		"func (repository *PostgresLifecycleRepository) endSpace(",
		"func (repository *PostgresLifecycleRepository) cancelSpace(",
	)
	if !strings.Contains(endSection, "terminateRoomParticipants(") {
		t.Fatal("authorized media-space end must release active participant capacity")
	}
	if !strings.Contains(endSection, "loadRoomForTermination(") {
		t.Fatal("authorized media-space end must recover a provider-failed room intent")
	}
	terminationLookup := postgresInstanceRepositorySection(
		t,
		lifecycleSource,
		"func loadRoomForTermination(",
		"func (room roomRow) project(",
	)
	for _, fragment := range []string{
		"status IN ('provisioning', 'active', 'closing', 'failed')",
		"ORDER BY attempt_number DESC",
		"LIMIT 1",
		"FOR UPDATE",
	} {
		if !strings.Contains(terminationLookup, fragment) {
			t.Fatalf("room termination recovery lookup is missing %q", fragment)
		}
	}
	termination := postgresInstanceRepositorySection(
		t,
		lifecycleSource,
		"func terminateRoomParticipants(",
		"func insertTransitionReceipt(",
	)
	for _, fragment := range []string{
		"SET status = 'left', version = version + 1, capacity_reserved = false",
		"terminal_at = $4, reconnecting_at = NULL, updated_at = $4",
		"WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3",
		"status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')",
	} {
		if !strings.Contains(termination, fragment) {
			t.Fatalf("participant termination is missing %q", fragment)
		}
	}
}

func postgresInstanceRepositorySource(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile("postgres_instance_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func postgresInstanceRepositorySection(
	t *testing.T,
	source string,
	startMarker string,
	endMarker string,
) string {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("repository source is missing start marker %q", startMarker)
	}
	endOffset := strings.Index(source[start:], endMarker)
	if endOffset < 0 {
		t.Fatalf("repository source is missing end marker %q", endMarker)
	}
	return source[start : start+endOffset]
}
