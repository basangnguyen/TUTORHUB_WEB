package observability

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewLoggerUsesConfiguredLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger, err := NewLogger(&output, "warn")
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	logger.Info("hidden")
	logger.Warn("visible", "request_id", "req-1")

	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON log: %v", err)
	}
	if payload["msg"] != "visible" || payload["request_id"] != "req-1" {
		t.Fatalf("unexpected log payload: %#v", payload)
	}
}

func TestNewLoggerRejectsUnknownLevel(t *testing.T) {
	t.Parallel()

	if _, err := NewLogger(&bytes.Buffer{}, "trace"); err == nil {
		t.Fatal("expected unsupported log level error")
	}
}
