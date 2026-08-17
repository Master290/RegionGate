package codec

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

const DefaultMaxPacketSize = 2 << 20

var (
	ErrEmptyPacket    = errors.New("empty packet")
	ErrNegativeLength = errors.New("negative packet length")
)

type PacketTooLargeError struct {
	Length int
	Limit  int
}

func (e *PacketTooLargeError) Error() string {
	return fmt.Sprintf("packet length %d exceeds limit %d", e.Length, e.Limit)
}

type Framer struct {
	maxPacketSize int
}

func NewFramer(maxPacketSize int) *Framer {
	if maxPacketSize <= 0 {
		maxPacketSize = DefaultMaxPacketSize
	}
	return &Framer{maxPacketSize: maxPacketSize}
}

// ReadFrame reads one length-prefixed Minecraft packet. dst may be reused.
func (f *Framer) ReadFrame(r *bufio.Reader, dst []byte) ([]byte, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, ErrNegativeLength
	}
	if length == 0 {
		return nil, ErrEmptyPacket
	}
	if int64(length) > int64(f.maxPacketSize) {
		return nil, &PacketTooLargeError{Length: int(length), Limit: f.maxPacketSize}
	}

	if cap(dst) < int(length) {
		dst = make([]byte, int(length))
	} else {
		dst = dst[:int(length)]
	}
	if _, err := io.ReadFull(r, dst); err != nil {
		return nil, err
	}
	return dst, nil
}

func (f *Framer) WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPacket
	}
	if len(payload) > f.maxPacketSize {
		return &PacketTooLargeError{Length: len(payload), Limit: f.maxPacketSize}
	}

	var prefix [MaxVarIntBytes]byte
	header := AppendVarInt(prefix[:0], int32(len(payload)))
	if err := writeAll(w, header); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func PacketID(payload []byte) (id int32, body []byte, err error) {
	id, size, err := ConsumeVarInt(payload)
	if err != nil {
		return 0, nil, err
	}
	return id, payload[size:], nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
