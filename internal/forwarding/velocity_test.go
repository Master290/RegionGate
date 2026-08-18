package forwarding

import (
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

func TestModernForwardingBuildResponse(t *testing.T) {
	secret := []byte("test-secret")
	forwarder, err := NewModernForwarding(secret)
	if err != nil {
		t.Fatal(err)
	}
	uid := [16]byte{1, 2, 3, 4}
	response, err := forwarder.BuildResponse(PlayerIdentity{
		Address: "203.0.113.7", UUID: uid, Username: "Daniar",
		Properties: []Property{{Name: "textures", Value: "value", Signature: "signature"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response) <= sha256.Size {
		t.Fatalf("response length=%d", len(response))
	}
	signature, payload := response[:sha256.Size], response[sha256.Size:]
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		t.Fatal("invalid response signature")
	}
	version, used, err := codec.ConsumeVarInt(payload)
	if err != nil || version != ModernVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	address, usedAddress, err := codec.ConsumeString(payload[used:], 255)
	if err != nil || address != "203.0.113.7" {
		t.Fatalf("address=%q err=%v", address, err)
	}
	uuidOffset := used + usedAddress
	if got := [16]byte(payload[uuidOffset : uuidOffset+16]); got != uid {
		t.Fatalf("uuid=%x", got)
	}
}

func TestModernForwardingRequiresSecretAndIdentity(t *testing.T) {
	if _, err := NewModernForwarding(nil); err != ErrEmptySecret {
		t.Fatalf("empty secret error=%v", err)
	}
	forwarder, err := NewModernForwarding([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := forwarder.BuildResponse(PlayerIdentity{}); err != ErrInvalidIdentity {
		t.Fatalf("invalid identity error=%v", err)
	}
}
