package botfilter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Master290/RegionGate/internal/forwarding"
	"gopkg.in/yaml.v3"
)

type Verdict uint8

const (
	Allow Verdict = iota
	Observe
	Deny
)

type Evidence struct {
	Identity          forwarding.PlayerIdentity
	OnlineMode        bool
	RemoteIP          string
	ActiveFromIP      int
	RecentAttempts    int
	UsernameChurn     int
	ValidKeepAlives   int
	ProtocolViolation bool
}

type Decision struct {
	Verdict    Verdict
	ObserveFor time.Duration
	RuleID     string
}

type Event struct {
	IP        string
	Username  string
	At        time.Time
	Violation bool
}

type Policy struct {
	Version   int          `yaml:"version"`
	BotFilter FilterPolicy `yaml:"bot_filter"`
}

type FilterPolicy struct {
	Enabled     bool        `yaml:"enabled"`
	Observation Observation `yaml:"observation"`
	Reputation  Reputation  `yaml:"reputation"`
	Signals     Signals     `yaml:"signals"`
}

type Observation struct {
	Hold               time.Duration `yaml:"hold"`
	KeepAliveInterval  time.Duration `yaml:"keepalive_interval"`
	RequiredKeepAlives int           `yaml:"required_keepalives"`
}

type Reputation struct {
	Window         time.Duration `yaml:"window"`
	ViolationLimit int           `yaml:"violation_limit"`
	DenyTTL        time.Duration `yaml:"deny_ttl"`
}

type Signals struct {
	SoftLoginBurst int           `yaml:"soft_login_burst"`
	BurstWindow    time.Duration `yaml:"burst_window"`
	UsernameChurn  int           `yaml:"username_churn"`
	ChurnWindow    time.Duration `yaml:"churn_window"`
}

func DefaultPolicy() Policy {
	return Policy{Version: 1, BotFilter: FilterPolicy{
		Observation: Observation{Hold: 10 * time.Second, KeepAliveInterval: 3 * time.Second, RequiredKeepAlives: 2},
		Reputation:  Reputation{Window: 5 * time.Minute, ViolationLimit: 3, DenyTTL: 10 * time.Minute},
		Signals:     Signals{SoftLoginBurst: 4, BurstWindow: 10 * time.Second, UsernameChurn: 6, ChurnWindow: time.Minute},
	}}
}

func LoadFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	policy := DefaultPolicy()
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return Policy{}, fmt.Errorf("parse bot filter policy: %w", err)
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (p Policy) Validate() error {
	if p.Version != 0 && p.Version != 1 {
		return fmt.Errorf("unsupported bot filter policy version %d", p.Version)
	}
	if !p.BotFilter.Enabled {
		return nil
	}
	if p.BotFilter.Observation.Hold <= 0 || p.BotFilter.Observation.KeepAliveInterval <= 0 || p.BotFilter.Observation.RequiredKeepAlives <= 0 {
		return errors.New("bot_filter observation values must be positive")
	}
	if p.BotFilter.Reputation.Window <= 0 || p.BotFilter.Reputation.ViolationLimit <= 0 || p.BotFilter.Reputation.DenyTTL <= 0 {
		return errors.New("bot_filter reputation values must be positive")
	}
	if p.BotFilter.Signals.SoftLoginBurst <= 0 || p.BotFilter.Signals.BurstWindow <= 0 || p.BotFilter.Signals.UsernameChurn <= 0 || p.BotFilter.Signals.ChurnWindow <= 0 {
		return errors.New("bot_filter signal values must be positive")
	}
	return nil
}

type reputation struct {
	Attempts     []time.Time
	Usernames    map[string]time.Time
	Violations   []time.Time
	BlockedUntil time.Time
}

type Metrics struct {
	Allowed            uint64
	Observed           uint64
	Denied             uint64
	ActiveObservations uint64
	ReputationEntries  uint64
	ReloadFailures     uint64
}

type Manager struct {
	mu       sync.Mutex
	policy   Policy
	entries  map[string]*reputation
	path     string
	modTime  time.Time
	allowed  atomic.Uint64
	observed atomic.Uint64
	denied   atomic.Uint64
	reloads  atomic.Uint64
}

func New(policy Policy, path string) *Manager {
	return &Manager{policy: policy, path: path, entries: make(map[string]*reputation)}
}

