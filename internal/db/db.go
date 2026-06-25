package db

import (
	"log"
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
	if config.Settings.Env == "dev" {
		gormLogLevel = logger.Info
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

	log.Println("Creating database tables...")
	err := DB.AutoMigrate(
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

	// Alter tables to apply drop unique constraint on carriers(email) and index adjustments
	log.Println("Healing database schema constraints...")
	_ = DB.Exec("ALTER TABLE carriers DROP CONSTRAINT IF EXISTS carriers_email_key").Error
	_ = DB.Exec("DROP INDEX IF EXISTS ix_carriers_email").Error
	_ = DB.Exec("CREATE INDEX IF NOT EXISTS ix_carriers_email ON carriers (email)").Error

	// Perform healing: Link existing records
	log.Println("Healing database: linking user_id to existing credentials and data...")
	_ = DB.Exec(`
		UPDATE email_credentials ec 
		SET user_id = u.id 
		FROM users u 
		WHERE ec.user_email = u.email AND ec.user_id IS NULL
	`).Error
	_ = DB.Exec(`
		UPDATE carriers 
		SET user_id = (SELECT id FROM users WHERE email = 'broker@dispatch.owera.ca' LIMIT 1) 
		WHERE user_id IS NULL
	`).Error
	_ = DB.Exec(`
		UPDATE customers 
		SET user_id = (SELECT id FROM users WHERE email = 'broker@dispatch.owera.ca' LIMIT 1) 
		WHERE user_id IS NULL
	`).Error
	_ = DB.Exec(`
		UPDATE freight_quotes 
		SET user_id = (SELECT id FROM users WHERE email = 'broker@dispatch.owera.ca' LIMIT 1) 
		WHERE user_id IS NULL
	`).Error

	// Seed default broker user
	var userCount int64
	DB.Model(&models.User{}).Count(&userCount)
	if userCount == 0 {
		log.Println("Seeding default User broker@dispatch.owera.ca...")
		hashedPassword, err := utils.HashPassword("REDACTED_DEV_PASSWORD")
		if err != nil {
			log.Printf("Failed to hash default password: %v\n", err)
			return
		}
		defaultUser := models.User{
			Name:           "Broker",
			Email:          "broker@dispatch.owera.ca",
			HashedPassword: &hashedPassword,
			IsActive:       true,
		}
		DB.Create(&defaultUser)
	}

	// Get the default broker user ID
	var brokerUser models.User
	DB.Where("email = ?", "broker@dispatch.owera.ca").First(&brokerUser)

	// Seed default Customers
	var customerCount int64
	DB.Model(&models.Customer{}).Count(&customerCount)
	if customerCount == 0 {
		log.Println("Seeding default Customers A, B, and C...")
		defaultCustomers := []models.Customer{
			{Name: "Dispatch Customer A (5% Markup)", Email: "customer_a@example.com", DefaultMarkupPercent: 5.0, UserID: &brokerUser.ID},
			{Name: "Dispatch Customer B (12% Markup)", Email: "customer_b@example.com", DefaultMarkupPercent: 12.0, UserID: &brokerUser.ID},
			{Name: "Dispatch Customer C (30% Markup)", Email: "customer_c@example.com", DefaultMarkupPercent: 30.0, UserID: &brokerUser.ID},
		}
		DB.Create(&defaultCustomers)
	}

	// Seed default Carriers
	var carrierCount int64
	DB.Model(&models.Carrier{}).Count(&carrierCount)
	if carrierCount == 0 {
		log.Println("Seeding default carriers UPS, FedEx, DHL, Amazon Freight, and Old Dominion...")
		defaultCarriers := []models.Carrier{
			{Name: "UPS Freight", Email: "carrier_ups@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
			{Name: "FedEx Freight", Email: "carrier_fedex@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
			{Name: "DHL Express", Email: "carrier_dhl@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
			{Name: "Amazon Freight", Email: "carrier_amz@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
			{Name: "Old Dominion", Email: "carrier_od@mailpit.local", CompetitivenessScore: 0.0, UserID: &brokerUser.ID},
		}
		DB.Create(&defaultCarriers)
	}

	// Seed default Email credentials
	var emailCredentialCount int64
	DB.Model(&models.EmailCredential{}).Count(&emailCredentialCount)
	if emailCredentialCount == 0 {
		log.Println("Seeding default EmailCredential for Mailpit...")
		encryptedSmtpPassword, _ := utils.EncryptPassword("devmode")
		encryptedImapPassword, _ := utils.EncryptPassword("devmode")
		defaultCreds := models.EmailCredential{
			UserID:                &brokerUser.ID,
			UserEmail:             "broker@dispatch.owera.ca",
			EmailProvider:         "Mailpit",
			Email:                 "broker@dispatch.owera.ca",
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
}
