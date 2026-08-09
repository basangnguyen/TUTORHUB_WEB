package media

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProviderLifecycleServiceStartProvisioningEnsuresRoomThenActivatesCAS(t *testing.T) {
	t.Parallel()

	spaceID := uuid.New()
	roomInstanceID := uuid.New()
	const (
		providerRoomName = "r_0123456789abcdef0123456789abcdef"
		providerRoomSID  = "RM_provider_room_sid"
	)
	order := []string{}
	provisioning := MediaSpace{
		ID: spaceID, Status: SpaceStatusOpen,
		ActiveRoomInstance: &RoomInstance{
			ID: roomInstanceID, Status: RoomInstanceProvisioning,
			ProviderRoomName: providerRoomName,
		},
	}
	active := MediaSpace{
		ID: spaceID, Status: SpaceStatusOpen,
		ActiveRoomInstance: &RoomInstance{
			ID: roomInstanceID, Status: RoomInstanceActive,
			ProviderRoomSID: providerRoomSID,
		},
	}
	base := &fakeProviderLifecycleBase{order: &order, startResult: provisioning}
	bindings := &fakeRoomBindingRepository{order: &order, activateResult: active}
	provider := &fakeRoomProvider{
		order: &order, ensureResult: ProviderRoom{SID: providerRoomSID},
	}
	service := newTestProviderLifecycleService(t, base, bindings, provider)
	access := lifecycleAccess()
	input := TransitionInput{
		ExpectedVersion: 3, IdempotencyKey: "provider-start-0001", ReasonCode: "scheduled_start",
	}

	result, err := service.StartSpace(context.Background(), access, spaceID, input)
	if err != nil {
		t.Fatalf("start provider-backed space: %v", err)
	}
	if result.ActiveRoomInstance == nil || result.ActiveRoomInstance.Status != RoomInstanceActive ||
		result.ActiveRoomInstance.ProviderRoomSID != providerRoomSID {
		t.Fatalf("unexpected activated space: %+v", result)
	}
	if !reflect.DeepEqual(order, []string{"base.start", "provider.ensure", "bindings.activate"}) {
		t.Fatalf("provider call escaped intent/CAS ordering: %+v", order)
	}
	if base.startCalls != 1 || base.startSpaceID != spaceID || base.startInput != input ||
		provider.ensureCalls != 1 || provider.ensuredRoomName != providerRoomName ||
		bindings.activateCalls != 1 || bindings.activateSpaceID != spaceID ||
		bindings.activateRoomInstanceID != roomInstanceID ||
		bindings.activateProviderSID != providerRoomSID ||
		!bindings.activateAt.Equal(mediaTestTime) ||
		bindings.activateAccess.TenantID != access.TenantID ||
		bindings.activateAccess.ActorID != access.ActorID {
		t.Fatalf("unexpected provider reconciliation call: base=%+v provider=%+v bindings=%+v", base, provider, bindings)
	}
}

func TestProviderLifecycleServiceStartRetryReturnsActiveInstanceWithoutProviderCall(t *testing.T) {
	t.Parallel()

	spaceID := uuid.New()
	active := MediaSpace{
		ID: spaceID, Status: SpaceStatusOpen,
		ActiveRoomInstance: &RoomInstance{
			ID: uuid.New(), Status: RoomInstanceActive, ProviderRoomSID: "RM_bound_sid",
		},
	}
	order := []string{}
	base := &fakeProviderLifecycleBase{order: &order, startResult: active}
	bindings := &fakeRoomBindingRepository{order: &order}
	provider := &fakeRoomProvider{order: &order}
	service := newTestProviderLifecycleService(t, base, bindings, provider)

	result, err := service.StartSpace(
		context.Background(), lifecycleAccess(), spaceID,
		TransitionInput{ExpectedVersion: 4, IdempotencyKey: "provider-start-retry-0001"},
	)
	if err != nil {
		t.Fatalf("retry active space: %v", err)
	}
	if result.ActiveRoomInstance == nil || result.ActiveRoomInstance.ProviderRoomSID != "RM_bound_sid" ||
		provider.ensureCalls != 0 || bindings.activateCalls != 0 ||
		!reflect.DeepEqual(order, []string{"base.start"}) {
		t.Fatalf("active retry called provider or CAS again: result=%+v order=%+v", result, order)
	}
}

