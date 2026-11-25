package services

import (
	"fmt"
	"html"
	"log"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

// RecordTransition saves a state transition audit log.
func RecordTransition(gdb *gorm.DB, quoteID string, fromStatus, toStatus string, notes string) error {
	transition := models.StateTransition{
		FreightQuoteID: quoteID,
		FromStatus:     fromStatus,
		ToStatus:       toStatus,
		Timestamp:      time.Now().UTC(),
		Notes:          &notes,
	}
	if err := gdb.Create(&transition).Error; err != nil {
		return err
	}
	log.Printf("Transitioned %s from %s to %s. Notes: %s\n", quoteID, fromStatus, toStatus, notes)
	return nil
}

func getOrgRoundDurations(gdb *gorm.DB, orgID uint) (round1 time.Duration, round2 time.Duration) {
	// Safe production defaults: 2 hours Round 1, 30 minutes Round 2
	r1Mins := 120
	r2Mins := 30
	if !config.Settings.IsProd() {
		r1Mins = 2
		r2Mins = 1
	}

	if orgID > 0 {
		var org models.Organization
		if err := gdb.First(&org, orgID).Error; err == nil {
			if org.Round1DurationMinutes >= 1 && org.Round1DurationMinutes <= 1440 {
				r1Mins = org.Round1DurationMinutes
			}
			if org.Round2DurationMinutes >= 1 && org.Round2DurationMinutes <= 720 {
				r2Mins = org.Round2DurationMinutes
			}
		}
	}
	return time.Duration(r1Mins) * time.Minute, time.Duration(r2Mins) * time.Minute
}

// TransitionQuote executes state transitions and triggers org-scoped workflow actions.
func TransitionQuote(gdb *gorm.DB, quote *models.FreightQuote, toStatus string, notes string) error {
	fromStatus := quote.Status
	if fromStatus == toStatus {
		return nil
	}

	quote.Status = toStatus
	quote.UpdatedAt = time.Now().UTC()
	if err := gdb.Save(quote).Error; err != nil {
		return err
	}

	if err := RecordTransition(gdb, quote.ID, fromStatus, toStatus, notes); err != nil {
		return err
	}

	r1Duration, r2Duration := getOrgRoundDurations(gdb, quote.OrganizationID)

	switch toStatus {
	case "OUT_TO_CARRIERS":
		endsAt := time.Now().UTC().Add(r1Duration)
		quote.FirstRoundEndsAt = &endsAt
		gdb.Save(quote)

		var carriers []models.Carrier
		if err := gdb.Where("organization_id = ?", quote.OrganizationID).Find(&carriers).Error; err == nil {
			for _, carrier := range carriers {
				pickupStr := quote.PickupDate.Format("2006-01-02")
				hazmatStr := "No"
				if quote.Hazmat {
					hazmatStr = "Yes"
				}
				accessorialsStr := "None"
				if quote.Accessorials != nil && *quote.Accessorials != "" {
					accessorialsStr = *quote.Accessorials
				}

				emailBody := fmt.Sprintf(`
					<h3>Freight Bid Request - Quote %s</h3>
					<p>We require competitive bidding for the following shipment:</p>
					<ul>
						<li><b>Origin:</b> %s</li>
						<li><b>Destination:</b> %s</li>
						<li><b>Weight:</b> %.2f lbs</li>
						<li><b>Class:</b> %s</li>
						<li><b>Hazmat:</b> %s</li>
						<li><b>Pickup Date:</b> %s</li>
						<li><b>Accessorials required:</b> %s</li>
					</ul>
					<p>Please reply to this email with your rate (e.g. "$1,200") and estimated transit days.</p>
				`,
					html.EscapeString(quote.ID),
					html.EscapeString(quote.Origin),
					html.EscapeString(quote.Destination),
					quote.WeightLbs,
					html.EscapeString(getSafeString(quote.FreightClass)),
					hazmatStr,
					pickupStr,
					html.EscapeString(accessorialsStr),
				)

				subj := fmt.Sprintf("RFQ: Freight Quote %s - %s to %s", quote.ID, quote.Origin, quote.Destination)
				SendEmailForOrg(gdb, quote.OrganizationID, carrier.Email, subj, emailBody)

				rfq := models.RequestForQuote{
					FreightQuoteID: quote.ID,
					CarrierID:      carrier.ID,
					SupplierID:     carrier.ID,
					Status:         "SENT",
					SentAt:         time.Now().UTC(),
					Subject:        getPtr(subj),
					Body:           &emailBody,
				}
				gdb.Create(&rfq)
			}
		}

	case "RE_BID_ROUND":
		var bids []models.CarrierBid
		gdb.Where("freight_quote_id = ? AND round = 1 AND bid_amount >= ?", quote.ID, MinValidBidAmount).Find(&bids)

		// FINANCIAL SAFETY FIX: NEVER fabricate a fake $1,500 carrier bid when no carriers respond.
		if len(bids) == 0 {
			reason := "No valid carrier bids received before Round 1 closed; broker intervention required"
			quote.Status = "NO_BIDS_ALERT"
			quote.LostReason = &reason
			quote.UpdatedAt = time.Now().UTC()
			gdb.Save(quote)
			_ = RecordTransition(gdb, quote.ID, "RE_BID_ROUND", "NO_BIDS_ALERT", reason)

			// Notify organization owner(s) so a human broker can follow up safely
			var owners []models.User
			gdb.Where("organization_id = ? AND role IN ? AND is_active = true", quote.OrganizationID, []string{"owner", "admin"}).Find(&owners)
			for _, owner := range owners {
				alertBody := fmt.Sprintf(
					"<h3>Action Required: No Carrier Bids Received (%s)</h3><p>Round 1 closed for lane <b>%s &rarr; %s</b> (%.0f lbs) with zero valid carrier bids. No customer quote was sent.</p>",
					html.EscapeString(quote.ID),
					html.EscapeString(quote.Origin),
					html.EscapeString(quote.Destination),
					quote.WeightLbs,
				)
				SendEmail(owner.Email, fmt.Sprintf("ACTION REQUIRED: No Bids for Quote %s", quote.ID), alertBody, "")
			}
			return nil
		}

		lowestBid := bids[0]
		for _, b := range bids {
			if b.BidAmount < lowestBid.BidAmount {
				lowestBid = b
			}
		}
		log.Printf("Round 1 lowest bid for %s is $%.2f by carrier ID %d\n", quote.ID, lowestBid.BidAmount, lowestBid.CarrierID)

		endsAt := time.Now().UTC().Add(r2Duration)
		quote.RebidRoundEndsAt = &endsAt
		gdb.Save(quote)

		var carriers []models.Carrier
		if err := gdb.Where("organization_id = ?", quote.OrganizationID).Find(&carriers).Error; err == nil {
			for _, carrier := range carriers {
				if carrier.ID == lowestBid.CarrierID {
					leadBody := fmt.Sprintf(`
						<h3>Bid Status Update - Quote %s</h3>
						<p>You are currently the lowest bidder at <b>$%.2f</b>. We will lock final bids shortly.</p>
					`, html.EscapeString(quote.ID), lowestBid.BidAmount)

					subj := fmt.Sprintf("Bid Leading: Quote %s", quote.ID)
					SendEmailForOrg(gdb, quote.OrganizationID, carrier.Email, subj, leadBody)

					gdb.Create(&models.RequestForQuote{
						FreightQuoteID: quote.ID,
						CarrierID:      carrier.ID,
						SupplierID:     carrier.ID,
						Status:         "LEADING_NOTIFIED",
						SentAt:         time.Now().UTC(),
						Subject:        getPtr(subj),
						Body:           &leadBody,
					})
				} else {
					rebidBody := fmt.Sprintf(`
						<h3>Bid Status Update - Quote %s</h3>
						<p>The current lowest bid for this lane is <b>$%.2f</b>.</p>
						<p>If you can beat this rate, please reply to this email with your best and final offer within the next %d minutes.</p>
					`, html.EscapeString(quote.ID), lowestBid.BidAmount, int(r2Duration.Minutes()))

					subj := fmt.Sprintf("RE-BID: Quote %s - Current Low $%.2f", quote.ID, lowestBid.BidAmount)
					SendEmailForOrg(gdb, quote.OrganizationID, carrier.Email, subj, rebidBody)

					gdb.Create(&models.RequestForQuote{
						FreightQuoteID: quote.ID,
						CarrierID:      carrier.ID,
						SupplierID:     carrier.ID,
						Status:         "RE_BID_SENT",
						SentAt:         time.Now().UTC(),
						Subject:        getPtr(subj),
						Body:           &rebidBody,
					})
				}
			}
		}

	case "QUOTE_SENT":
		var allBids []models.CarrierBid
		gdb.Preload("Carrier").
			Where("freight_quote_id = ? AND bid_amount >= ?", quote.ID, MinValidBidAmount).
			Find(&allBids)

		if len(allBids) == 0 {
			quote.Status = "NO_BIDS_ALERT"
			lostReason := "No valid bids received from any carrier"
			quote.LostReason = &lostReason
			gdb.Save(quote)
			_ = RecordTransition(gdb, quote.ID, "QUOTE_SENT", "NO_BIDS_ALERT", lostReason)
			return nil
		}

		winningBid := allBids[0]
		for _, b := range allBids {
			if b.BidAmount < winningBid.BidAmount {
				winningBid = b
			}
		}

		gdb.Model(&models.CarrierBid{}).Where("freight_quote_id = ?", quote.ID).Update("is_winning", false)
		gdb.Model(&winningBid).Update("is_winning", true)

		quote.WinningCarrierID = &winningBid.CarrierID
		quote.CostPrice = winningBid.BidAmount

		var customer models.Customer
		markupPct := 10.0
		if err := gdb.Where("id = ? AND organization_id = ?", quote.CustomerID, quote.OrganizationID).First(&customer).Error; err == nil {
			markupPct = customer.DefaultMarkupPercent
		}

		quote.MarkupPercent = markupPct
		quote.SellPrice = math.Round(quote.CostPrice*(1+markupPct/100.0)*100.0) / 100.0
		quote.MarginAmt = math.Round((quote.SellPrice-quote.CostPrice)*100.0) / 100.0
		if quote.SellPrice > 0 {
			quote.MarginPct = math.Round((quote.MarginAmt/quote.SellPrice)*100.0*100.0) / 100.0
		} else {
			quote.MarginPct = 0.0
		}

		expiresAt := time.Now().UTC().Add(24 * time.Hour)
		quote.QuoteExpiresAt = &expiresAt
		gdb.Save(quote)

		if customer.Email != "" {
			transitDays := 3
			if winningBid.TransitTimeDays > 0 {
				transitDays = winningBid.TransitTimeDays
			}
			serviceLevel := "Standard LTL"
			if winningBid.ServiceLevel != nil && *winningBid.ServiceLevel != "" {
				serviceLevel = *winningBid.ServiceLevel
			}

			customerBody := fmt.Sprintf(`
				<h3>Freight Quote Proposal - Ref: %s</h3>
				<p>We are pleased to offer the following competitive freight rate:</p>
				<table border="1" cellpadding="8" style="border-collapse:collapse;">
					<tr><td><b>Route:</b></td><td>%s to %s</td></tr>
					<tr><td><b>Weight:</b></td><td>%.2f lbs</td></tr>
					<tr><td><b>Transit Time:</b></td><td>%d Days</td></tr>
					<tr><td><b>Service Level:</b></td><td>%s</td></tr>
					<tr><td><b>Total Cost:</b></td><td><b>$%.2f</b> (includes all accessorials)</td></tr>
				</table>
				<p>This quote is valid for 24 hours. Please reply directly from <b>%s</b> with <b>"APPROVED"</b> to book, or <b>"REJECTED"</b> to decline.</p>
			`,
				html.EscapeString(quote.ID),
				html.EscapeString(quote.Origin),
				html.EscapeString(quote.Destination),
				quote.WeightLbs,
				transitDays,
				html.EscapeString(serviceLevel),
				quote.SellPrice,
				html.EscapeString(customer.Email),
			)

			SendEmailForOrg(
				gdb,
				quote.OrganizationID,
				customer.Email,
				fmt.Sprintf("FREIGHT PROPOSAL: %s to %s - $%.2f (%s)", quote.Origin, quote.Destination, quote.SellPrice, quote.ID),
				customerBody,
			)
		}

		quote.Status = "AWAITING_APPROVAL"
		gdb.Save(quote)
		_ = RecordTransition(gdb, quote.ID, "QUOTE_SENT", "AWAITING_APPROVAL", "Quote generated and sent to customer")

	case "APPROVED":
		var winningCarrier models.Carrier
		if quote.WinningCarrierID != nil {
			if err := gdb.Where("id = ? AND organization_id = ?", *quote.WinningCarrierID, quote.OrganizationID).First(&winningCarrier).Error; err == nil {
				carrierBody := fmt.Sprintf(`
					<h3>Booking Confirmation - Ref: %s</h3>
					<p>Your bid of <b>$%.2f</b> has been selected. Please proceed with booking.</p>
					<p>Shipment details:</p>
					<ul>
						<li><b>Origin:</b> %s</li>
						<li><b>Destination:</b> %s</li>
						<li><b>Weight:</b> %.2f lbs</li>
					</ul>
					<p>Please upload the signed BOL and schedule pickup.</p>
				`,
					html.EscapeString(quote.ID),
					quote.CostPrice,
					html.EscapeString(quote.Origin),
					html.EscapeString(quote.Destination),
					quote.WeightLbs,
				)

				SendEmailForOrg(
					gdb,
					quote.OrganizationID,
					winningCarrier.Email,
					fmt.Sprintf("BOOKING CONFIRMED: Freight Quote %s", quote.ID),
					carrierBody,
				)
			}
		}

		carrierName := "Assigned Carrier"
		if winningCarrier.Name != "" {
			carrierName = winningCarrier.Name
		}

		var customer models.Customer
		gdb.Where("id = ? AND organization_id = ?", quote.CustomerID, quote.OrganizationID).First(&customer)
		customerName := "Customer"
		if customer.Name != "" {
			customerName = customer.Name
		}

		// Generate real private S3-backed BOL and Invoice documents
		bolRes := GenerateBOLDocument(quote, carrierName, customerName)
		quote.BolS3Key = &bolRes.S3Key
		bolApiPath := fmt.Sprintf("/api/quotes/%s/document-url?type=bol", quote.ID)
		quote.BolURL = &bolApiPath

		invRes := GenerateInvoiceDocument(quote, customerName)
		quote.InvoiceS3Key = &invRes.S3Key
		invApiPath := fmt.Sprintf("/api/quotes/%s/document-url?type=invoice", quote.ID)
		quote.InvoiceURL = &invApiPath

		quote.PaymentStatus = "PENDING"
		quote.Status = "IN_TRANSIT"
		gdb.Save(quote)

		_ = RecordTransition(gdb, quote.ID, "APPROVED", "IN_TRANSIT", "Carrier notified, private S3 BOL & Commercial Invoice generated.")
	}

	return nil
}

// CheckPendingTimers processes elapsed timers across all active organizations (invoked by EventBridge Cron).
func CheckPendingTimers(gdb *gorm.DB) {
	checkPendingTimersInternal(gdb, 0)
}

// CheckPendingTimersForOrg processes elapsed timers strictly for one organization (used by dev simulator).
func CheckPendingTimersForOrg(gdb *gorm.DB, orgID uint) {
	if orgID == 0 {
		return
	}
	checkPendingTimersInternal(gdb, orgID)
}

func checkPendingTimersInternal(gdb *gorm.DB, orgID uint) {
	now := time.Now().UTC()

	scope := func(q *gorm.DB) *gorm.DB {
		if orgID > 0 {
			return q.Where("organization_id = ?", orgID)
		}
		return q
	}

	// 1. Round 1 expiry
	var quotesR1 []models.FreightQuote
	scope(gdb.Where("status = ? AND first_round_ends_at <= ?", "OUT_TO_CARRIERS", now)).Find(&quotesR1)
	for _, q := range quotesR1 {
		log.Printf("Timer expired for Round 1 of quote %s (Org %d). Progressing to re-bid.\n", q.ID, q.OrganizationID)
		_ = TransitionQuote(gdb, &q, "FIRST_ROUND_RECEIVED", "Round 1 timer expired")
		_ = TransitionQuote(gdb, &q, "RE_BID_ROUND", "Triggering competitive re-bid round")
	}

	// 2. Round 2 expiry
	var quotesR2 []models.FreightQuote
	scope(gdb.Where("status = ? AND rebid_round_ends_at <= ?", "RE_BID_ROUND", now)).Find(&quotesR2)
	for _, q := range quotesR2 {
		log.Printf("Timer expired for Round 2 of quote %s (Org %d). Finalizing bids.\n", q.ID, q.OrganizationID)
		_ = TransitionQuote(gdb, &q, "QUOTE_SENT", "Round 2 timer expired, finalizing bids")
	}

	// 3. Proposal expiration
	var expiredQuotes []models.FreightQuote
	scope(gdb.Where("status = ? AND quote_expires_at <= ?", "AWAITING_APPROVAL", now)).Find(&expiredQuotes)
	for _, q := range expiredQuotes {
		log.Printf("Quote proposal %s expired without customer approval. Marking LOST.\n", q.ID)
		lostReason := "Quote Expired"
		q.LostReason = &lostReason
		_ = TransitionQuote(gdb, &q, "LOST", "Proposal validation window expired")
	}
}

func getPtr(s string) *string {
	return &s
}
