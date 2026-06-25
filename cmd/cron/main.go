package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
)

func init() {
	log.Println("Initializing Cron Application...")

	if err := config.LoadConfig(); err != nil {
		log.Fatalf("Failed to load configuration: %v\n", err)
	}

	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v\n", err)
	}
}

type CronResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func Handler(ctx context.Context) (CronResponse, error) {
	log.Println("Cron handler triggered.")

	log.Println("Checking workflow pending timers...")
	services.CheckPendingTimers(db.DB)

	log.Println("Polling and ingesting emails...")
	services.PollAndIngestEmails(db.DB)

	return CronResponse{Status: "success"}, nil
}

func main() {
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		log.Println("Starting Lambda Cron Handler...")
		lambda.Start(Handler)
	} else {
		log.Println("Running cron tasks once locally...")
		_, _ = Handler(context.Background())
	}
}
