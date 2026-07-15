package media

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRepositoryRecordsAndDeduplicatesWebhookReceipts(t *testing.T) {
	t.Parallel()

	receipt := WebhookReceipt{
		EventID:             "EV_participant_joined_01",
		EventType:           "participant_joined",
		TenantID:            uuid.New(),
		ClassID:             uuid.New(),
		RoomName:            "th_tenant_class",
		ParticipantIdentity: "u_actor_s_session",
		OccurredAt:          time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC),
		ReceivedAt:          time.Date(2026, 7, 14, 5, 0, 1, 0, time.UTC),
	}
	database := &fakeMediaDB{tags: []pgconn.CommandTag{
		pgconn.NewCommandTag("INSERT 0 1"),
		pgconn.NewCommandTag("INSERT 0 0"),
	}}
	repository := NewPostgresRepository(database, time.Second)

	recorded, err := repository.RecordWebhookReceipt(context.Background(), receipt)
	if err != nil || !recorded {
		t.Fatalf("record webhook receipt: recorded=%t err=%v", recorded, err)
	}
	recorded, err = repository.RecordWebhookReceipt(context.Background(), receipt)
	if err != nil || recorded {
		t.Fatalf("deduplicate webhook receipt: recorded=%t err=%v", recorded, err)
	}
	if database.calls != 2 || !strings.Contains(database.query, "ON CONFLICT (event_id) DO NOTHING") {
		t.Fatalf("unexpected receipt query: calls=%d query=%q", database.calls, database.query)
	}
	if len(database.arguments) != 8 || database.arguments[0] != receipt.EventID {
		t.Fatalf("unexpected receipt arguments: %+v", database.arguments)
	}
}

type fakeMediaDB struct {
	calls     int
	query     string
	arguments []any
	tags      []pgconn.CommandTag
}

func (database *fakeMediaDB) Exec(
	_ context.Context,
	query string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	database.query = query
	database.arguments = append([]any(nil), arguments...)
	tag := database.tags[database.calls]
	database.calls++
	return tag, nil
}
