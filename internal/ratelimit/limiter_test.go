package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterUsesIndependentSlidingWindows(t *testing.T) {
	limiter := New(2, time.Second)
	now := time.Unix(10, 0)
	if !limiter.Allow("a", now) || !limiter.Allow("a", now.Add(time.Millisecond)) {
		t.Fatal("allowed requests were rejected")
	}
	if limiter.Allow("a", now.Add(2*time.Millisecond)) {
		t.Fatal("rate limit was not enforced")
	}
	if !limiter.Allow("b", now.Add(2*time.Millisecond)) {
		t.Fatal("independent key was rejected")
	}
	if !limiter.Allow("a", now.Add(time.Second+time.Millisecond)) {
		t.Fatal("expired request was not removed")
	}
}
