package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestServiceListDefaultsAndBindsCursorToTenantAndKind(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	updatedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		listItems:   []Conversation{{ID: uuid.New(), Kind: KindDirect, UpdatedAt: updatedAt}},
		listHasMore: true,
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(tenantID)
	kind := KindDirect
	page, err := service.List(context.Background(), access, ListInput{Kind: &kind})
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if repository.listInput.Limit != defaultListLimit || repository.listCursor.ID != uuid.Nil {
		t.Fatalf("unexpected normalized list input: %+v / %+v", repository.listInput, repository.listCursor)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if _, err := service.List(context.Background(), testAccess(uuid.New()), ListInput{
		Kind: &kind, Cursor: page.NextCursor,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign-tenant cursor error = %v, want invalid input", err)
	}
	classKind := KindClass
	if _, err := service.List(context.Background(), access, ListInput{
		Kind: &classKind, Cursor: page.NextCursor,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-kind cursor error = %v, want invalid input", err)
	}
}

func TestServiceNormalizesDirectTargetAndRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(uuid.New())
	if _, err := service.CreateDirect(
		context.Background(), access, "  STUDENT@EXAMPLE.TEST ",
	); err != nil {
		t.Fatalf("create direct conversation: %v", err)
	}
	if repository.directEmail != "student@example.test" {
		t.Fatalf("normalized email = %q", repository.directEmail)
	}
	for _, value := range []string{"", "Display Name <student@example.test>", "not-an-email"} {
		if _, err := service.CreateDirect(context.Background(), access, value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("email %q error = %v, want invalid input", value, err)
		}
	}
	if _, err := service.CreateClass(context.Background(), access, uuid.Nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil class error = %v, want not found", err)
	}
	invalidAccess := access
	invalidAccess.MembershipActive = false
	if _, err := service.Get(context.Background(), invalidAccess, uuid.New()); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("inactive access error = %v, want access denied", err)
	}
}

func TestServiceRejectsInvalidListBoundsAndKind(t *testing.T) {
	t.Parallel()
	service, err := NewService(&fakeRepository{})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(uuid.New())
	unknown := Kind("group")
	for _, input := range []ListInput{
		{Limit: -1},
		{Limit: maximumListLimit + 1},
		{Kind: &unknown},
		{Cursor: "not-a-cursor"},
	} {
		if _, err := service.List(context.Background(), access, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("input %+v error = %v, want invalid input", input, err)
		}
	}
}

func TestMessageServiceNormalizesContentAndValidatesMutations(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(uuid.New())
	conversationID := uuid.New()
	messageID := uuid.New()
	clientMessageID := uuid.New()

	if _, err := service.SendMessage(
		context.Background(),
		access,
		conversationID,
		SendMessageInput{
			ClientMessageID: clientMessageID,
			Content:         "  first\r\nsecond\r  ",
		},
	); err != nil {
		t.Fatalf("send normalized message: %v", err)
	}
	if repository.sendInput.ClientMessageID != clientMessageID ||
		repository.sendInput.Content != "first\nsecond" {
		t.Fatalf("unexpected normalized send input: %+v", repository.sendInput)
	}

	if _, err := service.EditMessage(
		context.Background(),
		access,
		conversationID,
		messageID,
		EditMessageInput{Content: "  edited\r\nmessage  ", ExpectedVersion: 3},
	); err != nil {
		t.Fatalf("edit normalized message: %v", err)
	}
	if repository.editInput.Content != "edited\nmessage" ||
		repository.editInput.ExpectedVersion != 3 {
		t.Fatalf("unexpected normalized edit input: %+v", repository.editInput)
	}

	invalidContents := []string{
		"",
		" \r\n\t ",
		"message\x00body",
		strings.Repeat("a", maximumMessageCharacters+1),
		string([]byte{0xff}),
	}
	for _, content := range invalidContents {
		if _, err := service.SendMessage(
			context.Background(), access, conversationID,
			SendMessageInput{ClientMessageID: uuid.New(), Content: content},
		); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("send content %q error=%v, want invalid input", content, err)
		}
	}

	if _, err := service.SendMessage(
		context.Background(), access, conversationID,
		SendMessageInput{Content: "message"},
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil client message ID error=%v, want invalid input", err)
	}
	if _, err := service.EditMessage(
		context.Background(), access, conversationID, messageID,
		EditMessageInput{Content: "message", ExpectedVersion: 0},
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero edit version error=%v, want invalid input", err)
	}
	if _, err := service.DeleteMessage(
		context.Background(), access, conversationID, messageID,
		DeleteMessageInput{ExpectedVersion: 0},
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero delete version error=%v, want invalid input", err)
	}
	if _, err := service.SendMessage(
		context.Background(), access, uuid.Nil,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "message"},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil conversation ID error=%v, want not found", err)
	}
}

