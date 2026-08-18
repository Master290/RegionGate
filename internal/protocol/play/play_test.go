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
	if body[4] != 0 || body[5] != 1 {
		t.Fatalf("hardcore/dimension count: hardcore=%d count=%d", body[4], body[5])
	}
}

func TestPositionAndTeleportConfirm(t *testing.T) {
	position := PositionLookPayload(0.5, 64, 0.5, 90, 0, 17)
	id, body, err := codec.PacketID(position)
	if err != nil || id != ClientboundPositionLookID || len(body) != 34 {
		t.Fatalf("position id=%d body=%d err=%v", id, len(body), err)
	}

	confirm := codec.AppendVarInt(nil, ServerboundTeleportConfirmID)
	confirm = codec.AppendVarInt(confirm, 17)
	teleportID, err := ParseTeleportConfirm(confirm)
	if err != nil || teleportID != 17 {
		t.Fatalf("teleport id=%d err=%v", teleportID, err)
	}
}

func TestVoidChunkPacket(t *testing.T) {
	payload := VoidChunkPayload(0, 0)
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundMapChunkID || len(body) < 16 {
		t.Fatalf("chunk id=%d body=%d err=%v", id, len(body), err)
	}
	if gotX, gotZ := int32(binary.BigEndian.Uint32(body[:4])), int32(binary.BigEndian.Uint32(body[4:8])); gotX != 0 || gotZ != 0 {
		t.Fatalf("chunk coordinates=%d,%d", gotX, gotZ)
	}
	if body[8] != 10 || body[9] != 0 {
		t.Fatalf("heightmaps root=%x", body[8:10])
	}
}

func TestSpawnPosition(t *testing.T) {
	payload := SpawnPositionPayload(0, 64, 0, 0)
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundSpawnPositionID || len(body) != 12 {
		t.Fatalf("spawn id=%d body=%d err=%v", id, len(body), err)
	}
	if packed := binary.BigEndian.Uint64(body[:8]); packed != 64 {
		t.Fatalf("packed position=%x", packed)
	}
}
