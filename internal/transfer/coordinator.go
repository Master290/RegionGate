package transfer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/session"
	"github.com/Master290/RegionGate/internal/transport"
)

var (
	ErrTransferAlreadyFinalized = errors.New("transfer is already finalized")
)

type Config struct {
	MaxPendingCommands int
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
	if err := state.AdvanceBarrier(session.BarrierAwaitingClientConfiguration); err != nil {
		return rollback(err, backendConn)
	}
	return &Prepared{session: state, backend: backendConn, packets: configuration.Packets}, nil
}

func (p *Prepared) ConfigurationPackets() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	packets := make([][]byte, len(p.packets))
	for i, packet := range p.packets {
		packets[i] = append([]byte(nil), packet...)
	}
	return packets
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
	if p.backend != nil {
		_ = p.backend.Close()
		p.backend = nil
	}
	return p.session.RollbackTransfer()
}
