package sync

type (
	// ErrorType represents sync error code and human-readable name
	ErrorType int

	// Error is the model for error message payload
	Error struct {
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
	ErrTooMuchServerErrors
	ErrTooMuchClientErrors
	ErrTooMuchCients
	ErrTooMuchConnections
	ErrUnknownIntension

	errors_end
)

// String() returns human-readable name of error code
func (e ErrorType) String() string {
	if !e.CheckErrorType() {
		return "illegal"
	}
	return [...]string{"illegal", "Broken message", "Unknown message type", "Access denied", "Sync app internal error", "Too much errors on server side", "Too much errors on client side", "Server has reached client limits", "Client has reached connections limit", "Unknown connection intension", "illegal"}[e]
}

// CheckErrorType() checks error code for consistency
func (e *ErrorType) CheckErrorType() bool {
	if errors_start < *e && *e < errors_end {
		return true
	}
	return false
}
