package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/protocol/status"
	"github.com/Master290/RegionGate/internal/session"
)

type Config struct {
	MaxConnections    int
	MaxPacketSize     int
	HandshakeTimeout  time.Duration
	StatusTimeout     time.Duration
	LoginTimeout      time.Duration
	WriteTimeout      time.Duration
	KeepAliveInterval time.Duration
	KeepAliveTimeout  time.Duration
	Status            status.Response
}

type Server struct {
	config   Config
	logger   *slog.Logger
	sem      chan struct{}
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	sessions map[net.Conn]*session.Session
}

func New(config Config, logger *slog.Logger) *Server {
	if config.MaxConnections <= 0 {
		config.MaxConnections = 10000
	}
	if config.MaxPacketSize <= 0 {
		config.MaxPacketSize = codec.DefaultMaxPacketSize
	}
	if config.HandshakeTimeout <= 0 {
		config.HandshakeTimeout = 10 * time.Second
	}
	if config.StatusTimeout <= 0 {
		config.StatusTimeout = 10 * time.Second
	}
	if config.LoginTimeout <= 0 {
		config.LoginTimeout = 30 * time.Second
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.KeepAliveInterval <= 0 {
		config.KeepAliveInterval = 15 * time.Second
	}
	if config.KeepAliveTimeout <= 0 {
		config.KeepAliveTimeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{config: config, logger: logger, sem: make(chan struct{}, config.MaxConnections), conns: make(map[net.Conn]struct{}), sessions: make(map[net.Conn]*session.Session)}
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	shutdownDone := make(chan struct{})
	defer close(shutdownDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
			s.closeConnections()
		case <-shutdownDone:
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			_ = conn.Close()
			return nil
		}
		select {
		case s.sem <- struct{}{}:
			s.track(conn)
			if ctx.Err() != nil {
				_ = conn.Close()
			}
			go func() {
				defer func() { <-s.sem }()
				s.serveConn(conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer func() {
		s.untrack(conn)
		s.untrackSession(conn)
		_ = conn.Close()
	}()

	reader := bufio.NewReader(conn)
	framer := codec.NewFramer(s.config.MaxPacketSize)
	_ = conn.SetReadDeadline(time.Now().Add(s.config.HandshakeTimeout))
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
	_ = conn.SetReadDeadline(time.Now().Add(s.config.LoginTimeout))
	state := session.New()
	s.trackSession(conn, state)
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
	if err := s.writeFrame(conn, framer, login.SuccessPayload(start.Username)); err != nil {
		return
	}
	frame, err = framer.ReadFrame(reader, nil)
	if err != nil || login.ParseAcknowledged(frame) != nil {
		return
	}
	if err := state.Transition(session.StateConfiguration); err != nil {
		return
	}
	registry, err := configuration.RegistryDataPayload(configuration.MinimalRegistryData())
	if err != nil || s.writeFrame(conn, framer, registry) != nil {
		return
	}
	if err := s.writeFrame(conn, framer, configuration.FinishPayload()); err != nil {
		return
	}
	frame, err = framer.ReadFrame(reader, nil)
	if err != nil || configuration.ParseFinishAcknowledged(frame) != nil {
		return
	}
	if err := state.Transition(session.StateLimboPlay); err != nil {
		return
	}
	if err := s.serveLimbo(conn, reader, framer, start.Username); err != nil {
		return
	}
	s.logger.Debug("offline login completed", "remote", conn.RemoteAddr(), "username", start.Username)
}

func (s *Server) serveLimbo(conn net.Conn, reader *bufio.Reader, framer *codec.Framer, username string) error {
	join := play.JoinGamePayload(play.JoinGameConfig{
		EntityID:           1,
		WorldName:          "minecraft:overworld",
		MaxPlayers:         1,
		ViewDistance:       8,
		SimulationDistance: 8,
		DimensionType:      "minecraft:overworld",
		DimensionName:      "minecraft:overworld",
		GameMode:           2,
		PreviousGameMode:   -1,
		RespawnScreen:      true,
		PortalCooldown:     0,
	})
	if err := s.writeFrame(conn, framer, join); err != nil {
		return err
	}
	for _, payload := range [][]byte{
		play.ChunkBatchStartPayload(),
		play.VoidChunkPayload(0, 0),
		play.ChunkBatchFinishedPayload(1),
		play.SpawnPositionPayload(0, 64, 0, 0),
		play.PositionLookPayload(0.5, 64, 0.5, 0, 0, 1),
	} {
		if err := s.writeFrame(conn, framer, payload); err != nil {
			return err
		}
	}

	for {
		frame, err := framer.ReadFrame(reader, nil)
		if err != nil {
			return err
		}
		teleportID, err := play.ParseTeleportConfirm(frame)
		if err == nil {
			if teleportID != 1 {
				return play.ErrMalformed
			}
			break
		}
	}

	type readResult struct {
		frame []byte
		err   error
	}
	frames := make(chan readResult)
	readerDone := make(chan struct{})
	defer close(readerDone)
	_ = conn.SetReadDeadline(time.Time{})
	go func() {
		for {
			frame, err := framer.ReadFrame(reader, nil)
			select {
			case frames <- readResult{frame: frame, err: err}:
			case <-readerDone:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	keepAliveID := int64(1)
	pendingKeepAlive := keepAliveID
	if err := s.writeFrame(conn, framer, play.KeepAlivePayload(keepAliveID)); err != nil {
		return err
	}
	ticker := time.NewTicker(s.config.KeepAliveInterval)
	defer ticker.Stop()
	timeout := time.NewTimer(s.config.KeepAliveTimeout)
	defer timeout.Stop()
	var timeoutC <-chan time.Time = timeout.C

	for {
		select {
		case result := <-frames:
			if result.err != nil {
				return result.err
			}
			id, _, packetErr := codec.PacketID(result.frame)
			if packetErr != nil {
				return play.ErrMalformed
			}
			if id == play.ServerboundKeepAliveID {
				value, err := play.ParseKeepAlive(result.frame)
				if err != nil || pendingKeepAlive == 0 || value != pendingKeepAlive {
					return play.ErrMalformed
				}
				pendingKeepAlive = 0
				if !timeout.Stop() {
					select {
					case <-timeout.C:
					default:
					}
				}
				timeoutC = nil
				continue
			}
			if id == play.ServerboundPositionID || id == play.ServerboundPositionLookID {
				if err := play.ParseMovement(result.frame); err != nil {
					return err
				}
				continue
			}
			return play.ErrMalformed
		case <-ticker.C:
			if pendingKeepAlive == 0 {
				keepAliveID++
				pendingKeepAlive = keepAliveID
				if err := s.writeFrame(conn, framer, play.KeepAlivePayload(keepAliveID)); err != nil {
					return err
				}
				timeout.Reset(s.config.KeepAliveTimeout)
				timeoutC = timeout.C
			}
		case <-timeoutC:
			return fmt.Errorf("limbo keepalive %d timed out", pendingKeepAlive)
		}
	}
}

func (s *Server) serveStatus(conn net.Conn, reader *bufio.Reader, framer *codec.Framer) {
	_ = conn.SetReadDeadline(time.Now().Add(s.config.StatusTimeout))
	request, err := framer.ReadFrame(reader, nil)
	if err != nil || status.ParseRequest(request) != nil {
		return
	}
	response, err := status.ResponsePayload(s.config.Status)
	if err != nil || s.writeFrame(conn, framer, response) != nil {
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
	_ = s.writeFrame(conn, framer, status.PongPayload(value))
}

func (s *Server) writeFrame(conn net.Conn, framer *codec.Framer, payload []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
	return framer.WriteFrame(conn, payload)
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) trackSession(conn net.Conn, state *session.Session) {
	s.mu.Lock()
	s.sessions[conn] = state
	s.mu.Unlock()
}

func (s *Server) untrackSession(conn net.Conn) {
	s.mu.Lock()
	delete(s.sessions, conn)
	s.mu.Unlock()
}

// Session returns the live session for a client connection, if login has
// completed far enough to create one.
func (s *Server) Session(conn net.Conn) (*session.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[conn]
	return state, ok
}

func (s *Server) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
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
