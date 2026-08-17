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
	"github.com/Master290/RegionGate/internal/protocol/status"
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

func TestServerOfflineLoginAndConfigurationFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	s := New(Config{}, nil)
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
