package play

import (
	"encoding/binary"
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestKeepAlive(t *testing.T) {
	payload := KeepAlivePayload(0x0102030405060708)
	value, err := ParseKeepAlive(append(codec.AppendVarInt(nil, ServerboundKeepAliveID), payload[len(codec.AppendVarInt(nil, ClientboundKeepAliveID)):]...))
	if err != nil || value != 0x0102030405060708 {
		t.Fatalf("value=%d err=%v", value, err)
	}
}

func TestMovementPacketLengths(t *testing.T) {
	position := append(codec.AppendVarInt(nil, ServerboundPositionID), make([]byte, 25)...)
	if err := ParseMovement(position); err != nil {
		t.Fatal(err)
	}
	look := append(codec.AppendVarInt(nil, ServerboundPositionLookID), make([]byte, 33)...)
	if err := ParseMovement(look); err != nil {
		t.Fatal(err)
	}
}

func TestJoinGameContainsExpectedScalars(t *testing.T) {
	payload := JoinGamePayload(JoinGameConfig{
		EntityID: 7, WorldName: "minecraft:overworld", MaxPlayers: 1,
		ViewDistance: 8, SimulationDistance: 8, DimensionType: "minecraft:overworld",
		DimensionName: "minecraft:overworld", GameMode: 2, PreviousGameMode: -1,
	})
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundJoinGameID || len(body) <= 4 {
		t.Fatalf("id=%d body=%d err=%v", id, len(body), err)
	}
	if got := int32(binary.BigEndian.Uint32(body[:4])); got != 7 {
		t.Fatalf("entity id=%d", got)
	}
	if body[4] != 0 || body[5] != 2 || body[6] != 0xff {
		t.Fatalf("game modes: hardcore=%d game=%d previous=%d", body[4], body[5], body[6])
	}
}
