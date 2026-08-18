package configuration

import (
	"errors"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed configuration packet")

const (
	ClientboundPluginMessageID       = 0x00
	ClientboundDisconnectID          = 0x01
	ClientboundFinishConfigurationID = 0x02
	ServerboundFinishConfigurationID = 0x02
	ClientboundKeepAliveID           = 0x03
	ServerboundKeepAliveID           = 0x03
	ClientboundPingID                = 0x04
	ServerboundPongID                = 0x04
)

func FinishPayload() []byte {
	return codec.AppendVarInt(nil, ClientboundFinishConfigurationID)
}

func FinishAcknowledgedPayload() []byte {
	return codec.AppendVarInt(nil, ServerboundFinishConfigurationID)
}

func ParseFinishAcknowledged(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundFinishConfigurationID || len(body) != 0 {
		return ErrMalformed
	}
	return nil
}
