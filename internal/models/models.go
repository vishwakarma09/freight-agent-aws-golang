package models

import (
	"time"
)

// Organization represents a tenant workspace. Every user belongs to an Organization.
// Multi-seat team workspaces (inviting additional members & RBAC) are unlocked on the Enterprise tier.
type Organization struct {
	ID                     uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                   string     `gorm:"not null" json:"name"`
	Slug                   string     `gorm:"uniqueIndex;not null" json:"slug"`
	PlanTier               string     `gorm:"default:'free';not null;column:plan_tier" json:"plan_tier"`           // free, boutique, growth, enterprise
	BillingStatus          string     `gorm:"default:'trialing';not null;column:billing_status" json:"billing_status"` // trialing, active, past_due, canceled
	StripeCustomerID       *string    `gorm:"index;column:stripe_customer_id" json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID   *string    `gorm:"index;column:stripe_subscription_id" json:"stripe_subscription_id,omitempty"`
	StripePriceID          *string    `gorm:"column:stripe_price_id" json:"stripe_price_id,omitempty"`
	TrialEndsAt            *time.Time `gorm:"column:trial_ends_at" json:"trial_ends_at,omitempty"`
	CurrentPeriodEnd       *time.Time `gorm:"column:current_period_end" json:"current_period_end,omitempty"`
	SeatLimit              int        `gorm:"default:1;not null;column:seat_limit" json:"seat_limit"`
	CarrierLimit           int        `gorm:"default:10;not null;column:carrier_limit" json:"carrier_limit"`             // -1 = unlimited
	InboxLimit             int        `gorm:"default:1;not null;column:inbox_limit" json:"inbox_limit"`                  // -1 = unlimited
	MonthlyQuoteLimit      int        `gorm:"default:25;not null;column:monthly_quote_limit" json:"monthly_quote_limit"` // included allowance
	QuotesUsedThisPeriod   int        `gorm:"default:0;not null;column:quotes_used_this_period" json:"quotes_used_this_period"`
	Round1DurationMinutes  int        `gorm:"default:120;not null;column:round1_duration_minutes" json:"round1_duration_minutes"`
	Round2DurationMinutes  int        `gorm:"default:30;not null;column:round2_duration_minutes" json:"round2_duration_minutes"`
	CreatedAt              time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt              time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// OrganizationInvite represents a pending seat invitation (Enterprise tier only).
type OrganizationInvite struct {
	ID              uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	OrganizationID  uint       `gorm:"index;not null;column:organization_id" json:"organization_id"`
	Email           string     `gorm:"index;not null" json:"email"`
	Role            string     `gorm:"default:'broker';not null" json:"role"` // admin, broker, accounting, viewer
	TokenHash       string     `gorm:"uniqueIndex;not null;column:token_hash" json:"-"`
	ExpiresAt       time.Time  `gorm:"not null;column:expires_at" json:"expires_at"`
	AcceptedAt      *time.Time `gorm:"column:accepted_at" json:"accepted_at,omitempty"`
	InvitedByUserID uint       `gorm:"column:invited_by_user_id" json:"invited_by_user_id"`
	CreatedAt       time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// StripeWebhookEvent provides strict idempotency for processed Stripe webhook events.
type StripeWebhookEvent struct {
	EventID     string    `gorm:"primaryKey;size:255;column:event_id" json:"event_id"`
	EventType   string    `gorm:"size:128;not null;column:event_type" json:"event_type"`
	ProcessedAt time.Time `gorm:"default:CURRENT_TIMESTAMP;column:processed_at" json:"processed_at"`
}

// QuoteUsageEvent tracks billable quote ingestion events and Stripe Billing Meter reporting.
type QuoteUsageEvent struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	OrganizationID     uint      `gorm:"index;not null;column:organization_id" json:"organization_id"`
	FreightQuoteID     string    `gorm:"index;not null;column:freight_quote_id" json:"freight_quote_id"`
	EventType          string    `gorm:"size:64;not null;column:event_type" json:"event_type"` // included, metered_overage
	Quantity           int       `gorm:"default:1;not null" json:"quantity"`
	StripeMeterEventID *string   `gorm:"column:stripe_meter_event_id" json:"stripe_meter_event_id,omitempty"`
	CreatedAt          time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

type User struct {
	ID                  uint          `gorm:"primaryKey;autoIncrement" json:"id"`
	OrganizationID      uint          `gorm:"index;column:organization_id" json:"organization_id"`
	Role                string        `gorm:"default:'owner';not null" json:"role"` // owner, admin, broker, accounting, viewer
	Name                string        `gorm:"not null" json:"name"`
	Email               string        `gorm:"uniqueIndex;not null" json:"email"`
	HashedPassword      *string       `gorm:"column:hashed_password" json:"-"`
	IsActive            bool          `gorm:"default:false;not null;column:is_active" json:"is_active"`
	ActivationToken     *string       `gorm:"uniqueIndex;column:activation_token" json:"-"`
	ActivationExpiresAt *time.Time    `gorm:"column:activation_expires_at" json:"-"`
	ResetTokenHash      *string       `gorm:"index;column:reset_token_hash" json:"-"`
	ResetTokenExpiresAt *time.Time    `gorm:"column:reset_token_expires_at" json:"-"`
	CreatedAt           time.Time     `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt           time.Time     `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	Organization        *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
}

type Customer struct {
	ID                   uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	OrganizationID       uint           `gorm:"uniqueIndex:idx_customer_org_email;index;column:organization_id" json:"organization_id"`
	UserID               *uint          `gorm:"column:user_id;index" json:"user_id,omitempty"`
	Name                 string         `gorm:"not null" json:"name"`
	Email                string         `gorm:"uniqueIndex:idx_customer_org_email;not null" json:"email"`
	DefaultMarkupPercent float64        `gorm:"default:10.0;column:default_markup_percent" json:"default_markup_percent"`
	CreatedAt            time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	Quotes               []FreightQuote `gorm:"foreignKey:CustomerID" json:"-"`
}

type Carrier struct {
	ID                             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	OrganizationID                 uint           `gorm:"index;column:organization_id" json:"organization_id"`
	UserID                         *uint          `gorm:"column:user_id;index" json:"user_id,omitempty"`
	Name                           string         `gorm:"not null" json:"name"`
	Email                          string         `gorm:"index;not null" json:"email"`
	CompetitivenessScore           float64        `gorm:"default:0.0;column:competitiveness_score" json:"competitiveness_score"`
	IsOverride                     bool           `gorm:"default:false;not null;column:is_override" json:"is_override"`
	SimulatedScore                 float64        `gorm:"default:0.0;not null;column:simulated_score" json:"simulated_score"`
	CreatedAt                      time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt                      time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	Bids                           []CarrierBid   `gorm:"foreignKey:CarrierID;constraint:OnDelete:CASCADE" json:"-"`
	QuotesWon                      []FreightQuote `gorm:"foreignKey:WinningCarrierID" json:"-"`
	CalculatedCompetitivenessScore float64        `gorm:"-" json:"calculated_competitiveness_score"`
}

type FreightQuote struct {
	ID               string     `gorm:"primaryKey" json:"id"` // e.g. "Q-1001"
	OrganizationID   uint       `gorm:"index;column:organization_id" json:"organization_id"`
	UserID           *uint      `gorm:"column:user_id;index" json:"user_id,omitempty"`
	CustomerID       uint       `gorm:"not null;column:customer_id" json:"customer_id"`
	Status           string     `gorm:"default:'INTAKE';not null" json:"status"`
	Origin           string     `gorm:"not null" json:"origin"`
	Destination      string     `gorm:"not null" json:"destination"`
	WeightLbs        float64    `gorm:"not null;column:weight_lbs" json:"weight_lbs"`
	Dimensions       *string    `json:"dimensions"`
	FreightClass     *string    `gorm:"column:freight_class" json:"freight_class"`
	Hazmat           bool       `gorm:"default:false" json:"hazmat"`
	Accessorials     *string    `json:"accessorials"`
	PickupDate       time.Time  `gorm:"not null;column:pickup_date" json:"pickup_date"`
	FirstRoundEndsAt *time.Time `gorm:"column:first_round_ends_at" json:"first_round_ends_at"`
	RebidRoundEndsAt *time.Time `gorm:"column:rebid_round_ends_at" json:"rebid_round_ends_at"`
	QuoteExpiresAt   *time.Time `gorm:"column:quote_expires_at" json:"quote_expires_at"`

	CostPrice     float64 `gorm:"default:0.0;column:cost_price" json:"cost_price"`
	SellPrice     float64 `gorm:"default:0.0;column:sell_price" json:"sell_price"`
	MarkupPercent float64 `gorm:"default:0.0;column:markup_percent" json:"markup_percent"`
	MarginAmt     float64 `gorm:"default:0.0;column:margin_amt" json:"margin_amt"`
	MarginPct     float64 `gorm:"default:0.0;column:margin_pct" json:"margin_pct"`

	WinningCarrierID *uint   `gorm:"column:winning_carrier_id" json:"winning_carrier_id"`
	LostReason       *string `gorm:"column:lost_reason" json:"lost_reason"`
	CompetitorInfo   *string `gorm:"column:competitor_info" json:"competitor_info"`

	PaymentStatus string  `gorm:"default:'UNPAID';column:payment_status" json:"payment_status"`
	BolURL        *string `gorm:"column:bol_url" json:"bol_url"`
	InvoiceURL    *string `gorm:"column:invoice_url" json:"invoice_url"`
	BolS3Key      *string `gorm:"column:bol_s3_key" json:"bol_s3_key,omitempty"`
	InvoiceS3Key  *string `gorm:"column:invoice_s3_key" json:"invoice_s3_key,omitempty"`
	ApprovalToken *string `gorm:"column:approval_token" json:"-"`

	EmailThreadID  *string `gorm:"column:email_thread_id" json:"email_thread_id,omitempty"`
	EmailMessageID *string `gorm:"column:email_message_id" json:"email_message_id,omitempty"`

	ShipmentVector string `gorm:"column:shipment_vector;type:vector(1536)" json:"-"`

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	Customer       *Customer         `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	WinningCarrier *Carrier          `gorm:"foreignKey:WinningCarrierID" json:"winning_carrier,omitempty"`
	Bids           []CarrierBid      `gorm:"foreignKey:FreightQuoteID" json:"bids"`
	Transitions    []StateTransition `gorm:"foreignKey:FreightQuoteID" json:"transitions"`
	RFQs           []RequestForQuote `gorm:"foreignKey:FreightQuoteID" json:"rfqs"`
}

