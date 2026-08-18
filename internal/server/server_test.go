package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/protocol/status"
	"github.com/Master290/RegionGate/internal/session"
	"github.com/Master290/RegionGate/internal/transfer"
	protocolTransport "github.com/Master290/RegionGate/internal/transport"
)

func TestServerStatusFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	s := New(Config{Status: status.Response{
		Version:     status.Version{Name: "1.20.4", Protocol: 765},
		Players:     status.Players{Max: 100, Online: 1},
		Description: status.Description{Text: "RegionGate"},
	}}, nil)
	done := make(chan struct{})
	go func() {
		s.serveConn(serverConn)
		close(done)
	}()

	framer := codec.NewFramer(1024)
	if err := framer.WriteFrame(clientConn, handshakePayload(handshake.NextStatus)); err != nil {
		t.Fatal(err)
	}
	if err := framer.WriteFrame(clientConn, codec.AppendVarInt(nil, 0x00)); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(clientConn)
	response, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, body, err := codec.PacketID(response)
	if err != nil || id != 0x00 {
		t.Fatalf("status response id=%d err=%v", id, err)
	}
	json, _, err := codec.ConsumeVarInt(body)
	if err != nil || json < 1 {
		t.Fatalf("response string length=%d err=%v", json, err)
	}

	if err := framer.WriteFrame(clientConn, pingPayload(456)); err != nil {
		t.Fatal(err)
	}
	pong, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	pongID, pongBody, err := codec.PacketID(pong)
	if err != nil || pongID != 0x01 || len(pongBody) != 8 || int64(binary.BigEndian.Uint64(pongBody)) != 456 {
		t.Fatalf("pong id=%d body=%x err=%v", pongID, pongBody, err)
	}

	_ = clientConn.Close()
	_ = serverConn.Close()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		// net.Pipe does not guarantee prompt goroutine wakeup after both ends close.
	}
}

func TestServerShutdownClosesConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(Config{}, nil).Serve(ctx, listener) }()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	_, readErr := conn.Read(one[:])
	_ = conn.Close()
	if readErr == nil {
		t.Fatal("expected connection close")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerEnforcesPerIPConnectionLimit(t *testing.T) {
	first, firstPeer := net.Pipe()
	second, secondPeer := net.Pipe()
	defer first.Close()
	defer firstPeer.Close()
	defer second.Close()
	defer secondPeer.Close()
	s := New(Config{MaxConnectionsPerIP: 1}, nil)
	if !s.track(first) {
		t.Fatal("first connection was rejected")
	}
	if s.track(second) {
		t.Fatal("second connection from same address was accepted")
	}
	s.untrack(first)
	if !s.track(second) {
		t.Fatal("connection slot was not released")
	}
	s.untrack(second)
}

func TestServerHandshakeTimeoutClosesSilentConnection(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		New(Config{HandshakeTimeout: 20 * time.Millisecond}, nil).serveConn(serverConn)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("silent connection was not closed after handshake timeout")
	}
}

func TestServerLoginRateLimitDoesNotAffectStatus(t *testing.T) {
	s := New(Config{
		LoginRateLimit:  1,
		LoginRateWindow: time.Minute,
		Status: status.Response{
			Version:     status.Version{Name: "1.20.4", Protocol: 765},
			Players:     status.Players{Max: 100},
			Description: status.Description{Text: "RegionGate"},
		},
	}, nil)
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}

	firstServer, firstClient := net.Pipe()
	firstDone := make(chan struct{})
	go func() {
		s.serveConn(remoteAddrConn{Conn: firstServer, remote: remote})
		close(firstDone)
	}()
	framer := codec.NewFramer(1024)
	if err := framer.WriteFrame(firstClient, handshakePayload(handshake.NextLogin)); err != nil {
		t.Fatal(err)
	}
	_ = firstClient.Close()
	<-firstDone

	secondServer, secondClient := net.Pipe()
	secondDone := make(chan struct{})
	go func() {
		s.serveConn(remoteAddrConn{Conn: secondServer, remote: remote})
		close(secondDone)
	}()
	if err := framer.WriteFrame(secondClient, handshakePayload(handshake.NextLogin)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("rate-limited login connection was not closed")
	}
	_ = secondClient.Close()

	statusServer, statusClient := net.Pipe()
	statusDone := make(chan struct{})
	go func() {
		s.serveConn(remoteAddrConn{Conn: statusServer, remote: remote})
		close(statusDone)
	}()
	if err := framer.WriteFrame(statusClient, handshakePayload(handshake.NextStatus)); err != nil {
		t.Fatal(err)
	}
	if err := framer.WriteFrame(statusClient, codec.AppendVarInt(nil, 0x00)); err != nil {
		t.Fatal(err)
	}
	if _, err := framer.ReadFrame(bufio.NewReader(statusClient), nil); err != nil {
		t.Fatalf("status request was affected by login rate limit: %v", err)
	}
	_ = statusClient.Close()
	<-statusDone
}

