package codec

import (
	"errors"
	"io"
)

const MaxVarIntBytes = 5

var (
	ErrVarIntTooLong = errors.New("varint exceeds 5 bytes")
	ErrVarIntPartial = errors.New("incomplete varint")
)

// ReadVarInt reads a Minecraft signed 32-bit VarInt.
func ReadVarInt(r io.ByteReader) (int32, error) {
	var value uint32
	for position := 0; position < MaxVarIntBytes; position++ {
		current, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && position > 0 {
				return 0, ErrVarIntPartial
			}
			return 0, err
		}

		value |= uint32(current&0x7f) << (7 * position)
		if current&0x80 == 0 {
			return int32(value), nil
		}
	}

	return 0, ErrVarIntTooLong
}

// ConsumeVarInt reads a VarInt from data and returns its value and encoded size.
func ConsumeVarInt(data []byte) (int32, int, error) {
	var value uint32
	for position := 0; position < MaxVarIntBytes; position++ {
		if position >= len(data) {
			return 0, 0, ErrVarIntPartial
		}

		current := data[position]
		value |= uint32(current&0x7f) << (7 * position)
		if current&0x80 == 0 {
			return int32(value), position + 1, nil
		}
	}

	return 0, 0, ErrVarIntTooLong
}

// AppendVarInt appends a Minecraft VarInt without allocating when dst has capacity.
func AppendVarInt(dst []byte, value int32) []byte {
	remaining := uint32(value)
	for {
		if remaining&^uint32(0x7f) == 0 {
			return append(dst, byte(remaining))
		}
		dst = append(dst, byte(remaining&0x7f)|0x80)
		remaining >>= 7
	}
}
