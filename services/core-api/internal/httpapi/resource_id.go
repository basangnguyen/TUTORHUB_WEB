package httpapi

import (
	"strings"

	"github.com/google/uuid"
)

// parseResourceUUID accepts the canonical hyphenated UUID representation only
// (case-insensitively). Keeping one URL representation avoids aliases between the
// application, edge controls, logs, and authorization tests.
func parseResourceUUID(value string) (uuid.UUID, bool) {
	if len(value) != len(uuid.Nil.String()) {
		return uuid.Nil, false
	}
	identifier, err := uuid.Parse(value)
	if err != nil || identifier == uuid.Nil ||
		!strings.EqualFold(value, identifier.String()) {
		return uuid.Nil, false
	}
	return identifier, true
}
