package bridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/transport"
)

var (
	ErrUnexpectedBackendKeepAlive = errors.New("unexpected backend keepalive response")
	ErrKeepAliveLimit             = errors.New("backend keepalive limit exceeded")
)

type ClientFrame struct {
	Payload []byte
	Err     error
}

type Config struct {
	MaxPendingKeepAlives int
}

func RunPlay(ctx context.Context, clientFrames <-chan ClientFrame, client, backend *transport.Transport, config Config) error {
	if config.MaxPendingKeepAlives <= 0 {
		config.MaxPendingKeepAlives = 16
	}
	stopInterrupt := interruptOnDone(ctx, client, backend)
	defer stopInterrupt()
	backendFrames := make(chan ClientFrame)
	backendDone := make(chan struct{})
	defer close(backendDone)
	go func() {
		for {
			frame, err := backend.ReadFrame()
			select {
			case backendFrames <- ClientFrame{Payload: frame, Err: err}:
			case <-backendDone:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	pendingKeepAlives := make(map[int64]struct{})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-backendFrames:
			if frame.Err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("read backend play packet: %w", frame.Err)
			}
			id, body, err := codec.PacketID(frame.Payload)
			if err != nil {
				return err
			}
			if id == play.ClientboundKeepAliveID {
				if len(body) != 8 || len(pendingKeepAlives) >= config.MaxPendingKeepAlives {
					return ErrKeepAliveLimit
				}
				keepAliveID := int64(binary.BigEndian.Uint64(body))
				pendingKeepAlives[keepAliveID] = struct{}{}
			}
			if err := client.WriteFrame(frame.Payload); err != nil {
				return fmt.Errorf("write client play packet: %w", err)
			}
		case frame := <-clientFrames:
			if frame.Err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("read client play packet: %w", frame.Err)
			}
			id, _, err := codec.PacketID(frame.Payload)
			if err != nil {
				return err
			}
			if id == play.ServerboundKeepAliveID {
				keepAliveID, err := play.ParseKeepAlive(frame.Payload)
				if err != nil {
					return err
				}
				if _, ok := pendingKeepAlives[keepAliveID]; !ok {
					return ErrUnexpectedBackendKeepAlive
				}
				delete(pendingKeepAlives, keepAliveID)
			}
			if err := backend.WriteFrame(frame.Payload); err != nil {
				return fmt.Errorf("write backend play packet: %w", err)
			}
		}
	}
}

func interruptOnDone(ctx context.Context, transports ...*transport.Transport) func() {
	if ctx == nil {
		ctx = context.Background()
	}
	interrupt := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			for _, t := range transports {
				if t == nil {
					continue
				}
				_ = t.SetReadDeadline(time.Now())
				_ = t.SetWriteDeadline(time.Now())
			}
		case <-interrupt:
		}
	}()
	return func() { close(interrupt) }
}