func TestProviderLifecycleServiceStartProviderOutageReturnsTypedUnavailableWithoutActivation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerRoom ProviderRoom
		providerErr  error
	}{
		{name: "provider error", providerErr: errors.New("provider-sensitive-detail")},
		{name: "missing provider SID", providerRoom: ProviderRoom{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			spaceID := uuid.New()
			order := []string{}
			base := &fakeProviderLifecycleBase{order: &order, startResult: MediaSpace{
				ID: spaceID, Status: SpaceStatusOpen,
				ActiveRoomInstance: &RoomInstance{
					ID: uuid.New(), Status: RoomInstanceProvisioning,
					ProviderRoomName: "r_0123456789abcdef0123456789abcdef",
				},
			}}
			bindings := &fakeRoomBindingRepository{order: &order}
			provider := &fakeRoomProvider{
				order: &order, ensureResult: test.providerRoom, ensureErr: test.providerErr,
			}
			service := newTestProviderLifecycleService(t, base, bindings, provider)

			result, err := service.StartSpace(
				context.Background(), lifecycleAccess(), spaceID,
				TransitionInput{ExpectedVersion: 1, IdempotencyKey: "provider-outage-0001"},
			)
			if !errors.Is(err, ErrMediaProviderUnavailable) ||
				strings.Contains(err.Error(), "provider-sensitive-detail") {
				t.Fatalf("provider outage was not a redacted typed 503 error: %v", err)
			}
			if result != (MediaSpace{}) || bindings.activateCalls != 0 ||
				!reflect.DeepEqual(order, []string{"base.start", "provider.ensure"}) {
				t.Fatalf("provider outage left an active projection: result=%+v order=%+v", result, order)
			}
		})
	}
}

func TestProviderLifecycleServiceStartRetriesSameRoomAfterActivationConflict(t *testing.T) {
	t.Parallel()

	spaceID := uuid.New()
	roomInstanceID := uuid.New()
	const (
		providerRoomName = "r_0123456789abcdef0123456789abcdef"
		providerRoomSID  = "RM_reused_provider_sid"
	)
	provisioning := MediaSpace{
		ID: spaceID, Status: SpaceStatusOpen,
		ActiveRoomInstance: &RoomInstance{
			ID: roomInstanceID, Status: RoomInstanceProvisioning,
			ProviderRoomName: providerRoomName,
		},
	}
	active := MediaSpace{
		ID: spaceID, Status: SpaceStatusOpen,
		ActiveRoomInstance: &RoomInstance{
			ID: roomInstanceID, Status: RoomInstanceActive, ProviderRoomSID: providerRoomSID,
		},
	}
	base := &fakeProviderLifecycleBase{startResult: provisioning}
	bindings := &fakeRoomBindingRepository{
		activateResult: active,
		activateErrors: []error{ErrSpaceVersionConflict, nil},
	}
	provider := &fakeRoomProvider{ensureResult: ProviderRoom{SID: providerRoomSID}}
	service := newTestProviderLifecycleService(t, base, bindings, provider)
	input := TransitionInput{ExpectedVersion: 2, IdempotencyKey: "provider-cas-retry-0001"}

	_, err := service.StartSpace(context.Background(), lifecycleAccess(), spaceID, input)
	if !errors.Is(err, ErrSpaceVersionConflict) {
		t.Fatalf("expected activation CAS conflict, got %v", err)
	}
	result, err := service.StartSpace(context.Background(), lifecycleAccess(), spaceID, input)
	if err != nil {
		t.Fatalf("retry activation conflict: %v", err)
	}
	if result.ActiveRoomInstance == nil || result.ActiveRoomInstance.ProviderRoomSID != providerRoomSID ||
		provider.ensureCalls != 2 || provider.ensuredRoomName != providerRoomName ||
		bindings.activateCalls != 2 || provider.deleteCalls != 0 {
		t.Fatalf("activation retry did not converge on one provider room: result=%+v provider=%+v bindings=%+v", result, provider, bindings)
	}
}

