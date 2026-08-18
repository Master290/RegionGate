package transport

import (
	"bytes"
	"net"
	"testing"
)

func BenchmarkTransportThroughput(b *testing.B) {
	benchmarks := []struct {
		name        string
		compression bool
		encryption  bool
	}{
		{name: "plain"},
		{name: "compressed", compression: true},
		{name: "encrypted", encryption: true},
		{name: "compressed_encrypted", compression: true, encryption: true},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			left, right := net.Pipe()
			sender := New(left, 64<<10)
			receiver := New(right, 64<<10)
			defer sender.Close()
			defer receiver.Close()
			for _, transport := range []*Transport{sender, receiver} {
				if benchmark.compression {
					if err := transport.EnableCompression(128); err != nil {
						b.Fatal(err)
					}
				}
				if benchmark.encryption {
					key := []byte("0123456789abcdef")
					if err := transport.EnableEncryption(key, key); err != nil {
						b.Fatal(err)
					}
				}
			}
			payload := bytes.Repeat([]byte("regiongate-packet-"), 64)
			readDone := make(chan error, 1)
			go func() {
				for range b.N {
					if _, err := receiver.ReadFrame(); err != nil {
						readDone <- err
						return
					}
				}
				readDone <- nil
			}()
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := sender.WriteFrame(payload); err != nil {
					b.Fatal(err)
				}
			}
			if err := <-readDone; err != nil {
				b.Fatal(err)
			}
		})
	}
}
