package db

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

var DB *gorm.DB

func InitDB() error {
	var err error

	gormLogLevel := logger.Silent
	if !config.Settings.IsProd() {
		gormLogLevel = logger.Warn
	}

	DB, err = gorm.Open(postgres.Open(config.Settings.DatabaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return err
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	// Optimize connection pool settings for AWS Lambda execution model
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	return nil
}

func MigrateAndSeed() {
	log.Println("Initializing pgvector database extension...")
	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		log.Printf("Could not initialize pgvector extension: %v. Falling back to standard operations.\n", err)
	}

	log.Println("Running database schema migrations...")
	err := DB.AutoMigrate(
		&models.Organization{},
		&models.OrganizationInvite{},
		&models.StripeWebhookEvent{},
		&models.QuoteUsageEvent{},
		&models.User{},
		&models.Customer{},
		&models.Carrier{},
		&models.FreightQuote{},
		&models.CarrierBid{},
		&models.StateTransition{},
		&models.EmailCredential{},
		&models.ProcessedEmail{},
		&models.RequestForQuote{},
	)
	if err != nil {
		log.Fatalf("Failed to run auto-migrations: %v\n", err)
	}

	// Drop legacy global unique constraints that broke multi-tenant isolation
	log.Println("Healing multi-tenant schema constraints...")
	_ = DB.Exec("ALTER TABLE carriers DROP CONSTRAINT IF EXISTS carriers_email_key").Error
	_ = DB.Exec("DROP INDEX IF EXISTS ix_carriers_email").Error
	_ = DB.Exec("CREATE INDEX IF NOT EXISTS ix_carriers_email ON carriers (email)").Error

	_ = DB.Exec("ALTER TABLE customers DROP CONSTRAINT IF EXISTS customers_email_key").Error
	_ = DB.Exec("DROP INDEX IF EXISTS idx_customers_email").Error
	_ = DB.Exec("DROP INDEX IF EXISTS ix_customers_email").Error

	_ = DB.Exec("ALTER TABLE email_credentials DROP CONSTRAINT IF EXISTS email_credentials_user_id_key").Error
	_ = DB.Exec("ALTER TABLE email_credentials DROP CONSTRAINT IF EXISTS email_credentials_user_email_key").Error
	_ = DB.Exec("DROP INDEX IF EXISTS idx_email_credentials_user_id").Error
	_ = DB.Exec("DROP INDEX IF EXISTS idx_email_credentials_user_email").Error

	// Backfill Organizations for any existing legacy users without an organization_id
	backfillOrganizations()

	// Never seed demo accounts or weak passwords in production
	if config.Settings.IsProd() {
		log.Println("Production environment detected: skipping demo database seeding.")
		return
	}

	seedDevData()
}

func backfillOrganizations() {
	var orphanUsers []models.User
	DB.Where("organization_id IS NULL OR organization_id = 0").Find(&orphanUsers)

	trialEnd := time.Now().AddDate(0, 0, 14)
	for _, u := range orphanUsers {
		slugBase := strings.ReplaceAll(strings.Split(strings.ToLower(u.Email), "@")[0], ".", "-")
		org := models.Organization{
			Name:                  fmt.Sprintf("%s's Workspace", u.Name),
			Slug:                  fmt.Sprintf("%s-%d", slugBase, u.ID),
			PlanTier:              "free",
			BillingStatus:         "trialing",
			TrialEndsAt:           &trialEnd,
			SeatLimit:             1,
			CarrierLimit:          10,
			InboxLimit:            1,
			MonthlyQuoteLimit:     25,
			Round1DurationMinutes: 120,
			Round2DurationMinutes: 30,
		}
		if err := DB.Create(&org).Error; err == nil {
			DB.Model(&u).Updates(map[string]interface{}{
				"organization_id": org.ID,
				"role":            "owner",
			})
			// Link existing tenant-scoped rows owned by this user
			DB.Exec("UPDATE customers SET organization_id = ? WHERE user_id = ? AND (organization_id IS NULL OR organization_id = 0)", org.ID, u.ID)
			DB.Exec("UPDATE carriers SET organization_id = ? WHERE user_id = ? AND (organization_id IS NULL OR organization_id = 0)", org.ID, u.ID)
			DB.Exec("UPDATE freight_quotes SET organization_id = ? WHERE user_id = ? AND (organization_id IS NULL OR organization_id = 0)", org.ID, u.ID)
			DB.Exec("UPDATE email_credentials SET organization_id = ? WHERE user_id = ? AND (organization_id IS NULL OR organization_id = 0)", org.ID, u.ID)
		}
	}
}

func seedDevData() {
	var userCount int64
	DB.Model(&models.User{}).Count(&userCount)
	if userCount > 0 {
		return
	}

	devEmail := os.Getenv("DEV_SEED_EMAIL")
	if devEmail == "" {
		devEmail = "broker@dispatch.owera.ca"
	}
	devPass := os.Getenv("DEV_SEED_PASSWORD")
	if devPass == "" {
		// Generated dev password logged once at startup (never hardcoded in source)
		tok, _ := utils.GenerateRandomToken(8)
		devPass = "Dev" + tok + "9"
		log.Printf("DEV SEED: Generated initial dev broker credentials -> email=%s password=%s (set DEV_SEED_PASSWORD in .env to customize)\n", devEmail, devPass)
	}

	trialEnd := time.Now().AddDate(0, 0, 14)
	org := models.Organization{
		Name:                  "Dispatch Dev Brokerage",
		Slug:                  "dispatch-dev",
		PlanTier:              "enterprise",
		BillingStatus:         "active",
		TrialEndsAt:           &trialEnd,
		SeatLimit:             25,
		CarrierLimit:          -1,
		InboxLimit:            -1,
		MonthlyQuoteLimit:     3000,
		Round1DurationMinutes: 2,
		Round2DurationMinutes: 1,
	}
	if err := DB.Create(&org).Error; err != nil {
		log.Printf("Failed to seed dev organization: %v\n", err)
		return
	}

	hashedPassword, err := utils.HashPassword(devPass)
	if err != nil {
		log.Printf("Failed to hash dev seed password: %v\n", err)
		return
	}

	brokerUser := models.User{
		OrganizationID: org.ID,
		Role:           "owner",
		Name:           "Broker",
		Email:          devEmail,
		HashedPassword: &hashedPassword,
		IsActive:       true,
	}
	DB.Create(&brokerUser)

	defaultCustomers := []models.Customer{
		{OrganizationID: org.ID, Name: "Dispatch Customer A (5% Markup)", Email: "customer_a@example.com", DefaultMarkupPercent: 5.0, UserID: &brokerUser.ID},
		{OrganizationID: org.ID, Name: "Dispatch Customer B (12% Markup)", Email: "customer_b@example.com", DefaultMarkupPercent: 12.0, UserID: &brokerUser.ID},
		{OrganizationID: org.ID, Name: "Dispatch Customer C (30% Markup)", Email: "customer_c@example.com", DefaultMarkupPercent: 30.0, UserID: &brokerUser.ID},
	}
	DB.Create(&defaultCustomers)

	defaultCarriers := []models.Carrier{
		{OrganizationID: org.ID, Name: "UPS Freight", Email: "carrier_ups@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
		{OrganizationID: org.ID, Name: "FedEx Freight", Email: "carrier_fedex@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
		{OrganizationID: org.ID, Name: "DHL Express", Email: "carrier_dhl@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
		{OrganizationID: org.ID, Name: "Amazon Freight", Email: "carrier_amz@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
		{OrganizationID: org.ID, Name: "Old Dominion", Email: "carrier_od@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
	}
	DB.Create(&defaultCarriers)

	encryptedSmtpPassword, _ := utils.EncryptPassword("devmode")
	encryptedImapPassword, _ := utils.EncryptPassword("devmode")
	defaultCreds := models.EmailCredential{
		OrganizationID:        org.ID,
		UserID:                &brokerUser.ID,
		UserEmail:             devEmail,
		EmailProvider:         "Mailpit",
		Email:                 devEmail,
		SMTPHost:              "mailpit",
		SMTPPort:              1025,
		EncryptedSMTPPassword: encryptedSmtpPassword,
		IMAPHost:              "mailpit",
		IMAPPort:              143,
		EncryptedIMAPPassword: encryptedImapPassword,
		UseDevMode:            true,
	}
	DB.Create(&defaultCreds)
}
