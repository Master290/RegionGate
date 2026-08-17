package handshake

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

const ProtocolVersion = 765

var ErrMalformed = errors.New("malformed handshake packet")

type NextState uint32

const (
	NextStatus NextState = 1
	NextLogin  NextState = 2
)

type Packet struct {
	ProtocolVersion int32
	ServerAddress   string
	ServerPort      uint16
	NextState       NextState
}

func Parse(payload []byte) (Packet, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != 0x00 {
		return Packet{}, ErrMalformed
	}
	version, used, err := codec.ConsumeVarInt(body)
	if err != nil {
		return Packet{}, ErrMalformed
	}
	body = body[used:]

	address, used, err := readString(body, 255)
	if err != nil {
		return Packet{}, ErrMalformed
	}
	body = body[used:]
	if len(body) < 2 {
		return Packet{}, ErrMalformed
	}
	port := binary.BigEndian.Uint16(body)
	body = body[2:]
	next, used, err := codec.ConsumeVarInt(body)
	if err != nil || len(body[used:]) != 0 || (next != int32(NextStatus) && next != int32(NextLogin)) {
		return Packet{}, ErrMalformed
	}
	return Packet{ProtocolVersion: version, ServerAddress: address, ServerPort: port, NextState: NextState(next)}, nil
}

func readString(data []byte, maxBytes int) (string, int, error) {
	length, used, err := codec.ConsumeVarInt(data)
	if err != nil || length < 0 || int64(length) > int64(maxBytes) {
		return "", 0, ErrMalformed
	}
	start := used
	end := start + int(length)
	if end > len(data) {
		return "", 0, ErrMalformed
	}
	return string(data[start:end]), end, nil
}

func ValidateVersion(version int32) error {
	if version != ProtocolVersion {
		return fmt.Errorf("unsupported Minecraft protocol version %d", version)
	}
	return nil
}
