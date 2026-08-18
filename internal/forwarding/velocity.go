package forwarding

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

const (
	VelocityChannel = "velocity:player_info"
	ModernVersion   = 1
)

var (
	ErrEmptySecret     = errors.New("velocity forwarding secret is empty")
	ErrInvalidIdentity = errors.New("invalid player identity")
)

type Property struct {
	Name      string
	Value     string
	Signature string
}

type PlayerIdentity struct {
	Address    string
	UUID       [16]byte
	Username   string
	Properties []Property
}

type ModernForwarding struct {
	secret []byte
}

func NewModernForwarding(secret []byte) (*ModernForwarding, error) {
	if len(secret) == 0 {
		return nil, ErrEmptySecret
	}
	return &ModernForwarding{secret: append([]byte(nil), secret...)}, nil
}

// BuildResponse returns the signed velocity:player_info response body. The
// HMAC covers the complete forwarding payload and is prepended to it.
func (f *ModernForwarding) BuildResponse(identity PlayerIdentity) ([]byte, error) {
	if identity.Address == "" || identity.Username == "" {
		return nil, ErrInvalidIdentity
	}
	payload := codec.AppendVarInt(nil, ModernVersion)
	payload = codec.AppendString(payload, identity.Address)
	payload = append(payload, identity.UUID[:]...)
	payload = codec.AppendString(payload, identity.Username)
	payload = codec.AppendVarInt(payload, int32(len(identity.Properties)))
	for _, property := range identity.Properties {
		payload = codec.AppendString(payload, property.Name)
		payload = codec.AppendString(payload, property.Value)
		if property.Signature == "" {
			payload = append(payload, 0)
			continue
		}
		payload = append(payload, 1)
		payload = codec.AppendString(payload, property.Signature)
	}

	mac := hmac.New(sha256.New, f.secret)
	_, _ = mac.Write(payload)
	return append(mac.Sum(nil), payload...), nil
}
