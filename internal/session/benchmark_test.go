package session

import (
	"testing"
	"time"
)

func BenchmarkBarrierMovementCoalescing(b *testing.B) {
	s := New()
	for _, state := range []State{StateLogin, StateConfiguration, StateLimboPlay} {
		if err := s.Transition(state); err != nil {
			b.Fatal(err)
		}
	}
	if err := s.BeginTransfer(time.Now(), nil, 8); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		_, err := s.HandleBarrierInput(Input{
			Kind:     InputMovement,
			HasLook:  true,
			Position: Position{X: float64(index), Y: 64, Z: -float64(index), Yaw: float32(index), OnGround: true},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
