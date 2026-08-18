package configuration

import (
	"bytes"
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
	if body[0] != 10 || body[len(body)-1] != 0 {
		t.Fatalf("unexpected root compound framing: %x...%x", body[:1], body[len(body)-1:])
	}
}

func TestNBTSizeLimit(t *testing.T) {
	if _, err := NewCompound().String("value", "large").Encode(1); err != ErrNBTTooLarge {
		t.Fatalf("error=%v, want ErrNBTTooLarge", err)
	}
}

func TestMinimalRegistryContainsProtocol765Fields(t *testing.T) {
	payload, err := MinimalRegistryData().Encode(2 << 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"has_precipitation",
		"narration",
		"translation_key",
		"parameters",
		"sender",
		"content",
		"minecraft:damage_type",
		"minecraft:in_fire",
		"message_id",
		"scaling",
		"exhaustion",
	} {
		if !bytes.Contains(payload, []byte(field)) {
			t.Fatalf("registry does not contain %q", field)
		}
	}
	if bytes.Contains(payload, []byte("precipitation")) && !bytes.Contains(payload, []byte("has_precipitation")) {
		t.Fatal("registry contains the pre-1.20.3 biome precipitation field")
	}
}

func TestMinimalDamageTypesContainVanillaRegistry(t *testing.T) {
	damageTypes := minimalDamageTypes()
	if len(damageTypes) != 44 {
		t.Fatalf("damage type count=%d", len(damageTypes))
	}
}
