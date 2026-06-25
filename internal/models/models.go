package models

import (
	"time"
)

type User struct {
	ID              uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"not null" json:"name"`
	Email           string     `gorm:"uniqueIndex;not null" json:"email"`
	HashedPassword  *string    `gorm:"column:hashed_password" json:"-"`
	IsActive        bool       `gorm:"default:false;not null;column:is_active" json:"is_active"`
	ActivationToken *string    `gorm:"uniqueIndex;column:activation_token" json:"-"`
	CreatedAt       time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

type Customer struct {
	ID                   uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID               *uint          `gorm:"column:user_id;index" json:"user_id,omitempty"`
	Name                 string         `gorm:"not null" json:"name"`
	Email                string         `gorm:"uniqueIndex;not null" json:"email"`
	DefaultMarkupPercent float64        `gorm:"default:10.0;column:default_markup_percent" json:"default_markup_percent"`
	CreatedAt            time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	Quotes               []FreightQuote `gorm:"foreignKey:CustomerID" json:"-"`
}

type Carrier struct {
	ID                             uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID                         *uint        `gorm:"column:user_id;index" json:"user_id,omitempty"`
	Name                           string       `gorm:"not null" json:"name"`
	Email                          string       `gorm:"index;not null" json:"email"`
	CompetitivenessScore           float64      `gorm:"default:0.0;column:competitiveness_score" json:"competitiveness_score"`
	IsOverride                     bool         `gorm:"default:false;not null;column:is_override" json:"is_override"`
	SimulatedScore                 float64      `gorm:"default:0.0;not null;column:simulated_score" json:"simulated_score"`
	CreatedAt                      time.Time    `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt                      time.Time    `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	Bids                           []CarrierBid `gorm:"foreignKey:CarrierID;constraint:OnDelete:CASCADE" json:"-"`
	QuotesWon                      []FreightQuote `gorm:"foreignKey:WinningCarrierID" json:"-"`
	// Virtual field computed at runtime
	CalculatedCompetitivenessScore float64      `gorm:"-" json:"calculated_competitiveness_score"`
}

type FreightQuote struct {
	ID               string            `gorm:"primaryKey" json:"id"` // e.g. "Q-1001"
	UserID           *uint             `gorm:"column:user_id;index" json:"user_id,omitempty"`
	CustomerID       uint              `gorm:"not null;column:customer_id" json:"customer_id"`
	Status           string            `gorm:"default:'INTAKE';not null" json:"status"`
	Origin           string            `gorm:"not null" json:"origin"`
	Destination      string            `gorm:"not null" json:"destination"`
	WeightLbs        float64           `gorm:"not null;column:weight_lbs" json:"weight_lbs"`
	Dimensions       *string           `json:"dimensions"`
	FreightClass     *string           `gorm:"column:freight_class" json:"freight_class"`
	Hazmat           bool              `gorm:"default:false" json:"hazmat"`
	Accessorials     *string           `json:"accessorials"`
	PickupDate       time.Time         `gorm:"not null;column:pickup_date" json:"pickup_date"`
	
	FirstRoundEndsAt *time.Time        `gorm:"column:first_round_ends_at" json:"first_round_ends_at"`
	RebidRoundEndsAt *time.Time        `gorm:"column:rebid_round_ends_at" json:"rebid_round_ends_at"`
	QuoteExpiresAt   *time.Time        `gorm:"column:quote_expires_at" json:"quote_expires_at"`
	
	CostPrice        float64           `gorm:"default:0.0;column:cost_price" json:"cost_price"`
	SellPrice        float64           `gorm:"default:0.0;column:sell_price" json:"sell_price"`
	MarkupPercent    float64           `gorm:"default:0.0;column:markup_percent" json:"markup_percent"`
	MarginAmt        float64           `gorm:"default:0.0;column:margin_amt" json:"margin_amt"`
	MarginPct        float64           `gorm:"default:0.0;column:margin_pct" json:"margin_pct"`
	
	WinningCarrierID *uint             `gorm:"column:winning_carrier_id" json:"winning_carrier_id"`
	LostReason       *string           `gorm:"column:lost_reason" json:"lost_reason"`
	CompetitorInfo   *string           `gorm:"column:competitor_info" json:"competitor_info"`
	
	PaymentStatus    string            `gorm:"default:'UNPAID';column:payment_status" json:"payment_status"`
	BolURL           *string           `gorm:"column:bol_url" json:"bol_url"`
	InvoiceURL       *string           `gorm:"column:invoice_url" json:"invoice_url"`
	
	EmailThreadID    *string           `gorm:"column:email_thread_id" json:"email_thread_id,omitempty"`
	EmailMessageID   *string           `gorm:"column:email_message_id" json:"email_message_id,omitempty"`
	
	ShipmentVector   string            `gorm:"column:shipment_vector;type:vector(1536)" json:"-"`
	
	CreatedAt        time.Time         `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt        time.Time         `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	
	Customer         *Customer         `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	WinningCarrier   *Carrier          `gorm:"foreignKey:WinningCarrierID" json:"winning_carrier,omitempty"`
	Bids             []CarrierBid      `gorm:"foreignKey:FreightQuoteID" json:"bids"`
	Transitions      []StateTransition `gorm:"foreignKey:FreightQuoteID" json:"transitions"`
	RFQs             []RequestForQuote `gorm:"foreignKey:FreightQuoteID" json:"rfqs"`
}

