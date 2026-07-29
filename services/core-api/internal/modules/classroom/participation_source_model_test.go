package classroom

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParticipationSourceRefValidateTaggedUnion(t *testing.T) {
	t.Parallel()
	sessionID := uuid.New()
	seriesID := uuid.New()
	tests := []struct {
		name   string
		source ParticipationSourceRef
		valid  bool
	}{
		{
			name:   "session",
			source: SessionParticipationSource(sessionID),
			valid:  true,
		},
		{
			name:   "series",
			source: SeriesParticipationSource(seriesID),
			valid:  true,
		},
		{
			name:   "occurrence",
			source: OccurrenceParticipationSource(seriesID, "20260727T030000Z"),
			valid:  true,
		},
		{
			name: "mixed session and series",
			source: ParticipationSourceRef{
				Kind:      ParticipationSourceSession,
				SessionID: sessionID,
				SeriesID:  seriesID,
			},
		},
		{
			name:   "occurrence leading whitespace",
			source: OccurrenceParticipationSource(seriesID, " 20260727T030000Z"),
		},
		{
			name:   "occurrence trailing whitespace",
			source: OccurrenceParticipationSource(seriesID, "20260727T030000Z "),
		},
		{
			name: "occurrence missing series",
			source: ParticipationSourceRef{
				Kind:          ParticipationSourceOccurrence,
				OccurrenceKey: "20260727T030000Z",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.source.Normalized()
			if test.valid && err != nil {
				t.Fatalf("Normalized() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidParticipationInput) {
				t.Fatalf("Normalized() error = %v, want invalid participation input", err)
			}
		})
	}
}

func TestParticipationSourceRefOccurrenceKeyUsesCharacterBoundary(t *testing.T) {
	t.Parallel()
	seriesID := uuid.New()
	eightRunes := strings.Repeat("界", 8)
	if err := OccurrenceParticipationSource(seriesID, eightRunes).Validate(); err != nil {
		t.Fatalf("eight-rune occurrence key rejected: %v", err)
	}
	if err := OccurrenceParticipationSource(
		seriesID,
		strings.Repeat("界", 128),
	).Validate(); err != nil {
		t.Fatalf("128-rune occurrence key rejected: %v", err)
	}
	if err := OccurrenceParticipationSource(
		seriesID,
		strings.Repeat("界", 129),
	).Validate(); !errors.Is(err, ErrInvalidParticipationInput) {
		t.Fatalf("129-rune occurrence key error = %v", err)
	}
}

func TestParticipationSourceFingerprintSeparatesKindsAndOccurrences(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	sources := []ParticipationSourceRef{
		SessionParticipationSource(id),
		SeriesParticipationSource(id),
		OccurrenceParticipationSource(id, "20260727T030000Z"),
		OccurrenceParticipationSource(id, "20260728T030000Z"),
	}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		fingerprint, err := source.Fingerprint()
		if err != nil {
			t.Fatalf("Fingerprint(%+v) error = %v", source, err)
		}
		if len(fingerprint) != 64 {
			t.Fatalf("fingerprint length = %d, want 64", len(fingerprint))
		}
		if _, duplicate := seen[fingerprint]; duplicate {
			t.Fatalf("duplicate fingerprint for %+v", source)
		}
		seen[fingerprint] = struct{}{}
	}
}
