package devseed

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		wantError   bool
	}{
		{name: "development", environment: "development"},
		{name: "test", environment: "test"},
		{name: "trimmed case insensitive", environment: " Development "},
		{name: "empty", environment: "", wantError: true},
		{name: "staging", environment: "staging", wantError: true},
		{name: "production", environment: "production", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateEnvironment(test.environment)
			if test.wantError && !errors.Is(err, ErrEnvironmentBlocked) {
				t.Fatalf("expected blocked environment error, got %v", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("expected environment to be allowed, got %v", err)
			}
		})
	}
}

func TestRunValidatesBeforeConnecting(t *testing.T) {
	t.Parallel()

	if err := Run(t.Context(), "", "production"); !errors.Is(err, ErrEnvironmentBlocked) {
		t.Fatalf("expected environment guard before database validation, got %v", err)
	}
	if err := Run(t.Context(), "", "development"); !errors.Is(err, ErrDatabaseURLRequired) {
		t.Fatalf("expected missing database URL error, got %v", err)
	}
}

func TestSeedContainsRequiredDeterministicFixtures(t *testing.T) {
	t.Parallel()

	requiredFragments := []string{
		"tutorhub-demo",
		"giangvien.demo@tutorhub.local",
		"hocsinh.demo@tutorhub.local",
		"Asia/Ho_Chi_Minh",
		"'UTC'",
		"'teacher'",
		"'student'",
		"DEMO-VI-01",
		"ON CONFLICT",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(seedSQL, fragment) {
			t.Fatalf("development seed is missing %q", fragment)
		}
	}
}
