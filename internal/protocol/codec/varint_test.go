package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestVarIntRoundTrip(t *testing.T) {
	values := []int32{0, 1, 127, 128, 255, 2147483647, -1, -2147483648}
	for _, want := range values {
		encoded := AppendVarInt(nil, want)
		got, size, err := ConsumeVarInt(encoded)
		if err != nil {
			t.Fatalf("ConsumeVarInt(%d): %v", want, err)
		}
		if got != want || size != len(encoded) {
			t.Fatalf("round trip: want value=%d size=%d, got value=%d size=%d", want, len(encoded), got, size)
		}

		read, err := ReadVarInt(bytes.NewBuffer(encoded))
		if err != nil || read != want {
			t.Fatalf("ReadVarInt(%d): value=%d err=%v", want, read, err)
		}
	}
}

func TestConsumeVarIntRejectsMalformedInput(t *testing.T) {
	if _, _, err := ConsumeVarInt([]byte{0x80}); !errors.Is(err, ErrVarIntPartial) {
		t.Fatalf("partial varint error = %v", err)
	}
	if _, _, err := ConsumeVarInt([]byte{0x80, 0x80, 0x80, 0x80, 0x80}); !errors.Is(err, ErrVarIntTooLong) {
		t.Fatalf("long varint error = %v", err)
	}
	if _, _, err := ConsumeVarInt([]byte{0xff, 0xff, 0xff, 0xff, 0x7f}); !errors.Is(err, ErrVarIntOverflow) {
		t.Fatalf("overflow varint error = %v", err)
	}
}
