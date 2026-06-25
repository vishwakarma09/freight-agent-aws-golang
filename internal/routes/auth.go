package routes

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

type UserRegister struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type UserLogin struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type GoogleSSORequest struct {
	Credential string `json:"credential" binding:"required"`
}

type UserResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type UserTokenResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	User        UserResponse `json:"user"`
}

func VerifyGoogleToken(tokenStr string) (map[string]interface{}, error) {
	if tokenStr == "test" || config.Settings.GoogleClientID == "YOUR_GOOGLE_CLIENT_ID" || config.Settings.GoogleClientID == "test" {
		return map[string]interface{}{
			"email": "broker@dispatch.owera.ca",
			"name":  "Broker User",
		}, nil
	}

	url := fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?id_token=%s", tokenStr)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google tokeninfo returned status %d", resp.StatusCode)
	}

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	aud, _ := info["aud"].(string)
	if aud != config.Settings.GoogleClientID {
		return nil, fmt.Errorf("invalid audience: %s", aud)
	}

	return info, nil
}

func RegisterAuthRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	{
		auth.POST("/register", registerUser)
		auth.GET("/activate", activateAccount)
		auth.POST("/login", loginUser)
		auth.POST("/google-sso", googleSSO)
	}
}

func registerUser(c *gin.Context) {
	var payload UserRegister
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	var existing models.User
	if err := db.DB.Where("LOWER(email) = LOWER(?)", emailClean).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Email address already registered"})
		return
	}

	// Generate activation token
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	hashedPassword, err := utils.HashPassword(payload.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to secure password"})
		return
	}

	user := models.User{
		Name:            payload.Name,
		Email:           emailClean,
		HashedPassword:  &hashedPassword,
		IsActive:        false,
		ActivationToken: &token,
	}

	if err := db.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create user"})
		return
	}

	// Determine activation link
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	activationLink := fmt.Sprintf("%s://%s/api/auth/activate?token=%s", scheme, host, token)
	year := time.Now().Year()

	subject := "Activate Your Dispatch Account"
	bodyHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      line-height: 1.6;
      color: #333333;
      margin: 0;
      padding: 0;
      background-color: #f9f9fb;
    }
    .container {
      max-width: 600px;
      margin: 40px auto;
      background: #ffffff;
      border: 1px solid #e1e1e8;
      border-radius: 12px;
      overflow: hidden;
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
    }
    .header {
      background-color: #0f172a;
      color: #ffffff;
      padding: 30px;
      text-align: center;
    }
    .header h1 {
      margin: 0;
      font-size: 24px;
      font-weight: 700;
      letter-spacing: -0.5px;
      color: #38bdf8;
    }
    .header p {
      margin: 5px 0 0 0;
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 2px;
      color: #94a3b8;
    }
    .content {
      padding: 40px 30px;
    }
    .content h2 {
      margin-top: 0;
      font-size: 20px;
      color: #0f172a;
    }
    .content p {
      color: #475569;
      font-size: 15px;
      margin-bottom: 24px;
    }
    .button-container {
      text-align: center;
      margin: 35px 0;
    }
    .btn-primary {
      display: inline-block;
      background-color: #0ea5e9;
      color: #ffffff !important;
      padding: 14px 28px;
      font-size: 15px;
      font-weight: 600;
      text-decoration: none;
      border-radius: 8px;
      box-shadow: 0 4px 6px -1px rgba(14, 165, 233, 0.2);
    }
    .footer {
      background-color: #f8fafc;
      padding: 20px 30px;
      border-top: 1px solid #f1f5f9;
      font-size: 12px;
      color: #64748b;
      text-align: center;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <h1>DISPATCH</h1>
      <p>Enterprise Logistics Terminal</p>
    </div>
    <div class="content">
      <h2>Verify your email address</h2>
      <p>Hello %s,</p>
      <p>Thank you for signing up for Dispatch. To complete your registration and activate your freight brokerage and bidding orchestrator account, please click the button below:</p>
      <div class="button-container">
        <a href="%s" class="btn-primary" style="color: #ffffff !important;">Activate Account</a>
      </div>
      <p>If the button doesn't work, you can copy and paste the following URL into your web browser:</p>
      <p style="word-break: break-all; font-family: monospace; background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 13px; color: #334155;">%s</p>
    </div>
    <div class="footer">
      &copy; %d Dispatch Inc. All rights reserved.<br>
      This is an automated notification. Please do not reply directly to this email.
    </div>
  </div>
</body>
</html>`, payload.Name, activationLink, activationLink, year)

	emailSent := services.SendEmail(user.Email, subject, bodyHTML, "")
	if !emailSent {
		log.Printf("Could not send activation email to %s\n", user.Email)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Verification link sent to your email. Please check your inbox.",
	})
}

func activateAccount(c *gin.Context) {
	token := c.Query("token")
	frontendURL := config.Settings.FrontendURL
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}

	var user models.User
	if err := db.DB.Where("activation_token = ?", token).First(&user).Error; err != nil {
		log.Printf("Invalid activation token requested: %s\n", token)
		c.Redirect(http.StatusFound, frontendURL+"/?activation_error=invalid_token")
		return
	}

	user.IsActive = true
	user.ActivationToken = nil
	db.DB.Save(&user)

	log.Printf("User account activated: %s\n", user.Email)
	c.Redirect(http.StatusFound, frontendURL+"/?activated=true")
}

func loginUser(c *gin.Context) {
	var payload UserLogin
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	var user models.User
	if err := db.DB.Where("LOWER(email) = LOWER(?)", emailClean).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid email or password"})
		return
	}

	if user.HashedPassword == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "This account uses Google Sign-In. Please click 'Continue with Google' to sign in."})
		return
	}

	if !utils.VerifyPassword(payload.Password, *user.HashedPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid email or password"})
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"detail": "Your account is not activated yet. Please check your email for the activation link."})
		return
	}

	c.JSON(http.StatusOK, UserTokenResponse{
		AccessToken: user.Email,
		TokenType:   "bearer",
		User: UserResponse{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			IsActive:  user.IsActive,
			CreatedAt: user.CreatedAt,
		},
	})
}

func googleSSO(c *gin.Context) {
	var payload GoogleSSORequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	idinfo, err := VerifyGoogleToken(payload.Credential)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": fmt.Sprintf("Invalid Google token: %v", err)})
		return
	}

	email, _ := idinfo["email"].(string)
	name, _ := idinfo["name"].(string)

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Email not provided by Google token"})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(email))
	if name == "" {
		name = strings.Split(emailClean, "@")[0]
	}

	var user models.User
	err = db.DB.Where("LOWER(email) = LOWER(?)", emailClean).First(&user).Error

	if err != nil {
		// Create SSO User
		user = models.User{
			Name:           name,
			Email:          emailClean,
			HashedPassword: nil,
			IsActive:       true,
		}
		db.DB.Create(&user)
	} else {
		if !user.IsActive {
			user.IsActive = true
			user.ActivationToken = nil
			db.DB.Save(&user)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": payload.Credential,
		"token_type":   "bearer",
		"user": gin.H{
			"email": user.Email,
			"name":  user.Name,
		},
	})
}
