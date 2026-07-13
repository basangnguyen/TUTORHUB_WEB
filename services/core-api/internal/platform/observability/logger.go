package observability

import (
	"fmt"
	"io"
	"log/slog"
)

func NewLogger(output io.Writer, level string) (*slog.Logger, error) {
	logLevel, err := parseLogLevel(level)
	if err != nil {
		return nil, err
	}

	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: logLevel,
	})

	return slog.New(handler), nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", value)
	}
}
