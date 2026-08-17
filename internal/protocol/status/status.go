package status

import (
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed status packet")

type Response struct {
	Version     Version     `json:"version"`
	Players     Players     `json:"players"`
	Description Description `json:"description"`
}

type Version struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

type Players struct {
	Max    int `json:"max"`
	Online int `json:"online"`
}

type Description struct {
	Text string `json:"text"`
}

func ParseRequest(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != 0x00 || len(body) != 0 {
		return ErrMalformed
	}
	return nil
}

func ParsePing(payload []byte) (int64, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != 0x01 || len(body) != 8 {
		return 0, ErrMalformed
	}
	return int64(binary.BigEndian.Uint64(body)), nil
}

func ResponsePayload(response Response) ([]byte, error) {
	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	payload := codec.AppendVarInt(nil, 0x00)
	return codec.AppendString(payload, string(data)), nil
}

func PongPayload(value int64) []byte {
	payload := codec.AppendVarInt(nil, 0x01)
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(value))
	return append(payload, raw[:]...)
}
