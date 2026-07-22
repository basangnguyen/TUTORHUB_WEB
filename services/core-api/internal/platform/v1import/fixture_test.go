package v1import

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadTestFixture(t *testing.T) ParsedFixture {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "v1import", "p2-11-anonymized.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	parsed, err := ParseFixture(data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return parsed
}

func TestParseFixtureAndBuildPlan(t *testing.T) {
	parsed := loadTestFixture(t)
	if parsed.Fixture.SourceSystem != "tutorhub-v1-fixture" {
		t.Fatalf("unexpected source system: %q", parsed.Fixture.SourceSystem)
	}
	plan, err := buildPlan(parsed.Fixture)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(plan) != 12 {
		t.Fatalf("expected 12 planned records, got %d", len(plan))
	}
	var skipped int
	for _, record := range plan {
		if record.SkipReason != "" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Fatalf("expected two dependency/out-of-scope skips, got %d", skipped)
	}
	first, err := buildPlan(parsed.Fixture)
	if err != nil {
		t.Fatalf("rebuild plan: %v", err)
	}
	for index := range plan {
		if plan[index].TargetID != first[index].TargetID || plan[index].SourceHash != first[index].SourceHash {
			t.Fatalf("plan is not deterministic at ordinal %d", index)
		}
	}
}

func TestParseFixtureRejectsUnsafeOrAmbiguousDocuments(t *testing.T) {
	t.Parallel()
	base := `{"fixture_version":1,"fixture_key":"p2-11-test","source_system":"tutorhub-v1-fixture","anonymized":true,"exported_at":"2026-07-22T00:00:00Z","users":[],"tenants":[],"memberships":[],"classes":[]}`
	tests := []struct {
		name string
		data string
		want error
	}{
		{name: "duplicate field", data: strings.Replace(base, `"users":[]`, `"users":[],"USERS":[]`, 1), want: ErrFixtureInvalid},
		{name: "unknown field", data: strings.Replace(base, `"users":[]`, `"users":[],"secret":"nope"`, 1), want: ErrFixtureInvalid},
		{name: "trailing document", data: base + ` {}`, want: ErrFixtureInvalid},
		{name: "not anonymized", data: strings.Replace(base, `"anonymized":true`, `"anonymized":false`, 1), want: ErrFixtureNotPrivate},
		{
			name: "real email",
			data: strings.Replace(base, `"users":[]`, `"users":[{"external_id":"u1","email":"person@gmail.com","display_name":"Anonymized","locale":"en","timezone":"UTC","status":"active","created_at":"2026-07-22T00:00:00Z","updated_at":"2026-07-22T00:00:00Z"}]`, 1),
			want: ErrFixtureNotPrivate,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseFixture([]byte(test.data))
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}
