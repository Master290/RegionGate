package session

import (
	"errors"
	"testing"
	"time"
)

func TestSessionTransferBarrier(t *testing.T) {
	s := New()
	for _, state := range []State{StateLogin, StateConfiguration, StateLimboPlay} {
		if err := s.Transition(state); err != nil {
			t.Fatalf("transition to %s: %v", state, err)
		}
	}

	if err := s.BeginTransfer(time.Unix(10, 0), []int64{42}, 2); err != nil {
		t.Fatal(err)
	}

	disposition, err := s.HandleBarrierInput(Input{Kind: InputKeepAlive, KeepAliveID: 42})
	if err != nil || disposition != InputConsumed {
		t.Fatalf("old keepalive: disposition=%d err=%v", disposition, err)
	}

	position := Position{X: 1, Y: 2, Z: 3, Yaw: 4, Pitch: 5, OnGround: true}
	disposition, err = s.HandleBarrierInput(Input{Kind: InputMovement, Position: position, HasLook: true})
	if err != nil || disposition != InputCoalesced {
		t.Fatalf("movement: disposition=%d err=%v", disposition, err)
	}
	position.X = 99
	position.Yaw = 88
	if _, err := s.HandleBarrierInput(Input{Kind: InputMovement, Position: position}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.HandleBarrierInput(Input{Kind: InputKeepAlive, KeepAliveID: 999}); !errors.Is(err, ErrUnexpectedKeepAlive) {
		t.Fatalf("unexpected keepalive error = %v", err)
	}

	command := PlayerCommand{EntityID: 1, ActionID: 2, Data: 3}
	if _, err := s.HandleBarrierInput(Input{Kind: InputPlayerCommand, Command: command}); err != nil {
		t.Fatal(err)
	}

	for _, phase := range []BarrierPhase{BarrierBackendLogin, BarrierBackendConfiguration, BarrierAwaitingClientConfigurationStart, BarrierClientConfiguration, BarrierAwaitingClientConfigurationFinish, BarrierReady} {
		if err := s.AdvanceBarrier(phase); err != nil {
			t.Fatalf("advance barrier to %d: %v", phase, err)
		}
	}

	replay, err := s.ReleaseTransfer()
	if err != nil {
		t.Fatal(err)
	}
	if s.State() != StateBackendPlay {
		t.Fatalf("state = %s, want backend_play", s.State())
	}
	if replay.Position == nil || replay.Position.X != 99 {
		t.Fatalf("replayed position = %#v", replay.Position)
	}
	if replay.Position.Yaw != 4 {
		t.Fatalf("position-only update replaced yaw: %#v", replay.Position)
	}
	if len(replay.Commands) != 1 || replay.Commands[0] != command {
		t.Fatalf("replayed commands = %#v", replay.Commands)
	}
}

func TestSessionTransferRollback(t *testing.T) {
	s := New()
	for _, state := range []State{StateLogin, StateConfiguration, StateLimboPlay} {
		if err := s.Transition(state); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.BeginTransfer(time.Now(), nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.RollbackTransfer(); err != nil {
		t.Fatal(err)
	}
	if s.State() != StateLimboPlay {
		t.Fatalf("state = %s, want limbo_play", s.State())
	}
}

func TestSessionRejectsInvalidTransitions(t *testing.T) {
	s := New()
	if err := s.Transition(StateBackendPlay); err == nil {
		t.Fatal("expected invalid transition")
	}
	if _, err := s.HandleBarrierInput(Input{Kind: InputMovement}); !errors.Is(err, ErrBarrierInactive) {
		t.Fatalf("inactive barrier error = %v", err)
	}
}
