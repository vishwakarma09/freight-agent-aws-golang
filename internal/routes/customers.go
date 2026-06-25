package routes

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

type CustomerCreate struct {
	Name                 string  `json:"name" binding:"required"`
	Email                string  `json:"email" binding:"required,email"`
	DefaultMarkupPercent float64 `json:"default_markup_percent"`
}

func RegisterCustomerRoutes(rg *gin.RouterGroup) {
	customers := rg.Group("/customers", AuthMiddleware())
	{
		customers.GET("", getCustomers)
		customers.POST("", createCustomer)
	}
}

func getCustomers(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var customers []models.Customer
	if err := db.DB.Where("user_id = ?", currentUser.ID).Find(&customers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to query customers"})
		return
	}

	c.JSON(http.StatusOK, customers)
}

func createCustomer(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var payload CustomerCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	// Check if customer email exists for this user
	var existing models.Customer
	err := db.DB.Where("email = ? AND user_id = ?", emailClean, currentUser.ID).First(&existing).Error
	if err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Customer email already registered for this user"})
		return
	}

	markup := payload.DefaultMarkupPercent
	if markup <= 0 {
		markup = 10.0 // Default fallback markup
	}

	customer := models.Customer{
		UserID:               &currentUser.ID,
		Name:                 payload.Name,
		Email:                emailClean,
		DefaultMarkupPercent: markup,
	}

	if err := db.DB.Create(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create customer"})
		return
	}

	c.JSON(http.StatusOK, customer)
}
