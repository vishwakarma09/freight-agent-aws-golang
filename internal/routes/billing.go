package routes

import (
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
)

type CheckoutRequest struct {
	PlanTier string `json:"plan_tier" binding:"required"` // boutique, growth, enterprise
	Interval string `json:"interval"`                     // monthly, annual
}

func RegisterBillingRoutes(rg *gin.RouterGroup) {
	// Public pricing catalog
	rg.GET("/billing/plans", getBillingPlans)

	// Public Stripe webhook (cryptographically verified via Stripe-Signature header)
	rg.POST("/webhooks/stripe", handleStripeWebhook)

	// Authenticated workspace subscription routes
	billing := rg.Group("/billing", AuthMiddleware())
	{
		billing.GET("/summary", getBillingSummary)
		billing.POST("/checkout", RequireRoles("owner", "admin"), createCheckoutSession)
		billing.POST("/portal", RequireRoles("owner", "admin"), createPortalSession)
	}
}

func getBillingPlans(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"model": "option_a_hybrid_workspace_quote_allowance",
		"plans": services.GetOptionAPlans(),
	})
}

func getBillingSummary(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var carrierCount int64
	db.DB.Model(&models.Carrier{}).Where("organization_id = ?", org.ID).Count(&carrierCount)

	var inboxCount int64
	db.DB.Model(&models.EmailCredential{}).Where("organization_id = ?", org.ID).Count(&inboxCount)

	var seatCount int64
	db.DB.Model(&models.User{}).Where("organization_id = ?", org.ID).Count(&seatCount)

	planSpec, _ := services.GetPlanSpec(org.PlanTier)

	c.JSON(http.StatusOK, gin.H{
		"organization_id":        org.ID,
		"organization_name":      org.Name,
		"plan_tier":              org.PlanTier,
		"plan_name":              planSpec.Name,
		"billing_status":         org.BillingStatus,
		"trial_ends_at":          org.TrialEndsAt,
		"current_period_end":     org.CurrentPeriodEnd,
		"has_stripe_customer":    org.StripeCustomerID != nil && *org.StripeCustomerID != "",
		"supports_organizations": org.PlanTier == "enterprise",
		"usage": gin.H{
			"quotes_used":    org.QuotesUsedThisPeriod,
			"quotes_limit":   org.MonthlyQuoteLimit,
			"carriers_used":  carrierCount,
			"carriers_limit": org.CarrierLimit,
			"inboxes_used":   inboxCount,
			"inboxes_limit":  org.InboxLimit,
			"seats_used":     seatCount,
			"seats_limit":    org.SeatLimit,
		},
		"overage_per_quote_usd": planSpec.OveragePerQuoteUSD,
		"plans":                 services.GetOptionAPlans(),
	})
}

func createCheckoutSession(c *gin.Context) {
	user, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	var payload CheckoutRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	if payload.Interval == "" {
		payload.Interval = "monthly"
	}

	checkoutURL, err := services.CreateStripeCheckoutSession(db.DB, org, user.Email, payload.PlanTier, payload.Interval)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"checkout_url": checkoutURL})
}

func createPortalSession(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	portalURL, err := services.CreateStripePortalSession(org)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"portal_url": portalURL})
}

func handleStripeWebhook(c *gin.Context) {
	const maxBodyBytes = int64(65536)
	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBodyBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Error reading webhook body"})
		return
	}

	sigHeader := c.GetHeader("Stripe-Signature")
	if sigHeader == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Missing Stripe-Signature header"})
		return
	}

	if err := services.HandleStripeWebhook(db.DB, payload, sigHeader); err != nil {
		log.Printf("Stripe webhook error: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}