func (m *Manager) Start(ctx context.Context) {
	if m.path == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reload()
			}
		}
	}()
}

func (m *Manager) reload() {
	info, err := os.Stat(m.path)
	if err != nil {
		m.reloads.Add(1)
		return
	}
	m.mu.Lock()
	unchanged := !info.ModTime().After(m.modTime)
	m.mu.Unlock()
	if unchanged {
		return
	}
	policy, err := LoadFile(m.path)
	if err != nil {
		m.reloads.Add(1)
		return
	}
	m.mu.Lock()
	m.policy = policy
	m.modTime = info.ModTime()
	m.mu.Unlock()
}

func (m *Manager) Evaluate(_ context.Context, evidence Evidence) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()
	policy := m.policy.BotFilter
	if !policy.Enabled {
		m.allowed.Add(1)
		return Decision{Verdict: Allow, RuleID: "disabled"}
	}
	now := time.Now()
	entry := m.entries[evidence.RemoteIP]
	if entry == nil {
		entry = &reputation{Usernames: make(map[string]time.Time)}
		m.entries[evidence.RemoteIP] = entry
	}
	m.prune(entry, now, policy)
	if entry.BlockedUntil.After(now) {
		m.denied.Add(1)
		return Decision{Verdict: Deny, RuleID: "reputation_deny"}
	}
	if evidence.ProtocolViolation {
		m.recordViolation(entry, now, policy)
		m.denied.Add(1)
		return Decision{Verdict: Deny, RuleID: "protocol_violation"}
	}
	entry.Attempts = append(entry.Attempts, now)
	if evidence.Identity.Username != "" {
		entry.Usernames[evidence.Identity.Username] = now
	}
	if len(entry.Attempts) >= policy.Signals.SoftLoginBurst || len(entry.Usernames) >= policy.Signals.UsernameChurn {
		m.observed.Add(1)
		return Decision{Verdict: Observe, ObserveFor: policy.Observation.Hold, RuleID: "soft_burst"}
	}
	m.allowed.Add(1)
	return Decision{Verdict: Allow, RuleID: "allow"}
}

func (m *Manager) Record(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[event.IP]
	if entry == nil {
		entry = &reputation{Usernames: make(map[string]time.Time)}
		m.entries[event.IP] = entry
	}
	if event.Violation {
		m.recordViolation(entry, event.At, m.policy.BotFilter)
	}
}

func (m *Manager) recordViolation(entry *reputation, now time.Time, policy FilterPolicy) {
	entry.Violations = append(entry.Violations, now)
	if len(entry.Violations) >= policy.Reputation.ViolationLimit {
		entry.BlockedUntil = now.Add(policy.Reputation.DenyTTL)
	}
}

func (m *Manager) prune(entry *reputation, now time.Time, policy FilterPolicy) {
	cutoff := now.Add(-policy.Reputation.Window)
	keepTimes := func(values []time.Time) []time.Time {
		index := 0
		for index < len(values) && values[index].Before(cutoff) {
			index++
		}
		return values[index:]
	}
	entry.Attempts = keepTimes(entry.Attempts)
	entry.Violations = keepTimes(entry.Violations)
	for username, at := range entry.Usernames {
		if at.Before(now.Add(-policy.Signals.ChurnWindow)) {
			delete(entry.Usernames, username)
		}
	}
}

func (m *Manager) Metrics() Metrics {
	m.mu.Lock()
	entries := uint64(len(m.entries))
	active := uint64(0)
	now := time.Now()
	for _, entry := range m.entries {
		if entry.BlockedUntil.After(now) {
			active++
		}
	}
	m.mu.Unlock()
	return Metrics{Allowed: m.allowed.Load(), Observed: m.observed.Load(), Denied: m.denied.Load(), ActiveObservations: active, ReputationEntries: entries, ReloadFailures: m.reloads.Load()}
}

func (m *Manager) Verify(ctx context.Context, identity forwarding.PlayerIdentity) error {
	decision := m.Evaluate(ctx, Evidence{Identity: identity, RemoteIP: identity.Address})
	if decision.Verdict == Deny {
		return errors.New("connection verification failed")
	}
	if decision.Verdict == Observe {
		timer := time.NewTimer(decision.ObserveFor)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}
