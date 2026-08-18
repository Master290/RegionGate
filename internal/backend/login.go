package backend

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/transport"
)

var (
	ErrBackendDisconnected   = errors.New("backend disconnected during login")
	ErrUnexpectedLoginPacket = errors.New("unexpected backend login packet")
	ErrUnsupportedEncryption = errors.New("backend encryption is not supported")
	ErrUnexpectedPlugin      = errors.New("unexpected backend login plugin request")
)

const (
	clientboundDisconnectID        = 0x00
	clientboundEncryptionRequestID = 0x01
	clientboundSetCompressionID    = 0x03
)

type LoginConfig struct {
	Timeout time.Duration
}

type LoginResult struct {
	UUID     [16]byte
	Username string
}

func CompleteLogin(ctx context.Context, backend *transport.Transport, forwarder *forwarding.ModernForwarding, identity forwarding.PlayerIdentity, config LoginConfig) (LoginResult, error) {
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	loginCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	_ = backend.SetReadDeadline(time.Now().Add(config.Timeout))
	_ = backend.SetWriteDeadline(time.Now().Add(config.Timeout))
	stopInterrupt := interruptOnDone(loginCtx, func() { _ = backend.SetReadDeadline(time.Now()) })
	defer func() {
		stopInterrupt()
		cancel()
	}()

	for {
		frame, err := backend.ReadFrame()
		if err != nil {
			if loginCtx.Err() != nil {
				return LoginResult{}, loginCtx.Err()
			}
			return LoginResult{}, fmt.Errorf("read backend login packet: %w", err)
		}
		id, body, err := codec.PacketID(frame)
		if err != nil {
			return LoginResult{}, ErrUnexpectedLoginPacket
		}
		switch id {
		case login.ClientboundPluginRequestID:
			request, err := login.ParsePluginRequest(frame)
			if err != nil || request.Channel != forwarding.VelocityChannel || forwarder == nil {
				return LoginResult{}, ErrUnexpectedPlugin
			}
			response, err := forwarder.BuildResponse(identity)
			if err != nil {
				return LoginResult{}, fmt.Errorf("build velocity forwarding response: %w", err)
			}
			if err := backend.WriteFrame(login.PluginResponsePayload(request.MessageID, response)); err != nil {
				return LoginResult{}, fmt.Errorf("write velocity forwarding response: %w", err)
			}
		case login.ClientboundLoginSuccessID:
			uid, username, err := login.ReadUUID(frame)
			if err != nil {
				return LoginResult{}, ErrUnexpectedLoginPacket
			}
			if err := backend.WriteFrame(codec.AppendVarInt(nil, login.ServerboundLoginAckID)); err != nil {
				return LoginResult{}, fmt.Errorf("write backend login acknowledgement: %w", err)
			}
			stopInterrupt()
			cancel()
			_ = backend.SetReadDeadline(time.Time{})
			_ = backend.SetWriteDeadline(time.Time{})
			return LoginResult{UUID: uid, Username: username}, nil
		case clientboundDisconnectID:
			return LoginResult{}, ErrBackendDisconnected
		case clientboundEncryptionRequestID:
			return LoginResult{}, ErrUnsupportedEncryption
		case clientboundSetCompressionID:
			threshold, used, err := codec.ConsumeVarInt(body)
			if err != nil || threshold < 0 || used != len(body) {
				return LoginResult{}, ErrUnexpectedLoginPacket
			}
			if err := backend.EnableCompression(int(threshold)); err != nil {
				return LoginResult{}, fmt.Errorf("enable backend compression: %w", err)
			}
		default:
			return LoginResult{}, ErrUnexpectedLoginPacket
		}
	}
}
