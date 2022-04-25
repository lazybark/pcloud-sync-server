package sync

import "time"

type (
	// ErrorType represents sync error code and human-readable name
	ErrorType int

	// Error is the model for error message payload
	Error struct {
		Timestamp     time.Time
		Type          ErrorType
		HumanReadable string
	}
)

// Error codes
const (
	errors_start ErrorType = iota

	ErrBrokenMessage
	ErrUnknownMessageType
	ErrAccessDenied
	ErrInternal

	errors_end
)

// String() returns human-readable name of error code
func (e ErrorType) String() string {
	return [...]string{"", "Broken message", "Unknown message type", "Access denied", "Sync app internal error", ""}[e]
}

// CheckErrorType() checks error code for consistency
func (e *ErrorType) CheckErrorType() bool {
	if errors_start < *e && *e < errors_end {
		return true
	}
	return false
}
