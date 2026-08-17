package codec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

var ErrStringTooLong = errors.New("minecraft string exceeds limit")

// ReadString reads a Minecraft UTF-8 string with a byte and rune limit.
func ReadString(r io.ByteReader, maxBytes int) (string, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return "", err
	}
	if length < 0 || int64(length) > int64(maxBytes) {
		return "", ErrStringTooLong
	}

	data := make([]byte, int(length))
	for i := range data {
		data[i], err = r.ReadByte()
		if err != nil {
			return "", err
		}
	}
	if !utf8Valid(data) {
		return "", fmt.Errorf("invalid UTF-8 string")
	}
	return string(data), nil
}

func AppendString(dst []byte, value string) []byte {
	dst = AppendVarInt(dst, int32(len(value)))
	return append(dst, value...)
}

func utf8Valid(data []byte) bool {
	return bytes.Equal(data, bytes.ToValidUTF8(data, nil))
}
