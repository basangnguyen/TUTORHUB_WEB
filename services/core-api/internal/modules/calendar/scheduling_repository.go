package calendar

import (
	"context"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type SchedulingRepository interface {
	GetWorkingSchedule(
		context.Context,
		tenancy.Context,
		time.Time,
	) (WorkingSchedule, error)
	PutWorkingSchedule(
		context.Context,
		tenancy.Context,
		PutWorkingScheduleInput,
		time.Time,
	) (WorkingSchedule, error)
	LoadAvailability(
		context.Context,
		tenancy.Context,
		availabilityParams,
	) ([]availabilitySource, error)
}