type CarrierBid struct {
	ID               uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	FreightQuoteID   string       `gorm:"not null;column:freight_quote_id" json:"freight_quote_id"`
	CarrierID        uint         `gorm:"not null;column:carrier_id" json:"carrier_id"`
	Round            int          `gorm:"default:1" json:"round"`
	BidAmount        float64      `gorm:"not null;column:bid_amount" json:"bid_amount"`
	TransitTimeDays  int          `gorm:"column:transit_time_days" json:"transit_time_days"`
	PickupWindow     *string      `gorm:"column:pickup_window" json:"pickup_window"`
	AccessorialsText *string      `gorm:"column:accessorials_text" json:"accessorials_text"`
	ServiceLevel     *string      `gorm:"column:service_level" json:"service_level"`
	Notes            *string      `json:"notes"`
	ReceivedAt       time.Time    `gorm:"default:CURRENT_TIMESTAMP;column:received_at" json:"received_at"`
	IsWinning        bool         `gorm:"default:false;column:is_winning" json:"is_winning"`
	RawEmail         *string      `gorm:"column:raw_email" json:"raw_email,omitempty"`
	Carrier          *Carrier     `gorm:"foreignKey:CarrierID" json:"carrier,omitempty"`
}

type StateTransition struct {
	ID             uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	FreightQuoteID string       `gorm:"not null;column:freight_quote_id" json:"freight_quote_id"`
	FromStatus     string       `gorm:"not null;column:from_status" json:"from_status"`
	ToStatus       string       `gorm:"not null;column:to_status" json:"to_status"`
	Timestamp      time.Time    `gorm:"default:CURRENT_TIMESTAMP" json:"timestamp"`
	Notes          *string      `json:"notes"`
}

type EmailCredential struct {
	ID                    uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID                *uint     `gorm:"uniqueIndex;column:user_id" json:"user_id,omitempty"`
	UserEmail             string    `gorm:"uniqueIndex;size:100;not null;column:user_email" json:"user_email"`
	EmailProvider         string    `gorm:"size:50;default:'Gmail';not null;column:email_provider" json:"email_provider"`
	Email                 string    `gorm:"size:100;not null" json:"email"`
	SMTPHost              string    `gorm:"size:100;default:'smtp.gmail.com';not null;column:smtp_host" json:"smtp_host"`
	SMTPPort              int       `gorm:"default:587;not null;column:smtp_port" json:"smtp_port"`
	EncryptedSMTPPassword string    `gorm:"size:500;not null;column:encrypted_smtp_password" json:"smtp_password"` // Maps to SMTP password in JSON response
	IMAPHost              string    `gorm:"size:100;default:'imap.gmail.com';not null;column:imap_host" json:"imap_host"`
	IMAPPort              int       `gorm:"default:993;not null;column:imap_port" json:"imap_port"`
	EncryptedIMAPPassword string    `gorm:"size:500;not null;column:encrypted_imap_password" json:"imap_password"` // Maps to IMAP password in JSON response
	UseDevMode            bool      `gorm:"default:false;not null;column:use_dev_mode" json:"use_dev_mode"`
	CreatedAt             time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt             time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

type ProcessedEmail struct {
	ID          string    `gorm:"primaryKey;size:255" json:"id"`
	ProcessedAt time.Time `gorm:"default:CURRENT_TIMESTAMP;column:processed_at" json:"processed_at"`
}

type RequestForQuote struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	FreightQuoteID string    `gorm:"not null;column:freight_quote_id" json:"freight_quote_id"`
	CarrierID      uint      `gorm:"not null;column:carrier_id" json:"carrier_id"`
	SupplierID     uint      `gorm:"not null;column:supplier_id" json:"supplier_id"`
	Status         string    `gorm:"default:'SENT';not null" json:"status"`
	SentAt         time.Time `gorm:"default:CURRENT_TIMESTAMP;not null;column:sent_at" json:"sent_at"`
	Subject        *string   `json:"subject"`
	Body           *string   `json:"body"`
	Carrier        *Carrier  `gorm:"foreignKey:CarrierID" json:"carrier,omitempty"`
}
