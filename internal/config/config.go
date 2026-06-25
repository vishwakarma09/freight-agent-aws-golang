package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env                string
	DatabaseURL        string
	FrontendURL        string
	SMTPHost           string
	SMTPPort           int
	SMTPUser           string
	SMTPPassword       string
	SMTPTLS            bool
	EmailsFromEmail    string
	CerebrasAPIKey     string
	BrokerEmail        string
	GoogleClientID     string
	GoogleClientSecret string
	S3BucketName       string
	SecretKey          string
}

var Settings *Config

func LoadConfig() error {
	// Attempt to load .env file if it exists, ignore error if it doesn't
	_ = godotenv.Load()

	Settings = &Config{
		Env:                getEnv("ENV", "dev"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgresql://user:password@localhost:5432/freight_bidding_db"),
		FrontendURL:        getEnv("FRONTEND_URL", "http://localhost:5173"),
		SMTPHost:           getEnv("SMTP_HOST", "localhost"),
		SMTPPort:           getEnvAsInt("SMTP_PORT", 1025),
		SMTPUser:           getEnv("SMTP_USER", ""),
		SMTPPassword:       getEnv("SMTP_PASSWORD", ""),
		SMTPTLS:            getEnvAsBool("SMTP_TLS", false),
		EmailsFromEmail:    getEnv("EMAILS_FROM_EMAIL", ""),
		CerebrasAPIKey:     getEnv("CEREBRAS_API_KEY", "YOUR_CEREBRAS_API_KEY"),
		BrokerEmail:        getEnv("BROKER_EMAIL", "broker@dispatch.owera.ca"),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", "YOUR_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", "YOUR_GOOGLE_CLIENT_SECRET"),
		S3BucketName:       getEnv("S3_BUCKET_NAME", "dispatch-private-go"),
		SecretKey:          getEnv("SECRET_KEY", "REMOVED_USE_AWS_SECRETS_MANAGER"),
	}

	return nil
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultVal
}

func getEnvAsBool(key string, defaultVal bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultVal
}