func TestMessageServiceListCursorScopeAndMonotonicReadInput(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	conversationID := uuid.New()
	oldestID := uuid.New()
	repository := &fakeRepository{
		messagePage: messageRepositoryPage{
			Items: []Message{
				{ID: uuid.New(), ConversationID: conversationID, Sequence: 12},
				{ID: oldestID, ConversationID: conversationID, Sequence: 11},
			},
			HasMore: true,
		},
		readState: MessageReadState{
			LastReadMessageID: oldestID,
			LastReadSequence:  11,
		},
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(tenantID)

	page, err := service.ListMessages(
		context.Background(), access, conversationID, MessageListInput{},
	)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if repository.messageListInput.Limit != defaultMessageListLimit ||
		repository.messageCursor.BeforeSequence != 0 || page.NextCursor == "" {
		t.Fatalf("unexpected normalized message page: input=%+v cursor=%+v page=%+v",
			repository.messageListInput, repository.messageCursor, page)
	}

	if _, err := service.ListMessages(
		context.Background(), testAccess(uuid.New()), conversationID,
		MessageListInput{Cursor: page.NextCursor},
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign-tenant message cursor error=%v, want invalid input", err)
	}
	if _, err := service.ListMessages(
		context.Background(), access, uuid.New(),
		MessageListInput{Cursor: page.NextCursor},
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-conversation message cursor error=%v, want invalid input", err)
	}
	for _, input := range []MessageListInput{
		{Limit: -1},
		{Limit: maximumMessageListLimit + 1},
		{Cursor: "not-a-message-cursor"},
	} {
		if _, err := service.ListMessages(
			context.Background(), access, conversationID, input,
		); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("message list input %+v error=%v, want invalid input", input, err)
		}
	}

	state, err := service.MarkRead(
		context.Background(), access, conversationID, oldestID,
	)
	if err != nil {
		t.Fatalf("mark messages read: %v", err)
	}
	if repository.markReadMessageID != oldestID || state.LastReadSequence != 11 {
		t.Fatalf("unexpected read state: repository=%s state=%+v",
			repository.markReadMessageID, state)
	}
	if _, err := service.MarkRead(
		context.Background(), access, conversationID, uuid.Nil,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil read-through message error=%v, want not found", err)
	}
}

type fakeRepository struct {
	listInput         ListInput
	listCursor        listCursor
	listItems         []Conversation
	listHasMore       bool
	listError         error
	directEmail       string
	direct            CreateResult
	directError       error
	classID           uuid.UUID
	class             CreateResult
	classError        error
	get               Conversation
	getError          error
	messageListInput  MessageListInput
	messageCursor     messageCursor
	messagePage       messageRepositoryPage
	messagePageError  error
	sendInput         SendMessageInput
	sendResult        MessageMutationResult
	sendError         error
	editInput         EditMessageInput
	editResult        Message
	editError         error
	deleteInput       DeleteMessageInput
	deleteResult      Message
	deleteError       error
	markReadMessageID uuid.UUID
	readState         MessageReadState
	readError         error
}

func (repository *fakeRepository) List(
	_ context.Context,
	_ AccessContext,
	input ListInput,
	cursor listCursor,
) ([]Conversation, bool, error) {
	repository.listInput = input
	repository.listCursor = cursor
	return repository.listItems, repository.listHasMore, repository.listError
}

func (repository *fakeRepository) Get(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
) (Conversation, error) {
	return repository.get, repository.getError
}

func (repository *fakeRepository) CreateDirect(
	_ context.Context,
	_ AccessContext,
	email string,
) (CreateResult, error) {
	repository.directEmail = email
	return repository.direct, repository.directError
}

func (repository *fakeRepository) CreateClass(
	_ context.Context,
	_ AccessContext,
	classID uuid.UUID,
) (CreateResult, error) {
	repository.classID = classID
	return repository.class, repository.classError
}

func (repository *fakeRepository) ListMessages(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	input MessageListInput,
	cursor messageCursor,
) (messageRepositoryPage, error) {
	repository.messageListInput = input
	repository.messageCursor = cursor
	return repository.messagePage, repository.messagePageError
}

func (repository *fakeRepository) SendMessage(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	input SendMessageInput,
) (MessageMutationResult, error) {
	repository.sendInput = input
	return repository.sendResult, repository.sendError
}

func (repository *fakeRepository) EditMessage(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
	input EditMessageInput,
) (Message, error) {
	repository.editInput = input
	return repository.editResult, repository.editError
}

func (repository *fakeRepository) DeleteMessage(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	_ uuid.UUID,
	input DeleteMessageInput,
) (Message, error) {
	repository.deleteInput = input
	return repository.deleteResult, repository.deleteError
}

func (repository *fakeRepository) MarkRead(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	messageID uuid.UUID,
) (MessageReadState, error) {
	repository.markReadMessageID = messageID
	return repository.readState, repository.readError
}

func testAccess(tenantID uuid.UUID) AccessContext {
	return AccessContext{
		TenantID: tenantID, ActorID: uuid.New(), MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleStudent},
	}
}

var _ Repository = (*fakeRepository)(nil)
