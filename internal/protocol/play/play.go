package play

import (
	"encoding/binary"
	"errors"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed play packet")

const (
	ClientboundJoinGameID     = 0x29
	ClientboundKeepAliveID    = 0x24
	ClientboundPositionLookID = 0x3E
	ServerboundKeepAliveID    = 0x15
	ServerboundPositionID     = 0x17
	ServerboundPositionLookID = 0x18
)

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
	payload = append(payload, config.GameMode, byte(config.PreviousGameMode))
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

func ParseKeepAlive(payload []byte) (int64, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundKeepAliveID || len(body) != 8 {
		return 0, ErrMalformed
	}
	return int64(binary.BigEndian.Uint64(body)), nil
}

func ParseMovement(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || (id != ServerboundPositionID && id != ServerboundPositionLookID) {
		return ErrMalformed
	}
	if id == ServerboundPositionID && len(body) != 25 {
		return ErrMalformed
	}
	if id == ServerboundPositionLookID && len(body) != 33 {
		return ErrMalformed
	}
	return nil
}

func appendBool(dst []byte, value bool) []byte {
	if value {
		return append(dst, 1)
	}
	return append(dst, 0)
}
