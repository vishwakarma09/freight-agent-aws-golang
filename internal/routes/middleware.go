package routes

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

// CORSMiddleware enforces a strict origin allowlist (never combining "*" with Allow-Credentials).
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimRight(strings.TrimSpace(c.GetHeader("Origin")), "/")
		allowed := false
		if origin != "" && config.Settings != nil {
			for _, o := range config.Settings.AllowedOrigins {
				if strings.EqualFold(o, origin) {
					allowed = true
					break
				}
			}
		}

		if allowed {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, Authorization, Accept, Origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH, DELETE")

		// Defensive API security headers
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// AuthMiddleware strictly validates signed HS256 Bearer JWTs and enforces active account status.
// Raw X-User-Email header authentication is permanently removed.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader == "" || !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required: missing Bearer token"})
			c.Abort()
			return
		}

		tokenStr := strings.TrimSpace(authHeader[7:])
		claims, err := utils.ValidateAccessToken(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required: invalid or expired session token"})
			c.Abort()
			return
		}

		var user models.User
		if err := db.DB.Preload("Organization").Where("id = ?", claims.UserID).First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required: user not found"})
			c.Abort()
			return
		}

		if !user.IsActive {
			c.JSON(http.StatusForbidden, gin.H{"detail": "Account is not activated"})
			c.Abort()
			return
		}

		if user.OrganizationID == 0 || user.Organization == nil {
			c.JSON(http.StatusForbidden, gin.H{"detail": "User is not assigned to an active workspace organization"})
			c.Abort()
			return
		}

		c.Set("currentUser", &user)
		c.Set("currentOrg", user.Organization)
		c.Next()
	}
}

// RequireRoles restricts a route to specific organization roles (e.g., "owner", "admin").
func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[strings.ToLower(r)] = true
	}
	return func(c *gin.Context) {
		user, ok := GetCurrentUser(c)
		if !ok || !allowed[strings.ToLower(user.Role)] {
			c.JSON(http.StatusForbidden, gin.H{"detail": "Insufficient permissions for this action"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetCurrentUser returns the authenticated user from gin Context.
func GetCurrentUser(c *gin.Context) (*models.User, bool) {
	val, exists := c.Get("currentUser")
	if !exists {
		return nil, false
	}
	user, ok := val.(*models.User)
	return user, ok
}

// GetCurrentOrg returns the authenticated user's Organization from gin Context.
func GetCurrentOrg(c *gin.Context) (*models.Organization, bool) {
	val, exists := c.Get("currentOrg")
	if !exists {
		return nil, false
	}
	org, ok := val.(*models.Organization)
	return org, ok
}

// Simple per-IP sliding-window rate limiter for public auth endpoints.
type ipRateEntry struct {
	count   int
	resetAt time.Time
}

var (
	rateMu    sync.Mutex
	rateStore = make(map[string]*ipRateEntry)
)

func RateLimitMiddleware(maxRequests int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.FullPath()
		now := time.Now()

		rateMu.Lock()
		entry, exists := rateStore[key]
		if !exists || now.After(entry.resetAt) {
			rateStore[key] = &ipRateEntry{count: 1, resetAt: now.Add(window)}
			rateMu.Unlock()
			c.Next()
			return
		}
		if entry.count >= maxRequests {
			rateMu.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{"detail": "Too many requests. Please wait a moment and try again."})
			c.Abort()
			return
		}
		entry.count++
		rateMu.Unlock()
		c.Next()
	}
}