func TestProviderLifecycleServiceStartCleansRoomAfterDefiniteConcurrentTerminalState(t *testing.T) {
	t.Parallel()

	spaceID := uuid.New()
	const providerRoomName = "r_0123456789abcdef0123456789abcdef"
	order := []string{}
	base := &fakeProviderLifecycleBase{order: &order, startResult: MediaSpace{
		ID: spaceID, Status: SpaceStatusOpen,
		ActiveRoomInstance: &RoomInstance{
			ID: uuid.New(), Status: RoomInstanceProvisioning,
			ProviderRoomName: providerRoomName,
		},
	}}
	bindings := &fakeRoomBindingRepository{order: &order, activateErr: errRoomActivationTerminal}
	provider := &fakeRoomProvider{
		order: &order, ensureResult: ProviderRoom{SID: "RM_late_room_sid"},
	}
	service := newTestProviderLifecycleService(t, base, bindings, provider)

	_, err := service.StartSpace(
		context.Background(), lifecycleAccess(), spaceID,
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: "provider-terminal-race-0001"},
	)
	if !errors.Is(err, ErrSpaceTransition) {
		t.Fatalf("expected terminal activation conflict, got %v", err)
	}
	if provider.deleteCalls != 1 || provider.deletedRoomName != providerRoomName ||
		!reflect.DeepEqual(order, []string{
			"base.start", "provider.ensure", "bindings.activate", "provider.delete",
		}) {
		t.Fatalf("late provider room was not cleaned after definite terminal CAS: %+v", order)
	}
}

func TestProviderLifecycleServiceEndMutatesBeforeLookupAndDeleteThenRetriesCleanup(t *testing.T) {
	t.Parallel()

	spaceID := uuid.New()
	const providerRoomName = "r_0123456789abcdef0123456789abcdef"
	ended := MediaSpace{ID: spaceID, Status: SpaceStatusEnded, Version: 5}
	order := []string{}
	base := &fakeProviderLifecycleBase{order: &order, endResult: ended}
	bindings := &fakeRoomBindingRepository{order: &order, providerRoomName: providerRoomName}
	provider := &fakeRoomProvider{
		order:        &order,
		deleteErrors: []error{errors.New("provider-sensitive-delete-detail"), nil},
	}
	service := newTestProviderLifecycleService(t, base, bindings, provider)
	access := lifecycleAccess()
	input := TransitionInput{
		ExpectedVersion: 4, IdempotencyKey: "provider-end-retry-0001", ReasonCode: "host_ended",
	}

	result, err := service.EndSpace(context.Background(), access, spaceID, input)
	if !errors.Is(err, ErrMediaProviderUnavailable) ||
		strings.Contains(err.Error(), "provider-sensitive-delete-detail") {
		t.Fatalf("delete failure was not a redacted typed provider error: %v", err)
	}
	if result != (MediaSpace{}) ||
		!reflect.DeepEqual(order, []string{"base.end", "bindings.lookup", "provider.delete"}) {
		t.Fatalf("end ordering did not preserve authority before provider effect: result=%+v order=%+v", result, order)
	}

	result, err = service.EndSpace(context.Background(), access, spaceID, input)
	if err != nil {
		t.Fatalf("retry provider cleanup: %v", err)
	}
	if result.ID != spaceID || result.Status != SpaceStatusEnded ||
		base.endCalls != 2 || bindings.providerRoomNameCalls != 2 || provider.deleteCalls != 2 ||
		provider.deletedRoomName != providerRoomName ||
		!reflect.DeepEqual(order, []string{
			"base.end", "bindings.lookup", "provider.delete",
			"base.end", "bindings.lookup", "provider.delete",
		}) {
		t.Fatalf("provider cleanup retry did not re-read persisted name: result=%+v order=%+v", result, order)
	}
}

func TestProviderLifecycleServiceEndAuthorizationFailureHasNoLookupOrProviderEffect(t *testing.T) {
	t.Parallel()

	order := []string{}
	base := &fakeProviderLifecycleBase{order: &order, endErr: ErrSpaceAccessDenied}
	bindings := &fakeRoomBindingRepository{
		order: &order, providerRoomName: "r_0123456789abcdef0123456789abcdef",
	}
	provider := &fakeRoomProvider{order: &order}
	service := newTestProviderLifecycleService(t, base, bindings, provider)

	_, err := service.EndSpace(
		context.Background(), lifecycleAccess(), uuid.New(),
		TransitionInput{ExpectedVersion: 1, IdempotencyKey: "provider-end-denied-0001"},
	)
	if !errors.Is(err, ErrSpaceAccessDenied) {
		t.Fatalf("expected authoritative end denial, got %v", err)
	}
	if bindings.providerRoomNameCalls != 0 || provider.deleteCalls != 0 ||
		!reflect.DeepEqual(order, []string{"base.end"}) {
		t.Fatalf("denied end performed binding lookup or provider effect: %+v", order)
	}
}

