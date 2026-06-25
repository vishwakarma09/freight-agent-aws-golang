package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/gin"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/routes"
)

var ginLambda *ginadapter.GinLambdaV2

func init() {
	log.Println("Initializing Application...")
	
	// Load configuration
	if err := config.LoadConfig(); err != nil {
		log.Fatalf("Failed to load configuration: %v\n", err)
	}

	// Initialize database
	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v\n", err)
	}

	// Perform migrations and seeds
	db.MigrateAndSeed()

	// Initialize Gin router
	r := routes.SetupRouter()

	// Check if running in Lambda
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		ginLambda = ginadapter.NewV2(r)
	}
}

func Handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return ginLambda.ProxyWithContext(ctx, req)
}

func main() {
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		log.Println("Starting Lambda HTTP V2 Handler...")
		lambda.Start(Handler)
	} else {
		log.Println("Starting Local HTTP Server on :8000...")
		r := routes.SetupRouter()
		if err := r.Run(":8000"); err != nil {
			log.Fatalf("Failed to start local server: %v\n", err)
		}
	}
}
