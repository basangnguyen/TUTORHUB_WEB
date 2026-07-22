package v1import

import (
	"errors"
	"testing"
)

func TestValidateEnvironment(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"development", "test", "staging", " Development "} {
		if err := ValidateEnvironment(environment); err != nil {
			t.Fatalf("expected %q to be allowed: %v", environment, err)
		}
	}
	for _, environment := range []string{"", "production", "prod"} {
		if err := ValidateEnvironment(environment); !errors.Is(err, ErrEnvironmentBlocked) {
			t.Fatalf("expected %q to be blocked, got %v", environment, err)
		}
	}
}

func TestExecuteValidatesEnvironmentBeforeDatabaseURL(t *testing.T) {
	t.Parallel()
	if _, err := Execute(t.Context(), "", "production", ParsedFixture{}, ModeApply, Options{}); !errors.Is(err, ErrEnvironmentBlocked) {
		t.Fatalf("expected environment guard first, got %v", err)
	}
}
