package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDiagnosticServiceValidatesBoundedSchemaAndRedactsFailures(t *testing.T) {
	t.Parallel()
	repository := &fakeDiagnosticRepository{}
	service, err := NewDiagnosticService(repository, func() time.Time { return mediaTestTime })
	if err != nil {
		t.Fatal(err)
	}
	access := AccessContext{TenantID: uuid.New(), ActorID: uuid.New(), SessionID: uuid.New()}
	input := RecordDiagnosticInput{
		EventID: uuid.New(), RoomInstanceID: uuid.New(), JoinAttemptID: uuid.New(),
		Stage: DiagnosticStageMedia, Outcome: DiagnosticOutcomeSucceeded,
		NetworkQuality: DiagnosticNetworkGood, MediaPath: DiagnosticMediaAudioVideo,
		DurationMS: 1250,
	}
	if err := service.RecordDiagnostic(context.Background(), access, uuid.New(), input); err != nil {
		t.Fatalf("record bounded diagnostic: %v", err)
	}
	if repository.recordedAt != mediaTestTime || repository.input != input {
		t.Fatalf("diagnostic was not normalized: %+v", repository)
	}

	invalid := input
	invalid.ErrorCode = "raw exception: token=secret"
	if err := service.RecordDiagnostic(context.Background(), access, uuid.New(), invalid); !errors.Is(err, ErrInvalidDiagnosticRequest) {
		t.Fatalf("raw error must be rejected, got %v", err)
	}
	if repository.recordCalls != 1 {
		t.Fatalf("invalid diagnostic reached repository: %d", repository.recordCalls)
	}
	invalid.ErrorCode = "network_timeout"
	if err := service.RecordDiagnostic(context.Background(), access, uuid.New(), invalid); !errors.Is(err, ErrInvalidDiagnosticRequest) {
		t.Fatalf("non-taxonomy error code must be rejected, got %v", err)
	}

	repository.err = errors.New("database-sensitive-detail")
	if err := service.RecordDiagnostic(context.Background(), access, uuid.New(), input); !errors.Is(err, ErrDiagnosticUnavailable) ||
		err.Error() != ErrDiagnosticUnavailable.Error() {
		t.Fatalf("unknown failure was not redacted: %v", err)
	}
}

func TestDiagnosticServiceBoundsExportRangeAndLimit(t *testing.T) {
	t.Parallel()
	repository := &fakeDiagnosticRepository{export: DiagnosticExport{}}
	service, _ := NewDiagnosticService(repository, nil)
	access := AccessContext{TenantID: uuid.New(), ActorID: uuid.New(), SessionID: uuid.New()}
	from := mediaTestTime.Add(-24 * time.Hour)
	export, err := service.ExportDiagnostics(context.Background(), access, DiagnosticExportFilter{
		From: from, To: mediaTestTime, Limit: 1000,
	})
	if err != nil || export.Items == nil || repository.exportCalls != 1 {
		t.Fatalf("valid export failed: export=%+v err=%v", export, err)
	}
	for _, filter := range []DiagnosticExportFilter{
		{From: mediaTestTime.Add(-32 * 24 * time.Hour), To: mediaTestTime, Limit: 1},
		{From: mediaTestTime, To: mediaTestTime, Limit: 1},
		{From: from, To: mediaTestTime, Limit: 1001},
	} {
		if _, err := service.ExportDiagnostics(context.Background(), access, filter); !errors.Is(err, ErrInvalidDiagnosticRequest) {
			t.Fatalf("invalid export accepted: %+v err=%v", filter, err)
		}
	}
}

type fakeDiagnosticRepository struct {
	input       RecordDiagnosticInput
	recordedAt  time.Time
	recordCalls int
	exportCalls int
	export      DiagnosticExport
	err         error
}

func (repository *fakeDiagnosticRepository) RecordDiagnostic(
	_ context.Context, _ AccessContext, _ uuid.UUID, input RecordDiagnosticInput, recordedAt time.Time,
) error {
	repository.recordCalls++
	repository.input = input
	repository.recordedAt = recordedAt
	return repository.err
}

func (repository *fakeDiagnosticRepository) ExportDiagnostics(
	_ context.Context, _ AccessContext, _ DiagnosticExportFilter,
) (DiagnosticExport, error) {
	repository.exportCalls++
	return repository.export, repository.err
}
