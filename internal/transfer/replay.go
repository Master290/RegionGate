package transfer

import (
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/session"
)

func ReplayPayloads(replay session.Replay) [][]byte {
	count := len(replay.Commands)
	if replay.Position != nil {
		count++
	}
	payloads := make([][]byte, 0, count)
	if position := replay.Position; position != nil {
		payloads = append(payloads, play.ServerboundPositionLookPayload(position.X, position.Y, position.Z, position.Yaw, position.Pitch, position.OnGround))
	}
	for _, command := range replay.Commands {
		payloads = append(payloads, play.PlayerCommandPayload(command.EntityID, command.ActionID, command.Data))
	}
	return payloads
}
