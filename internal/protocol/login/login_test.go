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

func TestPluginRequestAndResponse(t *testing.T) {
	request := codec.AppendVarInt(nil, ClientboundPluginRequestID)
	request = codec.AppendVarInt(request, 17)
	request = codec.AppendString(request, "velocity:player_info")
	request = append(request, 1, 2, 3)
	parsed, err := ParsePluginRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.MessageID != 17 || parsed.Channel != "velocity:player_info" || string(parsed.Data) != string([]byte{1, 2, 3}) {
		t.Fatalf("request=%+v", parsed)
	}

	response := PluginResponsePayload(parsed.MessageID, []byte{4, 5})
	id, body, err := codec.PacketID(response)
	if err != nil || id != ServerboundPluginResponseID {
		t.Fatalf("response id=%d err=%v", id, err)
	}
	messageID, used, err := codec.ConsumeVarInt(body)
	if err != nil || messageID != 17 || string(body[used:]) != string([]byte{1, 4, 5}) {
		t.Fatalf("message=%d body=%x err=%v", messageID, body[used:], err)
	}
}
