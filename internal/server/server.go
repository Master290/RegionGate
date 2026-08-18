package server

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Master290/RegionGate/internal/auth"
	"github.com/Master290/RegionGate/internal/bridge"
	"github.com/Master290/RegionGate/internal/challenge"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/codec"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/protocol/status"
	admissionqueue "github.com/Master290/RegionGate/internal/queue"
	"github.com/Master290/RegionGate/internal/ratelimit"
	"github.com/Master290/RegionGate/internal/session"
	"github.com/Master290/RegionGate/internal/transfer"
	"github.com/Master290/RegionGate/internal/transport"
)

type Config struct {
	MaxConnections      int
	MaxConnectionsPerIP int
	MaxPacketSize       int
	HandshakeTimeout    time.Duration
	StatusTimeout       time.Duration
	LoginTimeout        time.Duration
	WriteTimeout        time.Duration
	KeepAliveInterval   time.Duration
	KeepAliveTimeout    time.Duration
	Status              status.Response
	TransferCoordinator *transfer.Coordinator
	AdmissionQueue      *admissionqueue.FIFO
	QueueStatusInterval time.Duration
	LoginRateLimit      int
	LoginRateWindow     time.Duration
	ChallengeHook       challenge.Hook
	ChallengeTimeout    time.Duration
	OnlineAuthenticator *auth.Authenticator
}

type Server struct {
	config        Config
	logger        *slog.Logger
	sem           chan struct{}
	mu            sync.Mutex
	conns         map[net.Conn]struct{}
	sessions      map[net.Conn]*session.Session
	clients       map[net.Conn]*transport.Transport
	admissions    map[net.Conn]chan admissionRequest
	ipConnections map[string]int
	loginLimiter  *ratelimit.Limiter
	accepted      atomic.Uint64
	rejectedCap   atomic.Uint64
	rejectedIP    atomic.Uint64
	rateLimited   atomic.Uint64
}

type MetricsSnapshot struct {
	ActiveConnections  uint64
	Sessions           uint64
	QueueLength        uint64
	Accepted           uint64
	RejectedCapacity   uint64
	RejectedPerIP      uint64
	LoginRateLimited   uint64
	BackendHealthState uint64
}

type admissionRequest struct {
	ctx    context.Context
	result chan error
}

type prepareOutcome struct {
	prepared *transfer.Prepared
	err      error
}

const maxClientConfigurationSetupPackets = 16

