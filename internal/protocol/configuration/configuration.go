package configuration

import (
	"errors"

	"github.com/Master290/RegionGate/internal/protocol/codec"
)

var ErrMalformed = errors.New("malformed configuration packet")

const (
	ClientboundPluginMessageID       = 0x00
	ClientboundDisconnectID          = 0x01
	ClientboundFinishConfigurationID = 0x02
	ServerboundClientInformationID   = 0x00
	ServerboundPluginMessageID       = 0x01
	ServerboundFinishConfigurationID = 0x02
	ClientboundKeepAliveID           = 0x03
	ServerboundKeepAliveID           = 0x03
	ClientboundPingID                = 0x04
	ServerboundPongID                = 0x04
)

type ClientInformation struct {
	Locale              string
	ViewDistance        int8
	ChatMode            int32
	ChatColors          bool
	DisplayedSkinParts  byte
	MainHand            int32
	TextFiltering       bool
	AllowServerListings bool
}

type PluginMessage struct {
	Channel string
	Data    []byte
}

func FinishPayload() []byte {
	return codec.AppendVarInt(nil, ClientboundFinishConfigurationID)
}

func FinishAcknowledgedPayload() []byte {
	return codec.AppendVarInt(nil, ServerboundFinishConfigurationID)
}

func ParseFinishAcknowledged(payload []byte) error {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundFinishConfigurationID || len(body) != 0 {
		return ErrMalformed
	}
	return nil
}

func ParseClientInformation(payload []byte) (ClientInformation, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundClientInformationID {
		return ClientInformation{}, ErrMalformed
	}
	locale, used, err := codec.ConsumeString(body, 16)
	if err != nil || used >= len(body) {
		return ClientInformation{}, ErrMalformed
	}
	body = body[used:]
	information := ClientInformation{Locale: locale, ViewDistance: int8(body[0])}
	body = body[1:]
	information.ChatMode, used, err = codec.ConsumeVarInt(body)
	if err != nil || information.ChatMode < 0 || information.ChatMode > 2 {
		return ClientInformation{}, ErrMalformed
	}
	body = body[used:]
	if len(body) < 2 {
		return ClientInformation{}, ErrMalformed
	}
	if information.ChatColors, err = parseBool(body[0]); err != nil {
		return ClientInformation{}, err
	}
	information.DisplayedSkinParts = body[1]
	body = body[2:]
	information.MainHand, used, err = codec.ConsumeVarInt(body)
	if err != nil || information.MainHand < 0 || information.MainHand > 1 {
		return ClientInformation{}, ErrMalformed
	}
	body = body[used:]
	if len(body) != 2 {
		return ClientInformation{}, ErrMalformed
	}
	if information.TextFiltering, err = parseBool(body[0]); err != nil {
		return ClientInformation{}, err
	}
	if information.AllowServerListings, err = parseBool(body[1]); err != nil {
		return ClientInformation{}, err
	}
	return information, nil
}

func ParsePluginMessage(payload []byte) (PluginMessage, error) {
	id, body, err := codec.PacketID(payload)
	if err != nil || id != ServerboundPluginMessageID {
		return PluginMessage{}, ErrMalformed
	}
	channel, used, err := codec.ConsumeString(body, 32767)
	if err != nil || channel == "" {
		return PluginMessage{}, ErrMalformed
	}
	return PluginMessage{Channel: channel, Data: append([]byte(nil), body[used:]...)}, nil
}

func ParseClientSetup(payload []byte) error {
	id, _, err := codec.PacketID(payload)
	if err != nil {
		return ErrMalformed
	}
	switch id {
	case ServerboundClientInformationID:
		_, err = ParseClientInformation(payload)
	case ServerboundPluginMessageID:
		_, err = ParsePluginMessage(payload)
	default:
		return ErrMalformed
	}
	return err
}

func parseBool(value byte) (bool, error) {
	switch value {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, ErrMalformed
	}
}
