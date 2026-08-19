package login

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed login packet")

const (
	ServerboundLoginStartID         = 0x00
	ServerboundEncryptionResponseID = 0x01
	ServerboundPluginResponseID     = 0x02
	ServerboundLoginAckID           = 0x03
	ClientboundEncryptionRequestID  = 0x01
	ClientboundLoginSuccessID       = 0x02
	ClientboundPluginRequestID      = 0x04
)

const maxEncryptionFieldSize = 1024

type Start struct {
	Username string
	UUID     [16]byte
}

type PluginRequest struct {
	MessageID int32
	Channel   string
	Data      []byte
}

type EncryptionRequest struct {
	ServerID           string
	PublicKey          []byte
	VerifyToken        []byte
	ShouldAuthenticate bool
}

type EncryptionResponse struct {
	SharedSecret []byte
	VerifyToken  []byte
}

type Property struct {
	Name      string
	Value     string
	Signature string
}

func EncryptionRequestPayload(serverID string, publicKey, verifyToken []byte, shouldAuthenticate bool) []byte {
	payload := codec.AppendVarInt(nil, ClientboundEncryptionRequestID)
	payload = codec.AppendString(payload, serverID)
	payload = appendByteArray(payload, publicKey)
	payload = appendByteArray(payload, verifyToken)
	if shouldAuthenticate {
		return append(payload, 1)
	}
	return append(payload, 0)
}

func ParseEncryptionRequest(payload []byte) (EncryptionRequest, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ClientboundEncryptionRequestID {
		return EncryptionRequest{}, ErrMalformed
	}
	serverID, used, err := readString(body, 20)
	if err != nil {
		return EncryptionRequest{}, ErrMalformed
	}
	body = body[used:]
	publicKey, used, err := consumeByteArray(body, maxEncryptionFieldSize)
	if err != nil {
		return EncryptionRequest{}, err
	}
	body = body[used:]
	verifyToken, used, err := consumeByteArray(body, maxEncryptionFieldSize)
	if err != nil {
		return EncryptionRequest{}, err
	}
	body = body[used:]
	if len(body) != 1 || body[0] > 1 {
		return EncryptionRequest{}, ErrMalformed
	}
	return EncryptionRequest{ServerID: serverID, PublicKey: publicKey, VerifyToken: verifyToken, ShouldAuthenticate: body[0] == 1}, nil
}

func EncryptionResponsePayload(sharedSecret, verifyToken []byte) []byte {
	payload := codec.AppendVarInt(nil, ServerboundEncryptionResponseID)
	payload = appendByteArray(payload, sharedSecret)
	return appendByteArray(payload, verifyToken)
}

func ParseEncryptionResponse(payload []byte) (EncryptionResponse, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundEncryptionResponseID {
		return EncryptionResponse{}, ErrMalformed
	}
	sharedSecret, used, err := consumeByteArray(body, maxEncryptionFieldSize)
	if err != nil {
		return EncryptionResponse{}, err
	}
	body = body[used:]
	verifyToken, used, err := consumeByteArray(body, maxEncryptionFieldSize)
	if err != nil || used != len(body) {
		return EncryptionResponse{}, ErrMalformed
	}
	return EncryptionResponse{SharedSecret: sharedSecret, VerifyToken: verifyToken}, nil
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

// ParseDisconnectReason extracts a useful, bounded text reason from a Login
// Disconnect packet without exposing the full backend JSON to callers.
func ParseDisconnectReason(payload []byte) string {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != 0x00 {
		return ""
	}
	message, used, err := codec.ConsumeString(body, 32767)
	if err != nil || used != len(body) {
		return ""
	}
	var component struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(message), &component) == nil && component.Text != "" {
		return component.Text
	}
	return message
}

func SuccessPayload(username string) []byte {
	return SuccessProfilePayload(OfflineUUID(username), username, nil)
}

func SuccessProfilePayload(uid [16]byte, username string, properties []Property) []byte {
	payload := codec.AppendVarInt(nil, ClientboundLoginSuccessID)
	payload = append(payload, uid[:]...)
	payload = codec.AppendString(payload, username)
	payload = codec.AppendVarInt(payload, int32(len(properties)))
	for _, property := range properties {
		payload = codec.AppendString(payload, property.Name)
		payload = codec.AppendString(payload, property.Value)
		if property.Signature == "" {
			payload = append(payload, 0)
			continue
		}
		payload = append(payload, 1)
		payload = codec.AppendString(payload, property.Signature)
	}
	return payload
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

func appendByteArray(dst, value []byte) []byte {
	dst = codec.AppendVarInt(dst, int32(len(value)))
	return append(dst, value...)
}

func consumeByteArray(data []byte, limit int) ([]byte, int, error) {
	length, used, err := codec.ConsumeVarInt(data)
	if err != nil || length < 0 || int(length) > limit {
		return nil, 0, ErrMalformed
	}
	end := used + int(length)
	if end > len(data) {
		return nil, 0, ErrMalformed
	}
	return append([]byte(nil), data[used:end]...), end, nil
}
