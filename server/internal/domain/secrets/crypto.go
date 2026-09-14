package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const maskedSecretValue = "***"

type Cipher struct {
	gcm cipher.AEAD
}

func MaskedValue() string {
	return maskedSecretValue
}

func IsMaskedValue(v string) bool {
	return v == maskedSecretValue
}

func NewCipherFromEnv() (*Cipher, error) {
	key, err := deriveKey()
	if err != nil {
		return nil, err
	}
	return NewCipher(key)
}

func deriveKey() ([]byte, error) {
	if v := os.Getenv("MCP_SECRETS_KEY"); v != "" {
		if decoded, err := base64.StdEncoding.DecodeString(v); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
		sum := sha256.Sum256([]byte(v))
		return sum[:], nil
	}
	if v := os.Getenv("SERVER_API_KEY"); v != "" {
		sum := sha256.Sum256([]byte("mcp-secrets:" + v))
		return sum[:], nil
	}
	return nil, errors.New("MCP_SECRETS_KEY or SERVER_API_KEY required for MCP secret storage")
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{gcm: gcm}, nil
}

func (c *Cipher) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (c *Cipher) Decrypt(ciphertext []byte) (string, error) {
	nonceSize := c.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plain, err := c.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
