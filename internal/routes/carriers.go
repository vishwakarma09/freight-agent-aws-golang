package routes

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

type CarrierCreate struct {
	Name                 string  `json:"name" binding:"required"`
	Email                string  `json:"email" binding:"required,email"`
	CompetitivenessScore float64 `json:"competitiveness_score"`
	IsOverride           bool    `json:"is_override"`
	SimulatedScore       float64 `json:"simulated_score"`
}

func getCarrierCalculatedScore(gdb *gorm.DB, orgID uint, carrierID uint) float64 {
	var totalBids int64
	gdb.Model(&models.CarrierBid{}).
		Joins("JOIN freight_quotes ON freight_quotes.id = carrier_bids.freight_quote_id").
		Where("carrier_bids.carrier_id = ? AND freight_quotes.organization_id = ?", carrierID, orgID).
		Count(&totalBids)

	var wins int64
	gdb.Model(&models.FreightQuote{}).
		Where("winning_carrier_id = ? AND organization_id = ?", carrierID, orgID).
		Count(&wins)

	winRatePct := 0.0
	if totalBids > 0 {
		winRatePct = float64(wins) / float64(totalBids) * 100.0
	}

	score := math.Round(winRatePct/10.0*10.0) / 10.0
	return score
}

func populateCarrierScores(gdb *gorm.DB, orgID uint, carrier *models.Carrier) {
	carrier.CalculatedCompetitivenessScore = getCarrierCalculatedScore(gdb, orgID, carrier.ID)
	if carrier.IsOverride {
		carrier.CompetitivenessScore = carrier.SimulatedScore
	} else {
		carrier.CompetitivenessScore = carrier.CalculatedCompetitivenessScore
	}
}

func RegisterCarrierRoutes(rg *gin.RouterGroup) {
	carriers := rg.Group("/carriers", AuthMiddleware())
	{
		carriers.GET("", getCarriers)
		carriers.POST("", createCarrier)
		carriers.PUT("/:carrier_id", updateCarrier)
		carriers.DELETE("/:carrier_id", deleteCarrier)
	}
}

func getCarriers(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var carriers []models.Carrier
	if err := db.DB.Where("organization_id = ?", org.ID).Find(&carriers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to query carriers"})
		return
	}

	for i := range carriers {
		populateCarrierScores(db.DB, org.ID, &carriers[i])
	}

	c.JSON(http.StatusOK, carriers)
}

func createCarrier(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	// Enforce Option A carrier limit (-1 means unlimited)
	if org.CarrierLimit > 0 {
		var count int64
		db.DB.Model(&models.Carrier{}).Where("organization_id = ?", org.ID).Count(&count)
		if int(count) >= org.CarrierLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"detail": fmt.Sprintf("Plan carrier limit reached (%d/%d). Upgrade your plan in Subscription & Usage to add more carriers.", count, org.CarrierLimit),
			})
			return
		}
	}

	var payload CarrierCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	var existing models.Carrier
	if err := db.DB.Where("LOWER(email) = ? AND organization_id = ?", emailClean, org.ID).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Carrier email already registered in your workspace"})
		return
	}

	carrier := models.Carrier{
		OrganizationID:       org.ID,
		UserID:               &currentUser.ID,
		Name:                 strings.TrimSpace(payload.Name),
		Email:                emailClean,
		CompetitivenessScore: payload.CompetitivenessScore,
		IsOverride:           payload.IsOverride,
		SimulatedScore:       payload.SimulatedScore,
	}

	if err := db.DB.Create(&carrier).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create carrier"})
		return
	}

	populateCarrierScores(db.DB, org.ID, &carrier)
	c.JSON(http.StatusOK, carrier)
}

func updateCarrier(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	carrierID, err := strconv.ParseUint(c.Param("carrier_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid carrier ID"})
		return
	}

	var payload CarrierCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var carrier models.Carrier
	if err := db.DB.Where("id = ? AND organization_id = ?", carrierID, org.ID).First(&carrier).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Carrier not found"})
		return
	}

	carrier.Name = strings.TrimSpace(payload.Name)
	carrier.Email = strings.ToLower(strings.TrimSpace(payload.Email))
	carrier.IsOverride = payload.IsOverride
	carrier.SimulatedScore = payload.SimulatedScore

	if carrier.IsOverride {
		carrier.CompetitivenessScore = payload.SimulatedScore
	} else {
		carrier.CompetitivenessScore = getCarrierCalculatedScore(db.DB, org.ID, carrier.ID)
	}

	if err := db.DB.Save(&carrier).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to update carrier"})
		return
	}

	populateCarrierScores(db.DB, org.ID, &carrier)
	c.JSON(http.StatusOK, carrier)
}

func deleteCarrier(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	carrierID, err := strconv.ParseUint(c.Param("carrier_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid carrier ID"})
		return
	}

	var carrier models.Carrier
	if err := db.DB.Where("id = ? AND organization_id = ?", carrierID, org.ID).First(&carrier).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Carrier not found"})
		return
	}

	if err := db.DB.Delete(&carrier).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to delete carrier"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"detail": "Carrier deleted successfully"})
}
