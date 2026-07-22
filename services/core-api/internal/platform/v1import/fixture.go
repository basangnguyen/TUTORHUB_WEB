package v1import

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	FixtureVersion   = 1
	MaxFixtureBytes  = 2 * 1024 * 1024
	MaxEntityRecords = 5000
)

var externalKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var sourceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
var fixtureKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,127}$`)

var (
	ErrFixtureTooLarge   = errors.New("V1 fixture exceeds the size limit")
	ErrFixtureInvalid    = errors.New("V1 fixture is invalid")
	ErrFixtureNotPrivate = errors.New("V1 fixture must contain anonymized data only")
)

type Fixture struct {
	FixtureVersion int                `json:"fixture_version"`
	FixtureKey     string             `json:"fixture_key"`
	SourceSystem   string             `json:"source_system"`
	Anonymized     bool               `json:"anonymized"`
	ExportedAt     time.Time          `json:"exported_at"`
	Users          []LegacyUser       `json:"users"`
	Tenants        []LegacyTenant     `json:"tenants"`
	Memberships    []LegacyMembership `json:"memberships"`
	Classes        []LegacyClass      `json:"classes"`
}

type LegacyUser struct {
	ExternalID  string    `json:"external_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Locale      string    `json:"locale"`
	Timezone    string    `json:"timezone"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type LegacyTenant struct {
	ExternalID string     `json:"external_id"`
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	Locale     string     `json:"locale"`
	Timezone   string     `json:"timezone"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

type LegacyMembership struct {
	ExternalID       string     `json:"external_id"`
	TenantExternalID string     `json:"tenant_external_id"`
	UserExternalID   string     `json:"user_external_id"`
	Role             string     `json:"role"`
	Status           string     `json:"status"`
	JoinedAt         *time.Time `json:"joined_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type LegacyClass struct {
	ExternalID          string     `json:"external_id"`
	TenantExternalID    string     `json:"tenant_external_id"`
	OwnerUserExternalID string     `json:"owner_user_external_id"`
	Code                string     `json:"code"`
	Title               string     `json:"title"`
	Description         string     `json:"description"`
	Timezone            string     `json:"timezone"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	ArchivedAt          *time.Time `json:"archived_at,omitempty"`
}

type ParsedFixture struct {
	Fixture Fixture
	SHA256  [sha256.Size]byte
}

