package challenge

import (
	"context"

	"github.com/Master290/RegionGate/internal/forwarding"
)

type Hook interface {
	Verify(context.Context, forwarding.PlayerIdentity) error
}

type Func func(context.Context, forwarding.PlayerIdentity) error

func (f Func) Verify(ctx context.Context, identity forwarding.PlayerIdentity) error {
	return f(ctx, identity)
}

type Chain []Hook

func (c Chain) Verify(ctx context.Context, identity forwarding.PlayerIdentity) error {
	for _, hook := range c {
		if hook == nil {
			continue
		}
		if err := hook.Verify(ctx, identity); err != nil {
			return err
		}
	}
	return nil
}
