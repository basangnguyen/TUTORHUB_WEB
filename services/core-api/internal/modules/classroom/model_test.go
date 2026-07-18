package classroom

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateClassParamsNormalize(t *testing.T) {
	t.Parallel()

	timezone := " Asia/Ho_Chi_Minh "
	params, err := (CreateClassParams{
		OwnerUserID: uuid.New(),
		Code:        " sec101 ",
		Title:       "  An toàn thông tin  ",
		Description: "  Mô tả  ",
		Timezone:    &timezone,
	}).normalized()
	if err != nil {
		t.Fatalf("normalize params: %v", err)
	}
	if params.Code != "SEC101" ||
		params.Title != "An toàn thông tin" ||
		params.Description != "Mô tả" ||
		params.Timezone == nil ||
		*params.Timezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("unexpected normalized params: %+v", params)
	}
}

func TestCreateClassParamsRejectInvalidValues(t *testing.T) {
	t.Parallel()

	invalidTimezone := "Local"
	unknownTimezone := "Mars/Olympus"
	testCases := []CreateClassParams{
		{Code: "SEC101", Title: "Class"},
		{OwnerUserID: uuid.New(), Code: "x", Title: "Class"},
		{OwnerUserID: uuid.New(), Code: "SEC101"},
		{
			OwnerUserID: uuid.New(), Code: "SEC101", Title: "Class",
			Description: strings.Repeat("x", 4001),
		},
		{
			OwnerUserID: uuid.New(), Code: "SEC101", Title: "Class",
			Timezone: &invalidTimezone,
		},
		{
			OwnerUserID: uuid.New(), Code: "SEC101", Title: "Class",
			Timezone: &unknownTimezone,
		},
	}
	for _, params := range testCases {
		if _, err := params.normalized(); !errors.Is(err, ErrInvalidClassInput) {
			t.Fatalf("expected invalid params: params=%+v err=%v", params, err)
		}
	}
}

func TestUpdateClassParamsNormalizeAndValidate(t *testing.T) {
	t.Parallel()

	code := " sec-202 "
	title := "  Mạng nâng cao "
	description := "  "
	timezone := "UTC"
	status := ClassStatus("ACTIVE")
	params, err := (UpdateClassParams{
		Code:            &code,
		Title:           &title,
		Description:     &description,
		Timezone:        &timezone,
		Status:          &status,
		ExpectedVersion: 3,
	}).normalized()
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	if params.Code == nil || *params.Code != "SEC-202" ||
		params.Title == nil || *params.Title != "Mạng nâng cao" ||
		params.Description == nil || *params.Description != "" ||
		params.Timezone == nil || *params.Timezone != "UTC" ||
		params.Status == nil || *params.Status != ClassStatusActive {
		t.Fatalf("unexpected normalized update: %+v", params)
	}

	if _, err := (UpdateClassParams{ExpectedVersion: 1}).normalized(); !errors.Is(
		err,
		ErrInvalidClassInput,
	) {
		t.Fatalf("empty update must be rejected, got %v", err)
	}
	archived := ClassStatusArchived
	if _, err := (UpdateClassParams{
		Status: &archived, ExpectedVersion: 1,
	}).normalized(); !errors.Is(err, ErrInvalidClassInput) {
		t.Fatalf("direct archived update must be rejected, got %v", err)
	}
}

func TestDirectClassStatusTransitionMatrix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		current ClassStatus
		next    ClassStatus
		valid   bool
	}{
		{current: ClassStatusDraft, next: ClassStatusDraft, valid: true},
		{current: ClassStatusDraft, next: ClassStatusActive, valid: true},
		{current: ClassStatusActive, next: ClassStatusActive, valid: true},
		{current: ClassStatusActive, next: ClassStatusDraft},
		{current: ClassStatusArchived, next: ClassStatusActive},
	} {
		err := validateDirectStatusTransition(test.current, test.next)
		if (err == nil) != test.valid {
			t.Fatalf(
				"unexpected transition result %s -> %s: valid=%t err=%v",
				test.current,
				test.next,
				test.valid,
				err,
			)
		}
	}
}

func TestTransferClassOwnershipParams(t *testing.T) {
	t.Parallel()

	valid := TransferClassOwnershipParams{
		NewOwnerUserID: uuid.New(), ExpectedVersion: 2,
	}
	if _, err := valid.normalized(); err != nil {
		t.Fatalf("valid transfer params: %v", err)
	}
	if _, err := (TransferClassOwnershipParams{
		NewOwnerUserID: uuid.New(),
	}).normalized(); !errors.Is(err, ErrInvalidClassInput) {
		t.Fatalf("missing version must be rejected, got %v", err)
	}
	if _, err := (TransferClassOwnershipParams{
		ExpectedVersion: 1,
	}).normalized(); !errors.Is(err, ErrClassOwnerUnavailable) {
		t.Fatalf("missing target must be unavailable, got %v", err)
	}
}
