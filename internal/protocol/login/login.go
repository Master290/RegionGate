package login

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"unicode/utf8"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed login packet")

const (
	ServerboundLoginStartID     = 0x00
	ServerboundPluginResponseID = 0x02
	ServerboundLoginAckID       = 0x03
	ClientboundLoginSuccessID   = 0x02
	ClientboundPluginRequestID  = 0x04
)

type Start struct {
	Username string
	UUID     [16]byte
}

type PluginRequest struct {
	MessageID int32
	Channel   string
	Data      []byte
}

func ParsePluginRequest(payload []byte) (PluginRequest, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundPluginRequestID {
		return PluginRequest{}, ErrMalformed
	}
	messageID, used, err := codec.ConsumeVarInt(body)
	if err != nil || messageID < 0 {
		return PluginRequest{}, ErrMalformed
	}
	body = body[used:]
	channel, used, err := codec.ConsumeString(body, 32767)
	if err != nil {
		return PluginRequest{}, ErrMalformed
	}
	return PluginRequest{MessageID: messageID, Channel: channel, Data: append([]byte(nil), body[used:]...)}, nil
}

func PluginResponsePayload(messageID int32, data []byte) []byte {
	payload := codec.AppendVarInt(nil, ServerboundPluginResponseID)
	payload = codec.AppendVarInt(payload, messageID)
	if data == nil {
		return append(payload, 0)
	}
	payload = append(payload, 1)
	return append(payload, data...)
}

func StartPayload(username string, uid [16]byte) []byte {
	payload := codec.AppendVarInt(nil, ServerboundLoginStartID)
	payload = codec.AppendString(payload, username)
	return append(payload, uid[:]...)
}

func ParseStart(payload []byte) (Start, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundLoginStartID {
		return Start{}, ErrMalformed
	}
	username, used, err := readString(body, 16)
	if err != nil || len(body) != used+16 {
		return Start{}, ErrMalformed
	}
	if !validUsername(username) {
		return Start{}, ErrMalformed
	}
	var uid [16]byte
	copy(uid[:], body[used:])
	return Start{Username: username, UUID: uid}, nil
}

func validUsername(username string) bool {
	if len(username) == 0 || len(username) > 16 {
		return false
	}
	for _, char := range []byte(username) {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func ParseAcknowledged(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundLoginAckID || len(body) != 0 {
		return ErrMalformed
	}
	return nil
}

func SuccessPayload(username string) []byte {
	payload := codec.AppendVarInt(nil, ClientboundLoginSuccessID)
	uid := OfflineUUID(username)
	payload = append(payload, uid[:]...)
	payload = codec.AppendString(payload, username)
	return append(payload, 0)
}

// OfflineUUID follows java.util.UUID.nameUUIDFromBytes for OfflinePlayer names.
func OfflineUUID(username string) [16]byte {
	digest := md5.Sum([]byte("OfflinePlayer:" + username))
	digest[6] = (digest[6] & 0x0f) | 0x30
	digest[8] = (digest[8] & 0x3f) | 0x80
	return digest
}

func readString(data []byte, maxChars int) (string, int, error) {
	length, used, err := codec.ConsumeVarInt(data)
	if err != nil || length < 0 || int64(length) > int64(maxChars)*3 {
		return "", 0, ErrMalformed
	}
	start := used
	end := start + int(length)
	if end > len(data) {
		return "", 0, ErrMalformed
	}
	value := data[start:end]
	if !utf8.Valid(value) || utf8.RuneCount(value) > maxChars {
		return "", 0, ErrMalformed
	}
	return string(value), end, nil
}

func ReadUUID(payload []byte) ([16]byte, string, error) {
	var uid [16]byte
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundLoginSuccessID || len(body) < 16 {
		return uid, "", ErrMalformed
	}
	copy(uid[:], body[:16])
	username, used, err := readString(body[16:], 16)
	if err != nil || len(body[16+used:]) == 0 {
		return uid, "", ErrMalformed
	}
	properties, usedProperties, err := codec.ConsumeVarInt(body[16+used:])
	if err != nil || properties != 0 || usedProperties != len(body[16+used:]) {
		return uid, "", ErrMalformed
	}
	return uid, username, nil
}

func ReadPort(data []byte) uint16 {
	return binary.BigEndian.Uint16(data)
}
