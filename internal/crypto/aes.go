package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const (
	pbkdf2Iterations = 100000
	pbkdf2KeyLength  = 32
	saltSize         = 16
	gcmNonceSize     = 12
)

func Decrypt(ciphertext string, password string) (string, error) {
	blob, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	if len(blob) < saltSize+gcmNonceSize+16 {
		return "", fmt.Errorf("ciphertext too short")
	}

	salt := blob[:saltSize]
	nonce := blob[saltSize : saltSize+gcmNonceSize]
	encrypted := blob[saltSize+gcmNonceSize:]

	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	plaintext, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}

	return string(plaintext), nil
}
