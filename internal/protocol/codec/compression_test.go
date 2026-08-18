package codec

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestCompressionStateRoundTripsCompressedAndUncompressedPackets(t *testing.T) {
	state, err := NewCompressionState(16, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{
		AppendVarInt(nil, 0x01),
		bytes.Repeat([]byte{0xab}, 64),
	} {
		var wire bytes.Buffer
		if err := state.WriteFrame(&wire, payload); err != nil {
			t.Fatal(err)
		}
		got, err := state.ReadFrame(bufio.NewReader(&wire))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload=%x want=%x", got, payload)
		}
	}
}

func TestCompressionStateRejectsThresholdViolations(t *testing.T) {
	state, err := NewCompressionState(4, 64)
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	framer := NewFramer(64)
	packet := AppendVarInt(nil, 0)
	packet = append(packet, []byte{1, 2, 3, 4}...)
	if err := framer.WriteFrame(&wire, packet); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ReadFrame(bufio.NewReader(&wire)); !errors.Is(err, ErrInvalidCompressedPacket) {
		t.Fatalf("error=%v", err)
	}
}

func TestCompressionStateRejectsDecompressionBombLength(t *testing.T) {
	state, err := NewCompressionState(1, 32)
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	packet := AppendVarInt(nil, 33)
	packet = append(packet, 1)
	if err := NewFramer(64).WriteFrame(&wire, packet); err != nil {
		t.Fatal(err)
	}
	_, err = state.ReadFrame(bufio.NewReader(&wire))
	var sizeErr *PacketTooLargeError
	if !errors.As(err, &sizeErr) {
		t.Fatalf("error=%v", err)
	}
}

func TestCompressionReaderPoolRecoversAfterCorruptStream(t *testing.T) {
	state, err := NewCompressionState(1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("regiongate"), 32)
	var valid bytes.Buffer
	if err := state.WriteFrame(&valid, payload); err != nil {
		t.Fatal(err)
	}
	validWire := append([]byte(nil), valid.Bytes()...)
	corruptWire := append([]byte(nil), validWire...)
	corruptWire[len(corruptWire)-1] ^= 0xff
	if _, err := state.ReadFrame(bufio.NewReader(bytes.NewReader(corruptWire))); !errors.Is(err, ErrInvalidCompressedPacket) {
		t.Fatalf("corrupt stream error=%v", err)
	}
	got, err := state.ReadFrame(bufio.NewReader(bytes.NewReader(validWire)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload=%x want=%x", got, payload)
	}
}
