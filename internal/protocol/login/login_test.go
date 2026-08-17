package login

import (
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestLoginFlowPackets(t *testing.T) {
	startPayload := codec.AppendVarInt(nil, ServerboundLoginStartID)
	startPayload = codec.AppendString(startPayload, "Daniar")
	startPayload = append(startPayload, make([]byte, 16)...)
	start, err := ParseStart(startPayload)
	if err != nil || start.Username != "Daniar" {
		t.Fatalf("start=%#v err=%v", start, err)
	}

	success := SuccessPayload(start.Username)
	uid, username, err := ReadUUID(success)
	if err != nil || username != start.Username || uid != OfflineUUID(start.Username) {
		t.Fatalf("uuid=%x username=%q err=%v", uid, username, err)
	}

	ack := codec.AppendVarInt(nil, ServerboundLoginAckID)
	if err := ParseAcknowledged(ack); err != nil {
		t.Fatal(err)
	}
}

func TestLoginRejectsInvalidUsername(t *testing.T) {
	payload := codec.AppendVarInt(nil, ServerboundLoginStartID)
	payload = codec.AppendString(payload, "bad name")
	if _, err := ParseStart(payload); err == nil {
		t.Fatal("expected invalid username error")
	}
}
