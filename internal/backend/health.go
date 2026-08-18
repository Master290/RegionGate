package backend

import (
	"sync"
	"time"
)

type HealthState uint8

const (
	HealthUnknown HealthState = iota
	HealthHealthy
	HealthUnhealthy
)

type HealthSnapshot struct {
	State     HealthState
	ChangedAt time.Time
	LastError string
}

type healthState struct {
	mu       sync.RWMutex
	snapshot HealthSnapshot
}

func (h *healthState) get() HealthSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.snapshot
}

func (h *healthState) set(state HealthState, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lastError := ""
	if err != nil {
		lastError = err.Error()
	}
	if h.snapshot.State == state && h.snapshot.LastError == lastError {
		return
	}
	h.snapshot = HealthSnapshot{State: state, ChangedAt: time.Now(), LastError: lastError}
}
