package routes

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/services"
)

type SimulateEmailRequest struct {
	Sender    string `json:"sender" binding:"required"`
	Recipient string `json:"recipient" binding:"required"`
	Subject   string `json:"subject" binding:"required"`
	Body      string `json:"body" binding:"required"`
}

type SimulatorLog struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// simulatorDevOnlyGuard completely disables simulator endpoints in production (P0-2 fix).
func simulatorDevOnlyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.Settings != nil && config.Settings.IsProd() {
			c.JSON(http.StatusForbidden, gin.H{"detail": "Simulator endpoints are disabled in production environments"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func RegisterSimulatorRoutes(rg *gin.RouterGroup) {
	simulator := rg.Group("/simulator", simulatorDevOnlyGuard(), AuthMiddleware())
	{
		simulator.POST("/send-mock-email", sendMockEmail)
		simulator.POST("/fast-forward", fastForwardTimers)
		simulator.POST("/reset-database", RequireRoles("owner", "admin"), resetDatabase)
		simulator.GET("/logs", getLogs)
		simulator.GET("/mailpit-messages", getMailpitMessages)
	}
}

func sendMockEmail(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	var payload SimulateEmailRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var creds models.EmailCredential
	targetRecipient := payload.Recipient
	if err := db.DB.Where("organization_id = ?", org.ID).First(&creds).Error; err == nil && creds.Email != "" {
		targetRecipient = creds.Email
	}

	services.SendEmail(targetRecipient, payload.Subject, payload.Body, payload.Sender)

	time.Sleep(250 * time.Millisecond)
	mailpitHost := config.Settings.SMTPHost
	mailpitURLs := []string{
		fmt.Sprintf("http://%s:8025/api/v1/messages", mailpitHost),
		"http://mailpit:8025/api/v1/messages",
		"http://localhost:18025/api/v1/messages",
		"http://localhost:8025/api/v1/messages",
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	var mailpitResponse services.MailpitMessagesResponse
	for _, url := range mailpitURLs {
		resp, err := httpClient.Get(url + "?limit=10")
		if err == nil && resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&mailpitResponse); err == nil {
				resp.Body.Close()
				for _, msg := range mailpitResponse.Messages {
					var processed models.ProcessedEmail
					if err := db.DB.Where("id = ?", msg.ID).First(&processed).Error; err != nil {
						msgSubj := strings.TrimSpace(strings.ToLower(msg.Subject))
						targetSubj := strings.TrimSpace(strings.ToLower(payload.Subject))
						msgFrom := ""
						if msg.From != nil {
							msgFrom = strings.TrimSpace(strings.ToLower(msg.From.Address))
						}
						targetFrom := strings.TrimSpace(strings.ToLower(payload.Sender))

						if (strings.Contains(msgSubj, targetSubj) || strings.Contains(targetSubj, msgSubj)) &&
							(strings.Contains(msgFrom, targetFrom) || strings.Contains(targetFrom, msgFrom)) {
							db.DB.Create(&models.ProcessedEmail{ID: msg.ID, ProcessedAt: time.Now().UTC()})
							break
						}
					}
				}
				break
			}
			resp.Body.Close()
		}
	}

	res, err := services.ProcessIncomingEmail(db.DB, payload.Sender, targetRecipient, payload.Subject, payload.Body, org.ID, &currentUser.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

// fastForwardTimers strictly scopes timer fast-forwarding to the caller's Organization (P0-2 fix).
func fastForwardTimers(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	now := time.Now().UTC()

	r1Res := db.DB.Model(&models.FreightQuote{}).
		Where("organization_id = ? AND status = ? AND first_round_ends_at > ?", org.ID, "OUT_TO_CARRIERS", now).
		Update("first_round_ends_at", now)

	r2Res := db.DB.Model(&models.FreightQuote{}).
		Where("organization_id = ? AND status = ? AND rebid_round_ends_at > ?", org.ID, "RE_BID_ROUND", now).
		Update("rebid_round_ends_at", now)

	expRes := db.DB.Model(&models.FreightQuote{}).
		Where("organization_id = ? AND status = ? AND quote_expires_at > ?", org.ID, "AWAITING_APPROVAL", now).
		Update("quote_expires_at", now)

	services.CheckPendingTimersForOrg(db.DB, org.ID)

	totalProgressed := r1Res.RowsAffected + r2Res.RowsAffected + expRes.RowsAffected

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": fmt.Sprintf("Fast-forwarded %d active quotes in your workspace.", totalProgressed),
		"details": gin.H{
			"round1_quotes":  r1Res.RowsAffected,
			"round2_quotes":  r2Res.RowsAffected,
			"expired_quotes": expRes.RowsAffected,
		},
	})
}

// resetDatabase strictly deletes ONLY quotes, bids, RFQs, and transitions belonging to the caller's Organization (P0-2 fix).
func resetDatabase(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var quoteIDs []string
		if err := tx.Model(&models.FreightQuote{}).Where("organization_id = ?", org.ID).Pluck("id", &quoteIDs).Error; err != nil {
			return err
		}
		if len(quoteIDs) > 0 {
			if err := tx.Where("freight_quote_id IN ?", quoteIDs).Delete(&models.StateTransition{}).Error; err != nil {
				return err
			}
			if err := tx.Where("freight_quote_id IN ?", quoteIDs).Delete(&models.CarrierBid{}).Error; err != nil {
				return err
			}
			if err := tx.Where("freight_quote_id IN ?", quoteIDs).Delete(&models.RequestForQuote{}).Error; err != nil {
				return err
			}
			if err := tx.Where("organization_id = ?", org.ID).Delete(&models.FreightQuote{}).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Error resetting workspace data for org %d: %v\n", org.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to reset workspace simulation data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Workspace simulation quotes cleared successfully."})
}

// getLogs strictly scopes transition and bid logs to the caller's Organization (P0-2 fix).
func getLogs(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}

	var transitions []models.StateTransition
	db.DB.Joins("JOIN freight_quotes ON freight_quotes.id = state_transitions.freight_quote_id").
		Where("freight_quotes.organization_id = ?", org.ID).
		Order("state_transitions.timestamp desc").
		Limit(limit).
		Find(&transitions)

	var bids []models.CarrierBid
	db.DB.Preload("Carrier").
		Joins("JOIN freight_quotes ON freight_quotes.id = carrier_bids.freight_quote_id").
		Where("freight_quotes.organization_id = ?", org.ID).
		Order("carrier_bids.received_at desc").
		Limit(limit).
		Find(&bids)

	logs := make([]SimulatorLog, 0, len(transitions)+len(bids))

	for _, t := range transitions {
		level := "INFO"
		if t.ToStatus == "APPROVED" || t.ToStatus == "COMPLETED" || t.ToStatus == "QUOTE_SENT" {
			level = "SUCCESS"
		} else if t.ToStatus == "LOST" || t.ToStatus == "NO_BIDS_ALERT" {
			level = "WARN"
		}

		notes := fmt.Sprintf("Transitioned from %s to %s", t.FromStatus, t.ToStatus)
		if t.Notes != nil {
			notes = *t.Notes
		}

		logs = append(logs, SimulatorLog{
			Timestamp: t.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Level:     level,
			Message:   fmt.Sprintf("Quote %s: %s", t.FreightQuoteID, notes),
		})
	}

	for _, b := range bids {
		carrierName := "Carrier"
		if b.Carrier != nil {
			carrierName = b.Carrier.Name
		}
		logs = append(logs, SimulatorLog{
			Timestamp: b.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Level:     "SUCCESS",
			Message:   fmt.Sprintf("Quote %s: Bid received from %s for $%.2f (Round %d)", b.FreightQuoteID, carrierName, b.BidAmount, b.Round),
		})
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp > logs[j].Timestamp
	})

	if len(logs) > limit {
		logs = logs[:limit]
	}

	c.JSON(http.StatusOK, logs)
}

func getMailpitMessages(c *gin.Context) {
	mailpitHost := config.Settings.SMTPHost
	urls := []string{
		fmt.Sprintf("http://%s:8025/api/v1/messages", mailpitHost),
		"http://mailpit:8025/api/v1/messages",
		"http://localhost:18025/api/v1/messages",
		"http://localhost:8025/api/v1/messages",
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	for _, url := range urls {
		resp, err := httpClient.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var raw interface{}
			if err := json.NewDecoder(resp.Body).Decode(&raw); err == nil {
				c.JSON(http.StatusOK, raw)
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"messages": []interface{}{}})
}
