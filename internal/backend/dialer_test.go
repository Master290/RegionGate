package backend

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
)

func TestDialerWritesIndependentHandshakeAndLoginStart(t *testing.T) {
	uid := [16]byte{1, 2, 3, 4}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		framer := codec.NewFramer(1024)
		frame, err := framer.ReadFrame(reader, nil)
		if err != nil {
			serverDone <- err
			return
		}
		packet, err := handshake.Parse(frame)
		if err != nil || packet.NextState != handshake.NextLogin || packet.ServerAddress != "localhost" || packet.ServerPort != 25565 {
			serverDone <- fmt.Errorf("handshake packet=%+v err=%v", packet, err)
			return
		}
		frame, err = framer.ReadFrame(reader, nil)
		if err != nil {
			serverDone <- err
			return
		}
		start, err := login.ParseStart(frame)
		if err != nil || start.Username != "Daniar" || start.UUID != uid {
			serverDone <- fmt.Errorf("login start=%+v err=%v", start, err)
			return
		}
		serverDone <- nil
	}()

	dialer := NewDialer(Config{Address: listener.Addr().String(), Host: "localhost", Port: 25565})
	client, err := dialer.Dial(context.Background(), "Daniar", uid)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if health := dialer.Health(); health.State != HealthUnknown || !health.ChangedAt.IsZero() {
		t.Fatalf("health=%+v", health)
	}
}

func TestDialerMarksInvalidConfigurationUnhealthy(t *testing.T) {
	dialer := NewDialer(Config{})
	if _, err := dialer.Dial(context.Background(), "Daniar", [16]byte{}); err == nil {
		t.Fatal("expected empty address error")
	}
	health := dialer.Health()
	if health.State != HealthUnhealthy || health.LastError == "" || health.ChangedAt.IsZero() {
		t.Fatalf("health=%+v", health)
	}
}
