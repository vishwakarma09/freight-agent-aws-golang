package services

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

// Mailpit Message structures
type MailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

type MailpitMessageSummary struct {
	ID      string           `json:"ID"`
	From    *MailpitAddress  `json:"From"`
	To      []MailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
	Size    int              `json:"Size"`
}

type MailpitMessagesResponse struct {
	Messages []MailpitMessageSummary `json:"messages"`
}

type MailpitMessageDetail struct {
	ID      string          `json:"ID"`
	From    *MailpitAddress `json:"From"`
	To      []MailpitAddress
	Subject string          `json:"Subject"`
	Text    string          `json:"Text"`
	HTML    string          `json:"HTML"`
}

// SendEmail sends an SMTP email. In dev, this routes directly to Mailpit.
func SendEmail(toEmail string, subject string, bodyHTML string, fromEmail string) bool {
	if fromEmail == "" {
		fromEmail = config.Settings.EmailsFromEmail
		if fromEmail == "" {
			fromEmail = config.Settings.BrokerEmail
			if fromEmail == "" {
				fromEmail = "broker@dispatch.owera.ca"
			}
		}
	}

	host := config.Settings.SMTPHost
	port := config.Settings.SMTPPort
	user := config.Settings.SMTPUser
	password := config.Settings.SMTPPassword

	// Construct email body headers
	header := make(map[string]string)
	header["From"] = fromEmail
	header["To"] = toEmail
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = `text/html; charset="utf-8"`

	var msgBuilder strings.Builder
	for k, v := range header {
		msgBuilder.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msgBuilder.WriteString("\r\n")
	msgBuilder.WriteString(bodyHTML)
	msg := msgBuilder.String()

	addr := fmt.Sprintf("%s:%d", host, port)

	log.Printf("Connecting to SMTP server at %s...\n", addr)

	var err error
	if port == 465 {
		// Implicit TLS/SSL
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         host,
		}
		var conn *tls.Conn
		conn, err = tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			log.Printf("Failed to dial SMTP SSL: %v\n", err)
			return false
		}
		defer conn.Close()

		var c *smtp.Client
		c, err = smtp.NewClient(conn, host)
		if err != nil {
			log.Printf("Failed to create SMTP client: %v\n", err)
			return false
		}
		defer c.Close()

		if user != "" && password != "" {
			auth := smtp.PlainAuth("", user, password, host)
			if err = c.Auth(auth); err != nil {
				log.Printf("SMTP Auth failed: %v\n", err)
				return false
			}
		}

		if err = c.Mail(fromEmail); err != nil {
			log.Printf("SMTP Mail command failed: %v\n", err)
			return false
		}
		if err = c.Rcpt(toEmail); err != nil {
			log.Printf("SMTP Rcpt command failed: %v\n", err)
			return false
		}

		w, err := c.Data()
		if err != nil {
			log.Printf("SMTP Data command failed: %v\n", err)
			return false
		}
		_, err = w.Write([]byte(msg))
		if err != nil {
			log.Printf("SMTP Write failed: %v\n", err)
			return false
		}
		_ = w.Close()
		_ = c.Quit()
	} else {
		// STARTTLS or Non-secure
		var c *smtp.Client
		c, err = smtp.Dial(addr)
		if err != nil {
			log.Printf("Failed to dial SMTP: %v\n", err)
			return false
		}
		defer c.Close()

		if config.Settings.SMTPTLS || port == 587 {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         host,
			}
			if err = c.StartTLS(tlsConfig); err != nil {
				log.Printf("SMTP StartTLS failed: %v\n", err)
				return false
			}
		}

		if user != "" && password != "" {
			auth := smtp.PlainAuth("", user, password, host)
			if err = c.Auth(auth); err != nil {
				log.Printf("SMTP Auth failed: %v\n", err)
				return false
			}
		}

		if err = c.Mail(fromEmail); err != nil {
			log.Printf("SMTP Mail command failed: %v\n", err)
			return false
		}
		if err = c.Rcpt(toEmail); err != nil {
			log.Printf("SMTP Rcpt command failed: %v\n", err)
			return false
		}

		w, err := c.Data()
		if err != nil {
			log.Printf("SMTP Data command failed: %v\n", err)
			return false
		}
		_, err = w.Write([]byte(msg))
		if err != nil {
			log.Printf("SMTP Write failed: %v\n", err)
			return false
		}
		_ = w.Close()
		_ = c.Quit()
	}

	log.Printf("Email sent successfully to %s with subject: %s\n", toEmail, subject)
	return true
}

