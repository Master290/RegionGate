package codec

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sync"
)

var (
	ErrInvalidCompressionThreshold = errors.New("invalid compression threshold")
	ErrInvalidCompressedPacket     = errors.New("invalid compressed packet")
)

var compressionWriterPool = sync.Pool{
	New: func() any {
		writer, err := zlib.NewWriterLevel(io.Discard, zlib.DefaultCompression)
		if err != nil {
			panic(err)
		}
		return writer
	},
}

var compressionReaderPool sync.Pool

type CompressionState struct {
	threshold     int
	maxPacketSize int
	wireFramer    *Framer
}

func NewCompressionState(threshold, maxPacketSize int) (*CompressionState, error) {
	if threshold < 0 {
		return nil, ErrInvalidCompressionThreshold
	}
	if maxPacketSize <= 0 {
		maxPacketSize = DefaultMaxPacketSize
	}
	wireLimit := maxPacketSize + maxPacketSize/1000 + 64 + MaxVarIntBytes
	return &CompressionState{
		threshold:     threshold,
		maxPacketSize: maxPacketSize,
		wireFramer:    NewFramer(wireLimit),
	}, nil
}

func (c *CompressionState) Threshold() int { return c.threshold }

func (c *CompressionState) ReadFrame(r *bufio.Reader) ([]byte, error) {
	wire, err := c.wireFramer.ReadFrame(r, nil)
	if err != nil {
		return nil, err
	}
	dataLength, used, err := ConsumeVarInt(wire)
	if err != nil || dataLength < 0 {
		return nil, ErrInvalidCompressedPacket
	}
	data := wire[used:]
	if dataLength == 0 {
		if len(data) == 0 || len(data) >= c.threshold {
			return nil, ErrInvalidCompressedPacket
		}
		return data, nil
	}
	if int64(dataLength) > int64(c.maxPacketSize) {
		return nil, &PacketTooLargeError{Length: int(dataLength), Limit: c.maxPacketSize}
	}
	if int(dataLength) < c.threshold || len(data) == 0 {
		return nil, ErrInvalidCompressedPacket
	}

	reader := bytes.NewReader(data)
	var zr io.ReadCloser
	if pooled := compressionReaderPool.Get(); pooled != nil {
		zr = pooled.(io.ReadCloser)
		if err := zr.(zlib.Resetter).Reset(reader, nil); err != nil {
			_ = zr.Close()
			return nil, fmt.Errorf("%w: %v", ErrInvalidCompressedPacket, err)
		}
	} else {
		var err error
		zr, err = zlib.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCompressedPacket, err)
		}
	}
	defer func() {
		_ = zr.Close()
		compressionReaderPool.Put(zr)
	}()
	payload := make([]byte, int(dataLength))
	if _, err := io.ReadFull(zr, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCompressedPacket, err)
	}
	var extra [1]byte
	if n, err := zr.Read(extra[:]); n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return nil, ErrInvalidCompressedPacket
	}
	return payload, nil
}

func (c *CompressionState) WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPacket
	}
	if len(payload) > c.maxPacketSize {
		return &PacketTooLargeError{Length: len(payload), Limit: c.maxPacketSize}
	}
	if len(payload) < c.threshold {
		wire := AppendVarInt(nil, 0)
		wire = append(wire, payload...)
		return c.wireFramer.WriteFrame(w, wire)
	}

	var compressed bytes.Buffer
	zw := compressionWriterPool.Get().(*zlib.Writer)
	zw.Reset(&compressed)
	if _, err := zw.Write(payload); err != nil {
		_ = zw.Close()
		compressionWriterPool.Put(zw)
		return err
	}
	if err := zw.Close(); err != nil {
		compressionWriterPool.Put(zw)
		return err
	}
	compressionWriterPool.Put(zw)
	wire := AppendVarInt(nil, int32(len(payload)))
	wire = append(wire, compressed.Bytes()...)
	return c.wireFramer.WriteFrame(w, wire)
}
