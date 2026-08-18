package transport

import (
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestTransportHasIndependentFramingState(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	client := New(left, 1024)
	server := New(right, 1024)
	payload := codec.AppendVarInt(nil, 0x12)

	done := make(chan error, 1)
	go func() { done <- client.WriteFrame(payload) }()
	got, err := server.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload=%x want=%x", got, payload)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTransportSerializesConcurrentWriters(t *testing.T) {
	left, right := net.Pipe()
	client := New(left, 1024)
	server := New(right, 1024)
	defer client.Close()
	defer server.Close()

	const count = 16
	var writers sync.WaitGroup
	writers.Add(count)
	for id := int32(0); id < count; id++ {
		go func() {
			defer writers.Done()
			if err := client.WriteFrame(codec.AppendVarInt(nil, id)); err != nil {
				t.Errorf("write frame: %v", err)
			}
		}()
	}
	seen := make(map[int32]bool, count)
	for range count {
		frame, err := server.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := codec.ConsumeVarInt(frame)
		if err != nil {
			t.Fatal(err)
		}
		seen[id] = true
	}
	writers.Wait()
	if len(seen) != count {
		t.Fatalf("received %d unique frames", len(seen))
	}
}

func TestTransportRejectsWriteAfterClose(t *testing.T) {
	left, right := net.Pipe()
	transport := New(left, 1024)
	defer right.Close()
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFrame([]byte{0}); !errors.Is(err, ErrClosed) {
		t.Fatalf("write error=%v", err)
	}
}
