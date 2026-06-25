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

func RegisterSimulatorRoutes(rg *gin.RouterGroup) {
	simulator := rg.Group("/simulator", AuthMiddleware())
	{
		simulator.POST("/send-mock-email", sendMockEmail)
		simulator.POST("/fast-forward", fastForwardTimers)
		simulator.POST("/reset-database", resetDatabase)
		simulator.GET("/logs", getLogs)
		simulator.GET("/mailpit-messages", getMailpitMessages)
	}
}

func sendMockEmail(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var payload SimulateEmailRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	// 1. Determine target recipient based on credentials
	var creds models.EmailCredential
	targetRecipient := payload.Recipient
	if err := db.DB.Where("user_id = ?", currentUser.ID).First(&creds).Error; err == nil && creds.Email != "" {
		targetRecipient = creds.Email
	}

	// 2. Send via SMTP so Mailpit captures it
	services.SendEmail(targetRecipient, payload.Subject, payload.Body, payload.Sender)

	// 3. Mark processed in database by checking Mailpit
	time.Sleep(250 * time.Millisecond)
	mailpitHost := config.Settings.SMTPHost
	mailpitURLs := []string{
		fmt.Sprintf("http://%s:8025/api/v1/messages", mailpitHost),
		"http://mailpit:8025/api/v1/messages",
		"http://localhost:18025/api/v1/messages",
		"http://localhost:8025/api/v1/messages",
		"http://host.docker.internal:18025/api/v1/messages",
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	var mailpitResponse services.MailpitMessagesResponse
	registeredMailpit := false

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
						msgFrom := strings.TrimSpace(strings.ToLower(msg.From.Address))
						targetFrom := strings.TrimSpace(strings.ToLower(payload.Sender))

						if (strings.Contains(msgSubj, targetSubj) || strings.Contains(targetSubj, msgSubj)) &&
							(strings.Contains(msgFrom, targetFrom) || strings.Contains(targetFrom, msgFrom)) {
							db.DB.Create(&models.ProcessedEmail{ID: msg.ID, ProcessedAt: time.Now()})
							log.Printf("Marked Mailpit message %s as processed in DB\n", msg.ID)
							registeredMailpit = true
							break
						}
					}
				}
				break
			}
			resp.Body.Close()
		}
	}
	if !registeredMailpit {
		log.Println("Could not register email with Mailpit ID (mock bypass or connection error)")
	}

	// 4. Process email directly to return immediate response to simulator UI
	res, err := services.ProcessIncomingEmail(db.DB, payload.Sender, targetRecipient, payload.Subject, payload.Body, &currentUser.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func fastForwardTimers(c *gin.Context) {
	now := time.Now()

	// 1. Progress OUT_TO_CARRIERS
	var updatedRound1 int64
	db.DB.Model(&models.FreightQuote{}).
		Where("status = ? AND first_round_ends_at > ?", "OUT_TO_CARRIERS", now).
		Update("first_round_ends_at", now).
		Count(&updatedRound1)

	// 2. Progress RE_BID_ROUND
	var updatedRound2 int64
	db.DB.Model(&models.FreightQuote{}).
		Where("status = ? AND rebid_round_ends_at > ?", "RE_BID_ROUND", now).
		Update("rebid_round_ends_at", now).
		Count(&updatedRound2)

	// 3. Progress AWAITING_APPROVAL
	var updatedExpired int64
	db.DB.Model(&models.FreightQuote{}).
		Where("status = ? AND quote_expires_at > ?", "AWAITING_APPROVAL", now).
		Update("quote_expires_at", now).
		Count(&updatedExpired)

	// Trigger pending timers evaluation
	services.CheckPendingTimers(db.DB)

	totalProgressed := updatedRound1 + updatedRound2 + updatedExpired

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": fmt.Sprintf("Fast-forwarded %d active quotes.", totalProgressed),
		"details": gin.H{
			"round1_quotes":  updatedRound1,
			"round2_quotes":  updatedRound2,
			"expired_quotes": updatedExpired,
		},
	})
}

func resetDatabase(c *gin.Context) {
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		tx.Exec("DELETE FROM state_transitions")
		tx.Exec("DELETE FROM carrier_bids")
		tx.Exec("DELETE FROM rfqs")
		tx.Exec("DELETE FROM freight_quotes")
		tx.Exec("DELETE FROM processed_emails")
		return nil
	})

	if err != nil {
		log.Printf("Error resetting database: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"detail": fmt.Sprintf("Failed to reset database: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Database reset successfully."})
}

func getLogs(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 50
	}

	var transitions []models.StateTransition
	db.DB.Order("timestamp desc").Limit(limit).Find(&transitions)

	var bids []models.CarrierBid
	db.DB.Preload("Carrier").Order("received_at desc").Limit(limit).Find(&bids)

	logs := []SimulatorLog{}

	for _, t := range transitions {
		level := "INFO"
		if t.ToStatus == "APPROVED" || t.ToStatus == "COMPLETED" || t.ToStatus == "QUOTE_SENT" {
			level = "SUCCESS"
		} else if t.ToStatus == "LOST" {
			level = "WARN"
		}

		notes := ""
		if t.Notes != nil {
			notes = *t.Notes
		} else {
			notes = fmt.Sprintf("Transitioned from %s to %s", t.FromStatus, t.ToStatus)
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

	// Sort descending by timestamp
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
		"http://host.docker.internal:18025/api/v1/messages",
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
