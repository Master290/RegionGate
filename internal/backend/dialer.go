package backend

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/transport"
)

type Config struct {
	Address        string
	Host           string
	Port           uint16
	MaxPacketSize  int
	ConnectTimeout time.Duration
}

type Dialer struct {
	config Config
	health healthState
}

func NewDialer(config Config) *Dialer {
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 10 * time.Second
	}
	return &Dialer{config: config}
}

// Dial creates and initializes a backend-only transport. The caller owns the
// returned connection and continues the login plugin/configuration exchange.
func (d *Dialer) Dial(ctx context.Context, username string, uid [16]byte) (*transport.Transport, error) {
	if d.config.Address == "" {
		err := fmt.Errorf("backend address is empty")
		d.health.set(HealthUnhealthy, err)
		return nil, err
	}
	connectCtx, cancel := context.WithTimeout(ctx, d.config.ConnectTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(connectCtx, "tcp", d.config.Address)
	if err != nil {
		err = fmt.Errorf("dial backend %s: %w", d.config.Address, err)
		d.health.set(HealthUnhealthy, err)
		return nil, err
	}
	t := transport.New(conn, d.config.MaxPacketSize)
	_ = t.SetWriteDeadline(time.Now().Add(d.config.ConnectTimeout))
	if err := t.WriteFrame(handshake.Payload(handshake.Packet{
		ProtocolVersion: handshake.ProtocolVersion,
		ServerAddress:   d.config.Host,
		ServerPort:      d.config.Port,
		NextState:       handshake.NextLogin,
	})); err != nil {
		_ = t.Close()
		err = fmt.Errorf("write backend handshake: %w", err)
		d.health.set(HealthUnhealthy, err)
		return nil, err
	}
	if err := t.WriteFrame(login.StartPayload(username, uid)); err != nil {
		_ = t.Close()
		err = fmt.Errorf("write backend login start: %w", err)
		d.health.set(HealthUnhealthy, err)
		return nil, err
	}
	_ = t.SetWriteDeadline(time.Time{})
	return t, nil
}

func (d *Dialer) Health() HealthSnapshot { return d.health.get() }

// MarkHealthy records that the backend completed Login and Configuration.
func (d *Dialer) MarkHealthy() { d.health.set(HealthHealthy, nil) }

// MarkUnhealthy records a backend protocol or transport failure.
func (d *Dialer) MarkUnhealthy(err error) {
	if err != nil {
		d.health.set(HealthUnhealthy, err)
	}
}
