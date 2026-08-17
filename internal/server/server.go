package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/protocol/status"
	"github.com/Master290/RegionGate/internal/session"
)

type Config struct {
	MaxConnections int
	MaxPacketSize  int
	Status         status.Response
}

type Server struct {
	config Config
	logger *slog.Logger
	sem    chan struct{}
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
}

func New(config Config, logger *slog.Logger) *Server {
	if config.MaxConnections <= 0 {
		config.MaxConnections = 10000
	}
	if config.MaxPacketSize <= 0 {
		config.MaxPacketSize = codec.DefaultMaxPacketSize
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{config: config, logger: logger, sem: make(chan struct{}, config.MaxConnections), conns: make(map[net.Conn]struct{})}
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		s.closeConnections()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case s.sem <- struct{}{}:
			s.track(conn)
			go s.serveConn(conn)
		default:
			_ = conn.Close()
		}
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer func() {
		<-s.sem
		s.untrack(conn)
		_ = conn.Close()
	}()

	reader := bufio.NewReader(conn)
	framer := codec.NewFramer(s.config.MaxPacketSize)
	frame, err := framer.ReadFrame(reader, nil)
	if err != nil {
		return
	}
	handshakePacket, err := handshake.Parse(frame)
	if err != nil || handshakePacket.ProtocolVersion != handshake.ProtocolVersion {
		return
	}

	switch handshakePacket.NextState {
	case handshake.NextStatus:
		s.serveStatus(conn, reader, framer)
	case handshake.NextLogin:
		s.serveLogin(conn, reader, framer)
	}
}

func (s *Server) serveLogin(conn net.Conn, reader *bufio.Reader, framer *codec.Framer) {
	state := session.New()
	if err := state.Transition(session.StateLogin); err != nil {
		return
	}
	frame, err := framer.ReadFrame(reader, nil)
	if err != nil {
		return
	}
	start, err := login.ParseStart(frame)
	if err != nil {
		return
	}
	if err := framer.WriteFrame(conn, login.SuccessPayload(start.Username)); err != nil {
		return
	}
	frame, err = framer.ReadFrame(reader, nil)
	if err != nil || login.ParseAcknowledged(frame) != nil {
		return
	}
	if err := state.Transition(session.StateConfiguration); err != nil {
		return
	}
	if err := framer.WriteFrame(conn, configuration.FinishPayload()); err != nil {
		return
	}
	frame, err = framer.ReadFrame(reader, nil)
	if err != nil || configuration.ParseFinishAcknowledged(frame) != nil {
		return
	}
	if err := state.Transition(session.StateLimboPlay); err != nil {
		return
	}
	s.logger.Debug("offline login completed", "remote", conn.RemoteAddr(), "username", start.Username)
}

func (s *Server) serveStatus(conn net.Conn, reader *bufio.Reader, framer *codec.Framer) {
	request, err := framer.ReadFrame(reader, nil)
	if err != nil || status.ParseRequest(request) != nil {
		return
	}
	response, err := status.ResponsePayload(s.config.Status)
	if err != nil || framer.WriteFrame(conn, response) != nil {
		return
	}

	ping, err := framer.ReadFrame(reader, nil)
	if err != nil {
		return
	}
	value, err := status.ParsePing(ping)
	if err != nil {
		return
	}
	_ = framer.WriteFrame(conn, status.PongPayload(value))
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *Server) closeConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		_ = conn.Close()
	}
}

func ListenAndServe(ctx context.Context, address string, config Config, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", address, err)
	}
	defer listener.Close()
	return New(config, logger).Serve(ctx, listener)
}
