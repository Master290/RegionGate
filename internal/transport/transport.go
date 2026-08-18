package transport

import (
	"bufio"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

// Transport owns one TCP connection and its framing state. Client and backend
// connections must each have their own Transport instance.
var ErrClosed = errors.New("transport is closed")

type writeRequest struct {
	payload []byte
	result  chan error
}

type Transport struct {
	conn      net.Conn
	reader    *bufio.Reader
	framer    *codec.Framer
	writes    chan writeRequest
	done      chan struct{}
	closeOnce sync.Once
}

func New(conn net.Conn, maxPacketSize int) *Transport {
	t := &Transport{
		conn:   conn,
		reader: bufio.NewReader(conn),
		framer: codec.NewFramer(maxPacketSize),
		writes: make(chan writeRequest),
		done:   make(chan struct{}),
	}
	go t.writerLoop()
	return t
}

func (t *Transport) Conn() net.Conn { return t.conn }

func (t *Transport) ReadFrame() ([]byte, error) {
	return t.framer.ReadFrame(t.reader, nil)
}

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
			request.result <- t.framer.WriteFrame(t.conn, request.payload)
		case <-t.done:
			return
		}
	}
}
