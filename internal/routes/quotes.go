package routes

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

type FreightQuoteCreate struct {
	CustomerID   uint      `json:"customer_id" binding:"required"`
	Origin       string    `json:"origin" binding:"required"`
	Destination  string    `json:"destination" binding:"required"`
	WeightLbs    float64   `json:"weight_lbs" binding:"required,gt=0"`
	Dimensions   string    `json:"dimensions"`
	FreightClass string    `json:"freight_class"`
	Hazmat       bool      `json:"hazmat"`
	Accessorials string    `json:"accessorials"`
	PickupDate   time.Time `json:"pickup_date" binding:"required"`
}

type QuoteApprovalRequest struct {
	Approved       bool   `json:"approved"`
	LostReason     string `json:"lost_reason"`
	CompetitorInfo string `json:"competitor_info"`
}

type ManualOverridePayload struct {
	ToStatus string `json:"to_status" binding:"required"`
	Notes    string `json:"notes"`
}

type HistoricalRAGResult struct {
	ID             string  `gorm:"column:id" json:"id"`
	Origin         string  `gorm:"column:origin" json:"origin"`
	Destination    string  `gorm:"column:destination" json:"destination"`
	WeightLbs      float64 `gorm:"column:weight_lbs" json:"weight_lbs"`
	WinningCarrier string  `gorm:"column:winning_carrier" json:"winning_carrier"`
	CostPrice      float64 `gorm:"column:cost_price" json:"cost_price"`
	SellPrice      float64 `gorm:"column:sell_price" json:"sell_price"`
	MarginPct      float64 `gorm:"column:margin_pct" json:"margin_pct"`
	Similarity     float64 `json:"similarity"`
	Status         string  `gorm:"column:status" json:"status"`
	Distance       float64 `gorm:"column:distance" json:"-"`
}

func RegisterQuoteRoutes(rg *gin.RouterGroup) {
	quotes := rg.Group("/quotes", AuthMiddleware())
	{
		quotes.GET("", getQuotes)
		quotes.GET("/:quote_id", getQuote)
		quotes.POST("", createQuote)
		quotes.POST("/:quote_id/approve", approveQuote)
		quotes.POST("/:quote_id/manual-override", manualOverride)
		quotes.GET("/:quote_id/historical-rag", getHistoricalRAG)
		quotes.POST("/:quote_id/upload-url", getPresignedUploadURL)
		quotes.GET("/:quote_id/document-url", getPresignedDocumentURL)
	}
}

func getQuotes(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var quotes []models.FreightQuote
	err := db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		Where("organization_id = ?", org.ID).
		Order("created_at desc").
		Find(&quotes).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to list quotes"})
		return
	}

	c.JSON(http.StatusOK, quotes)
}

func getQuote(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	quoteID := c.Param("quote_id")

	var quote models.FreightQuote
	err := db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		Where("id = ? AND organization_id = ?", quoteID, org.ID).
		First(&quote).Error

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	c.JSON(http.StatusOK, quote)
}

