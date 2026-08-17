package session

import "fmt"

type State uint8

const (
	StateHandshake State = iota
	StateLogin
	StateConfiguration
	StateLimboPlay
	StateTransferBarrier
	StateBackendLogin
	StateBackendConfiguration
	StateBackendPlay
	StateClosing
)

func (s State) String() string {
	switch s {
	case StateHandshake:
		return "handshake"
	case StateLogin:
		return "login"
	case StateConfiguration:
		return "configuration"
	case StateLimboPlay:
		return "limbo_play"
	case StateTransferBarrier:
		return "transfer_barrier"
	case StateBackendLogin:
		return "backend_login"
	case StateBackendConfiguration:
		return "backend_configuration"
	case StateBackendPlay:
		return "backend_play"
	case StateClosing:
		return "closing"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

type InvalidTransitionError struct {
	From State
	To   State
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid session transition from %s to %s", e.From, e.To)
}

func canTransition(from, to State) bool {
	if to == StateClosing {
		return from != StateClosing
	}

	switch from {
	case StateHandshake:
		return to == StateLogin
	case StateLogin:
		return to == StateConfiguration
	case StateConfiguration:
		return to == StateLimboPlay
	case StateLimboPlay:
		return to == StateTransferBarrier
	case StateTransferBarrier:
		return to == StateBackendLogin || to == StateLimboPlay
	case StateBackendLogin:
		return to == StateBackendConfiguration || to == StateLimboPlay
	case StateBackendConfiguration:
		return to == StateBackendPlay || to == StateLimboPlay
	case StateBackendPlay:
		return to == StateTransferBarrier
	default:
		return false
	}
}
