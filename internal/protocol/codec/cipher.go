package codec

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

var ErrInvalidCipherParameters = errors.New("AES-CFB8 requires a 16-byte key and IV")

type CipherState struct {
	block     cipher.Block
	encryptIV [aes.BlockSize]byte
	decryptIV [aes.BlockSize]byte
}

func NewCipherState(key, iv []byte) (*CipherState, error) {
	if len(key) != aes.BlockSize || len(iv) != aes.BlockSize {
		return nil, ErrInvalidCipherParameters
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	state := &CipherState{block: block}
	copy(state.encryptIV[:], iv)
	copy(state.decryptIV[:], iv)
	return state, nil
}

func (c *CipherState) Encrypt(dst, src []byte) {
	cfb8(c.block, c.encryptIV[:], dst, src, true)
}

func (c *CipherState) Decrypt(dst, src []byte) {
	cfb8(c.block, c.decryptIV[:], dst, src, false)
}

func cfb8(block cipher.Block, shift, dst, src []byte, encrypt bool) {
	if len(dst) < len(src) {
		panic("codec: output smaller than input")
	}
	var encrypted [aes.BlockSize]byte
	for index, value := range src {
		block.Encrypt(encrypted[:], shift)
		result := value ^ encrypted[0]
		feedback := value
		if encrypt {
			feedback = result
		}
		dst[index] = result
		copy(shift, shift[1:])
		shift[len(shift)-1] = feedback
	}
}
