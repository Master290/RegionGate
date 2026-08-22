package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestRunPlayForwardsPacketsAndTracksBackendKeepAlive(t *testing.T) {
	clientProxyConn, clientPeerConn := net.Pipe()
	backendProxyConn, backendPeerConn := net.Pipe()
	clientProxy := transport.New(clientProxyConn, 4096)
	clientPeer := transport.New(clientPeerConn, 4096)
	backendProxy := transport.New(backendProxyConn, 4096)
	backendPeer := transport.New(backendPeerConn, 4096)
	defer clientProxy.Close()
	defer clientPeer.Close()
	defer backendProxy.Close()
	defer backendPeer.Close()

	clientFrames := make(chan ClientFrame, 2)
	bridgeDone := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { bridgeDone <- RunPlay(ctx, clientFrames, clientProxy, backendProxy, Config{}) }()

	go func() { _ = backendPeer.WriteFrame(play.KeepAlivePayload(77)) }()
	keepAlive, err := clientPeer.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	response := append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), keepAlive[1:]...)
	clientFrames <- ClientFrame{Payload: response}
	forwarded, err := backendPeer.ReadFrame()
	if err != nil || string(forwarded) != string(response) {
		t.Fatalf("forwarded=%x err=%v", forwarded, err)
	}

	movement := play.ServerboundPositionPayload(1, 64, 2, true)
	clientFrames <- ClientFrame{Payload: movement}
	forwarded, err = backendPeer.ReadFrame()
	if err != nil || string(forwarded) != string(movement) {
		t.Fatalf("movement=%x err=%v", forwarded, err)
	}
	cancel()
	if err := <-bridgeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("bridge error=%v", err)
	}
}

func TestRunPlayRejectsUnknownKeepAliveResponse(t *testing.T) {
	clientProxyConn, clientPeerConn := net.Pipe()
	backendProxyConn, backendPeerConn := net.Pipe()
	clientProxy := transport.New(clientProxyConn, 1024)
	backendProxy := transport.New(backendProxyConn, 1024)
	defer clientProxy.Close()
	defer clientPeerConn.Close()
	defer backendProxy.Close()
	defer backendPeerConn.Close()
	clientFrames := make(chan ClientFrame, 1)
	clientFrames <- ClientFrame{Payload: append(codec.AppendVarInt(nil, play.ServerboundKeepAliveID), make([]byte, 8)...)}
	err := RunPlay(context.Background(), clientFrames, clientProxy, backendProxy, Config{})
	if !errors.Is(err, ErrUnexpectedBackendKeepAlive) {
		t.Fatalf("bridge error=%v", err)
	}
}

func TestRunPlayCancellationUnblocksBlockedClientWrite(t *testing.T) {
	clientProxyConn, clientPeerConn := net.Pipe()
	backendProxyConn, backendPeerConn := net.Pipe()
	clientProxy := transport.New(clientProxyConn, 1024)
	backendProxy := transport.New(backendProxyConn, 1024)
	defer clientPeerConn.Close()
	defer backendPeerConn.Close()
	defer clientProxy.Close()
	defer backendProxy.Close()

	clientFrames := make(chan ClientFrame)
	ctx, cancel := context.WithCancel(context.Background())
	bridgeDone := make(chan error, 1)
	go func() { bridgeDone <- RunPlay(ctx, clientFrames, clientProxy, backendProxy, Config{}) }()

	go func() { _, _ = backendPeerConn.Write(codec.AppendVarInt(nil, 0x01)) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-bridgeDone:
		if err == nil {
			t.Fatal("bridge returned nil after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not unblock blocked client write")
	}
}
