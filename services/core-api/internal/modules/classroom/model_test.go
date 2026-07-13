package classroom

import (
	"testing"

	"github.com/google/uuid"
)

func TestCreateClassParamsNormalize(t *testing.T) {
	t.Parallel()

	params, err := (CreateClassParams{
		OwnerUserID: uuid.New(),
		Code:        " sec101 ",
		Title:       "  An toàn thông tin  ",
		Description: "  Mô tả  ",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize params: %v", err)
	}
	if params.Code != "SEC101" ||
		params.Title != "An toàn thông tin" ||
		params.Description != "Mô tả" {
		t.Fatalf("unexpected normalized params: %+v", params)
	}
}

func TestCreateClassParamsRejectInvalidValues(t *testing.T) {
	t.Parallel()

	testCases := []CreateClassParams{
		{Code: "SEC101", Title: "Class"},
		{OwnerUserID: uuid.New(), Code: "x", Title: "Class"},
		{OwnerUserID: uuid.New(), Code: "SEC101"},
	}
	for _, params := range testCases {
		if _, err := params.normalized(); err == nil {
			t.Fatalf("expected invalid params: %+v", params)
		}
	}
}
