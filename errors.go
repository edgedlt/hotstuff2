package hotstuff2

import (
	"errors"
	"fmt"
)

// Error classes for consensus operations.
// These represent categories of errors that integrators can handle uniformly.
// Use errors.Is() to check error class, then inspect the error message for details.
//
// Error Classification:
//   - ErrConfig: Hard configuration errors - must fix and restart
//   - ErrInvalidMessage: Malformed or invalid messages from peers - log and ignore
//   - ErrByzantine: Potential Byzantine behavior detected - may warrant peer penalties
//   - ErrInternal: Internal invariant violations - indicates bugs or corruption
var (
	// ErrConfig indicates a configuration error that prevents startup.
	// These are hard errors that require fixing the configuration and restarting.
	// Examples: missing required fields, invalid validator index, insufficient validators.
	ErrConfig = errors.New("configuration error")

	// ErrInvalidMessage indicates a malformed or invalid message was received.
	// These are soft errors - the message should be dropped but consensus continues.
	// Examples: deserialization failure, message too short, unknown message type.
	ErrInvalidMessage = errors.New("invalid message")

	// ErrByzantine indicates potential Byzantine behavior from a peer.
	// Integrators may want to track these for peer scoring or slashing.
	// Examples: invalid signature, timestamp manipulation, duplicate votes.
	ErrByzantine = errors.New("byzantine behavior detected")

	// ErrInternal indicates an internal invariant violation.
	// These suggest bugs in the library or storage corruption.
	// Examples: QC validation failed on locally-created QC, missing expected block.
	ErrInternal = errors.New("internal error")
)

// Unexported helpers to wrap errors with the appropriate class.

func wrapConfig(msg string) error {
	return fmt.Errorf("%w: %s", ErrConfig, msg)
}

func wrapConfigf(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrConfig, fmt.Sprintf(format, args...))
}

func wrapInvalidMessage(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidMessage, msg)
}

func wrapByzantine(msg string) error {
	return fmt.Errorf("%w: %s", ErrByzantine, msg)
}

func wrapByzantinef(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrByzantine, fmt.Sprintf(format, args...))
}

func wrapInternal(msg string) error {
	return fmt.Errorf("%w: %s", ErrInternal, msg)
}
