package outboxworker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var eventNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

type EventType struct {
	name    string
	version uint16
}

func NewEventType(name string, version uint16) (EventType, error) {
	name = strings.TrimSpace(name)
	if !eventNamePattern.MatchString(name) {
		return EventType{}, fmt.Errorf("event name must be a dotted lowercase identifier")
	}
	if version == 0 {
		return EventType{}, fmt.Errorf("event version must be positive")
	}
	return EventType{name: name, version: version}, nil
}

func MustEventType(name string, version uint16) EventType {
	eventType, err := NewEventType(name, version)
	if err != nil {
		panic(err)
	}
	return eventType
}

func ParseEventType(value string) (EventType, error) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndex(value, ".v")
	if separator < 1 || separator+2 >= len(value) {
		return EventType{}, fmt.Errorf("event type must end in .vN")
	}
	version, err := strconv.ParseUint(value[separator+2:], 10, 16)
	if err != nil || version == 0 {
		return EventType{}, fmt.Errorf("event type version must be a positive 16-bit integer")
	}
	return NewEventType(value[:separator], uint16(version))
}

func (eventType EventType) Name() string {
	return eventType.name
}

func (eventType EventType) Version() uint16 {
	return eventType.version
}

func (eventType EventType) String() string {
	if eventType.name == "" || eventType.version == 0 {
		return ""
	}
	return eventType.name + ".v" + strconv.FormatUint(uint64(eventType.version), 10)
}

type Event struct {
	ID            uuid.UUID
	TenantID      uuid.NullUUID
	AggregateType string
	AggregateID   uuid.UUID
	Type          EventType
	Payload       json.RawMessage
	OccurredAt    time.Time
	AvailableAt   time.Time
	Attempts      int
	Lease         LeaseRef
	Reclaimed     bool
	LeasedAt      time.Time
	LeasedUntil   time.Time
}

type LeaseRef struct {
	EventID uuid.UUID
	OwnerID uuid.UUID
	Token   int64
}

func (lease LeaseRef) Validate() error {
	if lease.EventID == uuid.Nil || lease.OwnerID == uuid.Nil || lease.Token < 1 {
		return fmt.Errorf("lease reference is incomplete")
	}
	return nil
}
