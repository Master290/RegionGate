package transport

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

// Transport owns one TCP connection and its framing state. Client and backend
// connections must each have their own Transport instance.
var (
	ErrClosed                       = errors.New("transport is closed")
	ErrBufferedEncryptionTransition = errors.New("cannot enable encryption with buffered input")
)

type writeRequest struct {
	payload []byte
	result  chan error
}

type Transport struct {
	conn          net.Conn
	reader        *bufio.Reader
	framer        *codec.Framer
	maxPacketSize int
	compression   atomic.Pointer[codec.CompressionState]
	encryption    atomic.Pointer[codec.CipherState]
	wireWriter    io.Writer
	writes        chan writeRequest
	done          chan struct{}
	closeOnce     sync.Once
}

func New(conn net.Conn, maxPacketSize int) *Transport {
	t := &Transport{
		conn:          conn,
		framer:        codec.NewFramer(maxPacketSize),
		maxPacketSize: maxPacketSize,
		writes:        make(chan writeRequest),
		done:          make(chan struct{}),
	}
	t.reader = bufio.NewReader(&cipherReader{conn: conn, state: &t.encryption})
	t.wireWriter = &cipherWriter{conn: conn, state: &t.encryption}
	go t.writerLoop()
	return t
}

func (t *Transport) Conn() net.Conn { return t.conn }

func (t *Transport) ReadFrame() ([]byte, error) {
	if compression := t.compression.Load(); compression != nil {
		return compression.ReadFrame(t.reader)
	}
	return t.framer.ReadFrame(t.reader, nil)
}

func (t *Transport) EnableCompression(threshold int) error {
	compression, err := codec.NewCompressionState(threshold, t.maxPacketSize)
	if err != nil {
		return err
	}
	t.compression.Store(compression)
	return nil
}

func (t *Transport) CompressionThreshold() (int, bool) {
	compression := t.compression.Load()
	if compression == nil {
		return 0, false
	}
	return compression.Threshold(), true
}

func (t *Transport) EnableEncryption(key, iv []byte) error {
	if t.reader.Buffered() != 0 {
		return ErrBufferedEncryptionTransition
	}
	state, err := codec.NewCipherState(key, iv)
	if err != nil {
		return err
	}
	t.encryption.Store(state)
	return nil
}

func (t *Transport) EncryptionEnabled() bool { return t.encryption.Load() != nil }

func (t *Transport) WriteFrame(payload []byte) error {
	request := writeRequest{payload: payload, result: make(chan error, 1)}
	select {
	case t.writes <- request:
	case <-t.done:
		return ErrClosed
	}
	select {
	case err := <-request.result:
		return err
	case <-t.done:
		return ErrClosed
	}
}

func (t *Transport) SetReadDeadline(deadline time.Time) error {
	return t.conn.SetReadDeadline(deadline)
}

func (t *Transport) SetWriteDeadline(deadline time.Time) error {
	return t.conn.SetWriteDeadline(deadline)
}

func (t *Transport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		close(t.done)
		err = t.conn.Close()
	})
	return err
}

func (t *Transport) writerLoop() {
	for {
		select {
		case request := <-t.writes:
			if compression := t.compression.Load(); compression != nil {
				request.result <- compression.WriteFrame(t.wireWriter, request.payload)
			} else {
				request.result <- t.framer.WriteFrame(t.wireWriter, request.payload)
			}
		case <-t.done:
			return
		}
	}
}

type cipherReader struct {
	conn  net.Conn
	state *atomic.Pointer[codec.CipherState]
}

func (r *cipherReader) Read(data []byte) (int, error) {
	count, err := r.conn.Read(data)
	if count > 0 {
		if state := r.state.Load(); state != nil {
			state.Decrypt(data[:count], data[:count])
		}
	}
	return count, err
}

type cipherWriter struct {
	conn  net.Conn
	state *atomic.Pointer[codec.CipherState]
}

func (w *cipherWriter) Write(data []byte) (int, error) {
	state := w.state.Load()
	if state == nil {
		return w.conn.Write(data)
	}
	encrypted := make([]byte, len(data))
	state.Encrypt(encrypted, data)
	written := 0
	for written < len(encrypted) {
		count, err := w.conn.Write(encrypted[written:])
		written += count
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrShortWrite
		}
	}
	return len(data), nil
}
