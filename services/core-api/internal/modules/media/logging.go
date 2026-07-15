package media

import (
	"context"
	"log/slog"

	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

type SlogEventSink struct {
	logger *slog.Logger
}

func NewSlogEventSink(logger *slog.Logger) *SlogEventSink {
	return &SlogEventSink{logger: logger}
}

func (sink *SlogEventSink) RecordClientEvent(_ context.Context, event ClientEvent) {
	if sink == nil || sink.logger == nil {
		return
	}
	sink.logger.Info(
		"classroom media client event",
		"tenant_id", logsafe.String(event.TenantID.String()),
		"class_id", logsafe.String(event.ClassID.String()),
		"actor_id", logsafe.String(event.ActorID.String()),
		"attempt_id", logsafe.String(event.AttemptID.String()),
		"stage", logsafe.String(event.Stage),
		"outcome", logsafe.String(event.Outcome),
		"error_code", logsafe.String(event.ErrorCode),
		"duration_ms", event.DurationMS,
	)
}
