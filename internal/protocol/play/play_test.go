package play

import (
	"encoding/binary"
	"math"
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

func TestConfigurationTransitionAndMovementEncoding(t *testing.T) {
	if id, body, err := codec.PacketID(StartConfigurationPayload()); err != nil || id != ClientboundStartConfigurationID || len(body) != 0 {
		t.Fatalf("start configuration id=%d body=%x err=%v", id, body, err)
	}
	if err := ParseConfigurationAcknowledged(codec.AppendVarInt(nil, ServerboundConfigurationAcknowledgedID)); err != nil {
		t.Fatal(err)
	}
	encoded := ServerboundPositionLookPayload(1.25, 64, -2.5, 90, -10, true)
	movement, err := DecodeMovement(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if movement.X != 1.25 || movement.Y != 64 || movement.Z != -2.5 || movement.Yaw != 90 || movement.Pitch != -10 || !movement.OnGround || !movement.HasLook {
		t.Fatalf("movement=%+v", movement)
	}
	position, err := DecodeMovement(ServerboundPositionPayload(2, 65, 3, false))
	if err != nil || position.HasLook || position.OnGround {
		t.Fatalf("position=%+v err=%v", position, err)
	}
	if err := ParseMovement(append(codec.AppendVarInt(nil, ServerboundPositionID), make([]byte, 24)...)); err == nil {
		t.Fatal("expected malformed movement body")
	}
}

func TestMovementRejectsNonFiniteValues(t *testing.T) {
	if _, err := DecodeMovement(ServerboundPositionPayload(math.Inf(1), 0, 0, true)); err == nil {
		t.Fatal("expected non-finite movement rejection")
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
