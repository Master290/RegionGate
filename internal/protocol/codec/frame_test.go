package codec

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestFramerReadWriteAndReuseBuffer(t *testing.T) {
	framer := NewFramer(64)
	var wire bytes.Buffer
	payload := AppendVarInt(nil, 0x17)
	payload = append(payload, 0xaa, 0xbb)
	if err := framer.WriteFrame(&wire, payload); err != nil {
		t.Fatal(err)
	}

	buffer := make([]byte, 8, 8)
	frame, err := framer.ReadFrame(bufio.NewReader(&wire), buffer)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != len(payload) || &frame[0] != &buffer[0] {
		t.Fatal("framer did not reuse destination buffer")
	}
	id, body, err := PacketID(frame)
	if err != nil || id != 0x17 || !bytes.Equal(body, []byte{0xaa, 0xbb}) {
		t.Fatalf("packet = id %d body %x err %v", id, body, err)
	}
}

func TestFramerRejectsOversizedPacket(t *testing.T) {
	framer := NewFramer(2)
	var wire bytes.Buffer
	wire.Write([]byte{3, 1, 2, 3})
	_, err := framer.ReadFrame(bufio.NewReader(&wire), nil)
	var sizeErr *PacketTooLargeError
	if !errors.As(err, &sizeErr) {
		t.Fatalf("error = %v, want PacketTooLargeError", err)
	}
}

func TestConsumeString(t *testing.T) {
	encoded := AppendString(nil, "Привет")
	value, used, err := ConsumeString(encoded, 6)
	if err != nil || value != "Привет" || used != len(encoded) {
		t.Fatalf("value=%q used=%d err=%v", value, used, err)
	}
	if _, _, err := ConsumeString(encoded, 5); err == nil {
		t.Fatal("expected character limit error")
	}
}
