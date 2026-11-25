package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

type UserRegister struct {
	Name             string `json:"name" binding:"required"`
	Email            string `json:"email" binding:"required,email"`
	Password         string `json:"password" binding:"required"`
	OrganizationName string `json:"organization_name"`
}

type UserLogin struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type GoogleSSORequest struct {
	Credential string `json:"credential" binding:"required"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

type AcceptInviteRequest struct {
	Token    string `json:"token" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type UserResponse struct {
	ID             uint                 `json:"id"`
	OrganizationID uint                 `json:"organization_id"`
	Role           string               `json:"role"`
	Name           string               `json:"name"`
	Email          string               `json:"email"`
	IsActive       bool                 `json:"is_active"`
	CreatedAt      time.Time            `json:"created_at"`
	Organization   *models.Organization `json:"organization,omitempty"`
}

type UserTokenResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	User        UserResponse `json:"user"`
}

// VerifyGoogleToken verifies a real Google ID token against Google's tokeninfo endpoint.
// SECURITY FIX (P0-1): Removed the hardcoded "test" / unconfigured-client-id backdoor.
func VerifyGoogleToken(tokenStr string) (map[string]interface{}, error) {
	clientID := strings.TrimSpace(config.Settings.GoogleClientID)
	if clientID == "" || clientID == "YOUR_GOOGLE_CLIENT_ID" || strings.HasPrefix(clientID, "xxxx") {
		return nil, errors.New("Google Sign-In is not configured on this server")
	}

	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(tokenStr)
	httpClient := &http.Client{Timeout: 8 * time.Second}
	resp, err := httpClient.Get(endpoint)
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
	if aud != clientID {
		return nil, errors.New("invalid Google token audience")
	}

	emailVerified, _ := info["email_verified"].(string)
	if emailVerified != "true" {
		return nil, errors.New("Google account email is not verified")
	}

	return info, nil
}

func RegisterAuthRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	{
		auth.POST("/register", RateLimitMiddleware(10, time.Minute), registerUser)
		auth.GET("/activate", activateAccount)
		auth.POST("/login", RateLimitMiddleware(15, time.Minute), loginUser)
		auth.POST("/google-sso", RateLimitMiddleware(15, time.Minute), googleSSO)
		auth.POST("/forgot-password", RateLimitMiddleware(5, time.Minute), forgotPassword)
		auth.POST("/reset-password", RateLimitMiddleware(10, time.Minute), resetPassword)
		auth.POST("/accept-invite", RateLimitMiddleware(10, time.Minute), acceptInvite)
		auth.GET("/me", AuthMiddleware(), getMe)
	}
}

func createPersonalOrganization(name string, email string) (*models.Organization, error) {
	orgName := strings.TrimSpace(name)
	if orgName == "" {
		orgName = strings.Split(email, "@")[0] + " Freight"
	}
	randSuffix, _ := utils.GenerateRandomToken(4)
	slug := fmt.Sprintf("org-%s", randSuffix)
	trialEnd := time.Now().UTC().AddDate(0, 0, 14)

	r1 := 120
	r2 := 30
	if !config.Settings.IsProd() {
		r1 = 2
		r2 = 1
	}

	org := models.Organization{
		Name:                  orgName,
		Slug:                  slug,
		PlanTier:              "free",
		BillingStatus:         "trialing",
		TrialEndsAt:           &trialEnd,
		SeatLimit:             1,
		CarrierLimit:          10,
		InboxLimit:            1,
		MonthlyQuoteLimit:     25,
		Round1DurationMinutes: r1,
		Round2DurationMinutes: r2,
	}
	if err := db.DB.Create(&org).Error; err != nil {
		return nil, err
	}
	return &org, nil
}

func toUserResponse(user *models.User) UserResponse {
	return UserResponse{
		ID:             user.ID,
		OrganizationID: user.OrganizationID,
		Role:           user.Role,
		Name:           user.Name,
		Email:          user.Email,
		IsActive:       user.IsActive,
		CreatedAt:      user.CreatedAt,
		Organization:   user.Organization,
	}
}

