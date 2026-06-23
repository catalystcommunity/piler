package messages

import "fmt"

// Error is a structured, client-visible RPC error. Handlers return one of
// these (via the constructors below) when the failure is the caller's
// concern; any other error is treated as an internal 500 and its detail is
// logged but never returned.
type Error struct {
	Code    int64
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Mirrors HTTP-ish status codes for familiarity; the transport is not HTTP.
func BadRequest(msg string) *Error   { return &Error{Code: 400, Message: msg} }
func Unauthorized(msg string) *Error { return &Error{Code: 401, Message: msg} }
func NotFound(msg string) *Error     { return &Error{Code: 404, Message: msg} }
func Internal(msg string) *Error     { return &Error{Code: 500, Message: msg} }
