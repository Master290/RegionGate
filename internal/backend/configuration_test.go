package backend

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestCompleteConfigurationHandlesKeepAliveAndFinishes(t *testing.T) {
	proxyConn, serverConn := net.Pipe()
	proxy := transport.New(proxyConn, 4096)
	server := transport.New(serverConn, 4096)
	defer proxy.Close()
	defer server.Close()

	serverDone := make(chan error, 1)
	go func() {
		if err := server.WriteFrame(append(codec.AppendVarInt(nil, 0x05), 1, 2, 3)); err != nil {
			serverDone <- err
			return
		}
		if err := server.WriteFrame(append(codec.AppendVarInt(nil, 0x03), make([]byte, 8)...)); err != nil {
			serverDone <- err
			return
		}
		keepAliveResponse, err := server.ReadFrame()
		if err != nil {
			serverDone <- err
			return
		}
		id, body, err := codec.PacketID(keepAliveResponse)
		if err != nil || id != 0x03 || len(body) != 8 {
			serverDone <- ErrBackendConfigurationPacket
			return
		}
		if err := server.WriteFrame(configuration.FinishPayload()); err != nil {
			serverDone <- err
			return
		}
		ack, err := server.ReadFrame()
		if err != nil {
			serverDone <- err
			return
		}
		if string(ack) != string(configuration.FinishAcknowledgedPayload()) {
			serverDone <- ErrBackendConfigurationPacket
			return
		}
		serverDone <- nil
	}()

	result, err := CompleteConfiguration(context.Background(), proxy, ConfigurationConfig{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packets) != 1 || string(result.Packets[0]) != string(append(codec.AppendVarInt(nil, 0x05), 1, 2, 3)) {
		t.Fatalf("result=%x", result.Packets)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
