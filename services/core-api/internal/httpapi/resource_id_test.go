package httpapi

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseResourceUUIDRequiresCanonicalNonNilValue(t *testing.T) {
	t.Parallel()

	identifier := uuid.MustParse("4ab9249b-d5a7-4ab7-9be6-f98f63e03fd7")
	for _, accepted := range []string{identifier.String(), strings.ToUpper(identifier.String())} {
		parsed, ok := parseResourceUUID(accepted)
		if !ok || parsed != identifier {
			t.Fatalf("canonical UUID %q was rejected: id=%s ok=%t", accepted, parsed, ok)
		}
	}

	for _, rejected := range []string{
		"",
		uuid.Nil.String(),
		"4ab9249bd5a74ab79be6f98f63e03fd7",
		"{4ab9249b-d5a7-4ab7-9be6-f98f63e03fd7}",
		"urn:uuid:4ab9249b-d5a7-4ab7-9be6-f98f63e03fd7",
		" 4ab9249b-d5a7-4ab7-9be6-f98f63e03fd7",
		"4ab9249b-d5a7-4ab7-9be6-f98f63e03fd7/",
	} {
		if parsed, ok := parseResourceUUID(rejected); ok || parsed != uuid.Nil {
			t.Fatalf("non-canonical UUID %q was accepted as %s", rejected, parsed)
		}
	}
}

func FuzzParseResourceUUID(f *testing.F) {
	for _, seed := range []string{
		"",
		uuid.Nil.String(),
		uuid.NewString(),
		strings.ToUpper(uuid.NewString()),
		"ffffffffffffffffffffffffffffffff",
		"urn:uuid:" + uuid.NewString(),
		strings.Repeat("a", 4096),
		"\u0000\u200b",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, value string) {
		identifier, ok := parseResourceUUID(value)
		if !ok {
			if identifier != uuid.Nil {
				t.Fatalf("rejected UUID returned non-nil value %s", identifier)
			}
			return
		}
		if identifier == uuid.Nil || len(value) != 36 ||
			!strings.EqualFold(value, identifier.String()) {
			t.Fatalf("accepted non-canonical UUID %q as %s", value, identifier)
		}
	})
}
