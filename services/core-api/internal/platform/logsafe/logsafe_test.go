package logsafe

import (
	"errors"
	"testing"
)

func TestStringRemovesLogLineSeparators(t *testing.T) {
	t.Parallel()

	got := String("first\r\nforged\u2028entry\u2029end")
	if got != "firstforgedentryend" {
		t.Fatalf("unexpected sanitized log value: %q", got)
	}
}

func TestErrorSanitizesAndHandlesNil(t *testing.T) {
	t.Parallel()

	if got := Error(errors.New("failed\nforged")); got != "failedforged" {
		t.Fatalf("unexpected sanitized error: %q", got)
	}
	if got := Error(nil); got != "" {
		t.Fatalf("nil error must produce an empty value, got %q", got)
	}
}
