package configuration

import (
	"errors"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed configuration packet")

const (
	ClientboundFinishConfigurationID = 0x02
	ServerboundFinishConfigurationID = 0x02
)

func FinishPayload() []byte {
	return codec.AppendVarInt(nil, ClientboundFinishConfigurationID)
}

func ParseFinishAcknowledged(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundFinishConfigurationID || len(body) != 0 {
		return ErrMalformed
	}
	return nil
}
