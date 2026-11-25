package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/billing/meterevent"
	portalsession "github.com/stripe/stripe-go/v81/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

// PlanSpec defines entitlements for each Option A SaaS tier.
type PlanSpec struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	MonthlyPriceUSD       int      `json:"monthly_price_usd"`
	AnnualMonthlyPriceUSD int      `json:"annual_monthly_price_usd"`
	OveragePerQuoteUSD    float64  `json:"overage_per_quote_usd"`
	SeatLimit             int      `json:"seat_limit"`          // 1 for single-broker tiers; 25+ for Enterprise Organizations
	CarrierLimit          int      `json:"carrier_limit"`       // -1 = unlimited
	InboxLimit            int      `json:"inbox_limit"`         // -1 = unlimited
	MonthlyQuoteLimit     int      `json:"monthly_quote_limit"` // included quotes / month
	SupportsOrganizations bool     `json:"supports_organizations"`
	Popular               bool     `json:"popular"`
	Features              []string `json:"features"`
}

// GetOptionAPlans returns the canonical Option A pricing catalog.
func GetOptionAPlans() []PlanSpec {
	return []PlanSpec{
		{
			ID:                    "free",
			Name:                  "Sandbox",
			Description:           "Explore autonomous AI freight quoting and carrier bidding with zero commitment.",
			MonthlyPriceUSD:       0,
			AnnualMonthlyPriceUSD: 0,
			OveragePerQuoteUSD:    0,
			SeatLimit:             1,
			CarrierLimit:          10,
			InboxLimit:            1,
			MonthlyQuoteLimit:     25,
			SupportsOrganizations: false,
			Popular:               false,
			Features: []string{
				"1 broker seat (personal workspace)",
				"25 AI-parsed freight quotes / month",
				"Up to 10 bidding carriers",
				"1 connected email inbox (SMTP/IMAP)",
				"AES-256-GCM / AWS KMS credential encryption",
			},
		},
		{
			ID:                    "boutique",
			Name:                  "Boutique Broker",
			Description:           "Built for independent and boutique freight brokers automating daily RFQs.",
			MonthlyPriceUSD:       99,
			AnnualMonthlyPriceUSD: 79,
			OveragePerQuoteUSD:    0.75,
			SeatLimit:             1,
			CarrierLimit:          30,
			InboxLimit:            1,
			MonthlyQuoteLimit:     150,
			SupportsOrganizations: false,
			Popular:               false,
			Features: []string{
				"1 broker seat (single-broker workspace)",
				"150 AI-parsed quotes / month (+$0.75/extra quote)",
				"Up to 30 bidding carriers",
				"1 connected quoting mailbox (tenant SMTP/IMAP)",
				"Automated 2-round carrier re-bid engine",
				"pgvector historical lane rate benchmarking",
			},
		},
		{
			ID:                    "growth",
			Name:                  "Growth Brokerage",
			Description:           "High-volume brokerage automation with unlimited carriers and full analytics.",
			MonthlyPriceUSD:       299,
			AnnualMonthlyPriceUSD: 249,
			OveragePerQuoteUSD:    0.50,
			SeatLimit:             1,
			CarrierLimit:          -1,
			InboxLimit:            3,
			MonthlyQuoteLimit:     750,
			SupportsOrganizations: false,
			Popular:               true,
			Features: []string{
				"750 AI-parsed quotes / month (+$0.50/extra quote)",
				"Unlimited bidding carriers",
				"Up to 3 connected quoting mailboxes",
				"Custom per-customer markup rules & lane analytics",
				"S3 private BOL & invoice vault with presigned access",
				"CSV spend exports & priority support",
			},
		},
		{
			ID:                    "enterprise",
			Name:                  "Enterprise / 3PL",
			Description:           "Multi-seat Organization workspaces with RBAC for brokerage teams and 3PLs.",
			MonthlyPriceUSD:       799,
			AnnualMonthlyPriceUSD: 649,
			OveragePerQuoteUSD:    0.35,
			SeatLimit:             25,
			CarrierLimit:          -1,
			InboxLimit:            -1,
			MonthlyQuoteLimit:     3000,
			SupportsOrganizations: true,
			Popular:               false,
			Features: []string{
				"Multi-Seat Organizations (up to 25 team members + RBAC)",
				"Shared carrier network, pipeline & team invitations",
				"3,000 AI-parsed quotes / month (+$0.35/extra quote)",
				"Unlimited carriers & unlimited quoting mailboxes",
				"Custom bidding round timers & org audit controls",
				"Dedicated onboarding & SLA support",
			},
		},
	}
}

