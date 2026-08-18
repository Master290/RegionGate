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

func TestJoinGameProtocol765WireFormat(t *testing.T) {
	payload := JoinGamePayload(JoinGameConfig{
		EntityID: 7, Hardcore: true, WorldName: "minecraft:overworld", MaxPlayers: 1,
		ViewDistance: 8, SimulationDistance: 8, DimensionType: "minecraft:overworld",
		DimensionName: "minecraft:overworld", HashedSeed: 42, GameMode: 2, PreviousGameMode: -1,
		ReducedDebugInfo: true, RespawnScreen: true, LimitedCrafting: true,
		Debug: true, Flat: true, PortalCooldown: 17,
	})
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundJoinGameID {
		t.Fatalf("id=%d err=%v", id, err)
	}
	entityID := consumeInt32(t, &body)
	hardcore := consumeBool(t, &body)
	worldCount := consumeVarInt(t, &body)
	worldName := consumeString(t, &body)
	maxPlayers := consumeVarInt(t, &body)
	viewDistance := consumeVarInt(t, &body)
	simulationDistance := consumeVarInt(t, &body)
	reducedDebug := consumeBool(t, &body)
	respawnScreen := consumeBool(t, &body)
	limitedCrafting := consumeBool(t, &body)
	dimensionType := consumeString(t, &body)
	dimensionName := consumeString(t, &body)
	hashedSeed := consumeInt64(t, &body)
	gameMode := consumeByte(t, &body)
	previousGameMode := int8(consumeByte(t, &body))
	debug := consumeBool(t, &body)
	flat := consumeBool(t, &body)
	hasDeathLocation := consumeBool(t, &body)
	portalCooldown := consumeVarInt(t, &body)

	if entityID != 7 || !hardcore || worldCount != 1 || worldName != "minecraft:overworld" ||
		maxPlayers != 1 || viewDistance != 8 || simulationDistance != 8 || !reducedDebug ||
		!respawnScreen || !limitedCrafting || dimensionType != "minecraft:overworld" ||
		dimensionName != "minecraft:overworld" || hashedSeed != 42 || gameMode != 2 ||
		previousGameMode != -1 || !debug || !flat || hasDeathLocation || portalCooldown != 17 {
		t.Fatalf("unexpected join game fields")
	}
	if len(body) != 0 {
		t.Fatalf("trailing join game bytes: %x", body)
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

func TestPlayerCommandEncoding(t *testing.T) {
	payload := PlayerCommandPayload(7, 1, 2)
	command, err := ParsePlayerCommand(payload)
	if err != nil || command != (PlayerCommand{EntityID: 7, ActionID: 1, Data: 2}) {
		t.Fatalf("command=%+v err=%v", command, err)
	}
	if _, err := ParsePlayerCommand(PlayerCommandPayload(7, 99, 2)); err == nil {
		t.Fatal("expected invalid action rejection")
	}
}

func TestActionBarPayload(t *testing.T) {
	payload, err := ActionBarPayload("Queue position: 3")
	if err != nil {
		t.Fatal(err)
	}
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundActionBarID || len(body) < 3 || body[0] != 10 {
		t.Fatalf("id=%d body=%x err=%v", id, body, err)
	}
}

func TestVoidChunkProtocol765WireFormat(t *testing.T) {
	payload := VoidChunkPayload(0, 0)
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundMapChunkID {
		t.Fatalf("chunk id=%d err=%v", id, err)
	}
	if gotX, gotZ := consumeInt32(t, &body), consumeInt32(t, &body); gotX != 0 || gotZ != 0 {
		t.Fatalf("chunk coordinates=%d,%d", gotX, gotZ)
	}
	if tag, end := consumeByte(t, &body), consumeByte(t, &body); tag != 10 || end != 0 {
		t.Fatalf("heightmaps root=%x end=%x", tag, end)
	}
	chunkLength := consumeVarInt(t, &body)
	if chunkLength != overworldSectionCount*8 || int(chunkLength) > len(body) {
		t.Fatalf("chunk data length=%d remaining=%d", chunkLength, len(body))
	}
	chunkData := body[:chunkLength]
	body = body[chunkLength:]
	for section := range overworldSectionCount {
		if nonAir := binary.BigEndian.Uint16(chunkData[:2]); nonAir != 0 {
			t.Fatalf("section %d non-air blocks=%d", section, nonAir)
		}
		chunkData = chunkData[2:]
		for _, container := range []string{"blocks", "biomes"} {
			bits, value, dataLength := chunkData[0], chunkData[1], chunkData[2]
			chunkData = chunkData[3:]
			if bits != 0 || value != 0 || dataLength != 0 {
				t.Fatalf("section %d %s palette=%d,%d,%d", section, container, bits, value, dataLength)
			}
		}
	}
	if blockEntities := consumeVarInt(t, &body); blockEntities != 0 {
		t.Fatalf("block entities=%d", blockEntities)
	}
	if skyMask, blockMask := consumeVarInt(t, &body), consumeVarInt(t, &body); skyMask != 0 || blockMask != 0 {
		t.Fatalf("light data masks=%d,%d", skyMask, blockMask)
	}
	for _, name := range []string{"empty sky", "empty block"} {
		if longs := consumeVarInt(t, &body); longs != 1 {
			t.Fatalf("%s mask longs=%d", name, longs)
		}
		if mask := uint64(consumeInt64(t, &body)); mask != overworldLightSectionMask {
			t.Fatalf("%s mask=%x", name, mask)
		}
	}
	if skyArrays, blockArrays := consumeVarInt(t, &body), consumeVarInt(t, &body); skyArrays != 0 || blockArrays != 0 {
		t.Fatalf("light arrays=%d,%d", skyArrays, blockArrays)
	}
	if len(body) != 0 {
		t.Fatalf("trailing chunk bytes: %x", body)
	}
}

func consumeByte(t *testing.T, body *[]byte) byte {
	t.Helper()
	if len(*body) == 0 {
		t.Fatal("unexpected end of payload")
	}
	value := (*body)[0]
	*body = (*body)[1:]
	return value
}

func consumeBool(t *testing.T, body *[]byte) bool {
	t.Helper()
	value := consumeByte(t, body)
	if value > 1 {
		t.Fatalf("invalid boolean %d", value)
	}
	return value == 1
}

func consumeInt32(t *testing.T, body *[]byte) int32 {
	t.Helper()
	if len(*body) < 4 {
		t.Fatal("unexpected end of int32")
	}
	value := int32(binary.BigEndian.Uint32((*body)[:4]))
	*body = (*body)[4:]
	return value
}

func consumeInt64(t *testing.T, body *[]byte) int64 {
	t.Helper()
	if len(*body) < 8 {
		t.Fatal("unexpected end of int64")
	}
	value := int64(binary.BigEndian.Uint64((*body)[:8]))
	*body = (*body)[8:]
	return value
}

func consumeVarInt(t *testing.T, body *[]byte) int32 {
	t.Helper()
	value, used, err := codec.ConsumeVarInt(*body)
	if err != nil {
		t.Fatal(err)
	}
	*body = (*body)[used:]
	return value
}

func consumeString(t *testing.T, body *[]byte) string {
	t.Helper()
	length := consumeVarInt(t, body)
	if length < 0 || int(length) > len(*body) {
		t.Fatalf("invalid string length %d", length)
	}
	value := string((*body)[:length])
	*body = (*body)[length:]
	return value
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
