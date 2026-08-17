package configuration

import (
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestFinishConfiguration(t *testing.T) {
	finish := FinishPayload()
	id, body, err := codec.PacketID(finish)
	if err != nil || id != ClientboundFinishConfigurationID || len(body) != 0 {
		t.Fatalf("finish id=%d body=%x err=%v", id, body, err)
	}
	if err := ParseFinishAcknowledged(codec.AppendVarInt(nil, ServerboundFinishConfigurationID)); err != nil {
		t.Fatal(err)
	}
}
