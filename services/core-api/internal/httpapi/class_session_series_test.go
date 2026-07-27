package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
)

func TestClassSessionSeriesResponseMatchesTypedRecurrenceContract(t *testing.T) {
	t.Parallel()

	series := classroom.ClassSessionSeries{
		ID: uuid.New(), ClassID: uuid.New(), Title: "Lịch học tuần",
		Description: "Ôn tập", LocalStart: "2026-07-27T19:30:00",
		Timezone: "Asia/Ho_Chi_Minh", DurationMinutes: 90,
		Rule: recurrence.Rule{
			Frequency: recurrence.FrequencyWeekly,
			Interval:  2,
			Weekdays:  []recurrence.Weekday{recurrence.Monday, recurrence.Wednesday},
			End: recurrence.End{
				Type: recurrence.EndAfterCount, Count: 12,
			},
		},
		OverlapPolicy: recurrence.OverlapReject,
		Status:        classroom.SeriesStatusScheduled, Version: 3, Sequence: 2,
		ICalUID:   uuid.NewString() + "@calendar.tutorhub",
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
		ViewerAccess: classroom.SessionViewerAccess{
			CanUpdate: true, CanCancel: true,
		},
	}

	payload, err := json.Marshal(newClassSessionSeriesResponse(series))
	if err != nil {
		t.Fatalf("marshal recurring session response: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode recurring session response: %v", err)
	}
	rule, ok := decoded["rule"].(map[string]any)
	if !ok {
		t.Fatalf("missing typed recurrence rule: %s", payload)
	}
	ends, ok := rule["ends"].(map[string]any)
	if !ok || ends["type"] != string(recurrence.EndAfterCount) || ends["count"] != float64(12) {
		t.Fatalf("unexpected recurrence end contract: %#v", rule)
	}
	if _, leaked := rule["End"]; leaked {
		t.Fatalf("domain field leaked into API contract: %s", payload)
	}
	if _, leaked := rule["end"]; leaked {
		t.Fatalf("legacy recurrence end field leaked into API contract: %s", payload)
	}
}

func TestClassSessionSeriesAuditResolverUsesSeriesIdentity(t *testing.T) {
	t.Parallel()

	seriesID := uuid.New()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/classes/example/session-series/"+seriesID.String()+"/occurrence", nil)
	request.SetPathValue("series_id", seriesID.String())
	draft, ok := classSessionSeriesResourceAuditMutation(request)
	if !ok || draft.ResourceID != seriesID || draft.ResourceType != "class_session_series" {
		t.Fatalf("unexpected recurring mutation audit draft: ok=%v draft=%+v", ok, draft)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/classes/example/session-series/"+seriesID.String(), nil)
	request.SetPathValue("series_id", seriesID.String())
	if _, ok := classSessionSeriesResourceAuditMutation(request); ok {
		t.Fatal("GET must not be recorded as a recurring mutation")
	}
}

func TestClassSessionSeriesResponseNormalizesTimestamps(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+7", 7*60*60)
	cancelledAt := fixedTime.In(location)
	series := classroom.ClassSessionSeries{
		ID: uuid.New(), ClassID: uuid.New(), Title: "Cancelled",
		LocalStart: "2026-07-27T19:30:00", Timezone: "Asia/Ho_Chi_Minh",
		DurationMinutes: 60,
		Rule: recurrence.Rule{
			Frequency: recurrence.FrequencyDaily, Interval: 1,
			End: recurrence.End{Type: recurrence.EndAfterCount, Count: 1},
		},
		OverlapPolicy: recurrence.OverlapReject,
		Status:        classroom.SeriesStatusCancelled, Version: 2, Sequence: 1,
		ICalUID:   uuid.NewString() + "@calendar.tutorhub",
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
		CancelledAt: &cancelledAt, CreatedAt: fixedTime.In(location),
		UpdatedAt: fixedTime.In(location),
	}
	response := newClassSessionSeriesResponse(series)
	if response.CreatedAt.Location() != time.UTC || response.UpdatedAt.Location() != time.UTC {
		t.Fatalf("series timestamps are not UTC: created=%v updated=%v", response.CreatedAt, response.UpdatedAt)
	}
}
