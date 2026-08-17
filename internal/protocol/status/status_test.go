package status

import (
	"encoding/binary"
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestStatusPayloads(t *testing.T) {
	request := codec.AppendVarInt(nil, 0x00)
	if err := ParseRequest(request); err != nil {
		t.Fatal(err)
	}

	response, err := ResponsePayload(Response{
		Version:     Version{Name: "1.20.4", Protocol: 765},
		Players:     Players{Max: 100, Online: 3},
		Description: Description{Text: "RegionGate"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := codec.PacketID(response); err != nil || id != 0x00 {
		t.Fatalf("response packet id=%d err=%v", id, err)
	}

	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], 123)
	ping := append(codec.AppendVarInt(nil, 0x01), raw[:]...)
	value, err := ParsePing(ping)
	if err != nil || value != 123 {
		t.Fatalf("ping value=%d err=%v", value, err)
	}
	pong := PongPayload(value)
	if id, body, err := codec.PacketID(pong); err != nil || id != 0x01 || len(body) != 8 {
		t.Fatalf("pong id=%d body=%d err=%v", id, len(body), err)
	}
}
