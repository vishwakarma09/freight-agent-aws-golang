package routes

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

// CORSMiddleware handles cross-origin resource sharing
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-User-Email")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// AuthMiddleware extracts X-User-Email and loads the current user
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		email := c.GetHeader("X-User-Email")
		if email == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required: X-User-Email header missing"})
			c.Abort()
			return
		}

		var user models.User
		if err := db.DB.Where("LOWER(email) = LOWER(?)", strings.TrimSpace(email)).First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required: User not found"})
			c.Abort()
			return
		}

		c.Set("currentUser", &user)
		c.Next()
	}
}

// GetCurrentUser returns the authenticated user from gin Context
func GetCurrentUser(c *gin.Context) (*models.User, bool) {
	val, exists := c.Get("currentUser")
	if !exists {
		return nil, false
	}
	user, ok := val.(*models.User)
	return user, ok
}
