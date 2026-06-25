package services

import (
	"fmt"
	"log"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

// RecordTransition saves a state transition audit log
func RecordTransition(db *gorm.DB, quoteID string, fromStatus, toStatus string, notes string) error {
	transition := models.StateTransition{
		FreightQuoteID: quoteID,
		FromStatus:     fromStatus,
		ToStatus:       toStatus,
		Timestamp:      time.Now(),
		Notes:          &notes,
	}
	if err := db.Create(&transition).Error; err != nil {
		return err
	}
	log.Printf("Transitioned %s from %s to %s. Notes: %s\n", quoteID, fromStatus, toStatus, notes)
	return nil
}

// TransitionQuote executes state transitions and triggers actions
func TransitionQuote(db *gorm.DB, quote *models.FreightQuote, toStatus string, notes string) error {
	fromStatus := quote.Status
	if fromStatus == toStatus {
		return nil
	}

	quote.Status = toStatus
	quote.UpdatedAt = time.Now()
	if err := db.Save(quote).Error; err != nil {
		return err
	}

	if err := RecordTransition(db, quote.ID, fromStatus, toStatus, notes); err != nil {
		return err
	}

	// Trigger side effects
	switch toStatus {
	case "OUT_TO_CARRIERS":
		// 1. Start the first round timer: 2 hours (or 2 minutes in simulation fast-mode)
		isFast := true // Default to fast-mode for demo
		duration := 2 * time.Hour
		if isFast {
			duration = 2 * time.Minute
		}
		endsAt := time.Now().Add(duration)
		quote.FirstRoundEndsAt = &endsAt
		db.Save(quote)

		// 2. Email all carriers belonging to this user
		var carriers []models.Carrier
		carrierQuery := db.Session(&gorm.Session{})
		if quote.UserID != nil {
			carrierQuery = carrierQuery.Where("user_id = ?", *quote.UserID)
		}
		if err := carrierQuery.Find(&carriers).Error; err == nil {
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
					<p>Please reply to this email with your rate (e.g. "$1,200") and transit days.</p>
				`, quote.ID, quote.Origin, quote.Destination, quote.WeightLbs, getSafeString(quote.FreightClass), hazmatStr, pickupStr, accessorialsStr)

				SendEmail(
					carrier.Email,
					fmt.Sprintf("RFQ: Freight Quote %s - %s to %s", quote.ID, quote.Origin, quote.Destination),
					emailBody,
					"",
				)

				// Record RFQ
				rfq := models.RequestForQuote{
					FreightQuoteID: quote.ID,
					CarrierID:      carrier.ID,
					SupplierID:     carrier.ID,
					Status:         "SENT",
					SentAt:         time.Now(),
					Subject:        getPtr(fmt.Sprintf("RFQ: Freight Quote %s - %s to %s", quote.ID, quote.Origin, quote.Destination)),
					Body:           &emailBody,
				}
				db.Create(&rfq)
			}
		}

	case "RE_BID_ROUND":
		// 1. Identify lowest rate from Round 1
		var bids []models.CarrierBid
		db.Where("freight_quote_id = ? AND round = 1", quote.ID).Find(&bids)

		if len(bids) == 0 {
			// Seed a mock bid if none received
			var mockCarrier models.Carrier
			mockQuery := db.Session(&gorm.Session{})
			if quote.UserID != nil {
				mockQuery = mockQuery.Where("user_id = ?", *quote.UserID)
			}
			if err := mockQuery.First(&mockCarrier).Error; err == nil {
				notesStr := "Auto-generated due to no carrier responses."
				bid := models.CarrierBid{
					FreightQuoteID:   quote.ID,
					CarrierID:        mockCarrier.ID,
					Round:            1,
					BidAmount:        1500.0,
					TransitTimeDays:  4,
					PickupWindow:     getPtr("09:00 - 17:00"),
					AccessorialsText: getPtr("None"),
					ServiceLevel:     getPtr("Standard"),
					Notes:            &notesStr,
					IsWinning:        false,
					ReceivedAt:       time.Now(),
				}
				db.Create(&bid)
				bids = []models.CarrierBid{bid}
			}
		}

		if len(bids) > 0 {
			lowestBid := bids[0]
			for _, b := range bids {
				if b.BidAmount < lowestBid.BidAmount {
					lowestBid = b
				}
			}
			log.Printf("Round 1 lowest bid for %s is $%.2f by carrier ID %d\n", quote.ID, lowestBid.BidAmount, lowestBid.CarrierID)

			// 2. Trigger Best and Final round
			isFast := true
			duration := 30 * time.Minute
			if isFast {
				duration = 30 * time.Second
			}
			endsAt := time.Now().Add(duration)
			quote.RebidRoundEndsAt = &endsAt
			db.Save(quote)

			// 3. Notify other carriers
			var carriers []models.Carrier
			carrierQuery := db.Session(&gorm.Session{})
			if quote.UserID != nil {
				carrierQuery = carrierQuery.Where("user_id = ?", *quote.UserID)
			}
			if err := carrierQuery.Find(&carriers).Error; err == nil {
				for _, carrier := range carriers {
					if carrier.ID == lowestBid.CarrierID {
						// Notify they are currently leading
						leadBody := fmt.Sprintf(`
							<h3>Bid Status Update - Quote %s</h3>
							<p>You are currently the lowest bidder at <b>$%.2f</b>. We will lock final bids shortly.</p>
						`, quote.ID, lowestBid.BidAmount)

						SendEmail(
							carrier.Email,
							fmt.Sprintf("Bid Leading: Quote %s", quote.ID),
							leadBody,
							"",
						)

						rfq := models.RequestForQuote{
							FreightQuoteID: quote.ID,
							CarrierID:      carrier.ID,
							SupplierID:     carrier.ID,
							Status:         "LEADING_NOTIFIED",
							SentAt:         time.Now(),
							Subject:        getPtr(fmt.Sprintf("Bid Leading: Quote %s", quote.ID)),
							Body:           &leadBody,
						}
						db.Create(&rfq)
					} else {
						rebidBody := fmt.Sprintf(`
							<h3>Bid Status Update - Quote %s</h3>
							<p>The current lowest bid for this lane is <b>$%.2f</b>.</p>
							<p>If you can beat this rate, please reply to this email with your best and final offer within the next 30 minutes.</p>
						`, quote.ID, lowestBid.BidAmount)

						SendEmail(
							carrier.Email,
							fmt.Sprintf("RE-BID: Quote %s - Current Low $%.2f", quote.ID, lowestBid.BidAmount),
							rebidBody,
							"",
						)

						rfq := models.RequestForQuote{
							FreightQuoteID: quote.ID,
							CarrierID:      carrier.ID,
							SupplierID:     carrier.ID,
							Status:         "RE_BID_SENT",
							SentAt:         time.Now(),
							Subject:        getPtr(fmt.Sprintf("RE-BID: Quote %s - Current Low $%.2f", quote.ID, lowestBid.BidAmount)),
							Body:           &rebidBody,
						}
						db.Create(&rfq)
					}
				}
			}
		}

	case "QUOTE_SENT":
		// 1. Lock winning rate
		var allBids []models.CarrierBid
		db.Preload("Carrier").Where("freight_quote_id = ?", quote.ID).Find(&allBids)

		if len(allBids) == 0 {
			quote.Status = "LOST"
			lostReason := "No bids received from any carrier"
			quote.LostReason = &lostReason
			db.Save(quote)
			return nil
		}

		winningBid := allBids[0]
		for _, b := range allBids {
			if b.BidAmount < winningBid.BidAmount {
				winningBid = b
			}
		}

		// Update winning bid
		db.Model(&winningBid).Update("is_winning", true)

		quote.WinningCarrierID = &winningBid.CarrierID
		quote.CostPrice = winningBid.BidAmount

		// 2. Apply customer specific markup
		var customer models.Customer
		markupPct := 10.0
		if err := db.Where("id = ?", quote.CustomerID).First(&customer).Error; err == nil {
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

		// 3. Set quote expiration (24 hours)
		expiresAt := time.Now().Add(24 * time.Hour)
		quote.QuoteExpiresAt = &expiresAt
		db.Save(quote)

		// 4. Email customer structured quote
		customerEmail := "customer@example.com"
		if customer.Email != "" {
			customerEmail = customer.Email
		}

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
			<p>This quote is valid for 24 hours. Please reply to this email with <b>"APPROVED"</b> to book, or <b>"REJECTED"</b> to decline.</p>
		`, quote.ID, quote.Origin, quote.Destination, quote.WeightLbs, transitDays, serviceLevel, quote.SellPrice)

		SendEmail(
			customerEmail,
			fmt.Sprintf("FREIGHT PROPOSAL: %s to %s - $%.2f (%s)", quote.Origin, quote.Destination, quote.SellPrice, quote.ID),
			customerBody,
			"",
		)

		// Transition automatically to AWAITING_APPROVAL
		quote.Status = "AWAITING_APPROVAL"
		db.Save(quote)
		_ = RecordTransition(db, quote.ID, "QUOTE_SENT", "AWAITING_APPROVAL", "Quote generated and sent to customer")

	case "APPROVED":
		// 1. Notify winning carrier
		var winningCarrier models.Carrier
		if quote.WinningCarrierID != nil {
			if err := db.Where("id = ?", *quote.WinningCarrierID).First(&winningCarrier).Error; err == nil {
				carrierBody := fmt.Sprintf(`
					<h3>Booking Confirmation - Ref: %s</h3>
					<p>Your bid of <b>$%.2f</b> has been selected. Please proceed with booking.</p>
					<p>Shipment details:</p>
					<ul>
						<li><b>Origin:</b> %s</li>
						<li><b>Destination:</b> %s</li>
						<li><b>Weight:</b> %.2f lbs</li>
					</ul>
					<p>Please upload the BOL and schedule pickup.</p>
				`, quote.ID, quote.CostPrice, quote.Origin, quote.Destination, quote.WeightLbs)

				SendEmail(
					winningCarrier.Email,
					fmt.Sprintf("BOOKING CONFIRMED: Freight Quote %s", quote.ID),
					carrierBody,
					"",
				)
			}
		}

		// 2. Generate BOL and Invoice mock
		carrierName := "Carrier"
		if winningCarrier.Name != "" {
			carrierName = winningCarrier.Name
		}
		bolRes := GenerateMockBOL(quote.ID, carrierName)
		quote.BolURL = &bolRes.BOLURL

		var customer models.Customer
		db.Where("id = ?", quote.CustomerID).First(&customer)
		customerName := "Customer"
		if customer.Name != "" {
			customerName = customer.Name
		}

		invRes := GenerateMockInvoice(quote.ID, customerName, quote.SellPrice)
		quote.InvoiceURL = &invRes.InvoiceURL

		quote.PaymentStatus = "PENDING"
		quote.Status = "IN_TRANSIT"
		db.Save(quote)

		_ = RecordTransition(db, quote.ID, "APPROVED", "IN_TRANSIT", "Carrier notified, BOL generated, Invoice generated.")
	}

	return nil
}

// CheckPendingTimers processes elapsed timers in EventBridge scheduler
func CheckPendingTimers(db *gorm.DB) {
	now := time.Now()

	// 1. Check Round 1
	var quotesR1 []models.FreightQuote
	db.Where("status = ? AND first_round_ends_at <= ?", "OUT_TO_CARRIERS", now).Find(&quotesR1)
	for _, q := range quotesR1 {
		log.Printf("Timer expired for Round 1 of quote %s. Progressing to re-bid.\n", q.ID)
		_ = TransitionQuote(db, &q, "FIRST_ROUND_RECEIVED", "Round 1 timer expired")
		_ = TransitionQuote(db, &q, "RE_BID_ROUND", "Triggering competitive re-bid round")
	}

	// 2. Check Round 2
	var quotesR2 []models.FreightQuote
	db.Where("status = ? AND rebid_round_ends_at <= ?", "RE_BID_ROUND", now).Find(&quotesR2)
	for _, q := range quotesR2 {
		log.Printf("Timer expired for Round 2 of quote %s. Progressing to final quote formulation.\n", q.ID)
		_ = TransitionQuote(db, &q, "QUOTE_SENT", "Round 2 timer expired, finalizing bids")
	}

	// 3. Check Quote Expiration
	var expiredQuotes []models.FreightQuote
	db.Where("status = ? AND quote_expires_at <= ?", "AWAITING_APPROVAL", now).Find(&expiredQuotes)
	for _, q := range expiredQuotes {
		log.Printf("Quote proposal %s has expired without approval. Marking as LOST.\n", q.ID)
		lostReason := "Quote Expired"
		q.LostReason = &lostReason
		_ = TransitionQuote(db, &q, "LOST", "Proposal validation window expired")
	}
}

func getPtr(s string) *string {
	return &s
}
