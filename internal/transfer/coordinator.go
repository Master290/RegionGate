package transfer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/configuration"
	"github.com/Master290/RegionGate/internal/protocol/play"
	"github.com/Master290/RegionGate/internal/session"
	"github.com/Master290/RegionGate/internal/transport"
)

var (
	ErrTransferAlreadyFinalized = errors.New("transfer is already finalized")
	ErrTransferTimedOut         = errors.New("transfer barrier timed out")
)

type Config struct {
	MaxPendingCommands int
	BarrierTimeout     time.Duration
	Login              backend.LoginConfig
	Configuration      backend.ConfigurationConfig
}

type Coordinator struct {
	dialer    *backend.Dialer
	forwarder *forwarding.ModernForwarding
	config    Config
}

type Prepared struct {
	mu        sync.Mutex
	session   *session.Session
	backend   *transport.Transport
	packets   [][]byte
	finalized bool
	done      chan struct{}
	finalErr  error
}

func NewCoordinator(dialer *backend.Dialer, forwarder *forwarding.ModernForwarding, config Config) *Coordinator {
	return &Coordinator{dialer: dialer, forwarder: forwarder, config: config}
}

// Prepare establishes the backend through the barrier up to the point where
// the client must acknowledge its configuration transition.
func (c *Coordinator) Prepare(ctx context.Context, state *session.Session, identity forwarding.PlayerIdentity, oldKeepAlives []int64) (*Prepared, error) {
	if c.dialer == nil || c.forwarder == nil {
		return nil, errors.New("transfer coordinator is not configured")
	}
	if c.config.BarrierTimeout <= 0 {
		c.config.BarrierTimeout = 30 * time.Second
	}
	if err := state.BeginTransfer(time.Now(), oldKeepAlives, c.config.MaxPendingCommands); err != nil {
		return nil, err
	}
	rollback := func(err error, backendConn *transport.Transport) (*Prepared, error) {
		if backendConn != nil {
			_ = backendConn.Close()
		}
		_ = state.RollbackTransfer()
		return nil, err
	}
	if err := state.AdvanceBarrier(session.BarrierBackendLogin); err != nil {
		return rollback(err, nil)
	}
	backendConn, err := c.dialer.Dial(ctx, identity.Username, identity.UUID)
	if err != nil {
		return rollback(err, nil)
	}
	if _, err := backend.CompleteLogin(ctx, backendConn, c.forwarder, identity, c.config.Login); err != nil {
		return rollback(err, backendConn)
	}
	if err := state.AdvanceBarrier(session.BarrierBackendConfiguration); err != nil {
		return rollback(err, backendConn)
	}
	configuration, err := backend.CompleteConfiguration(ctx, backendConn, c.config.Configuration)
	if err != nil {
		return rollback(err, backendConn)
	}
	if err := state.AdvanceBarrier(session.BarrierAwaitingClientConfigurationStart); err != nil {
		return rollback(err, backendConn)
	}
	prepared := &Prepared{session: state, backend: backendConn, packets: configuration.Packets, done: make(chan struct{})}
	go prepared.timeout(c.config.BarrierTimeout)
	return prepared, nil
}

func (p *Prepared) timeout(duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		p.mu.Lock()
		if !p.finalized {
			p.finalized = true
			p.finalErr = ErrTransferTimedOut
			close(p.done)
			if p.backend != nil {
				_ = p.backend.Close()
				p.backend = nil
			}
			_ = p.session.RollbackTransfer()
		}
		p.mu.Unlock()
	case <-p.done:
	}
}

func (p *Prepared) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finalErr
}

func (p *Prepared) Done() <-chan struct{} { return p.done }

func (p *Prepared) ConfigurationPackets() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	packets := make([][]byte, len(p.packets))
	for i, packet := range p.packets {
		packets[i] = append([]byte(nil), packet...)
	}
	return packets
}

// BeginClientConfiguration sends the Play-state transition packet. The
// caller must feed the resulting client ACK into AcknowledgeClientStart.
func (p *Prepared) BeginClientConfiguration(client *transport.Transport) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return ErrTransferAlreadyFinalized
	}
	phase, err := p.session.BarrierPhase()
	if err != nil {
		return err
	}
	if phase != session.BarrierAwaitingClientConfigurationStart {
		return session.ErrBarrierNotReady
	}
	return client.WriteFrame(play.StartConfigurationPayload())
}

// WriteClientConfiguration writes backend packets into the existing client
// transport and finishes the client Configuration state. The client ACK must
// be consumed by the session reader before AcknowledgeClientConfiguration.
func (p *Prepared) WriteClientConfiguration(client *transport.Transport) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return ErrTransferAlreadyFinalized
	}
	phase, err := p.session.BarrierPhase()
	if err != nil {
		return err
	}
	if phase != session.BarrierClientConfiguration {
		return session.ErrBarrierNotReady
	}
	for _, packet := range p.packets {
		if err := client.WriteFrame(packet); err != nil {
			return err
		}
	}
	if err := client.WriteFrame(configuration.FinishPayload()); err != nil {
		return err
	}
	return p.session.AdvanceBarrier(session.BarrierAwaitingClientConfigurationFinish)
}

func (p *Prepared) AcknowledgeClientStart() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return ErrTransferAlreadyFinalized
	}
	return p.session.AdvanceBarrier(session.BarrierClientConfiguration)
}

func (p *Prepared) AcknowledgeClientConfiguration() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return ErrTransferAlreadyFinalized
	}
	return p.session.AdvanceBarrier(session.BarrierReady)
}

func (p *Prepared) Release() (session.Replay, *transport.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return session.Replay{}, nil, ErrTransferAlreadyFinalized
	}
	replay, err := p.session.ReleaseTransfer()
	if err != nil {
		return session.Replay{}, nil, err
	}
	p.finalized = true
	if p.done != nil {
		close(p.done)
	}
	backendConn := p.backend
	p.backend = nil
	return replay, backendConn, nil
}

func (p *Prepared) Rollback() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return ErrTransferAlreadyFinalized
	}
	p.finalized = true
	if p.done != nil {
		close(p.done)
	}
	if p.backend != nil {
		_ = p.backend.Close()
		p.backend = nil
	}
	return p.session.RollbackTransfer()
}
