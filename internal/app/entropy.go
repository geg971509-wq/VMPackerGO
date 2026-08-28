package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

func runEntropy(seed string) (io.Reader, error) {
	if seed == "" {
		return rand.Reader, nil
	}
	key, err := hex.DecodeString(seed)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("seed must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize deterministic entropy: %w", err)
	}
	return &cipher.StreamReader{S: cipher.NewCTR(block, make([]byte, aes.BlockSize)), R: zeroReader{}}, nil
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}
