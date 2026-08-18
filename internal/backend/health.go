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
	h.snapshot.State = state
	h.snapshot.ChangedAt = time.Now()
	h.snapshot.LastError = ""
	if err != nil {
		h.snapshot.LastError = err.Error()
	}
}
