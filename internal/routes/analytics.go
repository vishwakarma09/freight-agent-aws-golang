package routes

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
)

type CarrierStat struct {
	ID         uint    `json:"id"`
	Name       string  `json:"name"`
	Email      string  `json:"email"`
	TotalBids  int64   `json:"total_bids"`
	Wins       int64   `json:"wins"`
	WinRatePct float64 `json:"win_rate_pct"`
	AvgBid     float64 `json:"avg_bid"`
}

type CustomerStat struct {
	ID             uint    `json:"id"`
	Name           string  `json:"name"`
	TotalQuotes    int64   `json:"total_quotes"`
	ApprovedQuotes int64   `json:"approved_quotes"`
	ConversionPct  float64 `json:"conversion_pct"`
}

type MonthlySpendPoint struct {
	Month string  `json:"month"`
	Spend float64 `json:"spend"`
}

func RegisterAnalyticsRoutes(rg *gin.RouterGroup) {
	analytics := rg.Group("/analytics", AuthMiddleware())
	{
		analytics.GET("", getAnalytics)
	}
}

func getAnalytics(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	// Optional date filter (?days=30|90|365)
	var since *time.Time
	if daysStr := c.Query("days"); daysStr != "" {
		if days, err := strconv.Atoi(daysStr); err == nil && days > 0 && days <= 3650 {
			t := time.Now().UTC().AddDate(0, 0, -days)
			since = &t
		}
	}

	baseQuotes := db.DB.Model(&models.FreightQuote{}).Where("organization_id = ?", org.ID)
	if since != nil {
		baseQuotes = baseQuotes.Where("created_at >= ?", *since)
	}

	// 1. Total Quotes count by status
	type StatusCount struct {
		Status string
		Count  int64
	}
	var statusCounts []StatusCount
	baseQuotes.Session(&gorm.Session{}).
		Select("status, count(id) as count").
		Group("status").
		Scan(&statusCounts)

	statusCountsMap := make(map[string]int64)
	for _, sc := range statusCounts {
		statusCountsMap[sc.Status] = sc.Count
	}

	pipelineStages := map[string]int64{
		"INTAKE":               statusCountsMap["INTAKE"],
		"OUT_TO_CARRIERS":      statusCountsMap["OUT_TO_CARRIERS"],
		"FIRST_ROUND_RECEIVED": statusCountsMap["FIRST_ROUND_RECEIVED"],
		"RE_BID_ROUND":         statusCountsMap["RE_BID_ROUND"],
		"QUOTE_SENT":           statusCountsMap["QUOTE_SENT"],
		"AWAITING_APPROVAL":    statusCountsMap["AWAITING_APPROVAL"],
		"APPROVED":             statusCountsMap["APPROVED"],
		"IN_TRANSIT":           statusCountsMap["IN_TRANSIT"],
		"COMPLETED":            statusCountsMap["COMPLETED"],
		"NO_BIDS_ALERT":        statusCountsMap["NO_BIDS_ALERT"],
		"LOST":                 statusCountsMap["LOST"],
	}

	// 2. Financial Metrics
	financialStates := []string{"APPROVED", "IN_TRANSIT", "COMPLETED"}
	type FinanceSummary struct {
		Receivables  float64
		Payables     float64
		MarginAmt    float64
		AvgMarginPct float64
	}
	var fs FinanceSummary
	finQ := db.DB.Model(&models.FreightQuote{}).
		Select("COALESCE(SUM(sell_price),0) as receivables, COALESCE(SUM(cost_price),0) as payables, COALESCE(SUM(margin_amt),0) as margin_amt, COALESCE(AVG(margin_pct),0) as avg_margin_pct").
		Where("status IN ? AND organization_id = ?", financialStates, org.ID)
	if since != nil {
		finQ = finQ.Where("created_at >= ?", *since)
	}
	finQ.Scan(&fs)

	receivables := math.Round(fs.Receivables*100.0) / 100.0
	payables := math.Round(fs.Payables*100.0) / 100.0
	marginAmt := math.Round(fs.MarginAmt*100.0) / 100.0
	avgMarginPct := math.Round(fs.AvgMarginPct*100.0) / 100.0

	// 3. Quote to Approval Conversion %
	var totalProposals int64
	propQ := db.DB.Model(&models.FreightQuote{}).
		Where("status IN ? AND organization_id = ?", []string{"AWAITING_APPROVAL", "APPROVED", "IN_TRANSIT", "COMPLETED", "LOST"}, org.ID)
	if since != nil {
		propQ = propQ.Where("created_at >= ?", *since)
	}
	propQ.Count(&totalProposals)

	var totalApproved int64
	appQ := db.DB.Model(&models.FreightQuote{}).
		Where("status IN ? AND organization_id = ?", financialStates, org.ID)
	if since != nil {
		appQ = appQ.Where("created_at >= ?", *since)
	}
	appQ.Count(&totalApproved)

	conversionPct := 0.0
	if totalProposals > 0 {
		conversionPct = math.Round(float64(totalApproved)/float64(totalProposals)*100.0*100.0) / 100.0
	}

	// 4. Average Turnaround Time
	var transitions []models.StateTransition
	db.DB.Joins("JOIN freight_quotes ON state_transitions.freight_quote_id = freight_quotes.id").
		Where("state_transitions.to_status = ? AND freight_quotes.organization_id = ?", "AWAITING_APPROVAL", org.ID).
		Find(&transitions)

	var totalDurationSeconds float64
	durationCount := 0
	for _, t := range transitions {
		var intakeT models.StateTransition
		if err := db.DB.Where("freight_quote_id = ? AND to_status = ?", t.FreightQuoteID, "OUT_TO_CARRIERS").First(&intakeT).Error; err == nil {
			delta := t.Timestamp.Sub(intakeT.Timestamp)
			if delta >= 0 {
				totalDurationSeconds += delta.Seconds()
				durationCount++
			}
		}
	}

	avgTurnaroundStr := "N/A"
	if durationCount > 0 {
		avgSeconds := totalDurationSeconds / float64(durationCount)
		minutes := int(avgSeconds) / 60
		seconds := int(avgSeconds) % 60
		avgTurnaroundStr = fmt.Sprintf("%dm %ds", minutes, seconds)
	}

	// 5. Real Monthly Spend Trend (last 6 months)
	monthlySpend := make([]MonthlySpendPoint, 0, 6)
	now := time.Now().UTC()
	for i := 5; i >= 0; i-- {
		mStart := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		mEnd := mStart.AddDate(0, 1, 0)
		var sum float64
		db.DB.Model(&models.FreightQuote{}).
			Select("COALESCE(SUM(sell_price), 0)").
			Where("organization_id = ? AND status IN ? AND created_at >= ? AND created_at < ?", org.ID, financialStates, mStart, mEnd).
			Scan(&sum)
		monthlySpend = append(monthlySpend, MonthlySpendPoint{
			Month: mStart.Format("JAN"),
			Spend: math.Round(sum*100.0) / 100.0,
		})
	}

	// 6. Carrier Stats
	var carriers []models.Carrier
	db.DB.Where("organization_id = ?", org.ID).Find(&carriers)

	carrierStats := make([]CarrierStat, 0, len(carriers))
	for _, carrier := range carriers {
		var totalBids int64
		db.DB.Model(&models.CarrierBid{}).
			Joins("JOIN freight_quotes ON freight_quotes.id = carrier_bids.freight_quote_id").
			Where("carrier_bids.carrier_id = ? AND freight_quotes.organization_id = ?", carrier.ID, org.ID).
			Count(&totalBids)

		var wins int64
		db.DB.Model(&models.FreightQuote{}).
			Where("winning_carrier_id = ? AND status IN ? AND organization_id = ?", carrier.ID, financialStates, org.ID).
			Count(&wins)

		winRatePct := 0.0
		if totalBids > 0 {
			winRatePct = math.Round(float64(wins)/float64(totalBids)*100.0*100.0) / 100.0
		}

		var effectiveWinRate float64
		var score float64
		if carrier.IsOverride {
			score = carrier.SimulatedScore
			effectiveWinRate = carrier.SimulatedScore * 10.0
		} else {
			score = math.Round(winRatePct/10.0*10.0) / 10.0
			effectiveWinRate = winRatePct
		}

		db.DB.Model(&carrier).Update("competitiveness_score", score)

		var avgBid float64
		db.DB.Model(&models.CarrierBid{}).
			Select("COALESCE(AVG(bid_amount), 0)").
			Where("carrier_id = ?", carrier.ID).
			Scan(&avgBid)

		carrierStats = append(carrierStats, CarrierStat{
			ID:         carrier.ID,
			Name:       carrier.Name,
			Email:      carrier.Email,
			TotalBids:  totalBids,
			Wins:       wins,
			WinRatePct: effectiveWinRate,
			AvgBid:     math.Round(avgBid*100.0) / 100.0,
		})
	}

	// 7. Customer Stats
	var customers []models.Customer
	db.DB.Where("organization_id = ?", org.ID).Find(&customers)

	customerStats := make([]CustomerStat, 0, len(customers))
	for _, customer := range customers {
		var totalQuotes int64
		db.DB.Model(&models.FreightQuote{}).
			Where("customer_id = ? AND organization_id = ?", customer.ID, org.ID).
			Count(&totalQuotes)

		var wins int64
		db.DB.Model(&models.FreightQuote{}).
			Where("customer_id = ? AND status IN ? AND organization_id = ?", customer.ID, financialStates, org.ID).
			Count(&wins)

		convPct := 0.0
		if totalQuotes > 0 {
			convPct = math.Round(float64(wins)/float64(totalQuotes)*100.0*100.0) / 100.0
		}

		customerStats = append(customerStats, CustomerStat{
			ID:             customer.ID,
			Name:           customer.Name,
			TotalQuotes:    totalQuotes,
			ApprovedQuotes: wins,
			ConversionPct:  convPct,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"pipeline_stages":                  pipelineStages,
		"receivables":                      receivables,
		"payables":                         payables,
		"gross_margin_value":               marginAmt,
		"average_margin_percent":           avgMarginPct,
		"quote_to_approval_conversion_pct": conversionPct,
		"average_turnaround_time":          avgTurnaroundStr,
		"monthly_spend":                    monthlySpend,
		"carrier_stats":                    carrierStats,
		"customer_stats":                   customerStats,
	})
}