func ParseFixture(data []byte) (ParsedFixture, error) {
	if len(data) > MaxFixtureBytes {
		return ParsedFixture{}, ErrFixtureTooLarge
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return ParsedFixture{}, fmt.Errorf("%w: %v", ErrFixtureInvalid, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fixture Fixture
	if err := decoder.Decode(&fixture); err != nil {
		return ParsedFixture{}, fmt.Errorf("%w: decode document: %v", ErrFixtureInvalid, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return ParsedFixture{}, fmt.Errorf("%w: %v", ErrFixtureInvalid, err)
	}
	if err := validateFixture(fixture); err != nil {
		return ParsedFixture{}, err
	}

	return ParsedFixture{Fixture: fixture, SHA256: sha256.Sum256(data)}, nil
}

func validateFixture(fixture Fixture) error {
	if fixture.FixtureVersion != FixtureVersion {
		return invalidFixture("fixture_version must be %d", FixtureVersion)
	}
	if !fixture.Anonymized {
		return ErrFixtureNotPrivate
	}
	if !sourceKeyPattern.MatchString(fixture.SourceSystem) {
		return invalidFixture("source_system has an invalid format")
	}
	if !fixtureKeyPattern.MatchString(fixture.FixtureKey) {
		return invalidFixture("fixture_key has an invalid format")
	}
	if fixture.ExportedAt.IsZero() {
		return invalidFixture("exported_at is required")
	}
	if len(fixture.Users) > MaxEntityRecords || len(fixture.Tenants) > MaxEntityRecords ||
		len(fixture.Memberships) > MaxEntityRecords || len(fixture.Classes) > MaxEntityRecords {
		return invalidFixture("an entity exceeds the %d record limit", MaxEntityRecords)
	}

	seen := make(map[string]struct{})
	for _, user := range fixture.Users {
		if err := validateExternalID("user", user.ExternalID, seen); err != nil {
			return err
		}
		if user.Email != strings.ToLower(strings.TrimSpace(user.Email)) ||
			!strings.HasSuffix(user.Email, ".invalid") {
			return ErrFixtureNotPrivate
		}
		if len(user.Email) < 3 || len(user.Email) > 320 {
			return invalidFixture("user %q has an invalid email length", user.ExternalID)
		}
		if err := validateCommonFields(user.ExternalID, user.DisplayName, user.Locale, user.Timezone, user.CreatedAt, user.UpdatedAt); err != nil {
			return err
		}
		if !oneOf(user.Status, "active", "disabled", "deleted") {
			return invalidFixture("user %q has unsupported status", user.ExternalID)
		}
	}

	seen = make(map[string]struct{})
	for _, tenant := range fixture.Tenants {
		if err := validateExternalID("tenant", tenant.ExternalID, seen); err != nil {
			return err
		}
		if tenant.Slug != strings.ToLower(strings.TrimSpace(tenant.Slug)) || len(tenant.Slug) < 3 || len(tenant.Slug) > 63 {
			return invalidFixture("tenant %q has an invalid slug", tenant.ExternalID)
		}
		if err := validateCommonFields(tenant.ExternalID, tenant.Name, tenant.Locale, tenant.Timezone, tenant.CreatedAt, tenant.UpdatedAt); err != nil {
			return err
		}
		if !oneOf(tenant.Status, "active", "disabled", "archived") {
			return invalidFixture("tenant %q has unsupported status", tenant.ExternalID)
		}
		if (tenant.Status == "archived") != (tenant.ArchivedAt != nil) {
			return invalidFixture("tenant %q has inconsistent archive fields", tenant.ExternalID)
		}
	}

	seen = make(map[string]struct{})
	for _, membership := range fixture.Memberships {
		if err := validateExternalID("membership", membership.ExternalID, seen); err != nil {
			return err
		}
		if !externalKeyPattern.MatchString(membership.TenantExternalID) || !externalKeyPattern.MatchString(membership.UserExternalID) {
			return invalidFixture("membership %q has an invalid reference", membership.ExternalID)
		}
		if !oneOf(membership.Role, "administrator", "instructor", "learner", "observer") {
			return invalidFixture("membership %q has unsupported role", membership.ExternalID)
		}
		if !oneOf(membership.Status, "active", "blocked", "removed") {
			return invalidFixture("membership %q has unsupported status", membership.ExternalID)
		}
		if membership.Status == "active" && membership.JoinedAt == nil {
			return invalidFixture("membership %q requires joined_at", membership.ExternalID)
		}
		if err := validateTimestamps(membership.ExternalID, membership.CreatedAt, membership.UpdatedAt); err != nil {
			return err
		}
	}

	seen = make(map[string]struct{})
	for _, class := range fixture.Classes {
		if err := validateExternalID("class", class.ExternalID, seen); err != nil {
			return err
		}
		if !externalKeyPattern.MatchString(class.TenantExternalID) || !externalKeyPattern.MatchString(class.OwnerUserExternalID) {
			return invalidFixture("class %q has an invalid reference", class.ExternalID)
		}
		if class.Code != strings.ToUpper(strings.TrimSpace(class.Code)) || len(class.Code) < 3 || len(class.Code) > 32 {
			return invalidFixture("class %q has an invalid code", class.ExternalID)
		}
		if strings.TrimSpace(class.Title) == "" || len(class.Title) > 200 || len(class.Description) > 4000 {
			return invalidFixture("class %q has invalid text fields", class.ExternalID)
		}
		if err := validateTimezone(class.ExternalID, class.Timezone); err != nil {
			return err
		}
		if err := validateTimestamps(class.ExternalID, class.CreatedAt, class.UpdatedAt); err != nil {
			return err
		}
		if !oneOf(class.Status, "draft", "open", "closed") {
			return invalidFixture("class %q has unsupported status", class.ExternalID)
		}
		if (class.Status == "closed") != (class.ArchivedAt != nil) {
			return invalidFixture("class %q has inconsistent archive fields", class.ExternalID)
		}
	}

	return nil
}

func validateExternalID(entity string, externalID string, seen map[string]struct{}) error {
	if !externalKeyPattern.MatchString(externalID) {
		return invalidFixture("%s external_id has an invalid format", entity)
	}
	if _, exists := seen[externalID]; exists {
		return invalidFixture("%s external_id %q is duplicated", entity, externalID)
	}
	seen[externalID] = struct{}{}
	return nil
}

func validateCommonFields(externalID string, name string, locale string, timezone string, createdAt time.Time, updatedAt time.Time) error {
	if strings.TrimSpace(name) == "" || len(name) > 200 {
		return invalidFixture("record %q has an invalid name", externalID)
	}
	if !oneOf(locale, "vi", "en") {
		return invalidFixture("record %q has unsupported locale", externalID)
	}
	if err := validateTimezone(externalID, timezone); err != nil {
		return err
	}
	return validateTimestamps(externalID, createdAt, updatedAt)
}

func validateTimezone(externalID string, timezone string) error {
	if timezone != strings.TrimSpace(timezone) || timezone == "" || len(timezone) > 100 || strings.EqualFold(timezone, "local") {
		return invalidFixture("record %q has an invalid timezone", externalID)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return invalidFixture("record %q has an unknown IANA timezone", externalID)
	}
	return nil
}

func validateTimestamps(externalID string, createdAt time.Time, updatedAt time.Time) error {
	if createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return invalidFixture("record %q has invalid timestamps", externalID)
	}
	return nil
}

func invalidFixture(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFixtureInvalid, fmt.Sprintf(format, args...))
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func deterministicTargetID(sourceSystem string, entityType EntityType, externalID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(sourceSystem+"\x00"+string(entityType)+"\x00"+externalID))
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder, "$"); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func consumeJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode object key at %s: %w", path, err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("non-string object key at %s", path)
			}
			folded := strings.ToLower(key)
			if _, exists := seen[folded]; exists {
				return fmt.Errorf("duplicate object key %q at %s", key, path)
			}
			seen[folded] = struct{}{}
			if err := consumeJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("unterminated object at %s", path)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := consumeJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("unterminated array at %s", path)
		}
	default:
		return fmt.Errorf("unexpected delimiter %q at %s", delimiter, path)
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON document")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}
