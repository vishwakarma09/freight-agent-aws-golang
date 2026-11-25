package routes

import (
	"net/http"
	"strconv"
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
		customers.PUT("/:customer_id", updateCustomer)
	}
}

func getCustomers(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var customers []models.Customer
	if err := db.DB.Where("organization_id = ?", org.ID).Find(&customers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to query customers"})
		return
	}

	c.JSON(http.StatusOK, customers)
}

func createCustomer(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	var payload CustomerCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	emailClean := strings.ToLower(strings.TrimSpace(payload.Email))

	var existing models.Customer
	if err := db.DB.Where("LOWER(email) = ? AND organization_id = ?", emailClean, org.ID).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Customer email already registered in your workspace"})
		return
	}

	markup := payload.DefaultMarkupPercent
	if markup <= 0 {
		markup = 10.0
	}

	customer := models.Customer{
		OrganizationID:       org.ID,
		UserID:               &currentUser.ID,
		Name:                 strings.TrimSpace(payload.Name),
		Email:                emailClean,
		DefaultMarkupPercent: markup,
	}

	if err := db.DB.Create(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create customer"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

func updateCustomer(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	customerID, err := strconv.ParseUint(c.Param("customer_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid customer ID"})
		return
	}

	var payload CustomerCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var customer models.Customer
	if err := db.DB.Where("id = ? AND organization_id = ?", customerID, org.ID).First(&customer).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Customer not found"})
		return
	}

	customer.Name = strings.TrimSpace(payload.Name)
	customer.Email = strings.ToLower(strings.TrimSpace(payload.Email))
	if payload.DefaultMarkupPercent > 0 {
		customer.DefaultMarkupPercent = payload.DefaultMarkupPercent
	}

	if err := db.DB.Save(&customer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to update customer"})
		return
	}

	c.JSON(http.StatusOK, customer)
}
