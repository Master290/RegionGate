package backend

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestCompleteLoginHandlesVelocityForwarding(t *testing.T) {
	proxyConn, serverConn := net.Pipe()
	proxy := transport.New(proxyConn, 4096)
	server := transport.New(serverConn, 4096)
	defer proxy.Close()
	defer server.Close()

	secret := []byte("forwarding-secret")
	forwarder, err := forwarding.NewModernForwarding(secret)
	if err != nil {
		t.Fatal(err)
	}
	identity := forwarding.PlayerIdentity{Address: "203.0.113.9", Username: "Daniar", UUID: [16]byte{1, 2, 3}}
	serverDone := make(chan error, 1)
	go func() {
		request := codec.AppendVarInt(nil, login.ClientboundPluginRequestID)
		request = codec.AppendVarInt(request, 7)
		request = codec.AppendString(request, forwarding.VelocityChannel)
		if err := server.WriteFrame(request); err != nil {
			serverDone <- err
			return
		}
		response, err := server.ReadFrame()
		if err != nil {
			serverDone <- err
			return
		}
		id, body, err := codec.PacketID(response)
		if err != nil || id != login.ServerboundPluginResponseID {
			serverDone <- ErrUnexpectedLoginPacket
			return
		}
		messageID, used, err := codec.ConsumeVarInt(body)
		if err != nil || messageID != 7 || len(body[used:]) <= 1+sha256.Size || body[used] != 1 {
			serverDone <- ErrUnexpectedLoginPacket
			return
		}
		signed := body[used+1:]
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(signed[sha256.Size:])
		if !hmac.Equal(signed[:sha256.Size], mac.Sum(nil)) {
			serverDone <- ErrUnexpectedLoginPacket
			return
		}
		if err := server.WriteFrame(login.SuccessPayload(identity.Username)); err != nil {
			serverDone <- err
			return
		}
		ack, err := server.ReadFrame()
		if err != nil {
			serverDone <- err
			return
		}
		if err := login.ParseAcknowledged(ack); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	result, err := CompleteLogin(context.Background(), proxy, forwarder, identity, LoginConfig{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Username != identity.Username || result.UUID != login.OfflineUUID(identity.Username) {
		t.Fatalf("result=%+v", result)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestCompleteLoginRejectsCompression(t *testing.T) {
	proxyConn, serverConn := net.Pipe()
	proxy := transport.New(proxyConn, 1024)
	server := transport.New(serverConn, 1024)
	defer proxy.Close()
	defer server.Close()

	go func() { _ = server.WriteFrame(codec.AppendVarInt(nil, clientboundSetCompressionID)) }()
	_, err := CompleteLogin(context.Background(), proxy, nil, forwarding.PlayerIdentity{}, LoginConfig{Timeout: time.Second})
	if !errors.Is(err, ErrUnsupportedCompression) {
		t.Fatalf("error=%v", err)
	}
}
