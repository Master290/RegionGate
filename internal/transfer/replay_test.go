package transfer

import (
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/session"
)

func TestReplayPayloadsEncodeNormalizedState(t *testing.T) {
	replay := session.Replay{
		Position: &session.Position{X: 1, Y: 64, Z: 2, Yaw: 30, Pitch: 10, OnGround: true},
		Commands: []session.PlayerCommand{{EntityID: 7, ActionID: 1, Data: 2}},
	}
	payloads := ReplayPayloads(replay)
	if len(payloads) != 2 {
		t.Fatalf("payload count=%d", len(payloads))
	}
	movement, err := play.DecodeMovement(payloads[0])
	if err != nil || movement.X != 1 || movement.Yaw != 30 || !movement.OnGround {
		t.Fatalf("movement=%+v err=%v", movement, err)
	}
	id, body, err := codec.PacketID(payloads[1])
	if err != nil || id != play.ServerboundPlayerCommandID {
		t.Fatalf("command id=%d err=%v", id, err)
	}
	for index, want := range []int32{7, 1, 2} {
		value, used, err := codec.ConsumeVarInt(body)
		if err != nil || value != want {
			t.Fatalf("command field %d=%d err=%v", index, value, err)
		}
		body = body[used:]
	}
	if len(body) != 0 {
		t.Fatalf("trailing command bytes=%x", body)
	}
}
