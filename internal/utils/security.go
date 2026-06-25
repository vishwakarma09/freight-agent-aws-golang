package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/fernet/fernet-go"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"golang.org/x/crypto/pbkdf2"
)

// getFernetKey derives a 32-byte key base64url encoded for Fernet
func getFernetKey() ([]byte, error) {
	secret := config.Settings.SecretKey
	if secret == "" {
		secret = "REMOVED_USE_AWS_SECRETS_MANAGER"
	}
	salt := []byte("REMOVED_USE_AES256_GCM_KMS")
	key := pbkdf2.Key([]byte(secret), salt, 100000, 32, sha256.New)
	
	// Fernet expects a 32-byte key base64url encoded
	b64Key := make([]byte, base64.URLEncoding.EncodedLen(len(key)))
	base64.URLEncoding.Encode(b64Key, key)
	return b64Key, nil
}

// EncryptPassword encrypts a string using Fernet
func EncryptPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	b64Key, err := getFernetKey()
	if err != nil {
		return "", err
	}
	key, err := fernet.DecodeKey(string(b64Key))
	if err != nil {
		return "", err
	}
	token, err := fernet.EncryptAndSign([]byte(password), key)
	if err != nil {
		return "", err
	}
	return string(token), nil
}

// DecryptPassword decrypts a Fernet token
func DecryptPassword(encryptedPassword string) (string, error) {
	if encryptedPassword == "" {
		return "", nil
	}
	b64Key, err := getFernetKey()
	if err != nil {
		return "", err
	}
	key, err := fernet.DecodeKey(string(b64Key))
	if err != nil {
		return "", err
	}
	// VerifyAndDecrypt accepts a list of keys and a TTL.
	// 0 means no expiration check (as in Python's implementation for email passwords).
	msg := fernet.VerifyAndDecrypt([]byte(encryptedPassword), 0, []*fernet.Key{key})
	if msg == nil {
		return "", errors.New("failed to decrypt or verify password")
	}
	return string(msg), nil
}

// HashPassword hashes a password using PBKDF2 HMAC SHA-256 with a 16-byte random salt
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	if err != nil {
		return "", err
	}

	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(key), nil
}

// VerifyPassword verifies if the password matches the PBKDF2 hash
func VerifyPassword(password string, hashed string) bool {
	parts := strings.Split(hashed, ":")
	if len(parts) != 2 {
		return false
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	key, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	newKey := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
	
	// Constant time comparison to prevent timing attacks
	if len(key) != len(newKey) {
		return false
	}
	var diff byte
	for i := 0; i < len(key); i++ {
		diff |= key[i] ^ newKey[i]
	}
	return diff == 0
}

// FormatTime formats a time object into ISO-8601 string
func FormatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
