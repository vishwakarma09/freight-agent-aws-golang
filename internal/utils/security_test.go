package utils

import (
	"strings"
	"testing"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
)

func setupTestConfig() {
	config.Settings = &config.Config{
		Env:           "test",
		JWTSecret:     "test-jwt-secret-key-that-is-over-32-chars-long-12345",
		EncryptionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

func TestJWTGenerateAndValidate(t *testing.T) {
	setupTestConfig()
	tok, err := GenerateAccessToken(42, "broker@acme.com", 7, "owner")
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}
	claims, err := ValidateAccessToken(tok)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if claims.UserID != 42 || claims.OrganizationID != 7 || claims.Email != "broker@acme.com" || claims.Role != "owner" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if _, err := ValidateAccessToken(tok + "tampered"); err == nil {
		t.Fatal("expected tampered token to fail validation")
	}
}

func TestAES256GCMEncryptDecrypt(t *testing.T) {
	setupTestConfig()
	secret := "my-gmail-app-password-16ch"
	enc, err := EncryptPassword(secret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}
	if !strings.HasPrefix(enc, "v2:gcm:") {
		t.Fatalf("expected v2:gcm prefix, got %s", enc)
	}
	dec, err := DecryptPassword(enc)
	if err != nil {
		t.Fatalf("DecryptPassword failed: %v", err)
	}
	if dec != secret {
		t.Fatalf("expected %q, got %q", secret, dec)
	}
}

func TestSanitizeUploadFilenameBlocksTraversal(t *testing.T) {
	badNames := []string{
		"../../quotes/Q-1001/bol.pdf",
		"../secret.pdf",
		"dir/bol.pdf",
		"evil.exe",
		"script.html",
		"",
	}
	for _, name := range badNames {
		if _, _, err := SanitizeUploadFilename(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
	safe, ct, err := SanitizeUploadFilename("BOL 1001.pdf")
	if err != nil || ct != "application/pdf" || safe != "BOL_1001.pdf" {
		t.Fatalf("unexpected valid filename result: safe=%q ct=%q err=%v", safe, ct, err)
	}
}

func TestSSRFGuardBlocksPrivateAndIMDS(t *testing.T) {
	if err := ValidateExternalHostPort("169.254.169.254", 587, []int{465, 587}, false); err == nil {
		t.Fatal("expected AWS IMDS IP to be blocked")
	}
	if err := ValidateExternalHostPort("127.0.0.1", 587, []int{465, 587}, false); err == nil {
		t.Fatal("expected loopback IP to be blocked")
	}
	if err := ValidateExternalHostPort("10.0.0.1", 587, []int{465, 587}, false); err == nil {
		t.Fatal("expected RFC1918 IP to be blocked")
	}
	if err := ValidateExternalHostPort("smtp.gmail.com", 22, []int{465, 587}, false); err == nil {
		t.Fatal("expected non-SMTP port 22 to be blocked")
	}
}

func TestPasswordStrength(t *testing.T) {
	if err := ValidatePasswordStrength("short1"); err == nil {
		t.Fatal("expected short password to fail")
	}
	if err := ValidatePasswordStrength("onlyletterslong"); err == nil {
		t.Fatal("expected password without digits to fail")
	}
	if err := ValidatePasswordStrength("StrongPass2026"); err != nil {
		t.Fatalf("expected strong password to pass, got %v", err)
	}
}
