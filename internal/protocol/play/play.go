package play

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed play packet")

const (
	ClientboundChunkBatchFinishedID        = 0x0C
	ClientboundChunkBatchStartID           = 0x0D
	ClientboundJoinGameID                  = 0x29
	ClientboundKeepAliveID                 = 0x24
	ClientboundMapChunkID                  = 0x25
	ClientboundPositionLookID              = 0x3E
	ClientboundSpawnPositionID             = 0x54
	ClientboundStartConfigurationID        = 0x67
	ServerboundConfigurationAcknowledgedID = 0x0B
	ServerboundTeleportConfirmID           = 0x00
	ServerboundKeepAliveID                 = 0x15
	ServerboundPositionID                  = 0x17
	ServerboundPositionLookID              = 0x18
	ServerboundPlayerCommandID             = 0x22
)

const overworldSectionCount = 24

type JoinGameConfig struct {
	EntityID           int32
	Hardcore           bool
	WorldName          string
	MaxPlayers         int32
	ViewDistance       int32
	SimulationDistance int32
	DimensionType      string
	DimensionName      string
	HashedSeed         int64
	GameMode           byte
	PreviousGameMode   int8
	ReducedDebugInfo   bool
	RespawnScreen      bool
	LimitedCrafting    bool
	Debug              bool
	Flat               bool
	PortalCooldown     int32
}

type Movement struct {
	X        float64
	Y        float64
	Z        float64
	Yaw      float32
	Pitch    float32
	OnGround bool
	HasLook  bool
}

func JoinGamePayload(config JoinGameConfig) []byte {
	payload := codec.AppendVarInt(nil, ClientboundJoinGameID)
	var raw [8]byte

	var int4 [4]byte
	binary.BigEndian.PutUint32(int4[:], uint32(config.EntityID))
	payload = append(payload, int4[:]...)
	if config.Hardcore {
		payload = append(payload, 1)
	} else {
		payload = append(payload, 0)
	}
	payload = codec.AppendVarInt(payload, 1)
	payload = codec.AppendString(payload, config.WorldName)
	payload = codec.AppendVarInt(payload, config.MaxPlayers)
	payload = codec.AppendVarInt(payload, config.ViewDistance)
	payload = codec.AppendVarInt(payload, config.SimulationDistance)
	payload = appendBool(payload, config.ReducedDebugInfo)
	payload = appendBool(payload, config.RespawnScreen)
	payload = appendBool(payload, config.LimitedCrafting)
	payload = codec.AppendString(payload, config.DimensionType)
	payload = codec.AppendString(payload, config.DimensionName)
	binary.BigEndian.PutUint64(raw[:], uint64(config.HashedSeed))
	payload = append(payload, raw[:]...)
	payload = append(payload, config.GameMode, byte(config.PreviousGameMode))
	payload = appendBool(payload, config.Debug)
	payload = appendBool(payload, config.Flat)
	payload = appendBool(payload, false) // has death location
	return codec.AppendVarInt(payload, config.PortalCooldown)
}

func KeepAlivePayload(id int64) []byte {
	payload := codec.AppendVarInt(nil, ClientboundKeepAliveID)
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(id))
	return append(payload, raw[:]...)
}

func ChunkBatchStartPayload() []byte {
	return codec.AppendVarInt(nil, ClientboundChunkBatchStartID)
}

func ChunkBatchFinishedPayload(size int32) []byte {
	payload := codec.AppendVarInt(nil, ClientboundChunkBatchFinishedID)
	return codec.AppendVarInt(payload, size)
}

func VoidChunkPayload(x, z int32) []byte {
	payload := codec.AppendVarInt(nil, ClientboundMapChunkID)
	payload = appendInt32(payload, x)
	payload = appendInt32(payload, z)
	payload = append(payload, 10, 0) // empty anonymous root compound for heightmaps

	chunkData := make([]byte, 0, overworldSectionCount*8)
	for range overworldSectionCount {
		chunkData = append(chunkData, 0, 0)    // non-air block count
		chunkData = append(chunkData, 0, 0, 0) // air single-value palette
		chunkData = append(chunkData, 0, 0, 0) // plains single-value biome palette
	}
	payload = codec.AppendVarInt(payload, int32(len(chunkData)))
	payload = append(payload, chunkData...)
	payload = codec.AppendVarInt(payload, 0) // block entities
	for range 6 {
		payload = codec.AppendVarInt(payload, 0) // light masks and light arrays
	}
	return payload
}