// CleanEmail extracts the clean email address from "Name <email>" format
func CleanEmail(sender string) string {
	re := regexp.MustCompile(`[\w\.-]+@[\w\.-]+`)
	matches := re.FindAllString(sender, -1)
	if len(matches) > 0 {
		return strings.ToLower(strings.TrimSpace(matches[0]))
	}
	return strings.ToLower(strings.TrimSpace(sender))
}

// ProcessIncomingEmail routes and processes emails (customer approvals, bids, or inquiries)
func ProcessIncomingEmail(db *gorm.DB, sender, recipient, subject, body string, userID *uint) (map[string]interface{}, error) {
	sender = strings.TrimSpace(sender)
	recipient = strings.TrimSpace(recipient)
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)

	// 1. Check if there's a Quote ID in the subject or body (e.g. Q-1001)
	quoteIDRe := regexp.MustCompile(`(Q-\d{4})`)
	quoteIDMatch := quoteIDRe.FindStringSubmatch(subject + " " + body)

	if len(quoteIDMatch) > 1 {
		quoteID := quoteIDMatch[1]
		var quote models.FreightQuote
		query := db.Preload("Customer")
		if userID != nil {
			query = query.Where("user_id = ?", *userID)
		}
		err := query.Where("id = ?", quoteID).First(&quote).Error
		if err == nil {
			senderClean := CleanEmail(sender)
			customerEmail := ""
			if quote.Customer != nil {
				customerEmail = strings.ToLower(quote.Customer.Email)
			}
			bodyLower := strings.ToLower(body)

			// If sender is the customer OR email explicitly contains approval/rejection keywords
			isCustomerReply := (customerEmail != "" && senderClean == customerEmail) ||
				strings.Contains(bodyLower, "approved") || strings.Contains(bodyLower, "approve") ||
				strings.Contains(bodyLower, "book") || strings.Contains(bodyLower, "rejected") ||
				strings.Contains(bodyLower, "reject") || strings.Contains(bodyLower, "decline")

			if isCustomerReply {
				if strings.Contains(bodyLower, "approved") || strings.Contains(bodyLower, "approve") || strings.Contains(bodyLower, "book") {
					if quote.Status == "AWAITING_APPROVAL" {
						err = TransitionQuote(db, &quote, "APPROVED", "Customer approved the proposal via email")
						if err != nil {
							return nil, err
						}
						return map[string]interface{}{
							"type":     "CUSTOMER_APPROVAL",
							"quote_id": quote.ID,
							"status":   "APPROVED",
							"message":  "Quote approved via email",
						}, nil
					}
				} else if strings.Contains(bodyLower, "rejected") || strings.Contains(bodyLower, "reject") || strings.Contains(bodyLower, "decline") {
					if quote.Status == "AWAITING_APPROVAL" {
						lostReason := "Customer rejected via email"
						quote.LostReason = &lostReason
						err = TransitionQuote(db, &quote, "LOST", "Customer rejected the proposal via email")
						if err != nil {
							return nil, err
						}
						return map[string]interface{}{
							"type":     "CUSTOMER_REJECTION",
							"quote_id": quote.ID,
							"status":   "LOST",
							"message":  "Quote rejected via email",
						}, nil
					}
				}
			}

			// Treat it as a carrier bid
			var carrier models.Carrier
			carrierQuery := db.Where("email = ?", senderClean)
			if userID != nil {
				carrierQuery = carrierQuery.Where("user_id = ?", *userID)
			}
			err = carrierQuery.First(&carrier).Error
			if err != nil {
				// Seed carrier
				name := strings.Title(strings.ReplaceAll(strings.ReplaceAll(strings.Split(senderClean, "@")[0], "carrier_", ""), ".", " "))
				carrier = models.Carrier{
					Name:                 name,
					Email:                senderClean,
					UserID:               userID,
					CompetitivenessScore: 0.0,
				}
				db.Create(&carrier)
			}

			bidData := ParseCarrierBidEmail(body)

			currentRound := 1
			if quote.Status == "RE_BID_ROUND" {
				currentRound = 2
			}

			var existingBid models.CarrierBid
			err = db.Where("freight_quote_id = ? AND carrier_id = ? AND round = ?", quote.ID, carrier.ID, currentRound).First(&existingBid).Error

			var bidRec models.CarrierBid
			if err == nil {
				// Update existing bid
				existingBid.BidAmount = bidData.BidAmount
				existingBid.TransitTimeDays = bidData.TransitTimeDays
				notes := fmt.Sprintf("Updated bid via email. Original: %s", getSafeString(existingBid.Notes))
				existingBid.Notes = &notes
				db.Save(&existingBid)
				bidRec = existingBid
			} else {
				// Create new bid
				bidRec = models.CarrierBid{
					FreightQuoteID:   quote.ID,
					CarrierID:        carrier.ID,
					Round:            currentRound,
					BidAmount:        bidData.BidAmount,
					TransitTimeDays:  bidData.TransitTimeDays,
					PickupWindow:     &bidData.PickupWindow,
					AccessorialsText: &bidData.AccessorialsText,
					ServiceLevel:     &bidData.ServiceLevel,
					Notes:            &bidData.Notes,
					ReceivedAt:       time.Now(),
					IsWinning:        false,
					RawEmail:         &body,
				}
				db.Create(&bidRec)
			}

			log.Printf("Recorded bid of $%.2f by carrier %s for quote %s (Round %d)\n", bidRec.BidAmount, carrier.Name, quote.ID, currentRound)

			return map[string]interface{}{
				"type":        "CARRIER_BID",
				"quote_id":    quote.ID,
				"carrier":     carrier.Name,
				"round":       currentRound,
				"bid_amount":  bidRec.BidAmount,
				"parsed_data": bidData,
			}, nil
		}
	}

	// 2. Unrecognized subject or body -> Check if recipient has broker/freight (Customer Inquiry)
	recipientLower := strings.ToLower(recipient)
	if strings.Contains(recipientLower, "broker") || strings.Contains(recipientLower, "freight") {
		senderClean := CleanEmail(sender)

		var customer models.Customer
		err := db.Where("email = ?", senderClean).First(&customer).Error
		if err != nil {
			name := strings.Title(strings.ReplaceAll(strings.Split(senderClean, "@")[0], ".", " "))
			customer = models.Customer{
				Name:                 name,
				Email:                senderClean,
				UserID:               userID,
				DefaultMarkupPercent: 12.0, // default simulator markup
			}
			db.Create(&customer)
		} else if customer.UserID == nil && userID != nil {
			customer.UserID = userID
			db.Save(&customer)
		}

		extracted := ParseCustomerEmail(subject, body, senderClean)

		// Generate quote ID
		var count int64
		db.Model(&models.FreightQuote{}).Count(&count)
		var quoteID string
		for {
			quoteID = fmt.Sprintf("Q-%d", 1000+count+1)
			var exists int64
			db.Model(&models.FreightQuote{}).Where("id = ?", quoteID).Count(&exists)
			if exists == 0 {
				break
			}
			count++
		}

		// Generate embedding
		desc := fmt.Sprintf("Origin: %s, Destination: %s, Class: %s, Weight: %.2f lbs", extracted.Origin, extracted.Destination, extracted.FreightClass, extracted.WeightLbs)
		vector := GetEmbedding(desc)

		pickupTime := time.Now().AddDate(0, 0, 3)
		if extracted.PickupDate != "" {
			if t, err := time.Parse("2006-01-02", extracted.PickupDate); err == nil {
				pickupTime = t
			}
		}

		accs := strings.Join(extracted.Accessorials, ", ")

		quote := models.FreightQuote{
			ID:             quoteID,
			UserID:         userID,
			CustomerID:     customer.ID,
			Status:         "INTAKE",
			Origin:         extracted.Origin,
			Destination:    extracted.Destination,
			WeightLbs:      extracted.WeightLbs,
			Dimensions:     &extracted.Dimensions,
			FreightClass:   &extracted.FreightClass,
			Hazmat:         extracted.Hazmat,
			Accessorials:   &accs,
			PickupDate:     pickupTime,
			ShipmentVector: FormatVector(vector),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		db.Create(&quote)

		// State transitions
		_ = TransitionQuote(db, &quote, "INTAKE", fmt.Sprintf("Email intake from %s", senderClean))
		_ = TransitionQuote(db, &quote, "OUT_TO_CARRIERS", "Broadcasting RFQs to carrier network")

		return map[string]interface{}{
			"type":        "CUSTOMER_INQUIRY",
			"quote_id":    quote.ID,
			"parsed_data": extracted,
			"status":      "Quote created and RFQs broadcasted",
		}, nil
	}

	return nil, fmt.Errorf("unrecognized email. To create a quote, send to broker@dispatch.owera.ca. To submit a carrier bid, ensure the subject contains 'Q-XXXX'")
}

func getSafeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PollAndIngestEmails polls configured email inboxes
func PollAndIngestEmails(db *gorm.DB) {
	var credsList []models.EmailCredential
	if err := db.Find(&credsList).Error; err != nil {
		log.Printf("Error checking email credentials list: %v\n", err)
		return
	}

	for _, creds := range credsList {
		if creds.UseDevMode && config.Settings.Env != "prod" {
			pollMailpit(db, &creds)
		} else {
			pollRealIMAP(db, &creds)
		}
	}
}

func pollMailpit(db *gorm.DB, creds *models.EmailCredential) {
	mailpitHost := config.Settings.SMTPHost
	url := "http://mailpit:8025/api/v1/messages"
	if mailpitHost != "mailpit" {
		url = fmt.Sprintf("http://%s:8025/api/v1/messages", mailpitHost)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url + "?limit=50")
	if err != nil {
		log.Printf("Error polling Mailpit: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Mailpit API returned status %d\n", resp.StatusCode)
		return
	}

	var mailpitResponse MailpitMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mailpitResponse); err != nil {
		log.Printf("Error decoding Mailpit response: %v\n", err)
		return
	}

	for _, msg := range mailpitResponse.Messages {
		var processed models.ProcessedEmail
		if err := db.Where("id = ?", msg.ID).First(&processed).Error; err == nil {
			continue // Already processed
		}

		// Recipient matching
		credEmail := strings.TrimSpace(strings.ToLower(creds.Email))
		recipientMatched := false
		for _, to := range msg.To {
			if strings.Contains(strings.ToLower(to.Address), credEmail) {
				recipientMatched = true
				break
			}
		}
		if !recipientMatched {
			continue
		}

		// Fetch detail
		detailURL := ""
		if mailpitHost != "mailpit" {
			detailURL = fmt.Sprintf("http://%s:8025/api/v1/message/%s", mailpitHost, msg.ID)
		} else {
			detailURL = fmt.Sprintf("http://mailpit:8025/api/v1/message/%s", msg.ID)
		}

		detailResp, err := client.Get(detailURL)
		if err != nil {
			continue
		}

		var detail MailpitMessageDetail
		err = json.NewDecoder(detailResp.Body).Decode(&detail)
		detailResp.Body.Close()
		if err != nil {
			continue
		}

		sender := ""
		if detail.From != nil {
			if detail.From.Name != "" {
				sender = fmt.Sprintf("%s <%s>", detail.From.Name, detail.From.Address)
			} else {
				sender = detail.From.Address
			}
		}

		body := detail.Text
		if body == "" {
			body = detail.HTML
		}

		log.Printf("Ingesting Mailpit email %s from %s with subject: %s\n", msg.ID, sender, detail.Subject)
		_, err = ProcessIncomingEmail(db, sender, creds.Email, detail.Subject, body, creds.UserID)
		if err != nil {
			log.Printf("Error processing email %s: %v\n", msg.ID, err)
		}

		db.Create(&models.ProcessedEmail{ID: msg.ID, ProcessedAt: time.Now()})
	}
}

