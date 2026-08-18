package codec

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestCipherStateMatchesNISTCFB8Vector(t *testing.T) {
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	iv, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	plain, _ := hex.DecodeString("6bc1bee22e409f96e93d7e117393172a")
	want, _ := hex.DecodeString("3b79424c9c0dd436bace9e0ed4586a4f")
	state, err := NewCipherState(key, iv)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(plain))
	state.Encrypt(got, plain)
	if !bytes.Equal(got, want) {
		t.Fatalf("ciphertext=%x want=%x", got, want)
	}
}

func TestCipherStateRoundTripAcrossChunks(t *testing.T) {
	key := []byte("0123456789abcdef")
	encryptor, err := NewCipherState(key, key)
	if err != nil {
		t.Fatal(err)
	}
	decryptor, err := NewCipherState(key, key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("minecraft protocol encryption crosses packet boundaries")
	encrypted := make([]byte, len(plain))
	encryptor.Encrypt(encrypted[:11], plain[:11])
	encryptor.Encrypt(encrypted[11:], plain[11:])
	decrypted := make([]byte, len(plain))
	decryptor.Decrypt(decrypted[:7], encrypted[:7])
	decryptor.Decrypt(decrypted[7:], encrypted[7:])
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("decrypted=%q", decrypted)
	}
}

func TestCipherStateRejectsInvalidParameters(t *testing.T) {
	if _, err := NewCipherState([]byte("short"), make([]byte, 16)); !errors.Is(err, ErrInvalidCipherParameters) {
		t.Fatalf("error=%v", err)
	}
}
