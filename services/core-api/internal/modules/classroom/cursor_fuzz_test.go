package classroom

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

func FuzzDecodeClassCursor(f *testing.F) {
	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	foreignTenantID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	classID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	createdAt := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	status := ClassStatusActive
	valid, err := encodeClassCursor(
		ClassCursor{ID: classID, CreatedAt: createdAt},
		tenantID,
		&status,
	)
	if err != nil {
		f.Fatalf("encode valid class cursor: %v", err)
	}
	foreign, err := encodeClassCursor(
		ClassCursor{ID: classID, CreatedAt: createdAt},
		foreignTenantID,
		&status,
	)
	if err != nil {
		f.Fatalf("encode foreign class cursor: %v", err)
	}
	if _, err := decodeClassCursor(foreign, tenantID, &status); err == nil {
		f.Fatal("foreign-tenant class cursor was accepted")
	}
	unknown := encodeClassCursorPayloadForFuzz(f, classCursorPayload{
		CreatedAt: createdAt.Format(time.RFC3339Nano),
		ID:        classID.String(),
		Status:    string(status),
		ScopeHash: classListScopeHash(tenantID, &status),
	})
	f.Add("")
	f.Add("not-a-class-cursor")
	f.Add(valid)
	f.Add(foreign)
	f.Add(unknown)
	f.Add(classCursorPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"created_at":"`+createdAt.Format(time.RFC3339Nano)+`","id":"`+classID.String()+`","status":"active","scope_hash":"bad"} trailing`)))

	f.Fuzz(func(t *testing.T, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		cursor, err := decodeClassCursor(value, tenantID, &status)
		if err != nil {
			return
		}
		if cursor == nil || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
			t.Fatalf("accepted invalid class cursor: value=%q cursor=%+v", value, cursor)
		}
	})
}

func FuzzDecodeRosterCursor(f *testing.F) {
	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	foreignTenantID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	classID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	userID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	search := "å user"
	status := EnrollmentStatusActive
	valid, err := encodeRosterCursor(
		RosterCursor{UserID: userID}, tenantID, classID, search, &status,
	)
	if err != nil {
		f.Fatalf("encode valid roster cursor: %v", err)
	}
	foreign, err := encodeRosterCursor(
		RosterCursor{UserID: userID}, foreignTenantID, classID, search, &status,
	)
	if err != nil {
		f.Fatalf("encode foreign roster cursor: %v", err)
	}
	if _, err := decodeRosterCursor(foreign, tenantID, classID, search, &status); err == nil {
		f.Fatal("foreign-tenant roster cursor was accepted")
	}
	unknown := encodeRosterCursorPayloadForFuzz(f, rosterCursorPayload{
		UserID:     userID.String(),
		FilterHash: rosterFilterHash(tenantID, classID, search, &status),
	})
	f.Add("")
	f.Add("not-a-roster-cursor")
	f.Add(valid)
	f.Add(foreign)
	f.Add(unknown)

	f.Fuzz(func(t *testing.T, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		cursor, err := decodeRosterCursor(value, tenantID, classID, search, &status)
		if err != nil {
			return
		}
		if cursor == nil || cursor.UserID == uuid.Nil {
			t.Fatalf("accepted invalid roster cursor: value=%q cursor=%+v", value, cursor)
		}
	})
}

func FuzzNormalizeRosterSearch(f *testing.F) {
	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	classID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	for _, seed := range []string{
		"",
		"  Learner   Name  ",
		"%_literal",
		"A\u030A learner",
		"Å learner",
		"\u200b\tstudent\nname",
		strings.Repeat("界", maximumRosterSearchLength+1),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, search string) {
		params, err := normalizeListRosterInput(
			ListRosterInput{Search: search, Limit: 1}, tenantID, classID,
		)
		if err != nil {
			return
		}
		want := strings.ToLower(norm.NFC.String(strings.Join(strings.Fields(search), " ")))
		if params.Search != want || len([]rune(params.Search)) > maximumRosterSearchLength {
			t.Fatalf("unexpected normalized roster search: input=%q got=%q want=%q", search, params.Search, want)
		}
		if (strings.Contains(search, "%") && !strings.Contains(params.Search, "%")) ||
			(strings.Contains(search, "_") && !strings.Contains(params.Search, "_")) {
			t.Fatalf("wildcard-like character was not preserved literally: input=%q got=%q", search, params.Search)
		}
	})
}

type cursorTestHelper interface {
	Helper()
	Fatalf(string, ...any)
}

func encodeClassCursorPayloadForFuzz(f cursorTestHelper, payload classCursorPayload) string {
	f.Helper()
	contents, err := json.Marshal(struct {
		classCursorPayload
		Unexpected bool `json:"unexpected"`
	}{classCursorPayload: payload, Unexpected: true})
	if err != nil {
		f.Fatalf("marshal class cursor payload: %v", err)
	}
	return classCursorPrefix + base64.RawURLEncoding.EncodeToString(contents)
}

func encodeRosterCursorPayloadForFuzz(f cursorTestHelper, payload rosterCursorPayload) string {
	f.Helper()
	contents, err := json.Marshal(struct {
		rosterCursorPayload
		Unexpected bool `json:"unexpected"`
	}{rosterCursorPayload: payload, Unexpected: true})
	if err != nil {
		f.Fatalf("marshal roster cursor payload: %v", err)
	}
	return rosterCursorPrefix + base64.RawURLEncoding.EncodeToString(contents)
}

func TestClassAndRosterCursorPayloadsRejectUnknownAndTrailingFields(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	classID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	userID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	createdAt := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	classStatus := ClassStatusActive
	classPayload := classCursorPayload{
		CreatedAt: createdAt.Format(time.RFC3339Nano),
		ID:        classID.String(),
		Status:    string(classStatus),
		ScopeHash: classListScopeHash(tenantID, &classStatus),
	}
	if _, err := decodeClassCursor(
		encodeClassCursorPayloadForFuzz(t, classPayload), tenantID, &classStatus,
	); err == nil {
		t.Fatal("class cursor with unknown field was accepted")
	}
	classContents, err := json.Marshal(classPayload)
	if err != nil {
		t.Fatalf("marshal class cursor payload: %v", err)
	}
	classTrailing := classCursorPrefix + base64.RawURLEncoding.EncodeToString(
		append(classContents, []byte(" {}")...),
	)
	if _, err := decodeClassCursor(classTrailing, tenantID, &classStatus); err == nil {
		t.Fatal("class cursor with trailing JSON value was accepted")
	}

	search := "å user"
	rosterStatus := EnrollmentStatusActive
	rosterPayload := rosterCursorPayload{
		UserID:     userID.String(),
		FilterHash: rosterFilterHash(tenantID, classID, search, &rosterStatus),
	}
	if _, err := decodeRosterCursor(
		encodeRosterCursorPayloadForFuzz(t, rosterPayload),
		tenantID, classID, search, &rosterStatus,
	); err == nil {
		t.Fatal("roster cursor with unknown field was accepted")
	}
	rosterContents, err := json.Marshal(rosterPayload)
	if err != nil {
		t.Fatalf("marshal roster cursor payload: %v", err)
	}
	rosterTrailing := rosterCursorPrefix + base64.RawURLEncoding.EncodeToString(
		append(rosterContents, []byte(" {}")...),
	)
	if _, err := decodeRosterCursor(
		rosterTrailing, tenantID, classID, search, &rosterStatus,
	); err == nil {
		t.Fatal("roster cursor with trailing JSON value was accepted")
	}
}