func GetPlanSpec(planID string) (PlanSpec, bool) {
	for _, p := range GetOptionAPlans() {
		if p.ID == strings.ToLower(strings.TrimSpace(planID)) {
			return p, true
		}
	}
	return PlanSpec{}, false
}

// ApplyPlanLimits updates an Organization's entitlement fields for the given plan tier.
func ApplyPlanLimits(org *models.Organization, planID string) {
	spec, ok := GetPlanSpec(planID)
	if !ok {
		spec, _ = GetPlanSpec("free")
	}
	org.PlanTier = spec.ID
	org.SeatLimit = spec.SeatLimit
	org.CarrierLimit = spec.CarrierLimit
	org.InboxLimit = spec.InboxLimit
	org.MonthlyQuoteLimit = spec.MonthlyQuoteLimit
}

// CanCreateQuote enforces Option A subscription status and monthly quote limits.
// Paid plans allow metered overages; Free/Sandbox enforces a hard cap at MonthlyQuoteLimit.
func CanCreateQuote(org *models.Organization) (allowed bool, isOverage bool, reason string) {
	if org == nil {
		return false, false, "organization not found"
	}
	if org.BillingStatus == "canceled" {
		return false, false, "Your subscription is canceled. Please reactivate a plan in Subscription & Usage to process new quotes."
	}
	if org.QuotesUsedThisPeriod >= org.MonthlyQuoteLimit {
		if org.PlanTier == "free" {
			return false, false, fmt.Sprintf("Free Sandbox limit reached (%d/%d quotes this month). Upgrade to Boutique or Growth to continue.", org.QuotesUsedThisPeriod, org.MonthlyQuoteLimit)
		}
		return true, true, ""
	}
	return true, false, ""
}

// RecordQuoteUsage atomically increments the org's quote counter, logs a QuoteUsageEvent,
// and reports metered overages to Stripe Billing Meters when applicable.
func RecordQuoteUsage(gdb *gorm.DB, org *models.Organization, quoteID string, isOverage bool) {
	if org == nil {
		return
	}
	gdb.Model(&models.Organization{}).
		Where("id = ?", org.ID).
		UpdateColumn("quotes_used_this_period", gorm.Expr("quotes_used_this_period + 1"))
	org.QuotesUsedThisPeriod++

	eventType := "included"
	var meterEventID *string

	if isOverage {
		eventType = "metered_overage"
		if id := reportStripeMeterEvent(org, quoteID); id != "" {
			meterEventID = &id
		}
	}

	usage := models.QuoteUsageEvent{
		OrganizationID:     org.ID,
		FreightQuoteID:     quoteID,
		EventType:          eventType,
		Quantity:           1,
		StripeMeterEventID: meterEventID,
		CreatedAt:          time.Now().UTC(),
	}
	_ = gdb.Create(&usage).Error
}

func reportStripeMeterEvent(org *models.Organization, quoteID string) string {
	if config.Settings.StripeSecretKey == "" || org.StripeCustomerID == nil || *org.StripeCustomerID == "" {
		return ""
	}
	stripe.Key = config.Settings.StripeSecretKey
	eventName := config.Settings.StripeMeterEventName
	if eventName == "" {
		eventName = "freight_quote_overage"
	}
	idempotencyKey := fmt.Sprintf("quote_overage_%d_%s", org.ID, quoteID)
	params := &stripe.BillingMeterEventParams{
		EventName: stripe.String(eventName),
		Identifier: stripe.String(idempotencyKey),
		Payload: map[string]string{
			"stripe_customer_id": *org.StripeCustomerID,
			"value":              "1",
		},
	}
	ev, err := meterevent.New(params)
	if err != nil {
		log.Printf("Stripe meter event reporting failed for org %d quote %s: %v\n", org.ID, quoteID, err)
		return ""
	}
	return ev.Identifier
}

