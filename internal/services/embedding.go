package services

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// GetEmbedding generates a deterministic 1536-dimensional unit vector
// for the given text using a local hash-based bag-of-words representation.
func GetEmbedding(text string) []float32 {
	vector := make([]float32, 1536)
	if text == "" {
		return vector
	}

	cleanText := strings.TrimSpace(strings.ToLower(text))
	re := regexp.MustCompile(`[a-z0-9]+`)
	words := re.FindAllString(cleanText, -1)

	if len(words) == 0 {
		words = []string{cleanText}
	}

	for _, word := range words {
		hash := sha256.Sum256([]byte(word))
		// Convert the hash (32 bytes) to a big integer and get modulo 1536
		bi := new(big.Int).SetBytes(hash[:])
		index := new(big.Int).Mod(bi, big.NewInt(1536)).Int64()
		vector[index] += 1.0
	}

	// Normalize to a unit vector (so cosine distance behaves correctly)
	var sumSquares float64
	for _, val := range vector {
		sumSquares += float64(val * val)
	}
	magnitude := math.Sqrt(sumSquares)
	if magnitude > 0 {
		for i, val := range vector {
			vector[i] = float32(float64(val) / magnitude)
		}
	}

	return vector
}

// FormatVector converts a float32 slice into a postgres vector string format, e.g. "[0.1,0.2,0.3]"
func FormatVector(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, val := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(val), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// ParseVector parses a postgres vector string format, e.g. "[0.1,0.2,0.3]", back into a float32 slice
func ParseVector(s string) ([]float32, error) {
	s = strings.Trim(s, "[]")
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	v := make([]float32, len(parts))
	for i, p := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse vector element: %w", err)
		}
		v[i] = float32(val)
	}
	return v, nil
}
