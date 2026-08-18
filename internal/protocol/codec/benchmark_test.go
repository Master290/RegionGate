package codec

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func BenchmarkVarInt(b *testing.B) {
	encoded := AppendVarInt(nil, 2147483647)
	b.Run("encode", func(b *testing.B) {
		buffer := make([]byte, 0, MaxVarIntBytes)
		b.ReportAllocs()
		for b.Loop() {
			buffer = AppendVarInt(buffer[:0], 2147483647)
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := ConsumeVarInt(encoded); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkFramer(b *testing.B) {
	payload := bytes.Repeat([]byte{0xab}, 512)
	framer := NewFramer(1024)
	var wire bytes.Buffer
	if err := framer.WriteFrame(&wire, payload); err != nil {
		b.Fatal(err)
	}
	encoded := append([]byte(nil), wire.Bytes()...)

	b.Run("read", func(b *testing.B) {
		raw := bytes.NewReader(encoded)
		reader := bufio.NewReaderSize(raw, len(encoded))
		dst := make([]byte, len(payload))
		b.SetBytes(int64(len(payload)))
		b.ReportAllocs()
		for b.Loop() {
			raw.Reset(encoded)
			reader.Reset(raw)
			if _, err := framer.ReadFrame(reader, dst); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("write", func(b *testing.B) {
		var output bytes.Buffer
		output.Grow(len(encoded))
		b.SetBytes(int64(len(payload)))
		b.ReportAllocs()
		for b.Loop() {
			output.Reset()
			if err := framer.WriteFrame(&output, payload); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCompression(b *testing.B) {
	for _, size := range []int{256, 4096, 65536} {
		for _, compressible := range []bool{true, false} {
			name := "random"
			payload := make([]byte, size)
			if compressible {
				name = "repeated"
				copy(payload, bytes.Repeat([]byte("regiongate"), size/10+1))
			} else {
				_, _ = rand.New(rand.NewSource(1)).Read(payload)
			}
			state, err := NewCompressionState(128, size*2)
			if err != nil {
				b.Fatal(err)
			}
			var wire bytes.Buffer
			if err := state.WriteFrame(&wire, payload); err != nil {
				b.Fatal(err)
			}
			encoded := append([]byte(nil), wire.Bytes()...)

			b.Run(fmt.Sprintf("read/%s/%d", name, size), func(b *testing.B) {
				raw := bytes.NewReader(encoded)
				reader := bufio.NewReaderSize(raw, len(encoded))
				b.SetBytes(int64(size))
				b.ReportAllocs()
				for b.Loop() {
					raw.Reset(encoded)
					reader.Reset(raw)
					if _, err := state.ReadFrame(reader); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(fmt.Sprintf("write/%s/%d", name, size), func(b *testing.B) {
				var output bytes.Buffer
				output.Grow(len(encoded))
				b.SetBytes(int64(size))
				b.ReportAllocs()
				for b.Loop() {
					output.Reset()
					if err := state.WriteFrame(&output, payload); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkAESCFB8(b *testing.B) {
	key := []byte("0123456789abcdef")
	state, err := NewCipherState(key, key)
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x5a}, 4096)
	output := make([]byte, len(payload))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		state.Encrypt(output, payload)
	}
}