type CarrierBid struct {
	ID               uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	FreightQuoteID   string    `gorm:"not null;column:freight_quote_id" json:"freight_quote_id"`
	CarrierID        uint      `gorm:"not null;column:carrier_id" json:"carrier_id"`
	Round            int       `gorm:"default:1" json:"round"`
	BidAmount        float64   `gorm:"not null;column:bid_amount" json:"bid_amount"`
	TransitTimeDays  int       `gorm:"column:transit_time_days" json:"transit_time_days"`
	PickupWindow     *string   `gorm:"column:pickup_window" json:"pickup_window"`
	AccessorialsText *string   `gorm:"column:accessorials_text" json:"accessorials_text"`
	ServiceLevel     *string   `gorm:"column:service_level" json:"service_level"`
	Notes            *string   `json:"notes"`
	ReceivedAt       time.Time `gorm:"default:CURRENT_TIMESTAMP;column:received_at" json:"received_at"`
	IsWinning        bool      `gorm:"default:false;column:is_winning" json:"is_winning"`
	RawEmail         *string   `gorm:"column:raw_email" json:"raw_email,omitempty"`
	Carrier          *Carrier  `gorm:"foreignKey:CarrierID" json:"carrier,omitempty"`
}

type StateTransition struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	FreightQuoteID string    `gorm:"not null;column:freight_quote_id" json:"freight_quote_id"`
	FromStatus     string    `gorm:"not null;column:from_status" json:"from_status"`
	ToStatus       string    `gorm:"not null;column:to_status" json:"to_status"`
	Timestamp      time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"timestamp"`
	Notes          *string   `json:"notes"`
}

type EmailCredential struct {
	ID                    uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	OrganizationID        uint      `gorm:"index;column:organization_id" json:"organization_id"`
	UserID                *uint     `gorm:"index;column:user_id" json:"user_id,omitempty"`
	UserEmail             string    `gorm:"size:100;not null;column:user_email" json:"user_email"`
	EmailProvider         string    `gorm:"size:50;default:'Gmail';not null;column:email_provider" json:"email_provider"`
	Email                 string    `gorm:"size:100;not null" json:"email"`
	SMTPHost              string    `gorm:"size:100;default:'smtp.gmail.com';not null;column:smtp_host" json:"smtp_host"`
	SMTPPort              int       `gorm:"default:587;not null;column:smtp_port" json:"smtp_port"`
	EncryptedSMTPPassword string    `gorm:"size:1024;not null;column:encrypted_smtp_password" json:"-"`
	IMAPHost              string    `gorm:"size:100;default:'imap.gmail.com';not null;column:imap_host" json:"imap_host"`
	IMAPPort              int       `gorm:"default:993;not null;column:imap_port" json:"imap_port"`
	EncryptedIMAPPassword string    `gorm:"size:1024;not null;column:encrypted_imap_password" json:"-"`
	UseDevMode            bool      `gorm:"default:false;not null;column:use_dev_mode" json:"use_dev_mode"`
	CreatedAt             time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt             time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// Masked indicators safe for API responses (never return ciphertexts or plaintexts)
	HasSMTPPassword bool `gorm:"-" json:"has_smtp_password"`
	HasIMAPPassword bool `gorm:"-" json:"has_imap_password"`
}

func (e *EmailCredential) PopulateMasks() {
	e.HasSMTPPassword = e.EncryptedSMTPPassword != ""
	e.HasIMAPPassword = e.EncryptedIMAPPassword != ""
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