func createQuote(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	// Enforce Option A subscription & monthly quote allowance
	allowed, isOverage, reason := services.CanCreateQuote(org)
	if !allowed {
		c.JSON(http.StatusPaymentRequired, gin.H{"detail": reason})
		return
	}

	var payload FreightQuoteCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// Verify customer belongs to caller's organization
	var customer models.Customer
	if err := db.DB.Where("id = ? AND organization_id = ?", payload.CustomerID, org.ID).First(&customer).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid Customer ID"})
		return
	}

	quoteID := services.NextQuoteID(db.DB)

	dims := payload.Dimensions
	if dims == "" {
		dims = "48x48x48"
	}
	fClass := payload.FreightClass
	if fClass == "" {
		fClass = "70"
	}

	// BUG FIX: include payload.Destination (previously payload.FreightClass was passed twice)
	description := fmt.Sprintf("Origin: %s, Destination: %s, Class: %s, Weight: %.2f lbs", payload.Origin, payload.Destination, fClass, payload.WeightLbs)
	vector := services.GetEmbedding(description)
	approvalTok, _ := utils.GenerateRandomToken(8)

	quote := models.FreightQuote{
		ID:             quoteID,
		OrganizationID: org.ID,
		UserID:         &currentUser.ID,
		CustomerID:     payload.CustomerID,
		Status:         "INTAKE",
		Origin:         strings.TrimSpace(payload.Origin),
		Destination:    strings.TrimSpace(payload.Destination),
		WeightLbs:      payload.WeightLbs,
		Dimensions:     &dims,
		FreightClass:   &fClass,
		Hazmat:         payload.Hazmat,
		Accessorials:   &payload.Accessorials,
		PickupDate:     payload.PickupDate,
		ApprovalToken:  &approvalTok,
		ShipmentVector: services.FormatVector(vector),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	if err := db.DB.Create(&quote).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create freight quote"})
		return
	}

	services.RecordQuoteUsage(db.DB, org, quote.ID, isOverage)

	_ = services.TransitionQuote(db.DB, &quote, "INTAKE", "Quote ingest parsed successfully")
	_ = services.TransitionQuote(db.DB, &quote, "OUT_TO_CARRIERS", "RFQ requests sent out to carriers")

	db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		First(&quote, "id = ?", quote.ID)

	c.JSON(http.StatusOK, quote)
}

func approveQuote(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	quoteID := c.Param("quote_id")

	var approval QuoteApprovalRequest
	if err := c.ShouldBindJSON(&approval); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND organization_id = ?", quoteID, org.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	var err error
	if approval.Approved {
		if quote.Status != "AWAITING_APPROVAL" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("Cannot approve quote in status %s", quote.Status)})
			return
		}
		err = services.TransitionQuote(db.DB, &quote, "APPROVED", "Broker confirmed customer approval of freight proposal")
	} else {
		lostReason := approval.LostReason
		if lostReason == "" {
			lostReason = "Customer rejected rate"
		}
		quote.LostReason = &lostReason
		if approval.CompetitorInfo != "" {
			quote.CompetitorInfo = &approval.CompetitorInfo
		}
		err = services.TransitionQuote(db.DB, &quote, "LOST", fmt.Sprintf("Customer rejected. Reason: %s", lostReason))
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Workflow action failed"})
		return
	}

	db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		First(&quote, "id = ?", quote.ID)

	c.JSON(http.StatusOK, quote)
}

func manualOverride(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	quoteID := c.Param("quote_id")

	var payload ManualOverridePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND organization_id = ?", quoteID, org.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	validStatuses := map[string]bool{
		"INTAKE":               true,
		"OUT_TO_CARRIERS":      true,
		"FIRST_ROUND_RECEIVED": true,
		"RE_BID_ROUND":         true,
		"QUOTE_SENT":           true,
		"AWAITING_APPROVAL":    true,
		"APPROVED":             true,
		"IN_TRANSIT":           true,
		"COMPLETED":            true,
		"NO_BIDS_ALERT":        true,
		"LOST":                 true,
	}

	if !validStatuses[payload.ToStatus] {
		c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("Invalid status: %s", payload.ToStatus)})
		return
	}

	notes := fmt.Sprintf("Manual Override: %s", payload.Notes)
	if payload.Notes == "" {
		notes = "Manual Override: No notes provided"
	}

	if err := services.TransitionQuote(db.DB, &quote, payload.ToStatus, notes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to transition status"})
		return
	}

	if payload.ToStatus == "COMPLETED" {
		quote.PaymentStatus = "PAID"
		db.DB.Save(&quote)
	}

	db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		First(&quote, "id = ?", quote.ID)

	c.JSON(http.StatusOK, quote)
}