// ResolveStripePriceID maps (planTier, interval) to a configured Stripe Price ID.
func ResolveStripePriceID(planTier string, interval string) (string, error) {
	planTier = strings.ToLower(strings.TrimSpace(planTier))
	interval = strings.ToLower(strings.TrimSpace(interval))
	isAnnual := interval == "annual" || interval == "year" || interval == "yearly"

	switch planTier {
	case "boutique":
		if isAnnual {
			return config.Settings.StripePriceBoutiqueAnnual, nil
		}
		return config.Settings.StripePriceBoutiqueMonthly, nil
	case "growth":
		if isAnnual {
			return config.Settings.StripePriceGrowthAnnual, nil
		}
		return config.Settings.StripePriceGrowthMonthly, nil
	case "enterprise":
		if isAnnual {
			return config.Settings.StripePriceEnterpriseAnnual, nil
		}
		return config.Settings.StripePriceEnterpriseMonth, nil
	default:
		return "", fmt.Errorf("invalid paid plan tier: %s", planTier)
	}
}

func planFromPriceID(priceID string) string {
	switch priceID {
	case config.Settings.StripePriceBoutiqueMonthly, config.Settings.StripePriceBoutiqueAnnual:
		if priceID != "" {
			return "boutique"
		}
	case config.Settings.StripePriceGrowthMonthly, config.Settings.StripePriceGrowthAnnual:
		if priceID != "" {
			return "growth"
		}
	case config.Settings.StripePriceEnterpriseMonth, config.Settings.StripePriceEnterpriseAnnual:
		if priceID != "" {
			return "enterprise"
		}
	}
	return ""
}

