package handshake

import (
	"encoding/binary"
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestParseHandshake(t *testing.T) {
	payload := codec.AppendVarInt(nil, 0x00)
	payload = codec.AppendVarInt(payload, ProtocolVersion)
	payload = codec.AppendString(payload, "localhost")
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], 25565)
	payload = append(payload, port[:]...)
	payload = codec.AppendVarInt(payload, int32(NextStatus))

	packet, err := Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if packet.ProtocolVersion != ProtocolVersion || packet.ServerAddress != "localhost" || packet.ServerPort != 25565 || packet.NextState != NextStatus {
		t.Fatalf("unexpected handshake: %#v", packet)
	}
}

func TestParseHandshakeRejectsTrailingData(t *testing.T) {
	payload := codec.AppendVarInt(nil, 0x00)
	payload = append(payload, codec.AppendVarInt(nil, ProtocolVersion)...)
	payload = append(payload, 0)
	if _, err := Parse(payload); err == nil {
		t.Fatal("expected malformed handshake error")
	}
}
