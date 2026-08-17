package session

import (
	"sync"
	"time"
)

type Session struct {
	mu      sync.Mutex
	state   State
	barrier *barrierState
}

func New() *Session {
	return &Session{state: StateHandshake}
}

func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) Transition(to State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transitionLocked(to)
}

func (s *Session) BeginTransfer(now time.Time, limboKeepAliveIDs []int64, maxPendingCommands int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.transitionLocked(StateTransferBarrier); err != nil {
		return err
	}
	s.barrier = newBarrier(now, limboKeepAliveIDs, maxPendingCommands)
	return nil
}

func (s *Session) HandleBarrierInput(input Input) (InputDisposition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateTransferBarrier || s.barrier == nil {
		return InputDropped, ErrBarrierInactive
	}
	return s.barrier.handle(input)
}

func (s *Session) AdvanceBarrier(phase BarrierPhase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateTransferBarrier || s.barrier == nil {
		return ErrBarrierInactive
	}
	if phase != s.barrier.phase+1 {
		return ErrBarrierNotReady
	}
	s.barrier.phase = phase
	return nil
}

func (s *Session) ReleaseTransfer() (Replay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateTransferBarrier || s.barrier == nil {
		return Replay{}, ErrBarrierInactive
	}
	if s.barrier.phase != BarrierReady {
		return Replay{}, ErrBarrierNotReady
	}

	replay := s.barrier.replay()
	s.state = StateBackendPlay
	s.barrier = nil
	return replay, nil
}

func (s *Session) RollbackTransfer() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateTransferBarrier || s.barrier == nil {
		return ErrBarrierInactive
	}
	s.state = StateLimboPlay
	s.barrier = nil
	return nil
}

func (s *Session) transitionLocked(to State) error {
	if !canTransition(s.state, to) {
		return &InvalidTransitionError{From: s.state, To: to}
	}
	s.state = to
	if to == StateClosing {
		s.barrier = nil
	}
	return nil
}
