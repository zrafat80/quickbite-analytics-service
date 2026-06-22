// Package errors defines AppError — the Go analogue of a NestJS HttpException
// subclass. Services and controllers return errors; the handler middleware
// inspects the type to build the response envelope and HTTP status.
package errors

import (
	"errors"
	"fmt"
)

// AppError is a typed error with a stable machine-readable code and an HTTP
// status. Compose new ones with errors.New(code, status, message), or use
// the package-level Wrap helpers in services to attach extra context.
type AppError struct {
	Code    string
	Status  int
	Message string
	cause   error
}

func New(code string, status int, message string) *AppError {
	return &AppError{Code: code, Status: status, Message: message}
}

func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.cause }

// WithCause returns a copy of e with cause attached — preserves the original
// Code/Status/Message but stashes the underlying error for logging.
func (e *AppError) WithCause(cause error) *AppError {
	if e == nil {
		return nil
	}
	return &AppError{Code: e.Code, Status: e.Status, Message: e.Message, cause: cause}
}

// As is a generics-free shortcut for errors.As targeting *AppError.
func As(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