// CreateStripeCheckoutSession creates a Stripe Checkout subscription session for an Organization.
func CreateStripeCheckoutSession(gdb *gorm.DB, org *models.Organization, ownerEmail string, planTier string, interval string) (string, error) {
	if config.Settings.StripeSecretKey == "" {
		return "", errors.New("Stripe is not configured on this server (STRIPE_SECRET_KEY missing)")
	}
	priceID, err := ResolveStripePriceID(planTier, interval)
	if err != nil {
		return "", err
	}
	if priceID == "" {
		return "", fmt.Errorf("Stripe Price ID for %s (%s) is not configured in AWS Secrets Manager / environment", planTier, interval)
	}

	stripe.Key = config.Settings.StripeSecretKey

	// Ensure a Stripe Customer exists for this organization
	var customerID string
	if org.StripeCustomerID != nil && *org.StripeCustomerID != "" {
		customerID = *org.StripeCustomerID
	} else {
		cust, err := customer.New(&stripe.CustomerParams{
			Email: stripe.String(ownerEmail),
			Name:  stripe.String(org.Name),
			Metadata: map[string]string{
				"organization_id": fmt.Sprintf("%d", org.ID),
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to create Stripe customer: %w", err)
		}
		customerID = cust.ID
		org.StripeCustomerID = &customerID
		gdb.Model(org).Update("stripe_customer_id", customerID)
	}

	frontendURL := strings.TrimRight(config.Settings.FrontendURL, "/")
	params := &stripe.CheckoutSessionParams{
		Customer:            stripe.String(customerID),
		Mode:                stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		AllowPromotionCodes: stripe.Bool(true),
		ClientReferenceID:   stripe.String(fmt.Sprintf("%d", org.ID)),
		SuccessURL:          stripe.String(frontendURL + "/subscription?checkout=success"),
		CancelURL:           stripe.String(frontendURL + "/subscription?checkout=canceled"),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: map[string]string{
			"organization_id": fmt.Sprintf("%d", org.ID),
			"plan_tier":       planTier,
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"organization_id": fmt.Sprintf("%d", org.ID),
				"plan_tier":       planTier,
			},
		},
	}

	sess, err := checkoutsession.New(params)
	if err != nil {
		return "", fmt.Errorf("failed to create Stripe Checkout session: %w", err)
	}
	return sess.URL, nil
}

// CreateStripePortalSession creates a Stripe Billing Portal session for self-service management.
func CreateStripePortalSession(org *models.Organization) (string, error) {
	if config.Settings.StripeSecretKey == "" {
		return "", errors.New("Stripe is not configured on this server")
	}
	if org.StripeCustomerID == nil || *org.StripeCustomerID == "" {
		return "", errors.New("no active Stripe customer record found for this workspace")
	}

	stripe.Key = config.Settings.StripeSecretKey
	frontendURL := strings.TrimRight(config.Settings.FrontendURL, "/")
	sess, err := portalsession.New(&stripe.BillingPortalSessionParams{
		Customer:  stripe.String(*org.StripeCustomerID),
		ReturnURL: stripe.String(frontendURL + "/subscription"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Stripe billing portal session: %w", err)
	}
	return sess.URL, nil
}

// HandleStripeWebhook verifies the Stripe-Signature header and processes events idempotently.
func HandleStripeWebhook(gdb *gorm.DB, payload []byte, sigHeader string) error {
	if config.Settings.StripeWebhookSecret == "" {
		return errors.New("STRIPE_WEBHOOK_SECRET is not configured")
	}

	event, err := webhook.ConstructEvent(payload, sigHeader, config.Settings.StripeWebhookSecret)
	if err != nil {
		return fmt.Errorf("invalid Stripe signature: %w", err)
	}

	// Idempotency check
	var existing models.StripeWebhookEvent
	if err := gdb.Where("event_id = ?", event.ID).First(&existing).Error; err == nil {
		log.Printf("Stripe webhook event %s already processed; skipping.\n", event.ID)
		return nil
	}

	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return err
		}
		orgIDStr := sess.Metadata["organization_id"]
		if orgIDStr == "" {
			orgIDStr = sess.ClientReferenceID
		}
		planTier := sess.Metadata["plan_tier"]

		var org models.Organization
		if err := gdb.Where("id = ?", orgIDStr).First(&org).Error; err == nil {
			if sess.Customer != nil && sess.Customer.ID != "" {
				cid := sess.Customer.ID
				org.StripeCustomerID = &cid
			}
			if sess.Subscription != nil && sess.Subscription.ID != "" {
				sid := sess.Subscription.ID
				org.StripeSubscriptionID = &sid
			}
			if planTier != "" {
				ApplyPlanLimits(&org, planTier)
			}
			org.BillingStatus = "active"
			gdb.Save(&org)
			log.Printf("Activated Stripe plan %s for organization %d\n", org.PlanTier, org.ID)
		}

	case "customer.subscription.created", "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return err
		}
		var org models.Organization
		if err := findOrgForStripeSub(gdb, &sub, &org); err == nil {
			sid := sub.ID
			org.StripeSubscriptionID = &sid
			org.BillingStatus = string(sub.Status)
			if sub.CurrentPeriodEnd > 0 {
				t := time.Unix(sub.CurrentPeriodEnd, 0).UTC()
				org.CurrentPeriodEnd = &t
			}
			// Sync plan tier from price ID or metadata
			tier := sub.Metadata["plan_tier"]
			if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
				pid := sub.Items.Data[0].Price.ID
				org.StripePriceID = &pid
				if mapped := planFromPriceID(pid); mapped != "" {
					tier = mapped
				}
			}
			if tier != "" {
				ApplyPlanLimits(&org, tier)
			}
			gdb.Save(&org)
		}

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return err
		}
		var org models.Organization
		if err := findOrgForStripeSub(gdb, &sub, &org); err == nil {
			ApplyPlanLimits(&org, "free")
			org.BillingStatus = "canceled"
			org.StripeSubscriptionID = nil
			org.StripePriceID = nil
			gdb.Save(&org)
			log.Printf("Downgraded organization %d after Stripe subscription cancellation\n", org.ID)
		}

	case "invoice.paid":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return err
		}
		if inv.Customer != nil && inv.Customer.ID != "" {
			var org models.Organization
			if err := gdb.Where("stripe_customer_id = ?", inv.Customer.ID).First(&org).Error; err == nil {
				org.BillingStatus = "active"
				org.QuotesUsedThisPeriod = 0
				gdb.Save(&org)
			}
		}

	case "invoice.payment_failed":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return err
		}
		if inv.Customer != nil && inv.Customer.ID != "" {
			gdb.Model(&models.Organization{}).
				Where("stripe_customer_id = ?", inv.Customer.ID).
				Update("billing_status", "past_due")
		}
	}

	// Mark event processed
	_ = gdb.Create(&models.StripeWebhookEvent{
		EventID:     event.ID,
		EventType:   string(event.Type),
		ProcessedAt: time.Now().UTC(),
	}).Error

	return nil
}

func findOrgForStripeSub(gdb *gorm.DB, sub *stripe.Subscription, org *models.Organization) error {
	if orgIDStr := sub.Metadata["organization_id"]; orgIDStr != "" {
		if err := gdb.Where("id = ?", orgIDStr).First(org).Error; err == nil {
			return nil
		}
	}
	if sub.Customer != nil && sub.Customer.ID != "" {
		if err := gdb.Where("stripe_customer_id = ?", sub.Customer.ID).First(org).Error; err == nil {
			return nil
		}
	}
	return gdb.Where("stripe_subscription_id = ?", sub.ID).First(org).Error
}
