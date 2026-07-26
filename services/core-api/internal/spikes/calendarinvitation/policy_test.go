package calendarinvitation

import (
	"strings"
	"testing"
)

func TestRenderPolicyAllowsOnlyConfiguredHTTPSOriginAndTZDataVersion(t *testing.T) {
	t.Parallel()

	policy, err := NewRenderPolicy(
		testTZDataVersion,
		"https://TutorHub.Example:443",
	)
	if err != nil {
		t.Fatalf("NewRenderPolicy(): %v", err)
	}
	snapshot := deterministicSnapshot(t, EffectInvite, 7)
	if _, err := RenderICalendar(snapshot, policy); err != nil {
		t.Fatalf("RenderICalendar() for canonical allowed origin: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		policy RenderPolicy
	}{
		{
			name:   "zero policy",
			policy: RenderPolicy{},
		},
		{
			name:   "untrusted origin",
			policy: policy,
			mutate: func(value *Snapshot) {
				value.DeepLink = "https://attacker.example/invitations/01"
			},
		},
		{
			name:   "userinfo origin confusion",
			policy: policy,
			mutate: func(value *Snapshot) {
				value.DeepLink = "https://tutorhub.example@attacker.example/invitations/01"
			},
		},
		{
			name:   "time zone data version mismatch",
			policy: policy,
			mutate: func(value *Snapshot) {
				value.TimeZoneDataVersion = "go-tzdata-older"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value := deterministicSnapshot(t, EffectInvite, 7)
			if test.mutate != nil {
				test.mutate(&value)
			}
			if _, err := RenderICalendar(value, test.policy); err == nil {
				t.Fatal("RenderICalendar() error = nil, want trusted-policy rejection")
			}
		})
	}
}

func TestNewRenderPolicyRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		origin  string
	}{
		{name: "missing version", origin: "https://tutorhub.example"},
		{
			name:    "dangerous version unicode",
			version: "tzdata\u202ereversed",
			origin:  "https://tutorhub.example",
		},
		{name: "HTTP", version: testTZDataVersion, origin: "http://tutorhub.example"},
		{
			name:    "path",
			version: testTZDataVersion,
			origin:  "https://tutorhub.example/app",
		},
		{
			name:    "query",
			version: testTZDataVersion,
			origin:  "https://tutorhub.example?tenant=one",
		},
		{
			name:    "userinfo",
			version: testTZDataVersion,
			origin:  "https://user@tutorhub.example",
		},
		{
			name:    "unicode hostname",
			version: testTZDataVersion,
			origin:  "https://tutorh\u00fcb.example",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRenderPolicy(test.version, test.origin); err == nil {
				t.Fatal("NewRenderPolicy() error = nil for unsafe configuration")
			}
		})
	}
	if _, err := NewRenderPolicy(testTZDataVersion); err == nil {
		t.Fatal("NewRenderPolicy() accepted an empty origin allowlist")
	}
}

func TestSnapshotRejectsHostLocalZoneAndNonCanonicalUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{
			name: "event Local time zone",
			mutate: func(value *Snapshot) {
				value.TimeZone = "Local"
			},
		},
		{
			name: "viewer Local time zone",
			mutate: func(value *Snapshot) {
				value.Recipient.ViewerTimeZone = "Local"
			},
		},
		{
			name: "uppercase UUID",
			mutate: func(value *Snapshot) {
				value.UID = "urn:uuid:" + strings.ToUpper(
					strings.TrimPrefix(testUID, "urn:uuid:"),
				)
			},
		},
		{
			name: "non canonical UUID shape",
			mutate: func(value *Snapshot) {
				value.UID = "urn:uuid:94e9ef6a47d94c7ca8f0f2329f1ed76f"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			snapshot := deterministicSnapshot(t, EffectInvite, 7)
			test.mutate(&snapshot)
			if err := snapshot.Validate(); err == nil {
				t.Fatal("Snapshot.Validate() error = nil")
			}
		})
	}
}

func TestSnapshotRejectsDangerousUnicodeFormatControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{
			name: "title bidi override",
			mutate: func(value *Snapshot) {
				value.Title = "Safe\u202etxt.exe"
			},
		},
		{
			name: "organizer zero width",
			mutate: func(value *Snapshot) {
				value.Organizer.DisplayName = "Teacher\u200bHidden"
			},
		},
		{
			name: "CTA word joiner",
			mutate: func(value *Snapshot) {
				value.Recipient.CTALabel = "Open\u2060TutorHub"
			},
		},
		{
			name: "deep link bidi isolate",
			mutate: func(value *Snapshot) {
				value.DeepLink = "https://tutorhub.example/\u2066hidden"
			},
		},
		{
			name: "time zone byte-order mark",
			mutate: func(value *Snapshot) {
				value.TimeZone = "America/New_York\ufeff"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			snapshot := deterministicSnapshot(t, EffectInvite, 7)
			test.mutate(&snapshot)
			if err := snapshot.Validate(); err == nil {
				t.Fatal("Snapshot.Validate() error = nil for dangerous Unicode control")
			}
		})
	}
}
