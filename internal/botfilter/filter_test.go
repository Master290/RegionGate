package botfilter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/forwarding"
)

func enabledPolicy() Policy {
	p := DefaultPolicy()
	p.BotFilter.Enabled = true
	return p
}

func TestManagerObservesBurstAndDeniesViolations(t *testing.T) {
	p := enabledPolicy()
	p.BotFilter.Signals.SoftLoginBurst = 2
	p.BotFilter.Reputation.ViolationLimit = 2
	p.BotFilter.Reputation.DenyTTL = time.Minute
	m := New(p, "")
	identity := forwarding.PlayerIdentity{Address: "198.51.100.10", Username: "player"}
	if got := m.Evaluate(context.Background(), Evidence{Identity: identity, RemoteIP: identity.Address}).Verdict; got != Allow {
		t.Fatalf("first verdict=%v", got)
	}
	if got := m.Evaluate(context.Background(), Evidence{Identity: identity, RemoteIP: identity.Address}).Verdict; got != Observe {
		t.Fatalf("burst verdict=%v", got)
	}
	m.Record(Event{IP: identity.Address, At: time.Now(), Violation: true})
	m.Record(Event{IP: identity.Address, At: time.Now(), Violation: true})
	if got := m.Evaluate(context.Background(), Evidence{Identity: identity, RemoteIP: identity.Address}).Verdict; got != Deny {
		t.Fatalf("blocked verdict=%v", got)
	}
}

func TestLoadFileAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	content := []byte("version: 1\nbot_filter:\n  enabled: true\n  observation:\n    hold: 10s\n    keepalive_interval: 3s\n    required_keepalives: 2\n  reputation:\n    window: 5m\n    violation_limit: 3\n    deny_ttl: 10m\n  signals:\n    soft_login_burst: 4\n    burst_window: 10s\n    username_churn: 6\n    churn_window: 1m\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadFile(path)
	if err != nil || !policy.BotFilter.Enabled {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
}

func TestDisabledPolicyAllows(t *testing.T) {
	m := New(DefaultPolicy(), "")
	decision := m.Evaluate(context.Background(), Evidence{RemoteIP: "203.0.113.1"})
	if decision.Verdict != Allow {
		t.Fatalf("decision=%+v", decision)
	}
}