func New(config Config, logger *slog.Logger) *Server {
	if config.MaxConnections <= 0 {
		config.MaxConnections = 10000
	}
	if config.MaxPacketSize <= 0 {
		config.MaxPacketSize = codec.DefaultMaxPacketSize
	}
	if config.MaxConnectionsPerIP <= 0 {
		config.MaxConnectionsPerIP = 16
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
	if config.QueueStatusInterval <= 0 {
		config.QueueStatusInterval = time.Second
	}
	if config.LoginRateLimit <= 0 {
		config.LoginRateLimit = 10
	}
	if config.LoginRateWindow <= 0 {
		config.LoginRateWindow = 10 * time.Second
	}
	if config.ChallengeTimeout <= 0 {
		config.ChallengeTimeout = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{config: config, logger: logger, sem: make(chan struct{}, config.MaxConnections), conns: make(map[net.Conn]struct{}), sessions: make(map[net.Conn]*session.Session), clients: make(map[net.Conn]*transport.Transport), admissions: make(map[net.Conn]chan admissionRequest), ipConnections: make(map[string]int), loginLimiter: ratelimit.New(config.LoginRateLimit, config.LoginRateWindow)}
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
		s.accepted.Add(1)
		select {
		case s.sem <- struct{}{}:
			if !s.track(conn) {
				<-s.sem
				_ = conn.Close()
				continue
			}
			if ctx.Err() != nil {
				_ = conn.Close()
			}
			go func() {
				defer func() { <-s.sem }()
				s.serveConn(conn)
			}()
		default:
			s.rejectedCap.Add(1)
			_ = conn.Close()
		}
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer func() {
		s.untrack(conn)
		s.untrackSession(conn)
		s.untrackClient(conn)
		s.untrackAdmission(conn)
		_ = conn.Close()
	}()

	client := transport.New(conn, s.config.MaxPacketSize)
	s.trackClient(conn, client)
	defer client.Close()
	_ = conn.SetReadDeadline(time.Now().Add(s.config.HandshakeTimeout))
	frame, err := client.ReadFrame()
	if err != nil {
		return
	}
	handshakePacket, err := handshake.Parse(frame)
	if err != nil || handshakePacket.ProtocolVersion != handshake.ProtocolVersion {
		return
	}

	switch handshakePacket.NextState {
	case handshake.NextStatus:
		s.serveStatus(client)
	case handshake.NextLogin:
		if !s.loginLimiter.Allow(remoteHost(conn.RemoteAddr()), time.Now()) {
			s.rateLimited.Add(1)
			s.logger.Warn("login rate limit exceeded", "remote", conn.RemoteAddr())
			return
		}
		s.serveLogin(client)
	}
}

func (s *Server) serveLogin(client *transport.Transport) {
	conn := client.Conn()
	_ = conn.SetReadDeadline(time.Now().Add(s.config.LoginTimeout))
	state := session.New()
	s.trackSession(conn, state)
	if err := state.Transition(session.StateLogin); err != nil {
		return
	}
	frame, err := client.ReadFrame()
	if err != nil {
		return
	}
	start, err := login.ParseStart(frame)
	if err != nil {
		return
	}
	identity := forwarding.PlayerIdentity{Address: remoteHost(conn.RemoteAddr()), UUID: login.OfflineUUID(start.Username), Username: start.Username}
	if s.config.OnlineAuthenticator != nil {
		identity, err = s.authenticateOnline(client, start.Username, identity.Address)
		if err != nil {
			s.logger.Warn("online authentication failed", "remote", conn.RemoteAddr(), "username", start.Username, "error", err)
			return
		}
	}
	properties := make([]login.Property, len(identity.Properties))
	for index, property := range identity.Properties {
		properties[index] = login.Property{Name: property.Name, Value: property.Value, Signature: property.Signature}
	}
	if err := s.writeFrame(client, login.SuccessProfilePayload(identity.UUID, identity.Username, properties)); err != nil {
		return
	}
	frame, err = client.ReadFrame()
	if err != nil || login.ParseAcknowledged(frame) != nil {
		return
	}
	if err := state.Transition(session.StateConfiguration); err != nil {
		return
	}
	registry, err := configuration.RegistryDataPayload(configuration.MinimalRegistryData())
	if err != nil || s.writeFrame(client, registry) != nil {
		return
	}
	if err := s.writeFrame(client, configuration.FinishPayload()); err != nil {
		return
	}
	if err := readClientConfiguration(client); err != nil {
		return
	}
	if err := state.Transition(session.StateLimboPlay); err != nil {
		return
	}
	if err := s.serveLimbo(client, state, identity); err != nil {
		return
	}
	s.logger.Debug("login completed", "remote", conn.RemoteAddr(), "username", identity.Username, "online", s.config.OnlineAuthenticator != nil)
}

func (s *Server) authenticateOnline(client *transport.Transport, username, address string) (forwarding.PlayerIdentity, error) {
	challenge, err := s.config.OnlineAuthenticator.Begin()
	if err != nil {
		return forwarding.PlayerIdentity{}, err
	}
	if err := s.writeFrame(client, login.EncryptionRequestPayload("", challenge.PublicKey(), challenge.VerifyToken(), true)); err != nil {
		return forwarding.PlayerIdentity{}, err
	}
	frame, err := client.ReadFrame()
	if err != nil {
		return forwarding.PlayerIdentity{}, err
	}
	response, err := login.ParseEncryptionResponse(frame)
	if err != nil {
		return forwarding.PlayerIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.config.LoginTimeout)
	defer cancel()
	profile, secret, err := s.config.OnlineAuthenticator.Authenticate(ctx, challenge, username, address, response.SharedSecret, response.VerifyToken)
	if err != nil {
		return forwarding.PlayerIdentity{}, err
	}
	if err := client.EnableEncryption(secret, secret); err != nil {
		return forwarding.PlayerIdentity{}, err
	}
	properties := make([]forwarding.Property, len(profile.Properties))
	for index, property := range profile.Properties {
		properties[index] = forwarding.Property{Name: property.Name, Value: property.Value, Signature: property.Signature}
	}
	return forwarding.PlayerIdentity{Address: address, UUID: profile.UUID, Username: profile.Username, Properties: properties}, nil
}

func readClientConfiguration(client *transport.Transport) error {
	for range maxClientConfigurationSetupPackets + 1 {
		frame, err := client.ReadFrame()
		if err != nil {
			return err
		}
		id, _, err := codec.PacketID(frame)
		if err != nil {
			return configuration.ErrMalformed
		}
		if id == configuration.ServerboundFinishConfigurationID {
			return configuration.ParseFinishAcknowledged(frame)
		}
		if err := configuration.ParseClientSetup(frame); err != nil {
			return err
		}
	}
	return configuration.ErrMalformed
}

func (s *Server) serveLimbo(client *transport.Transport, state *session.Session, identity forwarding.PlayerIdentity) error {
	conn := client.Conn()
	admissions := make(chan admissionRequest, 1)
	s.trackAdmission(conn, admissions)
	queueID := hex.EncodeToString(identity.UUID[:])
	queueCtx, cancelQueue := context.WithCancel(context.Background())
	defer cancelQueue()
	defer func() {
		if s.config.AdmissionQueue != nil {
			s.config.AdmissionQueue.Cancel(queueID)
		}
	}()
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
	if err := s.writeFrame(client, join); err != nil {
		return err
	}
	for _, payload := range [][]byte{
		play.ChunkBatchStartPayload(),
		play.VoidChunkPayload(0, 0),
		play.ChunkBatchFinishedPayload(1),
		play.SpawnPositionPayload(0, 64, 0, 0),
		play.PositionLookPayload(0.5, 64, 0.5, 0, 0, 1),
	} {
		if err := s.writeFrame(client, payload); err != nil {
			return err
		}
	}

	for {
		frame, err := client.ReadFrame()
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

	frames := make(chan bridge.ClientFrame)
	readerDone := make(chan struct{})
	defer close(readerDone)
	_ = conn.SetReadDeadline(time.Time{})
	go func() {
		for {
			frame, err := client.ReadFrame()
			select {
			case frames <- bridge.ClientFrame{Payload: frame, Err: err}:
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
	if err := s.writeFrame(client, play.KeepAlivePayload(keepAliveID)); err != nil {
		return err
	}
	ticker := time.NewTicker(s.config.KeepAliveInterval)
	defer ticker.Stop()
	queueTicker := time.NewTicker(s.config.QueueStatusInterval)
	defer queueTicker.Stop()
	timeout := time.NewTimer(s.config.KeepAliveTimeout)
	defer timeout.Stop()
	var timeoutC <-chan time.Time = timeout.C
	var prepared *transfer.Prepared
	var prepareDone <-chan struct{}
	var prepareResult <-chan prepareOutcome
	var activeAdmission admissionRequest
	clientConfigurationSent := false
	queued := false
	challengeStarted := false
	var challengeResult <-chan error
	enqueue := func() error {
		if queued || s.config.AdmissionQueue == nil || s.config.TransferCoordinator == nil {
			return nil
		}
		if _, err := s.config.AdmissionQueue.Enqueue(admissionqueue.Item{
			ID: queueID, Context: queueCtx,
			Admit: func(ctx context.Context) error { return s.Admit(ctx, conn) },
		}); err != nil {
			return err
		}
		queued = true
		return nil
	}
	var progress func() error
	progress = func() error {
		if prepared == nil {
			return nil
		}
		phase, err := state.BarrierPhase()
		if err != nil {
			return err
		}
		if phase == session.BarrierClientConfiguration && !clientConfigurationSent {
			if err := prepared.WriteClientConfiguration(client); err != nil {
				return err
			}
			clientConfigurationSent = true
		}
		if phase == session.BarrierReady {
			replay, backendConn, err := prepared.Release()
			if err != nil {
				return err
			}
			for _, payload := range transfer.ReplayPayloads(replay) {
				if err := backendConn.WriteFrame(payload); err != nil {
					_ = backendConn.Close()
					return err
				}
			}
			activeAdmission.result <- nil
			err = bridge.RunPlay(context.Background(), frames, client, backendConn, bridge.Config{})
			_ = backendConn.Close()
			return err
		}
		return nil
	}

	for {
		select {
		case err := <-challengeResult:
			challengeResult = nil
			if err != nil {
				return fmt.Errorf("limbo challenge failed: %w", err)
			}
			if err := enqueue(); err != nil {
				return err
			}
		case request := <-admissions:
			if s.config.TransferCoordinator == nil || prepared != nil || state.State() != session.StateLimboPlay {
				request.result <- errors.New("session is not available for transfer")
				continue
			}
			activeAdmission = request
			oldKeepAlives := pendingIDs(pendingKeepAlive)
			prepareResult = prepareTransfer(queueCtx, request.ctx, func(ctx context.Context) (*transfer.Prepared, error) {
				return s.config.TransferCoordinator.Prepare(ctx, state, identity, oldKeepAlives)
			})
		case result := <-prepareResult:
			prepareResult = nil
			if result.err != nil {
				activeAdmission.result <- result.err
				activeAdmission = admissionRequest{}
				pendingKeepAlive = 0
				timeoutC = nil
				continue
			}
			prepared = result.prepared
			prepareDone = prepared.Done()
			clientConfigurationSent = false
			if err := prepared.BeginClientConfiguration(client); err != nil {
				_ = prepared.Rollback()
				activeAdmission.result <- err
				prepared = nil
				prepareDone = nil
				continue
			}
		case <-prepareDone:
			if prepared != nil && prepared.Err() != nil {
				activeAdmission.result <- prepared.Err()
				prepared = nil
				prepareDone = nil
				activeAdmission = admissionRequest{}
				pendingKeepAlive = 0
				timeoutC = nil
			}
		case result := <-frames:
			if result.Err != nil {
				return result.Err
			}
			id, _, packetErr := codec.PacketID(result.Payload)
			if packetErr != nil {
				return play.ErrMalformed
			}
			if state.State() == session.StateTransferBarrier {
				if err := handleBarrierFrame(state, id, result.Payload); err != nil {
					return err
				}
				if err := progress(); err != nil {
					return err
				}
				continue
			}
			if id == play.ServerboundKeepAliveID {
				value, err := play.ParseKeepAlive(result.Payload)
				if err != nil || pendingKeepAlive == 0 || value != pendingKeepAlive {
					return play.ErrMalformed
				}
				pendingKeepAlive = 0
				if !challengeStarted {
					challengeStarted = true
					if s.config.ChallengeHook == nil {
						if err := enqueue(); err != nil {
							return err
						}
					} else {
						result := make(chan error, 1)
						challengeResult = result
						go func() {
							ctx, cancel := context.WithTimeout(queueCtx, s.config.ChallengeTimeout)
							defer cancel()
							result <- s.config.ChallengeHook.Verify(ctx, identity)
						}()
					}
				}
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
				if err := play.ParseMovement(result.Payload); err != nil {
					return err
				}
				continue
			}
			return play.ErrMalformed
		case <-ticker.C:
			if state.State() == session.StateLimboPlay && pendingKeepAlive == 0 {
				keepAliveID++
				pendingKeepAlive = keepAliveID
				if err := s.writeFrame(client, play.KeepAlivePayload(keepAliveID)); err != nil {
					return err
				}
				timeout.Reset(s.config.KeepAliveTimeout)
				timeoutC = timeout.C
			}
		case <-timeoutC:
			return fmt.Errorf("limbo keepalive %d timed out", pendingKeepAlive)
		case <-queueTicker.C:
			if queued && state.State() == session.StateLimboPlay {
				if position, ok := s.config.AdmissionQueue.Position(queueID); ok {
					payload, err := play.ActionBarPayload(fmt.Sprintf("Queue position: %d", position))
					if err != nil {
						return err
					}
					if err := s.writeFrame(client, payload); err != nil {
						return err
					}
				}
			}
		}
	}
}

func prepareTransfer(sessionCtx, requestCtx context.Context, prepare func(context.Context) (*transfer.Prepared, error)) <-chan prepareOutcome {
	resultChannel := make(chan prepareOutcome)
	go func() {
		ctx, cancel := context.WithCancel(requestCtx)
		stopSessionCancel := context.AfterFunc(sessionCtx, cancel)
		defer func() {
			stopSessionCancel()
			cancel()
		}()

		prepared, err := prepare(ctx)
		select {
		case resultChannel <- prepareOutcome{prepared: prepared, err: err}:
		case <-ctx.Done():
			if prepared != nil {
				_ = prepared.Rollback()
			}
		}
	}()
	return resultChannel
}

func handleBarrierFrame(state *session.Session, id int32, frame []byte) error {
	phase, err := state.BarrierPhase()
	if err != nil {
		return err
	}
	if phase == session.BarrierAwaitingClientConfigurationStart && id == play.ServerboundConfigurationAcknowledgedID {
		if err := play.ParseConfigurationAcknowledged(frame); err != nil {
			return err
		}
		return state.AdvanceBarrier(session.BarrierClientConfiguration)
	}
	if phase == session.BarrierAwaitingClientConfigurationFinish && id == configuration.ServerboundFinishConfigurationID {
		if err := configuration.ParseFinishAcknowledged(frame); err != nil {
			return err
		}
		return state.AdvanceBarrier(session.BarrierReady)
	}
	if phase == session.BarrierAwaitingClientConfigurationFinish && (id == configuration.ServerboundClientInformationID || id == configuration.ServerboundPluginMessageID) {
		return configuration.ParseClientSetup(frame)
	}
	if id == play.ServerboundConfigurationAcknowledgedID || id == configuration.ServerboundFinishConfigurationID {
		return play.ErrMalformed
	}
	switch id {
	case play.ServerboundKeepAliveID:
		keepAliveID, err := play.ParseKeepAlive(frame)
		if err != nil {
			return err
		}
		_, err = state.HandleBarrierInput(session.Input{Kind: session.InputKeepAlive, KeepAliveID: keepAliveID})
		return err
	case play.ServerboundPositionID, play.ServerboundPositionLookID:
		movement, err := play.DecodeMovement(frame)
		if err != nil {
			return err
		}
		_, err = state.HandleBarrierInput(session.Input{Kind: session.InputMovement, HasLook: movement.HasLook, Position: session.Position{
			X: movement.X, Y: movement.Y, Z: movement.Z, Yaw: movement.Yaw, Pitch: movement.Pitch, OnGround: movement.OnGround,
		}})
		return err
	case play.ServerboundPlayerCommandID:
		command, err := play.ParsePlayerCommand(frame)
		if err != nil {
			return err
		}
		_, err = state.HandleBarrierInput(session.Input{Kind: session.InputPlayerCommand, Command: session.PlayerCommand{
			EntityID: command.EntityID, ActionID: command.ActionID, Data: command.Data,
		}})
		return err
	default:
		_, err := state.HandleBarrierInput(session.Input{Kind: session.InputUnsafe})
		return err
	}
}

func (s *Server) serveStatus(client *transport.Transport) {
	conn := client.Conn()
	_ = conn.SetReadDeadline(time.Now().Add(s.config.StatusTimeout))
	request, err := client.ReadFrame()
	if err != nil || status.ParseRequest(request) != nil {
		return
	}
	response, err := status.ResponsePayload(s.config.Status)
	if err != nil || s.writeFrame(client, response) != nil {
		return
	}

	ping, err := client.ReadFrame()
	if err != nil {
		return
	}
	value, err := status.ParsePing(ping)
	if err != nil {
		return
	}
	_ = s.writeFrame(client, status.PongPayload(value))
}

func (s *Server) writeFrame(client *transport.Transport, payload []byte) error {
	_ = client.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
	return client.WriteFrame(payload)
}

func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ip := remoteHost(conn.RemoteAddr())
	if s.ipConnections[ip] >= s.config.MaxConnectionsPerIP {
		s.rejectedIP.Add(1)
		return false
	}
	s.conns[conn] = struct{}{}
	s.ipConnections[ip]++
	return true
}

func (s *Server) trackSession(conn net.Conn, state *session.Session) {
	s.mu.Lock()
	s.sessions[conn] = state
	s.mu.Unlock()
}

func (s *Server) trackClient(conn net.Conn, client *transport.Transport) {
	s.mu.Lock()
	s.clients[conn] = client
	s.mu.Unlock()
}

func (s *Server) trackAdmission(conn net.Conn, requests chan admissionRequest) {
	s.mu.Lock()
	s.admissions[conn] = requests
	s.mu.Unlock()
}

func (s *Server) untrackAdmission(conn net.Conn) {
	s.mu.Lock()
	delete(s.admissions, conn)
	s.mu.Unlock()
}

// Admit starts the configured backend transfer for a live Limbo session and
// returns after the client has entered backend Play.
func (s *Server) Admit(ctx context.Context, conn net.Conn) error {
	s.mu.Lock()
	requests, ok := s.admissions[conn]
	s.mu.Unlock()
	if !ok {
		return errors.New("connection is not in limbo")
	}
	request := admissionRequest{ctx: ctx, result: make(chan error, 1)}
	select {
	case requests <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) untrackClient(conn net.Conn) {
	s.mu.Lock()
	delete(s.clients, conn)
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

func (s *Server) Metrics() MetricsSnapshot {
	s.mu.Lock()
	active := len(s.conns)
	sessions := len(s.sessions)
	queue := 0
	backendHealth := uint64(0)
	if s.config.AdmissionQueue != nil {
		queue = s.config.AdmissionQueue.Len()
	}
	if s.config.TransferCoordinator != nil {
		backendHealth = uint64(s.config.TransferCoordinator.BackendHealth().State)
	}
	s.mu.Unlock()
	return MetricsSnapshot{
		ActiveConnections:  uint64(active),
		Sessions:           uint64(sessions),
		QueueLength:        uint64(queue),
		Accepted:           s.accepted.Load(),
		RejectedCapacity:   s.rejectedCap.Load(),
		RejectedPerIP:      s.rejectedIP.Load(),
		LoginRateLimited:   s.rateLimited.Load(),
		BackendHealthState: backendHealth,
	}
}

// ClientTransport returns the transport that owns writes for a live client.
func (s *Server) ClientTransport(conn net.Conn) (*transport.Transport, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	client, ok := s.clients[conn]
	return client, ok
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	ip := remoteHost(conn.RemoteAddr())
	delete(s.conns, conn)
	if count := s.ipConnections[ip]; count <= 1 {
		delete(s.ipConnections, ip)
	} else {
		s.ipConnections[ip] = count - 1
	}
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

func pendingIDs(id int64) []int64 {
	if id == 0 {
		return nil
	}
	return []int64{id}
}

func remoteHost(address net.Addr) string {
	host, _, err := net.SplitHostPort(address.String())
	if err == nil {
		return host
	}
	return address.String()
}