func registerUser(c *gin.Context) {
	var payload UserRegister
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if err := utils.ValidatePasswordStrength(payload.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))
	nameClean := strings.TrimSpace(payload.Name)

	var existing models.User
	if err := db.DB.Where("LOWER(email) = LOWER(?)", emailClean).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Email address already registered"})
		return
	}

	token, err := utils.GenerateRandomToken(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate activation token"})
		return
	}
	tokenHash := utils.HashToken(token)
	expiresAt := time.Now().UTC().Add(24 * time.Hour)

	hashedPassword, err := utils.HashPassword(payload.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to secure password"})
		return
	}

	orgLabel := payload.OrganizationName
	if orgLabel == "" {
		orgLabel = nameClean + "'s Workspace"
	}
	org, err := createPersonalOrganization(orgLabel, emailClean)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create workspace"})
		return
	}

	user := models.User{
		OrganizationID:      org.ID,
		Role:                "owner",
		Name:                nameClean,
		Email:               emailClean,
		HashedPassword:      &hashedPassword,
		IsActive:            false,
		ActivationToken:     &tokenHash,
		ActivationExpiresAt: &expiresAt,
	}

	if err := db.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create user"})
		return
	}

	baseURL := strings.TrimRight(config.Settings.FrontendURL, "/")
	if baseURL == "" {
		baseURL = "https://freight.owera.ca"
	}
	activationLink := fmt.Sprintf("%s/api/auth/activate?token=%s", baseURL, url.QueryEscape(token))
	year := time.Now().Year()

	subject := "Activate Your Dispatch Account"
	bodyHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#f8fafc;padding:24px;color:#0f172a;">
  <div style="max-width:580px;margin:0 auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;overflow:hidden;">
    <div style="background:#0f172a;color:#fff;padding:24px;text-align:center;">
      <h1 style="margin:0;color:#38bdf8;">DISPATCH</h1>
      <p style="margin:4px 0 0;font-size:11px;letter-spacing:2px;color:#94a3b8;">ENTERPRISE LOGISTICS TERMINAL</p>
    </div>
    <div style="padding:32px;">
      <h2>Verify your email address</h2>
      <p>Hello %s,</p>
      <p>Click below to activate your Dispatch account (link expires in 24 hours):</p>
      <p style="text-align:center;margin:28px 0;">
        <a href="%s" style="background:#0ea5e9;color:#fff;padding:12px 24px;border-radius:8px;text-decoration:none;font-weight:600;">Activate Account</a>
      </p>
      <p style="font-size:12px;color:#64748b;word-break:break-all;">%s</p>
    </div>
    <div style="background:#f8fafc;padding:16px;text-align:center;font-size:12px;color:#64748b;">
      &copy; %d Dispatch Inc. All rights reserved.
    </div>
  </div>
</body>
</html>`, html.EscapeString(nameClean), html.EscapeString(activationLink), html.EscapeString(activationLink), year)

	if !services.SendEmail(user.Email, subject, bodyHTML, "") {
		log.Printf("Could not send activation email to %s\n", user.Email)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Verification link sent to your email. Please check your inbox.",
	})
}

func activateAccount(c *gin.Context) {
	rawToken := strings.TrimSpace(c.Query("token"))
	frontendURL := strings.TrimRight(config.Settings.FrontendURL, "/")
	if frontendURL == "" {
		frontendURL = "https://freight.owera.ca"
	}

	if rawToken == "" {
		c.Redirect(http.StatusFound, frontendURL+"/?activation_error=invalid_token")
		return
	}

	tokenHash := utils.HashToken(rawToken)
	var user models.User
	// Match hashed token (or legacy unhashed token created before migration)
	if err := db.DB.Where("activation_token = ? OR activation_token = ?", tokenHash, rawToken).First(&user).Error; err != nil {
		c.Redirect(http.StatusFound, frontendURL+"/?activation_error=invalid_token")
		return
	}

	if user.ActivationExpiresAt != nil && time.Now().UTC().After(*user.ActivationExpiresAt) {
		c.Redirect(http.StatusFound, frontendURL+"/?activation_error=expired_token")
		return
	}

	user.IsActive = true
	user.ActivationToken = nil
	user.ActivationExpiresAt = nil
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
	if err := db.DB.Preload("Organization").Where("LOWER(email) = LOWER(?)", emailClean).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid email or password"})
		return
	}

	if user.HashedPassword == nil || *user.HashedPassword == "" {
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

	accessToken, err := utils.GenerateAccessToken(user.ID, user.Email, user.OrganizationID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to issue session token"})
		return
	}

	c.JSON(http.StatusOK, UserTokenResponse{
		AccessToken: accessToken,
		TokenType:   "bearer",
		User:        toUserResponse(&user),
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
	err = db.DB.Preload("Organization").Where("LOWER(email) = LOWER(?)", emailClean).First(&user).Error
	if err != nil {
		org, orgErr := createPersonalOrganization(name+"'s Workspace", emailClean)
		if orgErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to provision workspace"})
			return
		}
		user = models.User{
			OrganizationID: org.ID,
			Role:           "owner",
			Name:           name,
			Email:          emailClean,
			HashedPassword: nil,
			IsActive:       true,
			Organization:   org,
		}
		if err := db.DB.Create(&user).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create Google SSO account"})
			return
		}
	} else if !user.IsActive {
		user.IsActive = true
		user.ActivationToken = nil
		user.ActivationExpiresAt = nil
		db.DB.Save(&user)
	}

	accessToken, err := utils.GenerateAccessToken(user.ID, user.Email, user.OrganizationID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to issue session token"})
		return
	}

	c.JSON(http.StatusOK, UserTokenResponse{
		AccessToken: accessToken,
		TokenType:   "bearer",
		User:        toUserResponse(&user),
	})
}

func forgotPassword(c *gin.Context) {
	var payload ForgotPasswordRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))
	// Always return generic 200 response to avoid account enumeration
	genericResp := gin.H{
		"status":  "success",
		"message": "If an account exists for that email, a password reset link has been sent.",
	}

	var user models.User
	if err := db.DB.Where("LOWER(email) = LOWER(?)", emailClean).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, genericResp)
		return
	}

	rawToken, err := utils.GenerateRandomToken(24)
	if err != nil {
		c.JSON(http.StatusOK, genericResp)
		return
	}
	tokenHash := utils.HashToken(rawToken)
	exp := time.Now().UTC().Add(1 * time.Hour)
	user.ResetTokenHash = &tokenHash
	user.ResetTokenExpiresAt = &exp
	db.DB.Save(&user)

	resetLink := fmt.Sprintf("%s/login?reset_token=%s", strings.TrimRight(config.Settings.FrontendURL, "/"), url.QueryEscape(rawToken))
	bodyHTML := fmt.Sprintf(`<!DOCTYPE html>
