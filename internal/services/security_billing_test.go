package services

import (
	"testing"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

func TestValidateParsedBidRejectsPromptInjectionAndZeroBids(t *testing.T) {
	if err := ValidateParsedBid(ExtractedCarrierBid{BidAmount: 0, TransitTimeDays: 3}); err == nil {
		t.Fatal("expected $0 bid to be rejected")
	}
	if err := ValidateParsedBid(ExtractedCarrierBid{BidAmount: -500, TransitTimeDays: 3}); err == nil {
		t.Fatal("expected negative bid to be rejected")
	}
	if err := ValidateParsedBid(ExtractedCarrierBid{BidAmount: 10, TransitTimeDays: 3}); err == nil {
		t.Fatal("expected $10 below-floor bid to be rejected")
	}
	if err := ValidateParsedBid(ExtractedCarrierBid{BidAmount: 1250.50, TransitTimeDays: 3}); err != nil {
		t.Fatalf("expected valid $1250.50 bid to pass, got %v", err)
	}
}

func TestOptionAQuotaEnforcement(t *testing.T) {
	freeOrg := &models.Organization{
		PlanTier:             "free",
		BillingStatus:        "trialing",
		MonthlyQuoteLimit:    25,
		QuotesUsedThisPeriod: 25,
	}
	allowed, _, _ := CanCreateQuote(freeOrg)
	if allowed {
		t.Fatal("expected free org at 25/25 quotes to be hard-capped")
	}

	growthOrg := &models.Organization{
		PlanTier:             "growth",
		BillingStatus:        "active",
		MonthlyQuoteLimit:    750,
		QuotesUsedThisPeriod: 750,
	}
	allowed, isOverage, _ := CanCreateQuote(growthOrg)
	if !allowed || !isOverage {
		t.Fatalf("expected growth org at 750/750 to allow metered overage, got allowed=%v overage=%v", allowed, isOverage)
	}

	canceledOrg := &models.Organization{
		PlanTier:             "boutique",
		BillingStatus:        "canceled",
		MonthlyQuoteLimit:    150,
		QuotesUsedThisPeriod: 0,
	}
	allowed, _, _ = CanCreateQuote(canceledOrg)
	if allowed {
		t.Fatal("expected canceled org to be blocked from creating quotes")
	}
}
