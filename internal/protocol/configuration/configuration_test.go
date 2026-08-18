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

func TestParseClientInformation(t *testing.T) {
	payload := codec.AppendVarInt(nil, ServerboundClientInformationID)
	payload = codec.AppendString(payload, "en_us")
	payload = append(payload, 12)
	payload = codec.AppendVarInt(payload, 1)
	payload = append(payload, 1, 0x7f)
	payload = codec.AppendVarInt(payload, 1)
	payload = append(payload, 0, 1)

	information, err := ParseClientInformation(payload)
	if err != nil {
		t.Fatal(err)
	}
	if information.Locale != "en_us" || information.ViewDistance != 12 || information.ChatMode != 1 || !information.ChatColors || information.MainHand != 1 || information.TextFiltering || !information.AllowServerListings {
		t.Fatalf("information=%+v", information)
	}
}

func TestParseClientInformationRejectsMalformedFields(t *testing.T) {
	payload := codec.AppendVarInt(nil, ServerboundClientInformationID)
	payload = codec.AppendString(payload, "en_us")
	payload = append(payload, 12)
	payload = codec.AppendVarInt(payload, 3)
	payload = append(payload, 1, 0x7f)
	payload = codec.AppendVarInt(payload, 1)
	payload = append(payload, 0, 1)
	if _, err := ParseClientInformation(payload); err == nil {
		t.Fatal("invalid chat mode was accepted")
	}
}

func TestParsePluginMessage(t *testing.T) {
	payload := codec.AppendVarInt(nil, ServerboundPluginMessageID)
	payload = codec.AppendString(payload, "minecraft:brand")
	payload = append(payload, 1, 2, 3)
	message, err := ParsePluginMessage(payload)
	if err != nil {
		t.Fatal(err)
	}
	if message.Channel != "minecraft:brand" || string(message.Data) != string([]byte{1, 2, 3}) {
		t.Fatalf("message=%+v", message)
	}
}
