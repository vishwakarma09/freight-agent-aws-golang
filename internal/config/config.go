package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/joho/godotenv"
)

type Config struct {
	Env                string
	DatabaseURL        string
	FrontendURL        string
	AllowedOrigins     []string
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
	JWTSecret          string
	EncryptionKey      string
	KMSKeyID           string
	AWSSecretName      string
	RunMigrations      bool

	// Stripe Option A Billing
	StripeSecretKey             string
	StripeWebhookSecret         string
	StripePriceBoutiqueMonthly  string
	StripePriceBoutiqueAnnual   string
	StripePriceGrowthMonthly    string
	StripePriceGrowthAnnual     string
	StripePriceEnterpriseMonth  string
	StripePriceEnterpriseAnnual string
	StripeMeterEventName        string
}

var Settings *Config

func (c *Config) IsProd() bool {
	env := strings.ToLower(strings.TrimSpace(c.Env))
	return env == "prod" || env == "production"
}

func LoadConfig() error {
	envMode := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	isLambda := os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""

	// For local runs only, load gitignored .env.production (when ENV=prod) or .env
	if !isLambda {
		if envMode == "prod" || envMode == "production" {
			_ = godotenv.Load(".env.production")
		}
		_ = godotenv.Load(".env")
	}

	// Hydrate runtime secrets from AWS Secrets Manager when configured
	secretName := strings.TrimSpace(os.Getenv("AWS_SECRET_NAME"))
	if secretName != "" {
		if err := hydrateFromSecretsManager(secretName); err != nil {
			if envMode == "prod" || envMode == "production" {
				return fmt.Errorf("failed to load production secrets from AWS Secrets Manager (%s): %w", secretName, err)
			}
			log.Printf("Warning: could not load AWS Secrets Manager secret %s in %s mode: %v\n", secretName, envMode, err)
		}
	}

	env := getEnv("ENV", "dev")
	isProd := strings.EqualFold(env, "prod") || strings.EqualFold(env, "production")

	defaultFrontend := "http://localhost:5173"
	if isProd {
		defaultFrontend = "https://freight.owera.ca"
	}
	frontendURL := strings.TrimRight(getEnv("FRONTEND_URL", defaultFrontend), "/")

	originsRaw := getEnv("ALLOWED_ORIGINS", "")
	var allowedOrigins []string
	if originsRaw != "" {
		for _, o := range strings.Split(originsRaw, ",") {
			trimmed := strings.TrimRight(strings.TrimSpace(o), "/")
			if trimmed != "" && trimmed != "*" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}
	if len(allowedOrigins) == 0 {
		if isProd {
			allowedOrigins = []string{frontendURL, "https://freight.owera.ca"}
		} else {
			allowedOrigins = []string{frontendURL, "http://localhost:5173", "http://127.0.0.1:5173"}
		}
	}

	// Support SECRET_KEY as legacy alias for ENCRYPTION_KEY if explicitly provided in env/secrets
	encKey := getEnv("ENCRYPTION_KEY", "")
	if encKey == "" {
		encKey = getEnv("SECRET_KEY", "")
	}

	Settings = &Config{
		Env:                env,
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		FrontendURL:        frontendURL,
		AllowedOrigins:     dedupeStrings(allowedOrigins),
		SMTPHost:           getEnv("SMTP_HOST", "localhost"),
		SMTPPort:           getEnvAsInt("SMTP_PORT", 1025),
		SMTPUser:           getEnv("SMTP_USER", ""),
		SMTPPassword:       getEnv("SMTP_PASSWORD", ""),
		SMTPTLS:            getEnvAsBool("SMTP_TLS", isProd),
		EmailsFromEmail:    getEnv("EMAILS_FROM_EMAIL", ""),
		CerebrasAPIKey:     getEnv("CEREBRAS_API_KEY", ""),
		BrokerEmail:        getEnv("BROKER_EMAIL", "broker@dispatch.owera.ca"),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		S3BucketName:       getEnv("S3_BUCKET_NAME", ""),
		JWTSecret:          getEnv("JWT_SECRET", ""),
		EncryptionKey:      encKey,
		KMSKeyID:           getEnv("KMS_KEY_ID", ""),
		AWSSecretName:      secretName,
		RunMigrations:      getEnvAsBool("RUN_MIGRATIONS", !isLambda),

		StripeSecretKey:             getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:         getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceBoutiqueMonthly:  getEnv("STRIPE_PRICE_BOUTIQUE_MONTHLY", ""),
		StripePriceBoutiqueAnnual:   getEnv("STRIPE_PRICE_BOUTIQUE_ANNUAL", ""),
		StripePriceGrowthMonthly:    getEnv("STRIPE_PRICE_GROWTH_MONTHLY", ""),
		StripePriceGrowthAnnual:     getEnv("STRIPE_PRICE_GROWTH_ANNUAL", ""),
		StripePriceEnterpriseMonth:  getEnv("STRIPE_PRICE_ENTERPRISE_MONTHLY", ""),
		StripePriceEnterpriseAnnual: getEnv("STRIPE_PRICE_ENTERPRISE_ANNUAL", ""),
		StripeMeterEventName:        getEnv("STRIPE_METER_EVENT_NAME", "freight_quote_overage"),
	}

	if isProd {
		if Settings.DatabaseURL == "" {
			return errors.New("DATABASE_URL is required in production (configure via AWS Secrets Manager)")
		}
		if len(Settings.JWTSecret) < 32 {
			return errors.New("JWT_SECRET must be at least 32 characters in production (configure via AWS Secrets Manager)")
		}
		if Settings.KMSKeyID == "" && len(Settings.EncryptionKey) < 32 {
			return errors.New("either KMS_KEY_ID or a 32+ character ENCRYPTION_KEY is required in production")
		}
	} else {
		// In non-production dev environments only, generate ephemeral secrets if omitted so no hardcoded keys exist in source
		if Settings.DatabaseURL == "" {
			Settings.DatabaseURL = "postgresql://localhost:5432/freight_bidding_db?sslmode=disable"
		}
		if Settings.JWTSecret == "" {
			log.Println("WARNING: JWT_SECRET not set in dev environment; configure JWT_SECRET in .env for persistent dev sessions.")
			Settings.JWTSecret = fmt.Sprintf("dev-ephemeral-jwt-%d", time.Now().UnixNano())
		}
		if Settings.EncryptionKey == "" && Settings.KMSKeyID == "" {
			log.Println("WARNING: ENCRYPTION_KEY not set in dev environment; configure ENCRYPTION_KEY in .env for persistent credential encryption.")
			Settings.EncryptionKey = fmt.Sprintf("dev-ephemeral-enc-%d-000000000000", time.Now().UnixNano())
		}
	}

	return nil
}

func hydrateFromSecretsManager(secretName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}

	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretName,
	})
	if err != nil {
		return err
	}
	if out.SecretString == nil || *out.SecretString == "" {
		return errors.New("secret string is empty")
	}

	var kv map[string]string
	if err := json.Unmarshal([]byte(*out.SecretString), &kv); err != nil {
		return fmt.Errorf("secret %s must be a JSON key-value object: %w", secretName, err)
	}

	for k, v := range kv {
		if k != "" && v != "" {
			// Secrets Manager takes precedence over placeholder env vars
			_ = os.Setenv(k, v)
		}
	}
	log.Printf("Loaded %d secret keys from AWS Secrets Manager (%s)\n", len(kv), secretName)
	return nil
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
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
