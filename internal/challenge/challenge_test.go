package challenge

import (
	"context"
	"errors"
	"testing"

	"github.com/Master290/RegionGate/internal/forwarding"
)

func TestChainStopsAtRejectedChallenge(t *testing.T) {
	rejected := errors.New("rejected")
	calls := 0
	chain := Chain{
		Func(func(context.Context, forwarding.PlayerIdentity) error { calls++; return nil }),
		Func(func(context.Context, forwarding.PlayerIdentity) error { calls++; return rejected }),
		Func(func(context.Context, forwarding.PlayerIdentity) error { calls++; return nil }),
	}
	if err := chain.Verify(context.Background(), forwarding.PlayerIdentity{}); !errors.Is(err, rejected) {
		t.Fatalf("error=%v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