func newTestProviderLifecycleService(
	t *testing.T,
	base LifecycleServiceAPI,
	bindings RoomBindingRepository,
	provider RoomProvider,
) *ProviderLifecycleService {
	t.Helper()
	service, err := NewProviderLifecycleService(
		base, bindings, provider, func() time.Time { return mediaTestTime },
	)
	if err != nil {
		t.Fatalf("create provider lifecycle service: %v", err)
	}
	return service
}

type fakeProviderLifecycleBase struct {
	order        *[]string
	startResult  MediaSpace
	startErr     error
	endResult    MediaSpace
	endErr       error
	startCalls   int
	endCalls     int
	startSpaceID uuid.UUID
	startInput   TransitionInput
}

func (base *fakeProviderLifecycleBase) CreateSpace(
	context.Context,
	AccessContext,
	CreateSpaceInput,
) (CreateSpaceResult, error) {
	return CreateSpaceResult{}, errors.New("unexpected create")
}

func (base *fakeProviderLifecycleBase) GetSpace(
	context.Context,
	AccessContext,
	uuid.UUID,
) (MediaSpace, error) {
	return MediaSpace{}, errors.New("unexpected get")
}

func (base *fakeProviderLifecycleBase) StartSpace(
	_ context.Context,
	_ AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	appendProviderLifecycleOrder(base.order, "base.start")
	base.startCalls++
	base.startSpaceID = spaceID
	base.startInput = input
	return base.startResult, base.startErr
}

func (base *fakeProviderLifecycleBase) EndSpace(
	context.Context,
	AccessContext,
	uuid.UUID,
	TransitionInput,
) (MediaSpace, error) {
	appendProviderLifecycleOrder(base.order, "base.end")
	base.endCalls++
	return base.endResult, base.endErr
}

func (base *fakeProviderLifecycleBase) CancelSpace(
	context.Context,
	AccessContext,
	uuid.UUID,
	TransitionInput,
) (MediaSpace, error) {
	return MediaSpace{}, errors.New("unexpected cancel")
}

type fakeRoomBindingRepository struct {
	order                  *[]string
	activateResult         MediaSpace
	activateErr            error
	activateErrors         []error
	providerRoomName       string
	providerRoomNameErr    error
	activateCalls          int
	providerRoomNameCalls  int
	activateAccess         AccessContext
	activateSpaceID        uuid.UUID
	activateRoomInstanceID uuid.UUID
	activateProviderSID    string
	activateAt             time.Time
}

func (bindings *fakeRoomBindingRepository) ActivateRoomInstance(
	_ context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	providerRoomSID string,
	at time.Time,
) (MediaSpace, error) {
	appendProviderLifecycleOrder(bindings.order, "bindings.activate")
	bindings.activateCalls++
	bindings.activateAccess = access
	bindings.activateSpaceID = spaceID
	bindings.activateRoomInstanceID = roomInstanceID
	bindings.activateProviderSID = providerRoomSID
	bindings.activateAt = at
	err := bindings.activateErr
	if index := bindings.activateCalls - 1; index < len(bindings.activateErrors) {
		err = bindings.activateErrors[index]
	}
	return bindings.activateResult, err
}

func (bindings *fakeRoomBindingRepository) ProviderRoomName(
	context.Context,
	AccessContext,
	uuid.UUID,
) (string, error) {
	appendProviderLifecycleOrder(bindings.order, "bindings.lookup")
	bindings.providerRoomNameCalls++
	return bindings.providerRoomName, bindings.providerRoomNameErr
}

type fakeRoomProvider struct {
	order           *[]string
	ensureResult    ProviderRoom
	ensureErr       error
	deleteErr       error
	deleteErrors    []error
	ensureCalls     int
	deleteCalls     int
	ensuredRoomName string
	deletedRoomName string
}

func (provider *fakeRoomProvider) EnsureRoom(
	_ context.Context,
	roomName string,
) (ProviderRoom, error) {
	appendProviderLifecycleOrder(provider.order, "provider.ensure")
	provider.ensureCalls++
	provider.ensuredRoomName = roomName
	return provider.ensureResult, provider.ensureErr
}

func (provider *fakeRoomProvider) DeleteRoom(_ context.Context, roomName string) error {
	appendProviderLifecycleOrder(provider.order, "provider.delete")
	provider.deleteCalls++
	provider.deletedRoomName = roomName
	err := provider.deleteErr
	if index := provider.deleteCalls - 1; index < len(provider.deleteErrors) {
		err = provider.deleteErrors[index]
	}
	return err
}

func appendProviderLifecycleOrder(order *[]string, step string) {
	if order != nil {
		*order = append(*order, step)
	}
}
