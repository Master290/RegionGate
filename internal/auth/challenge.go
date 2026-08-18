package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

const (
	rsaKeyBits       = 1024
	verifyTokenBytes = 4
	sharedSecretSize = 16
)

var (
	ErrInvalidSharedSecret = errors.New("invalid Minecraft shared secret")
	ErrVerifyTokenMismatch = errors.New("Minecraft verify token mismatch")
)

type Challenge struct {
	privateKey  *rsa.PrivateKey
	publicKey   []byte
	verifyToken []byte
}

type Authenticator struct {
	privateKey *rsa.PrivateKey
	publicKey  []byte
	verifier   Verifier
}

func NewAuthenticator(verifier Verifier) (*Authenticator, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	if err != nil {
		return nil, err
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}
	if verifier == nil {
		verifier = SessionService{}
	}
	return &Authenticator{privateKey: privateKey, publicKey: publicKey, verifier: verifier}, nil
}

func (a *Authenticator) Begin() (*Challenge, error) {
	verifyToken := make([]byte, verifyTokenBytes)
	if _, err := io.ReadFull(rand.Reader, verifyToken); err != nil {
		return nil, err
	}
	return &Challenge{privateKey: a.privateKey, publicKey: a.publicKey, verifyToken: verifyToken}, nil
}

func (a *Authenticator) Authenticate(ctx context.Context, challenge *Challenge, username, address string, encryptedSecret, encryptedToken []byte) (Profile, []byte, error) {
	secret, err := challenge.Decrypt(encryptedSecret, encryptedToken)
	if err != nil {
		return Profile{}, nil, err
	}
	profile, err := a.verifier.HasJoined(ctx, username, ServerHash("", secret, challenge.publicKey), address)
	if err != nil {
		return Profile{}, nil, err
	}
	return profile, secret, nil
}

func NewChallenge() (*Challenge, error) {
	return newChallenge(rand.Reader)
}

func newChallenge(random io.Reader) (*Challenge, error) {
	privateKey, err := rsa.GenerateKey(random, rsaKeyBits)
	if err != nil {
		return nil, err
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}
	verifyToken := make([]byte, verifyTokenBytes)
	if _, err := io.ReadFull(random, verifyToken); err != nil {
		return nil, err
	}
	return &Challenge{privateKey: privateKey, publicKey: publicKey, verifyToken: verifyToken}, nil
}

func (c *Challenge) PublicKey() []byte {
	return append([]byte(nil), c.publicKey...)
}

func (c *Challenge) VerifyToken() []byte {
	return append([]byte(nil), c.verifyToken...)
}

func (c *Challenge) Decrypt(encryptedSecret, encryptedToken []byte) ([]byte, error) {
	secret, err := rsa.DecryptPKCS1v15(rand.Reader, c.privateKey, encryptedSecret)
	if err != nil || len(secret) != sharedSecretSize {
		return nil, ErrInvalidSharedSecret
	}
	token, err := rsa.DecryptPKCS1v15(rand.Reader, c.privateKey, encryptedToken)
	if err != nil || len(token) != len(c.verifyToken) || subtle.ConstantTimeCompare(token, c.verifyToken) != 1 {
		return nil, ErrVerifyTokenMismatch
	}
	return secret, nil
}

// ServerHash returns Minecraft's signed SHA-1 digest representation.
func ServerHash(serverID string, sharedSecret, publicKey []byte) string {
	digest := sha1.New()
	_, _ = digest.Write([]byte(serverID))
	_, _ = digest.Write(sharedSecret)
	_, _ = digest.Write(publicKey)
	value := digest.Sum(nil)
	negative := value[0]&0x80 != 0
	if negative {
		carry := byte(1)
		for index := len(value) - 1; index >= 0; index-- {
			next := ^value[index] + carry
			if carry == 1 && next != 0 {
				carry = 0
			}
			value[index] = next
		}
	}
	encoded := strings.TrimLeft(hex.EncodeToString(value), "0")
	if encoded == "" {
		encoded = "0"
	}
	if negative {
		return "-" + encoded
	}
	return encoded
}
