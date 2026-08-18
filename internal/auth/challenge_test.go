package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"testing"
)

func TestChallengeDecryptsSecretAndValidatesToken(t *testing.T) {
	challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParsePKIXPublicKey(challenge.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	publicKey := parsed.(*rsa.PublicKey)
	secret := []byte("0123456789abcdef")
	encryptedSecret, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, secret)
	if err != nil {
		t.Fatal(err)
	}
	encryptedToken, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, challenge.VerifyToken())
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := challenge.Decrypt(encryptedSecret, encryptedToken)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != string(secret) {
		t.Fatalf("secret=%x", decrypted)
	}
}

func TestChallengeRejectsWrongToken(t *testing.T) {
	challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := x509.ParsePKIXPublicKey(challenge.PublicKey())
	publicKey := parsed.(*rsa.PublicKey)
	encryptedSecret, _ := rsa.EncryptPKCS1v15(rand.Reader, publicKey, []byte("0123456789abcdef"))
	encryptedToken, _ := rsa.EncryptPKCS1v15(rand.Reader, publicKey, []byte{1, 2, 3, 4})
	if _, err := challenge.Decrypt(encryptedSecret, encryptedToken); err != ErrVerifyTokenMismatch {
		t.Fatalf("error=%v", err)
	}
}

func TestServerHashUsesSignedMinecraftFormat(t *testing.T) {
	tests := map[string]string{
		"Notch": "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48",
		"jeb_":  "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1",
		"simon": "88e16a1019277b15d58faf0541e11910eb756f6",
	}
	for input, expected := range tests {
		if actual := ServerHash(input, nil, nil); actual != expected {
			t.Fatalf("ServerHash(%q)=%q want %q", input, actual, expected)
		}
	}
}