func TestServerRegistersClientTransportForConnectionLifetime(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	s := New(Config{HandshakeTimeout: time.Second}, nil)
	done := make(chan struct{})
	go func() {
		s.serveConn(serverConn)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if client, ok := s.ClientTransport(serverConn); ok && client.Conn() == serverConn {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client transport was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	_ = clientConn.Close()
	<-done
	if _, ok := s.ClientTransport(serverConn); ok {
		t.Fatal("client transport was not removed")
	}
}

func TestBarrierFrameDispatchCoalescesAndAdvancesClientPhases(t *testing.T) {
	state := session.New()
	for _, next := range []session.State{session.StateLogin, session.StateConfiguration, session.StateLimboPlay} {
		if err := state.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.BeginTransfer(time.Unix(1, 0), []int64{9}, 2); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []session.BarrierPhase{session.BarrierBackendLogin, session.BarrierBackendConfiguration, session.BarrierAwaitingClientConfigurationStart} {
		if err := state.AdvanceBarrier(phase); err != nil {
			t.Fatal(err)
		}
	}
	if err := handleBarrierFrame(state, play.ServerboundPositionLookID, play.ServerboundPositionLookPayload(1, 64, 2, 30, 5, true)); err != nil {
		t.Fatal(err)
	}
	if err := handleBarrierFrame(state, play.ServerboundKeepAliveID, append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), make([]byte, 8)...)); err == nil {
		t.Fatal("expected stale keepalive rejection")
	}
	keepAlive := append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), make([]byte, 8)...)
	keepAlive[len(keepAlive)-1] = 9
	if err := handleBarrierFrame(state, play.ServerboundKeepAliveID, keepAlive); err != nil {
		t.Fatal(err)
	}
	if err := handleBarrierFrame(state, play.ServerboundConfigurationAcknowledgedID, codec.AppendVarInt(nil, play.ServerboundConfigurationAcknowledgedID)); err != nil {
		t.Fatal(err)
	}
	if state.State() != session.StateTransferBarrier {
		t.Fatalf("state=%s", state.State())
	}
	phase, _ := state.BarrierPhase()
	if phase != session.BarrierClientConfiguration {
		t.Fatalf("phase=%d", phase)
	}
}

