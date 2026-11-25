package routes

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

type UpdateOrgPayload struct {
	Name                  string `json:"name"`
	Round1DurationMinutes *int   `json:"round1_duration_minutes"`
	Round2DurationMinutes *int   `json:"round2_duration_minutes"`
}

type InviteMemberPayload struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required"`
}

func RegisterOrganizationRoutes(rg *gin.RouterGroup) {
	orgGroup := rg.Group("/organization", AuthMiddleware())
	{
		orgGroup.GET("", getOrganization)
		orgGroup.PUT("", RequireRoles("owner", "admin"), updateOrganization)
		orgGroup.GET("/members", getOrganizationMembers)
		orgGroup.POST("/invites", RequireRoles("owner", "admin"), inviteOrganizationMember)
		orgGroup.DELETE("/invites/:invite_id", RequireRoles("owner", "admin"), revokeOrganizationInvite)
		orgGroup.DELETE("/members/:user_id", RequireRoles("owner", "admin"), removeOrganizationMember)
	}
}

func getOrganization(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var memberCount int64
	db.DB.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&memberCount)

	var pendingInvites int64
	db.DB.Model(&models.OrganizationInvite{}).
		Where("organization_id = ? AND accepted_at IS NULL AND expires_at > ?", org.ID, time.Now().UTC()).
		Count(&pendingInvites)

	c.JSON(http.StatusOK, gin.H{
		"organization":           org,
		"member_count":           memberCount,
		"pending_invites_count":  pendingInvites,
		"multi_seat_unlocked":    org.PlanTier == "enterprise",
		"enterprise_tier_needed": org.PlanTier != "enterprise",
	})
}

func updateOrganization(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var payload UpdateOrgPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if name := strings.TrimSpace(payload.Name); name != "" {
		org.Name = name
	}
	if payload.Round1DurationMinutes != nil {
		v := *payload.Round1DurationMinutes
		if v < 1 || v > 1440 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "round1_duration_minutes must be between 1 and 1440"})
			return
		}
		org.Round1DurationMinutes = v
	}
	if payload.Round2DurationMinutes != nil {
		v := *payload.Round2DurationMinutes
		if v < 1 || v > 720 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "round2_duration_minutes must be between 1 and 720"})
			return
		}
		org.Round2DurationMinutes = v
	}

	org.UpdatedAt = time.Now().UTC()
	if err := db.DB.Save(org).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to update organization settings"})
		return
	}
	c.JSON(http.StatusOK, org)
}

func getOrganizationMembers(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var users []models.User
	db.DB.Where("organization_id = ?", org.ID).Order("created_at asc").Find(&users)

	members := make([]gin.H, 0, len(users))
	for _, u := range users {
		members = append(members, gin.H{
			"id":         u.ID,
			"name":       u.Name,
			"email":      u.Email,
			"role":       u.Role,
			"is_active":  u.IsActive,
			"created_at": u.CreatedAt,
		})
	}

	var invites []models.OrganizationInvite
	db.DB.Where("organization_id = ? AND accepted_at IS NULL AND expires_at > ?", org.ID, time.Now().UTC()).
		Order("created_at desc").
		Find(&invites)

	c.JSON(http.StatusOK, gin.H{
		"members":             members,
		"pending_invites":     invites,
		"seat_limit":          org.SeatLimit,
		"multi_seat_unlocked": org.PlanTier == "enterprise",
	})
}

func inviteOrganizationMember(c *gin.Context) {
	user, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	// Gate multi-seat Organizations strictly to the Enterprise tier
	if org.PlanTier != "enterprise" {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"detail": "Multi-seat Organizations and team invitations are available exclusively on the Enterprise / 3PL tier ($799/mo). Upgrade in Subscription & Usage.",
		})
		return
	}

	var payload InviteMemberPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	role := strings.ToLower(strings.TrimSpace(payload.Role))
	validRoles := map[string]bool{"admin": true, "broker": true, "accounting": true, "viewer": true}
	if !validRoles[role] {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid role. Allowed: admin, broker, accounting, viewer"})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	var existingUser models.User
	if err := db.DB.Where("LOWER(email) = LOWER(?)", emailClean).First(&existingUser).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "A user with this email is already registered"})
		return
	}

	var seatCount int64
	db.DB.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&seatCount)
	var pendingCount int64
	db.DB.Model(&models.OrganizationInvite{}).
		Where("organization_id = ? AND accepted_at IS NULL AND expires_at > ?", org.ID, time.Now().UTC()).
		Count(&pendingCount)

	if int(seatCount+pendingCount) >= org.SeatLimit {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"detail": fmt.Sprintf("Seat limit (%d) reached for your Enterprise Organization. Contact support to add seats.", org.SeatLimit),
		})
		return
	}

	rawToken, err := utils.GenerateRandomToken(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate invite token"})
		return
	}

	invite := models.OrganizationInvite{
		OrganizationID:  org.ID,
		Email:           emailClean,
		Role:            role,
		TokenHash:       utils.HashToken(rawToken),
		ExpiresAt:       time.Now().UTC().Add(72 * time.Hour),
		InvitedByUserID: user.ID,
	}
	if err := db.DB.Create(&invite).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create invitation"})
		return
	}

	inviteURL := fmt.Sprintf("%s/register?invite_token=%s", strings.TrimRight(config.Settings.FrontendURL, "/"), url.QueryEscape(rawToken))
	bodyHTML := fmt.Sprintf(`<!DOCTYPE html>
<html><body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;padding:24px;color:#0f172a;">
<h2>You've been invited to join %s on Dispatch</h2>
<p>%s invited you as a <b>%s</b>.</p>
<p>Click below to set your password and join the brokerage workspace (expires in 72 hours):</p>
<p><a href="%s" style="background:#0ea5e9;color:#fff;padding:12px 24px;border-radius:8px;text-decoration:none;font-weight:600;">Accept Invitation</a></p>
</body></html>`, html.EscapeString(org.Name), html.EscapeString(user.Name), html.EscapeString(role), html.EscapeString(inviteURL))

	services.SendEmail(emailClean, fmt.Sprintf("Invitation to join %s on Dispatch", org.Name), bodyHTML, "")

	c.JSON(http.StatusOK, gin.H{
		"invite":  invite,
		"message": fmt.Sprintf("Invitation sent to %s", emailClean),
	})
}

func revokeOrganizationInvite(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	inviteID, err := strconv.ParseUint(c.Param("invite_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid invite ID"})
		return
	}

	res := db.DB.Where("id = ? AND organization_id = ?", inviteID, org.ID).Delete(&models.OrganizationInvite{})
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Invite not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Invitation revoked"})
}

func removeOrganizationMember(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	targetID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid user ID"})
		return
	}
	if uint(targetID) == currentUser.ID {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "You cannot remove yourself from the organization"})
		return
	}

	var target models.User
	if err := db.DB.Where("id = ? AND organization_id = ?", targetID, org.ID).First(&target).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Member not found in your organization"})
		return
	}
	if target.Role == "owner" {
		c.JSON(http.StatusForbidden, gin.H{"detail": "Cannot remove the organization owner"})
		return
	}

	if err := db.DB.Delete(&target).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to remove member"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Member removed"})
}
