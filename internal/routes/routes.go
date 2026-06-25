package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	// CORS Middleware
	r.Use(CORSMiddleware())

	// Root Route
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "running",
			"engine": "Freight bidding workflow engine v1.0.0 on Go AWS Lambda",
		})
	})

	// API Group
	api := r.Group("/api")
	{
		RegisterAuthRoutes(api)
		RegisterQuoteRoutes(api)
		RegisterCarrierRoutes(api)
		RegisterCustomerRoutes(api)
		RegisterEmailCredentialRoutes(api)
		RegisterSimulatorRoutes(api)
		RegisterAnalyticsRoutes(api)
	}

	return r
}
