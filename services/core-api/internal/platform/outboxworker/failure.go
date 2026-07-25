package outboxworker

import (
	"errors"
	"regexp"
)

const (
	ErrorCodeHandlerFailed       = "handler_failed"
	ErrorCodeHandlerPanicked     = "handler_panicked"
	ErrorCodeHandlerTimeout      = "handler_timeout"
	ErrorCodeInvalidPayload      = "invalid_payload"
	ErrorCodeInvalidEventContext = "invalid_event_context"
	ErrorCodeAttemptsExhausted   = "attempts_exhausted"
)

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,99}$`)

type FailureDisposition uint8

const (
	FailureRetry FailureDisposition = iota + 1
	FailurePermanent
)

type HandlerError struct {
	code        string
	disposition FailureDisposition
}

func (failure HandlerError) Error() string {
	return failure.code
}

func Retryable(code string) error {
	return HandlerError{code: normalizedErrorCode(code), disposition: FailureRetry}
}

func Permanent(code string) error {
	return HandlerError{code: normalizedErrorCode(code), disposition: FailurePermanent}
}

func ClassifyFailure(err error) (string, FailureDisposition) {
	if err == nil {
		return "", 0
	}
	var failure HandlerError
	if errors.As(err, &failure) {
		return normalizedErrorCode(failure.code), failure.disposition
	}
	return ErrorCodeHandlerFailed, FailureRetry
}

func normalizedErrorCode(code string) string {
	if !errorCodePattern.MatchString(code) {
		return ErrorCodeHandlerFailed
	}
	return code
}
