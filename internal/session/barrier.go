package session

import (
	"errors"
	"time"
)

var (
	ErrBarrierInactive       = errors.New("transfer barrier is not active")
	ErrBarrierNotReady       = errors.New("transfer barrier is not ready to release")
	ErrUnexpectedKeepAlive   = errors.New("unexpected limbo keepalive response")
	ErrCommandQueueExhausted = errors.New("transfer barrier command queue is full")
)

type BarrierPhase uint8

const (
	BarrierDraining BarrierPhase = iota
	BarrierBackendLogin
	BarrierBackendConfiguration
	BarrierAwaitingClientConfigurationStart
	BarrierClientConfiguration
	BarrierAwaitingClientConfigurationFinish
	BarrierReady
)

type InputKind uint8

const (
	InputKeepAlive InputKind = iota
	InputMovement
	InputPlayerCommand
	InputUnsafe
)

type InputDisposition uint8

const (
	InputConsumed InputDisposition = iota
	InputCoalesced
	InputQueued
	InputDropped
)

type Position struct {
	X        float64
	Y        float64
	Z        float64
	Yaw      float32
	Pitch    float32
	OnGround bool
}

type PlayerCommand struct {
	EntityID int32
	ActionID int32
	Data     int32
}

type Input struct {
	Kind        InputKind
	KeepAliveID int64
	Position    Position
	HasLook     bool
	Command     PlayerCommand
}

type Replay struct {
	Position *Position
	Commands []PlayerCommand
}

type barrierState struct {
	startedAt       time.Time
	phase           BarrierPhase
	limboKeepAlives map[int64]struct{}
	latestPosition  Position
	hasPosition     bool
	commands        []PlayerCommand
	maxCommands     int
}

func newBarrier(now time.Time, keepAliveIDs []int64, maxCommands int) *barrierState {
	if maxCommands < 0 {
		maxCommands = 0
	}

	keepAlives := make(map[int64]struct{}, len(keepAliveIDs))
	for _, id := range keepAliveIDs {
		keepAlives[id] = struct{}{}
	}
	return &barrierState{
		startedAt:       now,
		phase:           BarrierDraining,
		limboKeepAlives: keepAlives,
		commands:        make([]PlayerCommand, 0, maxCommands),
		maxCommands:     maxCommands,
	}
}

func (b *barrierState) handle(input Input) (InputDisposition, error) {
	switch input.Kind {
	case InputKeepAlive:
		if _, ok := b.limboKeepAlives[input.KeepAliveID]; !ok {
			return InputDropped, ErrUnexpectedKeepAlive
		}
		delete(b.limboKeepAlives, input.KeepAliveID)
		return InputConsumed, nil
	case InputMovement:
		position := input.Position
		if !input.HasLook && b.hasPosition {
			position.Yaw = b.latestPosition.Yaw
			position.Pitch = b.latestPosition.Pitch
		}
		b.latestPosition = position
		b.hasPosition = true
		return InputCoalesced, nil
	case InputPlayerCommand:
		if len(b.commands) >= b.maxCommands {
			return InputDropped, ErrCommandQueueExhausted
		}
		b.commands = append(b.commands, input.Command)
		return InputQueued, nil
	default:
		return InputDropped, nil
	}
}

func (b *barrierState) replay() Replay {
	replay := Replay{Commands: append([]PlayerCommand(nil), b.commands...)}
	if b.hasPosition {
		position := b.latestPosition
		replay.Position = &position
	}
	return replay
}
