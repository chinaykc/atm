package agent

import (
	"errors"
	"strings"
)

// RetryableError marks an adapter failure that is likely transient enough for
// ATM to retry the same agent/check invocation.
type RetryableError struct {
	Message string
	Err     error
}

func (e *RetryableError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "retryable agent error"
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

func NewRetryableError(message string) error {
	return &RetryableError{Message: message}
}

func retryableError(message string, err error) error {
	return &RetryableError{Message: message, Err: err}
}

func IsRetryableError(err error) bool {
	var retryable *RetryableError
	return errors.As(err, &retryable)
}

func isRetryableAgentMessage(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return false
	}
	for _, nonRetryable := range []string{
		"usage_limit",
		"quota exceeded",
		"billing",
		"upgrade",
		"invalid request",
		"invalid_request",
		"cyber policy",
		"context window",
		"authentication_failed",
		"oauth_org_not_allowed",
		"model_not_found",
		"max_output_tokens",
	} {
		if strings.Contains(text, nonRetryable) {
			return false
		}
	}
	if strings.Contains(text, "usage limit") && !strings.Contains(text, "not your usage limit") {
		return false
	}
	for _, retryable := range []string{
		"429",
		"too many requests",
		"rate limit",
		"rate_limit",
		"timeout",
		"network error",
		"connection reset",
		"connection refused",
		"connection closed",
		"temporarily unavailable",
		"stream disconnected",
		"bad gateway",
		"service unavailable",
		"gateway timeout",
		"server_error",
		"server overloaded",
		"internal server error",
		"status 500",
		"status 502",
		"status 503",
		"status 504",
		"last status: 500",
		"last status: 502",
		"last status: 503",
		"last status: 504",
	} {
		if strings.Contains(text, retryable) {
			return true
		}
	}
	return false
}
