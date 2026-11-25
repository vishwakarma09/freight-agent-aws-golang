package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
)

func SetupRouter() *gin.Engine {
	if config.Settings != nil && config.Settings.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), CORSMiddleware())

	// Health check root route
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "running",
			"engine": "Dispatch freight bidding workflow engine v2.0.0",
			"env":    config.Settings.Env,
		})
	})

	api := r.Group("/api")
	{
		RegisterAuthRoutes(api)
		RegisterOrganizationRoutes(api)
		RegisterBillingRoutes(api)
		RegisterQuoteRoutes(api)
		RegisterCarrierRoutes(api)
		RegisterCustomerRoutes(api)
		RegisterEmailCredentialRoutes(api)
		RegisterSimulatorRoutes(api)
		RegisterAnalyticsRoutes(api)
	}

	return r
}
