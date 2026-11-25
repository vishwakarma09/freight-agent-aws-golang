package utils

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/pbkdf2"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
)

const (
	prefixGCM = "v2:gcm:"
	prefixKMS = "v2:kms:"
)

// JWTClaims represents the signed authentication claims for a user session.
type JWTClaims struct {
	UserID         uint   `json:"uid"`
	Email          string `json:"email"`
	OrganizationID uint   `json:"org_id"`
	Role           string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateAccessToken creates a signed HS256 JWT valid for 24 hours.
func GenerateAccessToken(userID uint, email string, orgID uint, role string) (string, error) {
	secret := config.Settings.JWTSecret
	if len(secret) < 16 {
		return "", errors.New("JWT_SECRET is not configured")
	}

	now := time.Now().UTC()
	claims := JWTClaims{
		UserID:         userID,
		Email:          strings.ToLower(strings.TrimSpace(email)),
		OrganizationID: orgID,
		Role:           role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "dispatch-freight-api",
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateAccessToken verifies an HS256 JWT signature and expiration.
func ValidateAccessToken(tokenStr string) (*JWTClaims, error) {
	secret := config.Settings.JWTSecret
	if len(secret) < 16 {
		return nil, errors.New("JWT_SECRET is not configured")
	}

	parsed, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithIssuer("dispatch-freight-api"), jwt.WithExpirationRequired())

	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*JWTClaims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// deriveAES256Key derives a 32-byte AES-256 key from ENCRYPTION_KEY (hex or passphrase).
func deriveAES256Key() ([]byte, error) {
	raw := strings.TrimSpace(config.Settings.EncryptionKey)
	if raw == "" {
		return nil, errors.New("ENCRYPTION_KEY is not configured")
	}
	// If a 64-char hex string (32 raw bytes) is supplied, decode directly
	if len(raw) == 64 {
		if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	// Otherwise hash to 32 bytes (AES-256)
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func encryptAES256GCM(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptAES256GCM(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, data, nil)
}

// EncryptPassword encrypts sensitive credentials using AWS KMS envelope encryption (AES-256-GCM)
// when KMS_KEY_ID is configured, or local AES-256-GCM with ENCRYPTION_KEY otherwise.
func EncryptPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}

	if config.Settings != nil && config.Settings.KMSKeyID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to load AWS config for KMS: %w", err)
		}
		kmsClient := kms.NewFromConfig(cfg)
		dataKey, err := kmsClient.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
			KeyId:   &config.Settings.KMSKeyID,
			KeySpec: types.DataKeySpecAes256,
		})
		if err != nil {
			return "", fmt.Errorf("KMS GenerateDataKey failed: %w", err)
		}

		sealed, err := encryptAES256GCM([]byte(password), dataKey.Plaintext)
		// Zero plaintext data key immediately
		for i := range dataKey.Plaintext {
			dataKey.Plaintext[i] = 0
		}
		if err != nil {
			return "", err
		}

		return prefixKMS +
			base64.StdEncoding.EncodeToString(dataKey.CiphertextBlob) + ":" +
			base64.StdEncoding.EncodeToString(sealed), nil
	}

	key, err := deriveAES256Key()
	if err != nil {
		return "", err
	}
	sealed, err := encryptAES256GCM([]byte(password), key)
	if err != nil {
		return "", err
	}
	return prefixGCM + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptPassword decrypts a v2 KMS envelope or AES-256-GCM encrypted credential.
func DecryptPassword(encryptedPassword string) (string, error) {
	if encryptedPassword == "" {
		return "", nil
	}

	if strings.HasPrefix(encryptedPassword, prefixKMS) {
		rest := strings.TrimPrefix(encryptedPassword, prefixKMS)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "", errors.New("malformed KMS ciphertext")
		}
		encKeyBytes, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return "", err
		}
		sealedBytes, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return "", err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return "", err
		}
		kmsClient := kms.NewFromConfig(cfg)
		decOut, err := kmsClient.Decrypt(ctx, &kms.DecryptInput{
			CiphertextBlob: encKeyBytes,
		})
		if err != nil {
			return "", fmt.Errorf("KMS Decrypt failed: %w", err)
		}
		plain, err := decryptAES256GCM(sealedBytes, decOut.Plaintext)
		for i := range decOut.Plaintext {
			decOut.Plaintext[i] = 0
		}
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}

	if strings.HasPrefix(encryptedPassword, prefixGCM) {
		key, err := deriveAES256Key()
		if err != nil {
			return "", err
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encryptedPassword, prefixGCM))
		if err != nil {
			return "", err
		}
		plain, err := decryptAES256GCM(raw, key)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}

	return "", errors.New("unsupported or legacy credential encryption format; please re-save your email credentials")
}

// HashPassword hashes a password using PBKDF2-HMAC-SHA256 with a 16-byte random salt (210k iterations).
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := pbkdf2.Key([]byte(password), salt, 210000, 32, sha256.New)
	return "v2:" + hex.EncodeToString(salt) + ":" + hex.EncodeToString(key), nil
}

// VerifyPassword verifies both v2 (210k) and legacy v1 (100k) PBKDF2 hashes in constant time.
func VerifyPassword(password string, hashed string) bool {
	if hashed == "" {
		return false
	}

	iterations := 100000
	parts := strings.Split(hashed, ":")
	if len(parts) == 3 && parts[0] == "v2" {
		iterations = 210000
		parts = parts[1:]
	}
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

	newKey := pbkdf2.Key([]byte(password), salt, iterations, 32, sha256.New)
	return subtle.ConstantTimeCompare(key, newKey) == 1
}

// ValidatePasswordStrength enforces a minimum production password policy.
func ValidatePasswordStrength(password string) error {
	if len(password) < 10 {
		return errors.New("password must be at least 10 characters long")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("password must include at least one letter and one number")
	}
	return nil
}

// GenerateRandomToken returns a cryptographically secure hex token of n random bytes.
func GenerateRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken hashes a bearer/reset/invite token using SHA-256 before storing in DB.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// FormatTime formats a time object into ISO-8601 string.
func FormatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
