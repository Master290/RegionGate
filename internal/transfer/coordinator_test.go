package transfer

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/session"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestPrepareRejectsUnconfiguredCoordinatorWithoutMutatingSession(t *testing.T) {
	state := session.New()
	for _, next := range []session.State{session.StateLogin, session.StateConfiguration, session.StateLimboPlay} {
		if err := state.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	coordinator := NewCoordinator(nil, nil, Config{})
	_, err := coordinator.Prepare(context.Background(), state, forwarding.PlayerIdentity{}, nil)
	if err == nil || state.State() != session.StateLimboPlay {
		t.Fatalf("err=%v state=%s", err, state.State())
	}
}

func TestPreparedLifecycleRequiresClientAcknowledgement(t *testing.T) {
	state := sessionAtAwaitingClientConfiguration(t)
	left, right := net.Pipe()
	backendConn := transport.New(left, 1024)
	defer right.Close()
	prepared := &Prepared{session: state, backend: backendConn, packets: [][]byte{{1, 2, 3}}, done: make(chan struct{})}

	if _, _, err := prepared.Release(); !errors.Is(err, session.ErrBarrierNotReady) {
		t.Fatalf("early release error=%v", err)
	}
	position := session.Position{X: 9, Y: 64, Z: 3}
	if _, err := state.HandleBarrierInput(session.Input{Kind: session.InputMovement, Position: position}); err != nil {
		t.Fatal(err)
	}
	packets := prepared.ConfigurationPackets()
	packets[0][0] = 99
	if prepared.ConfigurationPackets()[0][0] != 1 {
		t.Fatal("configuration packets were not copied")
	}
	if err := prepared.AcknowledgeClientConfiguration(); err != nil {
		t.Fatal(err)
	}
	replay, releasedBackend, err := prepared.Release()
	if err != nil {
		t.Fatal(err)
	}
	if replay.Position == nil || replay.Position.X != position.X || releasedBackend != backendConn || state.State() != session.StateBackendPlay {
		t.Fatalf("replay=%+v backend=%p state=%s", replay, releasedBackend, state.State())
	}
	_ = releasedBackend.Close()
	if err := prepared.Rollback(); !errors.Is(err, ErrTransferAlreadyFinalized) {
		t.Fatalf("second finalize error=%v", err)
	}
}

func TestPreparedRollbackReturnsToLimbo(t *testing.T) {
	state := sessionAtAwaitingClientConfiguration(t)
	left, right := net.Pipe()
	backendConn := transport.New(left, 1024)
	defer right.Close()
	prepared := &Prepared{session: state, backend: backendConn, done: make(chan struct{})}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if state.State() != session.StateLimboPlay {
		t.Fatalf("state=%s", state.State())
	}
	if err := backendConn.WriteFrame([]byte{1}); !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("backend write error=%v", err)
	}
}

func TestPreparedTimeoutRollsBackAndClosesBackend(t *testing.T) {
	state := sessionAtAwaitingClientConfiguration(t)
	left, right := net.Pipe()
	backendConn := transport.New(left, 1024)
	defer right.Close()
	prepared := &Prepared{session: state, backend: backendConn, done: make(chan struct{})}
	go prepared.timeout(20 * time.Millisecond)
	select {
	case <-prepared.done:
	case <-time.After(time.Second):
		t.Fatal("transfer timeout did not fire")
	}
	if state.State() != session.StateLimboPlay || !errors.Is(prepared.Err(), ErrTransferTimedOut) {
		t.Fatalf("state=%s error=%v", state.State(), prepared.Err())
	}
	if err := backendConn.WriteFrame([]byte{1}); !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("backend write error=%v", err)
	}
}

func sessionAtAwaitingClientConfiguration(t *testing.T) *session.Session {
	t.Helper()
	state := session.New()
	for _, next := range []session.State{session.StateLogin, session.StateConfiguration, session.StateLimboPlay} {
		if err := state.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.BeginTransfer(time.Unix(1, 0), []int64{1}, 4); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []session.BarrierPhase{session.BarrierBackendLogin, session.BarrierBackendConfiguration, session.BarrierAwaitingClientConfiguration} {
		if err := state.AdvanceBarrier(phase); err != nil {
			t.Fatal(err)
		}
	}
	return state
}
