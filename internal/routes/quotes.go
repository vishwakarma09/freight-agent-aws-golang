package routes

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
)

type FreightQuoteCreate struct {
	CustomerID   uint      `json:"customer_id" binding:"required"`
	Origin       string    `json:"origin" binding:"required"`
	Destination  string    `json:"destination" binding:"required"`
	WeightLbs    float64   `json:"weight_lbs" binding:"required"`
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
	}
}

func getQuotes(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var quotes []models.FreightQuote
	err := db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		Where("user_id = ?", currentUser.ID).
		Order("created_at desc").
		Find(&quotes).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to list quotes"})
		return
	}

	c.JSON(http.StatusOK, quotes)
}

func getQuote(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	quoteID := c.Param("quote_id")

	var quote models.FreightQuote
	err := db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		Where("id = ? AND user_id = ?", quoteID, currentUser.ID).
		First(&quote).Error

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	c.JSON(http.StatusOK, quote)
}

func createQuote(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var payload FreightQuoteCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// 1. Verify customer exists
	var customer models.Customer
	if err := db.DB.Where("id = ? AND user_id = ?", payload.CustomerID, currentUser.ID).First(&customer).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid Customer ID"})
		return
	}

	// 2. Generate sequential ID
	var count int64
	db.DB.Model(&models.FreightQuote{}).Count(&count)
	var quoteID string
	for {
		quoteID = fmt.Sprintf("Q-%d", 1000+count+1)
		var exists int64
		db.DB.Model(&models.FreightQuote{}).Where("id = ?", quoteID).Count(&exists)
		if exists == 0 {
			break
		}
		count++
	}

	// 3. Compute vector embedding
	description := fmt.Sprintf("Origin: %s, Destination: %s, Class: %s, Weight: %.2f lbs", payload.Origin, payload.FreightClass, payload.FreightClass, payload.WeightLbs)
	vector := services.GetEmbedding(description)

	dims := payload.Dimensions
	if dims == "" {
		dims = "48x48x48"
	}
	fClass := payload.FreightClass
	if fClass == "" {
		fClass = "70"
	}

	quote := models.FreightQuote{
		ID:             quoteID,
		UserID:         &currentUser.ID,
		CustomerID:     payload.CustomerID,
		Status:         "INTAKE",
		Origin:         payload.Origin,
		Destination:    payload.Destination,
		WeightLbs:      payload.WeightLbs,
		Dimensions:     &dims,
		FreightClass:   &fClass,
		Hazmat:         payload.Hazmat,
		Accessorials:   &payload.Accessorials,
		PickupDate:     payload.PickupDate,
		ShipmentVector: services.FormatVector(vector),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := db.DB.Create(&quote).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create freight quote"})
		return
	}

	// 5. Trigger workflow state machine
	_ = services.TransitionQuote(db.DB, &quote, "INTAKE", "Quote ingest parsed successfully")
	_ = services.TransitionQuote(db.DB, &quote, "OUT_TO_CARRIERS", "RFQ requests sent out to carriers")

	// Reload quote relations
	db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		First(&quote, "id = ?", quote.ID)

	c.JSON(http.StatusOK, quote)
}

func approveQuote(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	quoteID := c.Param("quote_id")

	var approval QuoteApprovalRequest
	if err := c.ShouldBindJSON(&approval); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var quote models.FreightQuote
	err := db.DB.Where("id = ? AND user_id = ?", quoteID, currentUser.ID).First(&quote).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	if approval.Approved {
		if quote.Status != "AWAITING_APPROVAL" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": fmt.Sprintf("Cannot approve quote in status %s", quote.Status)})
			return
		}
		err = services.TransitionQuote(db.DB, &quote, "APPROVED", "Customer approved the freight quote proposal")
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

	// Reload
	db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		First(&quote, "id = ?", quote.ID)

	c.JSON(http.StatusOK, quote)
}

func manualOverride(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	quoteID := c.Param("quote_id")

	var payload ManualOverridePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var quote models.FreightQuote
	err := db.DB.Where("id = ? AND user_id = ?", quoteID, currentUser.ID).First(&quote).Error
	if err != nil {
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

	err = services.TransitionQuote(db.DB, &quote, payload.ToStatus, notes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to transition status"})
		return
	}

	if payload.ToStatus == "COMPLETED" {
		quote.PaymentStatus = "PAID"
		db.DB.Save(&quote)
	}

	// Reload
	db.DB.Preload("Customer").
		Preload("WinningCarrier").
		Preload("Bids.Carrier").
		Preload("Transitions").
		Preload("RFQs.Carrier").
		First(&quote, "id = ?", quote.ID)

	c.JSON(http.StatusOK, quote)
}

func getHistoricalRAG(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	quoteID := c.Param("quote_id")

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND user_id = ?", quoteID, currentUser.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	if quote.ShipmentVector == "" {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	var results []HistoricalRAGResult
	// Run raw SQL for similarity search using pgvector
	err := db.DB.Raw(`
		SELECT q.id, q.origin, q.destination, q.weight_lbs, c.name as winning_carrier,
		       q.cost_price, q.sell_price, q.margin_pct, q.status,
		       (q.shipment_vector <=> ?) as distance
		FROM freight_quotes q
		LEFT JOIN carriers c ON q.winning_carrier_id = c.id
		WHERE q.id != ? AND q.user_id = ? AND q.cost_price > 0
		ORDER BY q.shipment_vector <=> ?
		LIMIT 3
	`, quote.ShipmentVector, quote.ID, currentUser.ID, quote.ShipmentVector).Scan(&results).Error

	if err != nil {
		log.Printf("pgvector similarity search failed: %v. Running fallback...\n", err)
		// Fallback query
		var fallbackQuotes []models.FreightQuote
		db.DB.Preload("WinningCarrier").
			Where("id != ? AND user_id = ? AND cost_price > 0 AND (origin = ? OR destination = ?)", quote.ID, currentUser.ID, quote.Origin, quote.Destination).
			Limit(3).Find(&fallbackQuotes)

		formattedResults := []HistoricalRAGResult{}
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

	formattedResults := []HistoricalRAGResult{}
	for _, r := range results {
		// Calculate similarity percentage from distance
		similarity := math.Round((1.0-r.Distance)*100.0*100.0) / 100.0
		r.Similarity = similarity
		if r.WinningCarrier == "" {
			r.WinningCarrier = "N/A"
		}
		formattedResults = append(formattedResults, r)
	}

	c.JSON(http.StatusOK, formattedResults)
}

func getPresignedUploadURL(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	quoteID := c.Param("quote_id")
	filename := c.Query("filename")

	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Filename query parameter is required"})
		return
	}

	var quote models.FreightQuote
	if err := db.DB.Where("id = ? AND user_id = ?", quoteID, currentUser.ID).First(&quote).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Freight quote not found"})
		return
	}

	fileKey := fmt.Sprintf("quotes/%s/%s", quoteID, filename)
	uploadURL, err := services.GeneratePresignedUploadURL(fileKey, 3600)
	if err != nil {
		log.Printf("Failed to generate presigned upload URL: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate secure upload URL"})
		return
	}

	bucketName := config.Settings.S3BucketName
	if bucketName == "" {
		bucketName = "dispatch-private-go"
	}
	assetURL := fmt.Sprintf("https://%s.s3.amazonaws.com/%s", bucketName, fileKey)

	c.JSON(http.StatusOK, gin.H{
		"upload_url": uploadURL,
		"file_key":   fileKey,
		"asset_url":  assetURL,
	})
}