func TestBarrierFrameRejectsDuplicateConfigurationAcknowledgement(t *testing.T) {
	state := session.New()
	for _, next := range []session.State{session.StateLogin, session.StateConfiguration, session.StateLimboPlay} {
		if err := state.Transition(next); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.BeginTransfer(time.Now(), nil, 1); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []session.BarrierPhase{session.BarrierBackendLogin, session.BarrierBackendConfiguration, session.BarrierAwaitingClientConfigurationStart} {
		if err := state.AdvanceBarrier(phase); err != nil {
			t.Fatal(err)
		}
	}
	ack := codec.AppendVarInt(nil, play.ServerboundConfigurationAcknowledgedID)
	if err := handleBarrierFrame(state, play.ServerboundConfigurationAcknowledgedID, ack); err != nil {
		t.Fatal(err)
	}
	if err := handleBarrierFrame(state, play.ServerboundConfigurationAcknowledgedID, ack); err == nil {
		t.Fatal("duplicate configuration acknowledgement was accepted")
	}
	phase, err := state.BarrierPhase()
	if err != nil || phase != session.BarrierClientConfiguration {
		t.Fatalf("phase=%d err=%v", phase, err)
	}
	if err := state.AdvanceBarrier(session.BarrierAwaitingClientConfigurationFinish); err != nil {
		t.Fatal(err)
	}
	finish := configuration.FinishAcknowledgedPayload()
	if err := handleBarrierFrame(state, configuration.ServerboundFinishConfigurationID, finish); err != nil {
		t.Fatal(err)
	}
	if err := handleBarrierFrame(state, configuration.ServerboundFinishConfigurationID, finish); err == nil {
		t.Fatal("duplicate finish configuration acknowledgement was accepted")
	}
	phase, err = state.BarrierPhase()
	if err != nil || phase != session.BarrierReady {
		t.Fatalf("phase=%d err=%v", phase, err)
	}
}

func TestServerOfflineLoginAndConfigurationFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	s := New(Config{KeepAliveInterval: 20 * time.Millisecond, KeepAliveTimeout: time.Second}, nil)
	done := make(chan struct{})
	go func() {
		s.serveConn(serverConn)
		close(done)
	}()

	framer := codec.NewFramer(1024)
	if err := framer.WriteFrame(clientConn, handshakePayload(handshake.NextLogin)); err != nil {
		t.Fatal(err)
	}
	start := codec.AppendVarInt(nil, login.ServerboundLoginStartID)
	start = codec.AppendString(start, "Daniar")
	start = append(start, make([]byte, 16)...)
	if err := framer.WriteFrame(clientConn, start); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(clientConn)
	success, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, username, err := login.ReadUUID(success); err != nil || username != "Daniar" {
		t.Fatalf("login success username=%q err=%v", username, err)
	}

	if err := framer.WriteFrame(clientConn, codec.AppendVarInt(nil, login.ServerboundLoginAckID)); err != nil {
		t.Fatal(err)
	}
	registry, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	registryID, registryBody, err := codec.PacketID(registry)
	if err != nil || registryID != 0x05 || len(registryBody) < 3 {
		t.Fatalf("registry id=%d body=%d err=%v", registryID, len(registryBody), err)
	}
	finish, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	finishID, finishBody, err := codec.PacketID(finish)
	if err != nil || finishID != configuration.ClientboundFinishConfigurationID || len(finishBody) != 0 {
		t.Fatalf("finish id=%d body=%x err=%v", finishID, finishBody, err)
	}

	if err := framer.WriteFrame(clientConn, codec.AppendVarInt(nil, configuration.ServerboundFinishConfigurationID)); err != nil {
		t.Fatal(err)
	}
	join, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	joinID, _, err := codec.PacketID(join)
	if err != nil || joinID != play.ClientboundJoinGameID {
		t.Fatalf("join id=%d err=%v", joinID, err)
	}
	for _, wantID := range []int32{
		play.ClientboundChunkBatchStartID,
		play.ClientboundMapChunkID,
		play.ClientboundChunkBatchFinishedID,
		play.ClientboundSpawnPositionID,
		play.ClientboundPositionLookID,
	} {
		frame, err := framer.ReadFrame(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := codec.PacketID(frame)
		if err != nil || id != wantID {
			t.Fatalf("play initialization id=%d want=%d err=%v", id, wantID, err)
		}
	}
	confirm := codec.AppendVarInt(nil, play.ServerboundTeleportConfirmID)
	confirm = codec.AppendVarInt(confirm, 1)
	if err := framer.WriteFrame(clientConn, confirm); err != nil {
		t.Fatal(err)
	}
	keepAlive, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	keepAliveID, keepAliveBody, err := codec.PacketID(keepAlive)
	if err != nil || keepAliveID != play.ClientboundKeepAliveID || len(keepAliveBody) != 8 {
		t.Fatalf("keepalive id=%d body=%x err=%v", keepAliveID, keepAliveBody, err)
	}
	response := append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), keepAliveBody...)
	if err := framer.WriteFrame(clientConn, response); err != nil {
		t.Fatal(err)
	}
	secondKeepAlive, err := framer.ReadFrame(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondID, secondBody, err := codec.PacketID(secondKeepAlive)
	if err != nil || secondID != play.ClientboundKeepAliveID || len(secondBody) != 8 {
		t.Fatalf("second keepalive id=%d body=%x err=%v", secondID, secondBody, err)
	}
	if string(secondBody) == string(keepAliveBody) {
		t.Fatal("keepalive ID was not advanced")
	}
	response = append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), secondBody...)
	if err := framer.WriteFrame(clientConn, response); err != nil {
		t.Fatal(err)
	}
	_ = clientConn.Close()
	_ = serverConn.Close()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServerAdmissionTransfersClientToBackendPlay(t *testing.T) {
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backendListener.Close()
	secret := []byte("integration-secret")
	forwarder, err := forwarding.NewModernForwarding(secret)
	if err != nil {
		t.Fatal(err)
	}
	dialer := backend.NewDialer(backend.Config{Address: backendListener.Addr().String(), Host: "localhost", Port: 25565})
	coordinator := transfer.NewCoordinator(dialer, forwarder, transfer.Config{BarrierTimeout: time.Second})

	backendDone := make(chan error, 1)
	go func() {
		conn, err := backendListener.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		server := protocolTransport.New(conn, 4096)
		defer server.Close()
		if _, err := server.ReadFrame(); err != nil { // handshake
			backendDone <- err
			return
		}
		if _, err := server.ReadFrame(); err != nil { // login start
			backendDone <- err
			return
		}
		request := codec.AppendVarInt(nil, login.ClientboundPluginRequestID)
		request = codec.AppendVarInt(request, 1)
		request = codec.AppendString(request, forwarding.VelocityChannel)
		if err := server.WriteFrame(request); err != nil {
			backendDone <- err
			return
		}
		if _, err := server.ReadFrame(); err != nil { // forwarding response
			backendDone <- err
			return
		}
		if err := server.WriteFrame(login.SuccessPayload("Daniar")); err != nil {
			backendDone <- err
			return
		}
		if _, err := server.ReadFrame(); err != nil { // login ack
			backendDone <- err
			return
		}
		if err := server.WriteFrame(configuration.FinishPayload()); err != nil {
			backendDone <- err
			return
		}
		if _, err := server.ReadFrame(); err != nil { // backend finish ack
			backendDone <- err
			return
		}
		if err := server.WriteFrame(play.KeepAlivePayload(88)); err != nil {
			backendDone <- err
			return
		}
		response, err := server.ReadFrame()
		if err != nil {
			backendDone <- err
			return
		}
		value, err := play.ParseKeepAlive(response)
		if err != nil || value != 88 {
			backendDone <- play.ErrMalformed
			return
		}
		backendDone <- nil
	}()

	serverConn, clientConn := net.Pipe()
	s := New(Config{TransferCoordinator: coordinator, KeepAliveInterval: time.Second, KeepAliveTimeout: time.Second}, nil)
	serveDone := make(chan struct{})
	go func() {
		s.serveConn(serverConn)
		close(serveDone)
	}()
	client := protocolTransport.New(clientConn, 4096)
	defer client.Close()
	if err := client.WriteFrame(handshakePayload(handshake.NextLogin)); err != nil {
		t.Fatal(err)
	}
	start := codec.AppendVarInt(nil, login.ServerboundLoginStartID)
	start = codec.AppendString(start, "Daniar")
	start = append(start, make([]byte, 16)...)
	if err := client.WriteFrame(start); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadFrame(); err != nil { // login success
		t.Fatal(err)
	}
	if err := client.WriteFrame(codec.AppendVarInt(nil, login.ServerboundLoginAckID)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadFrame(); err != nil { // registry
		t.Fatal(err)
	}
	if _, err := client.ReadFrame(); err != nil { // finish configuration
		t.Fatal(err)
	}
	if err := client.WriteFrame(configuration.FinishAcknowledgedPayload()); err != nil {
		t.Fatal(err)
	}
	for range 6 { // join plus five Limbo initialization packets
		if _, err := client.ReadFrame(); err != nil {
			t.Fatal(err)
		}
	}
	confirm := codec.AppendVarInt(nil, play.ServerboundTeleportConfirmID)
	confirm = codec.AppendVarInt(confirm, 1)
	if err := client.WriteFrame(confirm); err != nil {
		t.Fatal(err)
	}
	limboKeepAlive, err := client.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	_, limboBody, err := codec.PacketID(limboKeepAlive)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteFrame(append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), limboBody...)); err != nil {
		t.Fatal(err)
	}

	admitDone := make(chan error, 1)
	go func() { admitDone <- s.Admit(context.Background(), serverConn) }()
	startConfiguration, err := client.ReadFrame()
	if err != nil || string(startConfiguration) != string(play.StartConfigurationPayload()) {
		t.Fatalf("start configuration=%x err=%v", startConfiguration, err)
	}
	if err := client.WriteFrame(codec.AppendVarInt(nil, play.ServerboundConfigurationAcknowledgedID)); err != nil {
		t.Fatal(err)
	}
	finish, err := client.ReadFrame()
	if err != nil || string(finish) != string(configuration.FinishPayload()) {
		t.Fatalf("finish configuration=%x err=%v", finish, err)
	}
	if err := client.WriteFrame(configuration.FinishAcknowledgedPayload()); err != nil {
		t.Fatal(err)
	}
	if err := <-admitDone; err != nil {
		t.Fatal(err)
	}
	backendKeepAlive, err := client.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	_, backendBody, err := codec.PacketID(backendKeepAlive)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteFrame(append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), backendBody...)); err != nil {
		t.Fatal(err)
	}
	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	<-serveDone
}

func handshakePayload(next handshake.NextState) []byte {
	payload := codec.AppendVarInt(nil, 0x00)
	payload = codec.AppendVarInt(payload, handshake.ProtocolVersion)
	payload = codec.AppendString(payload, "localhost")
	payload = append(payload, 0x63, 0xdd)
	return codec.AppendVarInt(payload, int32(next))
}

func pingPayload(value int64) []byte {
	payload := codec.AppendVarInt(nil, 0x01)
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(value))
	return append(payload, raw[:]...)
}

type remoteAddrConn struct {
	net.Conn
	remote net.Addr
}

func (c remoteAddrConn) RemoteAddr() net.Addr { return c.remote }
