package conversation

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

func (service *Service) ListMessages(
	ctx context.Context,
	access AccessContext,
	conversationID uuid.UUID,
	input MessageListInput,
) (MessagePage, error) {
	if service == nil {
		return MessagePage{}, fmt.Errorf("list messages: service is unavailable")
	}
	if !validAccess(access) {
		return MessagePage{}, ErrAccessDenied
	}
	if conversationID == uuid.Nil {
		return MessagePage{}, ErrNotFound
	}
	input.Cursor = strings.TrimSpace(input.Cursor)
	if input.Limit == 0 {
		input.Limit = defaultMessageListLimit
	}
	if input.Limit < 1 || input.Limit > maximumMessageListLimit {
		return MessagePage{}, ErrInvalidInput
	}
	cursor, err := decodeMessageCursor(access.TenantID, conversationID, input.Cursor)
	if err != nil {
		return MessagePage{}, err
	}
	result, err := service.repository.ListMessages(
		ctx, access, conversationID, input, cursor,
	)
	if err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{
		Items:             result.Items,
		UnreadCount:       result.UnreadCount,
		UnreadCountCapped: result.UnreadCountCapped,
		ReadState:         result.ReadState,
	}
	if page.Items == nil {
		page.Items = []Message{}
	}
	if result.HasMore && len(page.Items) > 0 {
		page.NextCursor, err = encodeMessageCursor(
			access.TenantID,
			conversationID,
			messageCursor{BeforeSequence: page.Items[len(page.Items)-1].Sequence},
		)
		if err != nil {
			return MessagePage{}, err
		}
	}
	return page, nil
}

func (service *Service) SendMessage(
	ctx context.Context,
	access AccessContext,
	conversationID uuid.UUID,
	input SendMessageInput,
) (MessageMutationResult, error) {
	if service == nil {
		return MessageMutationResult{}, fmt.Errorf("send message: service is unavailable")
	}
	if !validAccess(access) {
		return MessageMutationResult{}, ErrAccessDenied
	}
	if conversationID == uuid.Nil {
		return MessageMutationResult{}, ErrNotFound
	}
	if input.ClientMessageID == uuid.Nil {
		return MessageMutationResult{}, ErrInvalidInput
	}
	content, err := normalizeMessageContent(input.Content)
	if err != nil {
		return MessageMutationResult{}, err
	}
	input.Content = content
	return service.repository.SendMessage(ctx, access, conversationID, input)
}

func (service *Service) EditMessage(
	ctx context.Context,
	access AccessContext,
	conversationID, messageID uuid.UUID,
	input EditMessageInput,
) (Message, error) {
	if service == nil {
		return Message{}, fmt.Errorf("edit message: service is unavailable")
	}
	if !validAccess(access) {
		return Message{}, ErrAccessDenied
	}
	if conversationID == uuid.Nil || messageID == uuid.Nil {
		return Message{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return Message{}, ErrInvalidInput
	}
	content, err := normalizeMessageContent(input.Content)
	if err != nil {
		return Message{}, err
	}
	input.Content = content
	return service.repository.EditMessage(
		ctx, access, conversationID, messageID, input,
	)
}

func (service *Service) DeleteMessage(
	ctx context.Context,
	access AccessContext,
	conversationID, messageID uuid.UUID,
	input DeleteMessageInput,
) (Message, error) {
	if service == nil {
		return Message{}, fmt.Errorf("delete message: service is unavailable")
	}
	if !validAccess(access) {
		return Message{}, ErrAccessDenied
	}
	if conversationID == uuid.Nil || messageID == uuid.Nil {
		return Message{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return Message{}, ErrInvalidInput
	}
	return service.repository.DeleteMessage(
		ctx, access, conversationID, messageID, input,
	)
}

func (service *Service) MarkRead(
	ctx context.Context,
	access AccessContext,
	conversationID, messageID uuid.UUID,
) (MessageReadState, error) {
	if service == nil {
		return MessageReadState{}, fmt.Errorf("mark messages read: service is unavailable")
	}
	if !validAccess(access) {
		return MessageReadState{}, ErrAccessDenied
	}
	if conversationID == uuid.Nil || messageID == uuid.Nil {
		return MessageReadState{}, ErrNotFound
	}
	return service.repository.MarkRead(ctx, access, conversationID, messageID)
}

func normalizeMessageContent(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, '\x00') ||
		utf8.RuneCountInString(value) > maximumMessageCharacters ||
		len([]byte(value)) > maximumMessageBytes {
		return "", ErrInvalidInput
	}
	return value, nil
}
