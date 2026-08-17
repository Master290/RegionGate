package configuration

import (
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestRegistryDataPayload(t *testing.T) {
	payload, err := RegistryDataPayload(MinimalRegistryData())
	if err != nil {
		t.Fatal(err)
	}
	id, body, err := codec.PacketID(payload)
	if err != nil || id != 0x05 || len(body) < 3 {
		t.Fatalf("id=%d body=%d err=%v", id, len(body), err)
	}
	if body[0] != 10 || body[1] != 0 || body[2] != 0 {
		t.Fatalf("unexpected root compound header: %x", body[:3])
	}
}

func TestNBTSizeLimit(t *testing.T) {
	if _, err := NewCompound().String("value", "large").Encode(1); err != ErrNBTTooLarge {
		t.Fatalf("error=%v, want ErrNBTTooLarge", err)
	}
}
