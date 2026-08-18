package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	events   map[string][]time.Time
	requests uint64
}

func New(limit int, window time.Duration) *Limiter {
	if limit <= 0 {
		limit = 10
	}
	if window <= 0 {
		window = 10 * time.Second
	}
	return &Limiter{limit: limit, window: window, events: make(map[string][]time.Time)}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	events := l.events[key]
	first := 0
	for first < len(events) && !events[first].After(cutoff) {
		first++
	}
	events = events[first:]
	allowed := len(events) < l.limit
	if allowed {
		events = append(events, now)
	}
	if len(events) == 0 {
		delete(l.events, key)
	} else {
		l.events[key] = events
	}
	l.requests++
	if l.requests%1024 == 0 {
		l.cleanup(cutoff)
	}
	return allowed
}

func (l *Limiter) cleanup(cutoff time.Time) {
	for key, events := range l.events {
		if len(events) == 0 || !events[len(events)-1].After(cutoff) {
			delete(l.events, key)
		}
	}
}