func SpawnPositionPayload(x, y, z int32, angle float32) []byte {
	payload := codec.AppendVarInt(nil, ClientboundSpawnPositionID)
	packed := (uint64(uint32(x)&0x3ffffff) << 38) | (uint64(uint32(z)&0x3ffffff) << 12) | uint64(uint32(y)&0xfff)
	var position [8]byte
	binary.BigEndian.PutUint64(position[:], packed)
	payload = append(payload, position[:]...)
	return appendFloat32(payload, angle)
}

func PositionLookPayload(x, y, z float64, yaw, pitch float32, teleportID int32) []byte {
	payload := codec.AppendVarInt(nil, ClientboundPositionLookID)
	payload = appendFloat64(payload, x)
	payload = appendFloat64(payload, y)
	payload = appendFloat64(payload, z)
	payload = appendFloat32(payload, yaw)
	payload = appendFloat32(payload, pitch)
	payload = append(payload, 0) // all coordinates are absolute
	return codec.AppendVarInt(payload, teleportID)
}

func StartConfigurationPayload() []byte {
	return codec.AppendVarInt(nil, ClientboundStartConfigurationID)
}

func ParseConfigurationAcknowledged(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundConfigurationAcknowledgedID || len(body) != 0 {
		return ErrMalformed
	}
	return nil
}

func ServerboundPositionPayload(x, y, z float64, onGround bool) []byte {
	payload := codec.AppendVarInt(nil, ServerboundPositionID)
	payload = appendFloat64(payload, x)
	payload = appendFloat64(payload, y)
	payload = appendFloat64(payload, z)
	return appendBool(payload, onGround)
}

func ServerboundPositionLookPayload(x, y, z float64, yaw, pitch float32, onGround bool) []byte {
	payload := codec.AppendVarInt(nil, ServerboundPositionLookID)
	payload = appendFloat64(payload, x)
	payload = appendFloat64(payload, y)
	payload = appendFloat64(payload, z)
	payload = appendFloat32(payload, yaw)
	payload = appendFloat32(payload, pitch)
	return appendBool(payload, onGround)
}

func PlayerCommandPayload(entityID, actionID, data int32) []byte {
	payload := codec.AppendVarInt(nil, ServerboundPlayerCommandID)
	payload = codec.AppendVarInt(payload, entityID)
	payload = codec.AppendVarInt(payload, actionID)
	return codec.AppendVarInt(payload, data)
}

func ParseTeleportConfirm(payload []byte) (int32, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundTeleportConfirmID {
		return 0, ErrMalformed
	}
	teleportID, used, err := codec.ConsumeVarInt(body)
	if err != nil || used != len(body) {
		return 0, ErrMalformed
	}
	return teleportID, nil
}

func ParseKeepAlive(payload []byte) (int64, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundKeepAliveID || len(body) != 8 {
		return 0, ErrMalformed
	}
	return int64(binary.BigEndian.Uint64(body)), nil
}

func ParseMovement(payload []byte) error {
	_, err := DecodeMovement(payload)
	return err
}

func DecodeMovement(payload []byte) (Movement, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || (id != ServerboundPositionID && id != ServerboundPositionLookID) {
		return Movement{}, ErrMalformed
	}
	if id == ServerboundPositionID && len(body) != 25 {
		return Movement{}, ErrMalformed
	}
	if id == ServerboundPositionLookID && len(body) != 33 {
		return Movement{}, ErrMalformed
	}
	movement := Movement{
		X:       math.Float64frombits(binary.BigEndian.Uint64(body[0:8])),
		Y:       math.Float64frombits(binary.BigEndian.Uint64(body[8:16])),
		Z:       math.Float64frombits(binary.BigEndian.Uint64(body[16:24])),
		HasLook: id == ServerboundPositionLookID,
	}
	onGroundOffset := 24
	if movement.HasLook {
		movement.Yaw = math.Float32frombits(binary.BigEndian.Uint32(body[24:28]))
		movement.Pitch = math.Float32frombits(binary.BigEndian.Uint32(body[28:32]))
		onGroundOffset = 32
	}
	if body[onGroundOffset] > 1 || !finite64(movement.X) || !finite64(movement.Y) || !finite64(movement.Z) || !finite32(movement.Yaw) || !finite32(movement.Pitch) {
		return Movement{}, ErrMalformed
	}
	movement.OnGround = body[onGroundOffset] == 1
	return movement, nil
}

func finite64(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func finite32(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func appendBool(dst []byte, value bool) []byte {
	if value {
		return append(dst, 1)
	}
	return append(dst, 0)
}

func appendInt32(dst []byte, value int32) []byte {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], uint32(value))
	return append(dst, raw[:]...)
}

func appendFloat32(dst []byte, value float32) []byte {
	return appendInt32(dst, int32(math.Float32bits(value)))
}

func appendFloat64(dst []byte, value float64) []byte {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], math.Float64bits(value))
	return append(dst, raw[:]...)
}
