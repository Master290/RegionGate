package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/protocol/status"
	"github.com/Master290/RegionGate/internal/session"
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
