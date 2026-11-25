package utils

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	allowedExts    = map[string]string{
		".pdf":  "application/pdf",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".csv":  "text/csv",
		".txt":  "text/plain",
	}
)

// ValidateExternalHostPort prevents SSRF by resolving DNS and blocking private, loopback,
// link-local (including AWS IMDS 169.254.169.254), and reserved IP ranges, and enforcing allowed ports.
func ValidateExternalHostPort(host string, port int, allowedPorts []int, allowDevLocal bool) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("host is required")
	}
	if strings.ContainsAny(host, " \t\r\n/@\\") {
		return errors.New("invalid characters in host")
	}

	portOK := false
	for _, p := range allowedPorts {
		if port == p {
			portOK = true
			break
		}
	}
	if !portOK {
		return fmt.Errorf("port %d is not permitted", port)
	}

	lowerHost := strings.ToLower(host)
	if !allowDevLocal && (lowerHost == "localhost" || lowerHost == "mailpit" || strings.HasSuffix(lowerHost, ".local") || strings.HasSuffix(lowerHost, ".internal")) {
		return errors.New("internal or local hostnames are not permitted")
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("could not resolve host %s", host)
	}

	for _, ip := range ips {
		if isBlockedIP(ip) && !allowDevLocal {
			return fmt.Errorf("host %s resolves to a restricted network address", host)
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// Explicitly block AWS IMDS / CGNAT / documentation / IPv4-mapped private IPv6
	if ip4 := ip.To4(); ip4 != nil {
		// 100.64.0.0/10 (CGNAT)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		// 169.254.0.0/16 (Link-Local / IMDS)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}

// SanitizeHeaderValue strips CR/LF characters to prevent SMTP header injection.
func SanitizeHeaderValue(val string) string {
	val = strings.ReplaceAll(val, "\r", "")
	val = strings.ReplaceAll(val, "\n", "")
	return strings.TrimSpace(val)
}

// SanitizeUploadFilename validates an upload filename against path traversal and enforces allowed extensions.
func SanitizeUploadFilename(raw string) (safeName string, contentType string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", errors.New("filename is required")
	}
	if strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, "/\\") {
		return "", "", errors.New("invalid filename: path separators and traversal sequences are forbidden")
	}

	base := filepath.Base(trimmed)
	ext := strings.ToLower(filepath.Ext(base))
	ct, ok := allowedExts[ext]
	if !ok {
		return "", "", fmt.Errorf("unsupported file extension %q; allowed: .pdf, .png, .jpg, .jpeg, .csv, .txt", ext)
	}

	cleaned := safeFilenameRe.ReplaceAllString(base, "_")
	if len(cleaned) > 120 {
		cleaned = cleaned[:120-len(ext)] + ext
	}
	return cleaned, ct, nil
}
