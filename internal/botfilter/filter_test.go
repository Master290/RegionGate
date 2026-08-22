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

func TestBurstWindowExpiresIndependentlyOfReputationWindow(t *testing.T) {
	p := enabledPolicy()
	p.BotFilter.Signals.SoftLoginBurst = 2
	p.BotFilter.Signals.BurstWindow = 20 * time.Millisecond
	p.BotFilter.Reputation.Window = time.Minute
	m := New(p, "")
	identity := forwarding.PlayerIdentity{Address: "198.51.100.11", Username: "player"}

	if got := m.Evaluate(context.Background(), Evidence{Identity: identity, RemoteIP: identity.Address}).Verdict; got != Allow {
		t.Fatalf("first verdict=%v", got)
	}
	time.Sleep(30 * time.Millisecond)
	if got := m.Evaluate(context.Background(), Evidence{Identity: identity, RemoteIP: identity.Address}).Verdict; got != Allow {
		t.Fatalf("expired burst verdict=%v", got)
	}
}

func TestObservationSettingsAreExposedToServer(t *testing.T) {
	p := enabledPolicy()
	p.BotFilter.Observation.RequiredKeepAlives = 3
	p.BotFilter.Observation.KeepAliveInterval = 25 * time.Millisecond
	m := New(p, "")
	identity := forwarding.PlayerIdentity{Address: "198.51.100.12"}
	if got := m.RequiredKeepAlives(identity); got != 3 {
		t.Fatalf("required keepalives=%d", got)
	}
	if got := m.KeepAliveInterval(identity); got != 25*time.Millisecond {
		t.Fatalf("keepalive interval=%s", got)
	}
}

func TestCleanupRemovesStaleEntries(t *testing.T) {
	p := enabledPolicy()
	p.BotFilter.Signals.BurstWindow = time.Minute
	p.BotFilter.Signals.ChurnWindow = time.Minute
	p.BotFilter.Reputation.Window = time.Minute
	m := New(p, "")
	now := time.Now()
	m.entries["198.51.100.20"] = &reputation{
		BurstAttempts: []time.Time{now.Add(-2 * time.Minute)},
		Usernames:     map[string]time.Time{"old": now.Add(-2 * time.Minute)},
		Violations:    []time.Time{now.Add(-2 * time.Minute)},
	}
	m.entries["198.51.100.21"] = &reputation{
		BurstAttempts: []time.Time{now},
		Usernames:     map[string]time.Time{"active": now},
	}

	m.cleanup()
	if _, ok := m.entries["198.51.100.20"]; ok {
		t.Fatal("stale reputation entry was not removed")
	}
	if _, ok := m.entries["198.51.100.21"]; !ok {
		t.Fatal("active reputation entry was removed")
	}
}