func parsePart(part *multipart.Part, body *string) {
	mediaType, params, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil {
		return
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(part, params["boundary"])
		for {
			subPart, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return
			}
			parsePart(subPart, body)
			subPart.Close()
		}
	} else if mediaType == "text/plain" || mediaType == "text/html" {
		bodyBytes, err := io.ReadAll(part)
		if err == nil && *body == "" {
			*body = string(bodyBytes)
		}
	}
}

func pollRealIMAP(db *gorm.DB, creds *models.EmailCredential) {
	imapHost := strings.TrimSpace(creds.IMAPHost)
	imapPort := creds.IMAPPort
	emailUser := strings.TrimSpace(creds.Email)
	emailPass, err := utils.DecryptPassword(creds.EncryptedIMAPPassword)
	if err != nil {
		log.Printf("Failed to decrypt IMAP password: %v\n", err)
		return
	}

	if imapHost == "" || emailUser == "" || emailPass == "" {
		log.Printf("IMAP credentials incomplete for %s. Skipping real IMAP poll.\n", emailUser)
		return
	}

	addr := fmt.Sprintf("%s:%d", imapHost, imapPort)
	var c *client.Client
	if imapPort == 993 {
		c, err = client.DialTLS(addr, nil)
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		log.Printf("Failed to connect to IMAP for %s: %v\n", emailUser, err)
		return
	}
	defer c.Logout()

	if err := c.Login(emailUser, emailPass); err != nil {
		log.Printf("IMAP login failed for %s: %v\n", emailUser, err)
		return
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("IMAP select INBOX failed for %s: %v\n", emailUser, err)
		return
	}

	if mbox.Messages == 0 {
		return
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(criteria)
	if err != nil {
		log.Printf("IMAP search failed: %v\n", err)
		return
	}

	if len(ids) == 0 {
		return
	}

	// Limit to the latest 5 unseen messages
	start := 0
	if len(ids) > 5 {
		start = len(ids) - 5
	}
	ids = ids[start:]

	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)

	// Fetch full message body
	var section imap.BodySectionName
	items := []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}

	messages := make(chan *imap.Message, 5)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	for msg := range messages {
		subject := ""
		if msg.Envelope != nil {
			subject = msg.Envelope.Subject
		}
		sender := ""
		if msg.Envelope != nil && len(msg.Envelope.From) > 0 {
			f := msg.Envelope.From[0]
			sender = fmt.Sprintf("%s <%s@%s>", f.PersonalName, f.MailboxName, f.HostName)
		}

		messageID := ""
		if msg.Envelope != nil {
			messageID = msg.Envelope.MessageId
		}
		if messageID == "" {
			messageID = fmt.Sprintf("imap_%d_%d", msg.SeqNum, time.Now().UnixNano())
		}

		var processed models.ProcessedEmail
		if err := db.Where("id = ?", messageID).First(&processed).Error; err == nil {
			continue // Already processed
		}

		// Read body
		r := msg.GetBody(&section)
		if r == nil {
			continue
		}

		body := ""
		// Parse raw mail message
		parsedMsg, err := mail.ReadMessage(r)
		if err == nil {
			mediaType, params, err := mime.ParseMediaType(parsedMsg.Header.Get("Content-Type"))
			if err == nil && strings.HasPrefix(mediaType, "multipart/") {
				mr := multipart.NewReader(parsedMsg.Body, params["boundary"])
				for {
					part, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						break
					}
					parsePart(part, &body)
					part.Close()
				}
			} else {
				bodyBytes, _ := io.ReadAll(parsedMsg.Body)
				body = string(bodyBytes)
			}
		}

		log.Printf("Ingesting real IMAP email %s from %s with subject: %s\n", messageID, sender, subject)
		_, err = ProcessIncomingEmail(db, sender, creds.Email, subject, body, creds.UserID)
		if err != nil {
			log.Printf("Error processing real IMAP email %s: %v\n", messageID, err)
		}

		db.Create(&models.ProcessedEmail{ID: messageID, ProcessedAt: time.Now()})

		// Mark as Seen on IMAP server
		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.SeenFlag}
		_ = c.Store(seqset, item, flags, nil)
	}

	if err := <-done; err != nil {
		log.Printf("IMAP fetch failed: %v\n", err)
	}
}
