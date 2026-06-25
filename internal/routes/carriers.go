package routes

import (
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

func getCarrierCalculatedScore(gdb *gorm.DB, carrierID uint) float64 {
	var totalBids int64
	gdb.Model(&models.CarrierBid{}).Where("carrier_id = ?", carrierID).Count(&totalBids)

	var wins int64
	gdb.Model(&models.FreightQuote{}).Where("winning_carrier_id = ?", carrierID).Count(&wins)

	winRatePct := 0.0
	if totalBids > 0 {
		winRatePct = float64(wins) / float64(totalBids) * 100.0
	}

	// Calculate win rate percentage clamped and scaled to [0.0, 10.0], rounded to 1 decimal place
	score := math.Round(winRatePct/10.0*10.0) / 10.0
	return score
}

func populateCarrierScores(gdb *gorm.DB, carrier *models.Carrier) {
	carrier.CalculatedCompetitivenessScore = getCarrierCalculatedScore(gdb, carrier.ID)
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
	currentUser, _ := GetCurrentUser(c)

	var carriers []models.Carrier
	if err := db.DB.Where("user_id = ?", currentUser.ID).Find(&carriers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to query carriers"})
		return
	}

	for i := range carriers {
		populateCarrierScores(db.DB, &carriers[i])
	}

	c.JSON(http.StatusOK, carriers)
}

func createCarrier(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var payload CarrierCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	// Check if already registered
	var existing models.Carrier
	err := db.DB.Where("email = ? AND user_id = ?", emailClean, currentUser.ID).First(&existing).Error
	if err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Carrier email already registered for this user"})
		return
	}

	carrier := models.Carrier{
		UserID:               &currentUser.ID,
		Name:                 payload.Name,
		Email:                emailClean,
		CompetitivenessScore: payload.CompetitivenessScore,
		IsOverride:           payload.IsOverride,
		SimulatedScore:       payload.SimulatedScore,
	}

	if err := db.DB.Create(&carrier).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create carrier"})
		return
	}

	populateCarrierScores(db.DB, &carrier)
	c.JSON(http.StatusOK, carrier)
}

func updateCarrier(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	carrierIDStr := c.Param("carrier_id")
	carrierID, err := strconv.ParseUint(carrierIDStr, 10, 64)
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
	err = db.DB.Where("id = ? AND user_id = ?", carrierID, currentUser.ID).First(&carrier).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Carrier not found"})
		return
	}

	carrier.Name = payload.Name
	carrier.Email = strings.ToLower(strings.TrimSpace(payload.Email))
	carrier.IsOverride = payload.IsOverride
	carrier.SimulatedScore = payload.SimulatedScore

	if carrier.IsOverride {
		carrier.CompetitivenessScore = payload.SimulatedScore
	} else {
		carrier.CompetitivenessScore = getCarrierCalculatedScore(db.DB, carrier.ID)
	}

	if err := db.DB.Save(&carrier).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to update carrier"})
		return
	}

	populateCarrierScores(db.DB, &carrier)
	c.JSON(http.StatusOK, carrier)
}

func deleteCarrier(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	carrierIDStr := c.Param("carrier_id")
	carrierID, err := strconv.ParseUint(carrierIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid carrier ID"})
		return
	}

	var carrier models.Carrier
	err = db.DB.Where("id = ? AND user_id = ?", carrierID, currentUser.ID).First(&carrier).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Carrier not found"})
		return
	}

	if err := db.DB.Delete(&carrier).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to delete carrier"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"detail": "Carrier deleted successfully"})
}