func getHistoricalRAG(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	quoteID := c.Param("quote_id")

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND organization_id = ?", quoteID, org.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	if quote.ShipmentVector == "" {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	var results []HistoricalRAGResult
	err := db.DB.Raw(`
		SELECT q.id, q.origin, q.destination, q.weight_lbs, c.name as winning_carrier,
		       q.cost_price, q.sell_price, q.margin_pct, q.status,
		       (q.shipment_vector <=> ?) as distance
		FROM freight_quotes q
		LEFT JOIN carriers c ON q.winning_carrier_id = c.id
		WHERE q.id != ? AND q.organization_id = ? AND q.cost_price > 0
		ORDER BY q.shipment_vector <=> ?
		LIMIT 3
	`, quote.ShipmentVector, quote.ID, org.ID, quote.ShipmentVector).Scan(&results).Error

	if err != nil {
		log.Printf("pgvector similarity search failed: %v. Running fallback...\n", err)
		var fallbackQuotes []models.FreightQuote
		db.DB.Preload("WinningCarrier").
			Where("id != ? AND organization_id = ? AND cost_price > 0 AND (origin = ? OR destination = ?)", quote.ID, org.ID, quote.Origin, quote.Destination).
			Limit(3).Find(&fallbackQuotes)

		formattedResults := make([]HistoricalRAGResult, 0, len(fallbackQuotes))
		for _, r := range fallbackQuotes {
			carrierName := "N/A"
			if r.WinningCarrier != nil {
				carrierName = r.WinningCarrier.Name
			}
			formattedResults = append(formattedResults, HistoricalRAGResult{
				ID:             r.ID,
				Origin:         r.Origin,
				Destination:    r.Destination,
				WeightLbs:      r.WeightLbs,
				WinningCarrier: carrierName,
				CostPrice:      r.CostPrice,
				SellPrice:      r.SellPrice,
				MarginPct:      r.MarginPct,
				Similarity:     50.0,
				Status:         r.Status,
			})
		}
		c.JSON(http.StatusOK, formattedResults)
		return
	}

	formattedResults := make([]HistoricalRAGResult, 0, len(results))
	for _, r := range results {
		similarity := math.Round((1.0-r.Distance)*100.0*100.0) / 100.0
		r.Similarity = similarity
		if r.WinningCarrier == "" {
			r.WinningCarrier = "N/A"
		}
		formattedResults = append(formattedResults, r)
	}

	c.JSON(http.StatusOK, formattedResults)
}

// getPresignedUploadURL enforces org prefixing, strict filename sanitization (P0-6 fix), and Content-Type allowlisting.
func getPresignedUploadURL(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	quoteID := c.Param("quote_id")
	rawFilename := c.Query("filename")

	safeName, contentType, err := utils.SanitizeUploadFilename(rawFilename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND organization_id = ?", quoteID, org.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	fileKey := fmt.Sprintf("orgs/%d/quotes/%s/%s_%s", org.ID, quote.ID, uuid.NewString()[:8], safeName)
	uploadURL, err := services.GeneratePresignedUploadURL(fileKey, contentType, 900)
	if err != nil {
		log.Printf("Failed to generate presigned upload URL: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate secure upload URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"upload_url":   uploadURL,
		"file_key":     fileKey,
		"content_type": contentType,
		"expires_in":   900,
	})
}

// getPresignedDocumentURL returns a short-lived (15 min) presigned GET URL for a quote's private S3 BOL or Invoice.
func getPresignedDocumentURL(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	quoteID := c.Param("quote_id")
	docType := strings.ToLower(strings.TrimSpace(c.Query("type")))

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND organization_id = ?", quoteID, org.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	var key string
	switch docType {
	case "bol":
		if quote.BolS3Key != nil {
			key = *quote.BolS3Key
		}
	case "invoice":
		if quote.InvoiceS3Key != nil {
			key = *quote.InvoiceS3Key
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"detail": "type must be 'bol' or 'invoice'"})
		return
	}

	expectedPrefix := fmt.Sprintf("orgs/%d/quotes/%s/", org.ID, quote.ID)
	if key == "" || !strings.HasPrefix(key, expectedPrefix) || strings.Contains(key, "..") {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Document not yet generated for this quote"})
		return
	}

	downloadURL, err := services.GeneratePresignedDownloadURL(key, 900)
	if err != nil {
		log.Printf("Failed to presign private document %s: %v\n", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate secure document link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"download_url": downloadURL,
		"expires_in":   900,
	})
}
