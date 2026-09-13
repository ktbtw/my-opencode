package store

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
	"strings"
)

const encryptedFieldPrefix = "enc:v1:"

type projectMemoryFieldCipher struct {
	current string
	keys    map[string][]byte
}

func newProjectMemoryFieldCipher(cfg MySQLConfig) (*projectMemoryFieldCipher, error) {
	current := strings.TrimSpace(os.Getenv("PROJECT_MEMORY_ENCRYPTION_KEY_ID"))
	if current == "" {
		current = "default"
	}
	keys := map[string][]byte{}
	for _, entry := range strings.Split(os.Getenv("PROJECT_MEMORY_ENCRYPTION_KEYS"), ",") {
		keyID, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || strings.TrimSpace(keyID) == "" || strings.TrimSpace(encoded) == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil || (len(decoded) != 16 && len(decoded) != 24 && len(decoded) != 32) {
			return nil, fmt.Errorf("invalid project memory encryption key %q", keyID)
		}
		keys[strings.TrimSpace(keyID)] = decoded
	}
	if encoded := strings.TrimSpace(os.Getenv("PROJECT_MEMORY_ENCRYPTION_KEY")); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || (len(decoded) != 16 && len(decoded) != 24 && len(decoded) != 32) {
			return nil, errors.New("invalid PROJECT_MEMORY_ENCRYPTION_KEY")
		}
		keys[current] = decoded
	}
	if len(keys) == 0 {
		// Stable deployment fallback. Production deployments should set the
		// dedicated encryption key so rotation is independent of MySQL access.
		fallback := sha256.Sum256([]byte("chat-codex/project-memory/" + cfg.Database + "\x00" + cfg.Password))
		keys[current] = fallback[:]
	}
	if _, ok := keys[current]; !ok {
		return nil, fmt.Errorf("current project memory encryption key %q is missing", current)
	}
	return &projectMemoryFieldCipher{current: current, keys: keys}, nil
}

func (c *projectMemoryFieldCipher) encrypt(value, purpose string) (string, error) {
	if c == nil || value == "" || strings.HasPrefix(value, encryptedFieldPrefix) {
		return value, nil
	}
	block, err := aes.NewCipher(c.keys[c.current])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), []byte(purpose))
	return encryptedFieldPrefix + c.current + ":" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *projectMemoryFieldCipher) decrypt(value, purpose string) (string, error) {
	if c == nil || value == "" || !strings.HasPrefix(value, encryptedFieldPrefix) {
		return value, nil
	}
	rest := strings.TrimPrefix(value, encryptedFieldPrefix)
	keyID, encoded, ok := strings.Cut(rest, ":")
	if !ok {
		return "", errors.New("invalid encrypted project memory field")
	}
	key, ok := c.keys[keyID]
	if !ok {
		return "", fmt.Errorf("project memory encryption key %q is unavailable", keyID)
	}
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("encrypted project memory field is truncated")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(purpose))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
