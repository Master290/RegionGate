package backend

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestCompleteConfigurationClassifiesTransportDisconnect(t *testing.T) {
	proxyConn, serverConn := net.Pipe()
	proxy := transport.New(proxyConn, 1024)
	_ = serverConn.Close()
	defer proxy.Close()
	_, err := CompleteConfiguration(context.Background(), proxy, ConfigurationConfig{Timeout: time.Second})
	if !errors.Is(err, ErrBackendConfigurationDisconnected) {
		t.Fatalf("error=%v", err)
	}
}

func TestCompleteConfigurationEnforcesBufferBounds(t *testing.T) {
	proxyConn, serverConn := net.Pipe()
	proxy := transport.New(proxyConn, 1024)
	server := transport.New(serverConn, 1024)
	defer proxy.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		if err := server.WriteFrame(append(codec.AppendVarInt(nil, 0x05), 1, 2)); err != nil {
			done <- err
			return
		}
		done <- server.WriteFrame(append(codec.AppendVarInt(nil, 0x06), 3, 4))
	}()
	_, err := CompleteConfiguration(context.Background(), proxy, ConfigurationConfig{Timeout: time.Second, MaxPackets: 1, MaxBytes: 32})
	if !errors.Is(err, ErrBackendConfigurationTooLarge) {
		t.Fatalf("error=%v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

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
