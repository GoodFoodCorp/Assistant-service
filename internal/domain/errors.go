package domain

import "fmt"

type ErrorCode string

const (
	ErrCodeValidation ErrorCode = "VALIDATION_ERROR"
	ErrCodeForbidden  ErrorCode = "FORBIDDEN"
	ErrCodeUpstream   ErrorCode = "UPSTREAM_ERROR"
)

type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func NewValidationError(msg string) *Error { return &Error{Code: ErrCodeValidation, Message: msg} }
func NewForbiddenError(msg string) *Error  { return &Error{Code: ErrCodeForbidden, Message: msg} }
func NewUpstreamError(msg string) *Error   { return &Error{Code: ErrCodeUpstream, Message: msg} }
