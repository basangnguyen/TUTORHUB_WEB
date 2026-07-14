package media

import (
	"context"
	"log/slog"
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
		"tenant_id", event.TenantID,
		"class_id", event.ClassID,
		"actor_id", event.ActorID,
		"attempt_id", event.AttemptID,
		"stage", event.Stage,
		"outcome", event.Outcome,
		"error_code", event.ErrorCode,
		"duration_ms", event.DurationMS,
	)
}