<html><body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;padding:24px;color:#0f172a;">
<h2>Reset Your Dispatch Password</h2>
<p>Hello %s,</p>
<p>We received a request to reset your password. This link expires in 1 hour:</p>
<p><a href="%s" style="background:#0ea5e9;color:#fff;padding:12px 24px;border-radius:8px;text-decoration:none;font-weight:600;">Reset Password</a></p>
<p style="font-size:12px;color:#64748b;">If you did not request this, you can safely ignore this email.</p>
</body></html>`, html.EscapeString(user.Name), html.EscapeString(resetLink))

	services.SendEmail(user.Email, "Reset Your Dispatch Password", bodyHTML, "")
	c.JSON(http.StatusOK, genericResp)
}

func resetPassword(c *gin.Context) {
	var payload ResetPasswordRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if err := utils.ValidatePasswordStrength(payload.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	tokenHash := utils.HashToken(strings.TrimSpace(payload.Token))
	var user models.User
	if err := db.DB.Where("reset_token_hash = ?", tokenHash).First(&user).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid or expired password reset token"})
		return
	}

	if user.ResetTokenExpiresAt == nil || time.Now().UTC().After(*user.ResetTokenExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Password reset token has expired"})
		return
	}

	hashed, err := utils.HashPassword(payload.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to secure password"})
		return
	}

	user.HashedPassword = &hashed
	user.IsActive = true
	user.ResetTokenHash = nil
	user.ResetTokenExpiresAt = nil
	db.DB.Save(&user)

	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Password updated successfully. You may now sign in."})
}

func acceptInvite(c *gin.Context) {
	var payload AcceptInviteRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if err := utils.ValidatePasswordStrength(payload.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	tokenHash := utils.HashToken(strings.TrimSpace(payload.Token))
	var invite models.OrganizationInvite
	if err := db.DB.Where("token_hash = ? AND accepted_at IS NULL", tokenHash).First(&invite).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid or already used invitation token"})
		return
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "This invitation has expired"})
		return
	}

	var org models.Organization
	if err := db.DB.First(&org, invite.OrganizationID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Organization no longer exists"})
		return
	}
	if org.PlanTier != "enterprise" {
		c.JSON(http.StatusPaymentRequired, gin.H{"detail": "Multi-seat Organization access requires an active Enterprise plan"})
		return
	}

	var seatCount int64
	db.DB.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&seatCount)
	if int(seatCount) >= org.SeatLimit {
		c.JSON(http.StatusPaymentRequired, gin.H{"detail": "Organization seat limit reached"})
		return
	}

	var existing models.User
	if err := db.DB.Where("LOWER(email) = LOWER(?)", invite.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "An account with this email already exists"})
		return
	}

	hashed, err := utils.HashPassword(payload.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to secure password"})
		return
	}

	user := models.User{
		OrganizationID: org.ID,
		Role:           invite.Role,
		Name:           strings.TrimSpace(payload.Name),
		Email:          strings.ToLower(strings.TrimSpace(invite.Email)),
		HashedPassword: &hashed,
		IsActive:       true,
		Organization:   &org,
	}
	if err := db.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create team member account"})
		return
	}

	now := time.Now().UTC()
	invite.AcceptedAt = &now
	db.DB.Save(&invite)

	accessToken, err := utils.GenerateAccessToken(user.ID, user.Email, user.OrganizationID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to issue session token"})
		return
	}

	c.JSON(http.StatusOK, UserTokenResponse{
		AccessToken: accessToken,
		TokenType:   "bearer",
		User:        toUserResponse(&user),
	})
}

func getMe(c *gin.Context) {
	user, _ := GetCurrentUser(c)
	c.JSON(http.StatusOK, toUserResponse(user))
}
