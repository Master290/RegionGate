package backend

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/transport"
)

var (
	ErrBackendConfigurationDisconnected = errors.New("backend disconnected during configuration")
	ErrBackendConfigurationPacket       = errors.New("unexpected backend configuration packet")
	ErrBackendConfigurationTooLarge     = errors.New("backend configuration buffer limit exceeded")
)

type ConfigurationConfig struct {
	Timeout    time.Duration
	MaxPackets int
	MaxBytes   int
}

type ConfigurationResult struct {
	Packets [][]byte
}

func CompleteConfiguration(ctx context.Context, backend *transport.Transport, config ConfigurationConfig) (ConfigurationResult, error) {
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	if config.MaxPackets <= 0 {
		config.MaxPackets = 128
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = 8 << 20
	}
	configCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	_ = backend.SetReadDeadline(time.Now().Add(config.Timeout))
	_ = backend.SetWriteDeadline(time.Now().Add(config.Timeout))
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-configCtx.Done():
			_ = backend.SetReadDeadline(time.Now())
		case <-finished:
		}
	}()

	result := ConfigurationResult{Packets: make([][]byte, 0, config.MaxPackets)}
	bytesBuffered := 0
	for {
		frame, err := backend.ReadFrame()
		if err != nil {
			if configCtx.Err() != nil {
				return ConfigurationResult{}, configCtx.Err()
			}
			return ConfigurationResult{}, fmt.Errorf("%w: %v", ErrBackendConfigurationDisconnected, err)
		}
		id, body, err := codec.PacketID(frame)
		if err != nil {
			return ConfigurationResult{}, ErrBackendConfigurationPacket
		}
		switch id {
		case configuration.ClientboundFinishConfigurationID:
			if len(body) != 0 {
				return ConfigurationResult{}, ErrBackendConfigurationPacket
			}
			if err := backend.WriteFrame(configuration.FinishAcknowledgedPayload()); err != nil {
				return ConfigurationResult{}, fmt.Errorf("write backend configuration acknowledgement: %w", err)
			}
			_ = backend.SetReadDeadline(time.Time{})
			_ = backend.SetWriteDeadline(time.Time{})
			return result, nil
		case configuration.ClientboundKeepAliveID:
			if len(body) != 8 {
				return ConfigurationResult{}, ErrBackendConfigurationPacket
			}
			response := append(codec.AppendVarInt(nil, configuration.ServerboundKeepAliveID), body...)
			if err := backend.WriteFrame(response); err != nil {
				return ConfigurationResult{}, fmt.Errorf("write backend configuration keepalive: %w", err)
			}
		case configuration.ClientboundPingID:
			if len(body) != 4 {
				return ConfigurationResult{}, ErrBackendConfigurationPacket
			}
			response := append(codec.AppendVarInt(nil, configuration.ServerboundPongID), body...)
			if err := backend.WriteFrame(response); err != nil {
				return ConfigurationResult{}, fmt.Errorf("write backend configuration pong: %w", err)
			}
		case configuration.ClientboundDisconnectID:
			return ConfigurationResult{}, ErrBackendConfigurationDisconnected
		default:
			if len(result.Packets) >= config.MaxPackets || bytesBuffered+len(frame) > config.MaxBytes {
				return ConfigurationResult{}, ErrBackendConfigurationTooLarge
			}
			result.Packets = append(result.Packets, append([]byte(nil), frame...))
			bytesBuffered += len(frame)
		}
	}
}
