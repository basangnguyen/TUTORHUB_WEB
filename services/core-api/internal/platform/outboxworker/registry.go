package outboxworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var handlerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var aggregateTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type TenantMode uint8

const (
	TenantRequired TenantMode = iota + 1
	TenantOptional
)

type HandlerSpec[T any] struct {
	EventType     EventType
	HandlerName   string
	AggregateType string
	TenantMode    TenantMode
	Validate      func(Event, T) error
	// Handle must stop promptly when ctx is cancelled. Go cannot safely terminate a
	// non-cooperative handler goroutine, so every production registration must have
	// a cancellation/timeout test before it is added to the worker allowlist.
	Handle func(context.Context, Event, T) error
}

type RegisteredHandler struct {
	EventType   EventType
	HandlerName string
	handle      func(context.Context, Event) error
}

func (handler RegisteredHandler) Handle(ctx context.Context, event Event) error {
	if handler.handle == nil {
		return Permanent(ErrorCodeInvalidEventContext)
	}
	return handler.handle(ctx, event)
}

type Registry struct {
	handlers map[string]RegisteredHandler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]RegisteredHandler)}
}

func RegisterJSON[T any](registry *Registry, spec HandlerSpec[T]) error {
	if registry == nil {
		return fmt.Errorf("handler registry is required")
	}
	eventType := spec.EventType.String()
	if eventType == "" {
		return fmt.Errorf("registered event type is required")
	}
	if !handlerNamePattern.MatchString(spec.HandlerName) {
		return fmt.Errorf("handler name must be a bounded lowercase identifier")
	}
	if !aggregateTypePattern.MatchString(spec.AggregateType) {
		return fmt.Errorf("aggregate type must be a bounded lowercase identifier")
	}
	if spec.TenantMode != TenantRequired && spec.TenantMode != TenantOptional {
		return fmt.Errorf("tenant mode must be explicit")
	}
	if spec.Handle == nil {
		return fmt.Errorf("handler function is required")
	}
	if _, exists := registry.handlers[eventType]; exists {
		return fmt.Errorf("event type %s is already registered", eventType)
	}

	registry.handlers[eventType] = RegisteredHandler{
		EventType:   spec.EventType,
		HandlerName: spec.HandlerName,
		handle: func(ctx context.Context, event Event) error {
			if event.Type != spec.EventType || event.AggregateType != spec.AggregateType {
				return Permanent(ErrorCodeInvalidEventContext)
			}
			if spec.TenantMode == TenantRequired && !event.TenantID.Valid {
				return Permanent(ErrorCodeInvalidEventContext)
			}

			var payload T
			decoder := json.NewDecoder(bytes.NewReader(event.Payload))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil {
				return Permanent(ErrorCodeInvalidPayload)
			}
			if err := ensureJSONEOF(decoder); err != nil {
				return Permanent(ErrorCodeInvalidPayload)
			}
			if spec.Validate != nil {
				if err := spec.Validate(event, payload); err != nil {
					return Permanent(ErrorCodeInvalidPayload)
				}
			}
			return spec.Handle(ctx, event, payload)
		},
	}
	return nil
}

func (registry *Registry) Resolve(eventType EventType) (RegisteredHandler, bool) {
	if registry == nil {
		return RegisteredHandler{}, false
	}
	handler, ok := registry.handlers[eventType.String()]
	return handler, ok
}

func (registry *Registry) Allowlist() []string {
	if registry == nil {
		return nil
	}
	allowlist := make([]string, 0, len(registry.handlers))
	for eventType := range registry.handlers {
		allowlist = append(allowlist, eventType)
	}
	sort.Strings(allowlist)
	return allowlist
}

func (registry *Registry) HandlerNames() map[string]string {
	if registry == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(registry.handlers))
	for eventType, handler := range registry.handlers {
		result[eventType] = handler.HandlerName
	}
	return result
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil && len(strings.TrimSpace(string(extra))) > 0 {
		return fmt.Errorf("payload contains multiple JSON values")
	}
	return err
}
