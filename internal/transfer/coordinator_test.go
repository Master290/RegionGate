package transfer

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/session"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestPrepareRollsBackWhenBackendDisconnectsDuringConfiguration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	backendDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		server := transport.New(conn, 4096)
		defer server.Close()
		if _, err := server.ReadFrame(); err != nil {
			backendDone <- err
			return
		}
		if _, err := server.ReadFrame(); err != nil {
			backendDone <- err
			return
		}
		if err := server.WriteFrame(login.SuccessPayload("Daniar")); err != nil {
			backendDone <- err
			return
		}
		ack, err := server.ReadFrame()
		if err == nil {
			err = login.ParseAcknowledged(ack)
		}
		backendDone <- err
	}()

	state := session.New()
	for _, next := range []session.State{session.StateLogin, session.StateConfiguration, session.StateLimboPlay} {
		if err := state.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	forwarder, err := forwarding.NewModernForwarding([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	dialer := backend.NewDialer(backend.Config{Address: listener.Addr().String(), Host: "localhost", Port: 25565})
	coordinator := NewCoordinator(dialer, forwarder, Config{
		Login:         backend.LoginConfig{Timeout: time.Second},
		Configuration: backend.ConfigurationConfig{Timeout: time.Second},
	})
	identity := forwarding.PlayerIdentity{Username: "Daniar", UUID: login.OfflineUUID("Daniar")}
	_, err = coordinator.Prepare(context.Background(), state, identity, nil)
	if !errors.Is(err, backend.ErrBackendConfigurationDisconnected) {
		t.Fatalf("error=%v", err)
	}
	if state.State() != session.StateLimboPlay {
		t.Fatalf("state=%s", state.State())
	}
	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
}

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
	clientLeft, clientRight := net.Pipe()
	client := transport.New(clientLeft, 1024)
	remoteClient := transport.New(clientRight, 1024)
	defer client.Close()
	defer remoteClient.Close()
	clientRead := make(chan error, 1)
	go func() {
		first, err := remoteClient.ReadFrame()
		if err != nil || string(first) != string(play.StartConfigurationPayload()) {
			clientRead <- errors.New("invalid start configuration")
			return
		}
		packet, err := remoteClient.ReadFrame()
		if err != nil || string(packet) != string([]byte{1, 2, 3}) {
			clientRead <- errors.New("invalid configuration packet")
			return
		}
		finish, err := remoteClient.ReadFrame()
		if err != nil || string(finish) != string(configuration.FinishPayload()) {
			clientRead <- errors.New("invalid finish configuration")
			return
		}
		clientRead <- nil
	}()
	if err := prepared.BeginClientConfiguration(client); err != nil {
		t.Fatal(err)
	}
	if err := prepared.AcknowledgeClientStart(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepared.Release(); !errors.Is(err, session.ErrBarrierNotReady) {
		t.Fatalf("release before finish error=%v", err)
	}
	if err := prepared.WriteClientConfiguration(client); err != nil {
		t.Fatal(err)
	}
	if err := <-clientRead; err != nil {
		t.Fatal(err)
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

func TestPreparedClientDisconnectRollsBackAndClosesBackend(t *testing.T) {
	state := sessionAtAwaitingClientConfiguration(t)
	backendLeft, backendRight := net.Pipe()
	backendConn := transport.New(backendLeft, 1024)
	defer backendRight.Close()
	prepared := &Prepared{session: state, backend: backendConn, done: make(chan struct{})}

	clientLeft, clientRight := net.Pipe()
	client := transport.New(clientLeft, 1024)
	_ = clientRight.Close()
	if err := prepared.BeginClientConfiguration(client); err == nil {
		t.Fatal("expected client disconnect error")
	}
	_ = client.Close()
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
	for _, phase := range []session.BarrierPhase{session.BarrierBackendLogin, session.BarrierBackendConfiguration, session.BarrierAwaitingClientConfigurationStart} {
		if err := state.AdvanceBarrier(phase); err != nil {
			t.Fatal(err)
		}
	}
	return state
}
