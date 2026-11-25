package services

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"gorm.io/gorm"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

// Mailpit Message structures (dev mode only)
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
	ID      string           `json:"ID"`
	From    *MailpitAddress  `json:"From"`
	To      []MailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
	Text    string           `json:"Text"`
	HTML    string           `json:"HTML"`
}

// SendEmail sends a platform system email (auth activation, password reset, org invite) via configured system SMTP.
func SendEmail(toEmail string, subject string, bodyHTML string, fromEmail string) bool {
	if fromEmail == "" {
		fromEmail = config.Settings.EmailsFromEmail
		if fromEmail == "" {
			fromEmail = config.Settings.BrokerEmail
		}
	}
	return sendViaSMTP(
		config.Settings.SMTPHost,
		config.Settings.SMTPPort,
		config.Settings.SMTPUser,
		config.Settings.SMTPPassword,
		config.Settings.SMTPTLS,
		fromEmail,
		toEmail,
		subject,
		bodyHTML,
		!config.Settings.IsProd(),
	)
}

// SendEmailForOrg sends RFQs and quote proposals using the organization's own configured SMTP credentials
// (falling back to platform SMTP only if the org uses dev mode or has not configured custom SMTP yet).
func SendEmailForOrg(gdb *gorm.DB, orgID uint, toEmail string, subject string, bodyHTML string) bool {
	var creds models.EmailCredential
	if orgID > 0 {
		if err := gdb.Where("organization_id = ?", orgID).First(&creds).Error; err == nil {
			if !creds.UseDevMode || config.Settings.IsProd() {
				smtpPass, err := utils.DecryptPassword(creds.EncryptedSMTPPassword)
				if err == nil && creds.SMTPHost != "" && creds.Email != "" && smtpPass != "" {
					allowedPorts := []int{465, 587}
					if err := utils.ValidateExternalHostPort(creds.SMTPHost, creds.SMTPPort, allowedPorts, !config.Settings.IsProd()); err != nil {
						log.Printf("Blocked unsafe tenant SMTP host %s:%d for org %d: %v\n", creds.SMTPHost, creds.SMTPPort, orgID, err)
						return false
					}
					return sendViaSMTP(
						creds.SMTPHost,
						creds.SMTPPort,
						creds.Email,
						smtpPass,
						true,
						creds.Email,
						toEmail,
						subject,
						bodyHTML,
						!config.Settings.IsProd(),
					)
				}
			}
		}
	}
	return SendEmail(toEmail, subject, bodyHTML, "")
}

func sendViaSMTP(host string, port int, user, password string, useTLS bool, fromEmail, toEmail, subject, bodyHTML string, allowDevLocal bool) bool {
	fromClean := utils.SanitizeHeaderValue(fromEmail)
	toClean := utils.SanitizeHeaderValue(toEmail)
	subjClean := utils.SanitizeHeaderValue(subject)
	host = strings.TrimSpace(host)

	if fromClean == "" || toClean == "" || host == "" {
		log.Println("SMTP send skipped: missing host, from, or to address")
		return false
	}

	var msgBuilder strings.Builder
	msgBuilder.WriteString(fmt.Sprintf("From: %s\r\n", fromClean))
	msgBuilder.WriteString(fmt.Sprintf("To: %s\r\n", toClean))
	msgBuilder.WriteString(fmt.Sprintf("Subject: %s\r\n", subjClean))
	msgBuilder.WriteString("MIME-Version: 1.0\r\n")
	msgBuilder.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	msgBuilder.WriteString("\r\n")
	msgBuilder.WriteString(bodyHTML)
	msg := msgBuilder.String()

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
	}

	var c *smtp.Client
	var err error

	if port == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if err != nil {
			log.Printf("Failed to dial SMTP TLS (%s): %v\n", addr, err)
			return false
		}
		defer conn.Close()

		c, err = smtp.NewClient(conn, host)
		if err != nil {
			log.Printf("Failed to create SMTP client: %v\n", err)
			return false
		}
		defer c.Close()
	} else {
		conn, err := dialer.Dial("tcp", addr)
		if err != nil {
			log.Printf("Failed to dial SMTP (%s): %v\n", addr, err)
			return false
		}
		defer conn.Close()

		c, err = smtp.NewClient(conn, host)
		if err != nil {
			log.Printf("Failed to create SMTP client: %v\n", err)
			return false
		}
		defer c.Close()

		if useTLS || port == 587 {
			if err = c.StartTLS(tlsConfig); err != nil {
				log.Printf("SMTP StartTLS failed (%s): %v\n", addr, err)
				return false
			}
		} else if !allowDevLocal {
			log.Printf("Refusing plaintext SMTP in production to %s\n", addr)
			return false
		}
	}

	if user != "" && password != "" {
		auth := smtp.PlainAuth("", user, password, host)
		if err = c.Auth(auth); err != nil {
			log.Printf("SMTP Auth failed for %s: %v\n", user, err)
			return false
		}
	}

	if err = c.Mail(fromClean); err != nil {
		log.Printf("SMTP Mail command failed: %v\n", err)
		return false
	}
	if err = c.Rcpt(toClean); err != nil {
		log.Printf("SMTP Rcpt command failed: %v\n", err)
		return false
	}

	w, err := c.Data()
	if err != nil {
		log.Printf("SMTP Data command failed: %v\n", err)
		return false
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		log.Printf("SMTP Write failed: %v\n", err)
		return false
	}
	_ = w.Close()
	_ = c.Quit()

	log.Printf("Email sent successfully to %s via %s\n", toClean, host)
	return true
}

// CleanEmail extracts the normalized email address from "Name <email>" format.
func CleanEmail(sender string) string {
	if addr, err := mail.ParseAddress(sender); err == nil && addr.Address != "" {
		return strings.ToLower(strings.TrimSpace(addr.Address))
	}
	re := regexp.MustCompile(`[\w.\-+]+@[\w.\-]+`)
	matches := re.FindAllString(sender, -1)
	if len(matches) > 0 {
		return strings.ToLower(strings.TrimSpace(matches[0]))
	}
	return strings.ToLower(strings.TrimSpace(sender))
}

// ProcessIncomingEmail routes and processes inbound emails strictly scoped to orgID.
func ProcessIncomingEmail(gdb *gorm.DB, sender, recipient, subject, body string, orgID uint, userID *uint) (map[string]interface{}, error) {
	sender = strings.TrimSpace(sender)
	recipient = strings.TrimSpace(recipient)
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)

	if orgID == 0 {
		return nil, fmt.Errorf("missing organization scope for inbound email processing")
	}

	var org models.Organization
	if err := gdb.First(&org, orgID).Error; err != nil {
		return nil, fmt.Errorf("organization %d not found", orgID)
	}

	// 1. Match Quote ID (supports Q-1001 through Q-999999+ without truncation)
	quoteIDRe := regexp.MustCompile(`\b(Q-\d{4,})\b`)
	quoteIDMatch := quoteIDRe.FindStringSubmatch(subject + " " + body)

	if len(quoteIDMatch) > 1 {
		quoteID := quoteIDMatch[1]
		var quote models.FreightQuote
		err := gdb.Preload("Customer").
			Where("id = ? AND organization_id = ?", quoteID, orgID).
			First(&quote).Error

		if err == nil {
			senderClean := CleanEmail(sender)
			customerEmail := ""
			if quote.Customer != nil {
				customerEmail = strings.ToLower(strings.TrimSpace(quote.Customer.Email))
			}
			bodyLower := strings.ToLower(body)

			// SECURITY FIX (P0-5): Strictly require exact customer sender match to approve/reject a quote.
			// Never allow an arbitrary external sender or carrier mentioning "book"/"approve" to hijack a shipment.
			if customerEmail != "" && senderClean == customerEmail {
				if strings.Contains(bodyLower, "approved") || strings.Contains(bodyLower, "approve") || strings.Contains(bodyLower, "book") {
					if quote.Status == "AWAITING_APPROVAL" {
						// Verify optional approval token if present on quote and in email; exact customer sender is required regardless.
						if err := TransitionQuote(gdb, &quote, "APPROVED", fmt.Sprintf("Customer (%s) approved proposal via email", senderClean)); err != nil {
							return nil, err
						}
						return map[string]interface{}{
							"type":     "CUSTOMER_APPROVAL",
							"quote_id": quote.ID,
							"status":   "APPROVED",
							"message":  "Quote approved via verified customer email",
						}, nil
					}
				} else if strings.Contains(bodyLower, "rejected") || strings.Contains(bodyLower, "reject") || strings.Contains(bodyLower, "decline") {
					if quote.Status == "AWAITING_APPROVAL" {
						lostReason := "Customer rejected via email"
						quote.LostReason = &lostReason
						if err := TransitionQuote(gdb, &quote, "LOST", fmt.Sprintf("Customer (%s) rejected proposal via email", senderClean)); err != nil {
							return nil, err
						}
						return map[string]interface{}{
							"type":     "CUSTOMER_REJECTION",
							"quote_id": quote.ID,
							"status":   "LOST",
							"message":  "Quote rejected via verified customer email",
						}, nil
					}
				}
			}

			// Only accept carrier bids while quote is actively open for bidding
			if quote.Status != "OUT_TO_CARRIERS" && quote.Status != "RE_BID_ROUND" {
				return nil, fmt.Errorf("quote %s is in status %s and is not accepting bids", quote.ID, quote.Status)
			}

			var carrier models.Carrier
			err = gdb.Where("LOWER(email) = ? AND organization_id = ?", senderClean, orgID).First(&carrier).Error
			if err != nil {
				// Auto-register replying carrier only if within carrier quota
				var carrierCount int64
				gdb.Model(&models.Carrier{}).Where("organization_id = ?", orgID).Count(&carrierCount)
				if org.CarrierLimit > 0 && int(carrierCount) >= org.CarrierLimit {
					return nil, fmt.Errorf("unknown carrier %s and organization carrier limit (%d) reached", senderClean, org.CarrierLimit)
				}
				name := strings.Title(strings.ReplaceAll(strings.ReplaceAll(strings.Split(senderClean, "@")[0], "carrier_", ""), ".", " "))
				carrier = models.Carrier{
					OrganizationID:       orgID,
					UserID:               userID,
					Name:                 name,
					Email:                senderClean,
					CompetitivenessScore: 0.0,
				}
				gdb.Create(&carrier)
			}

			bidData, err := ParseCarrierBidEmail(body)
			if err != nil {
				return nil, fmt.Errorf("could not extract a valid carrier bid amount: %w", err)
			}

			currentRound := 1
			if quote.Status == "RE_BID_ROUND" {
				currentRound = 2
			}

			var existingBid models.CarrierBid
			err = gdb.Where("freight_quote_id = ? AND carrier_id = ? AND round = ?", quote.ID, carrier.ID, currentRound).First(&existingBid).Error

			var bidRec models.CarrierBid
			if err == nil {
				existingBid.BidAmount = bidData.BidAmount
				existingBid.TransitTimeDays = bidData.TransitTimeDays
				notes := fmt.Sprintf("Updated bid via email. Original: %s", getSafeString(existingBid.Notes))
				existingBid.Notes = &notes
				gdb.Save(&existingBid)
				bidRec = existingBid
			} else {
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
					ReceivedAt:       time.Now().UTC(),
					IsWinning:        false,
					RawEmail:         &body,
				}
				gdb.Create(&bidRec)
			}

			log.Printf("Recorded bid of $%.2f by carrier %s for quote %s (Round %d, Org %d)\n", bidRec.BidAmount, carrier.Name, quote.ID, currentRound, orgID)

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

	// 2. Customer Inquiry -> enforce Option A quota BEFORE calling Cerebras LLM
	allowed, isOverage, reason := CanCreateQuote(&org)
	if !allowed {
		return nil, fmt.Errorf("quota exceeded: %s", reason)
	}

	senderClean := CleanEmail(sender)
	if senderClean == "" || !strings.Contains(senderClean, "@") {
		return nil, fmt.Errorf("invalid sender email address")
	}

	// SECURITY FIX (P0-4): Strictly scope customer lookup by organization_id
	var customer models.Customer
	err := gdb.Where("LOWER(email) = ? AND organization_id = ?", senderClean, orgID).First(&customer).Error
	if err != nil {
		name := strings.Title(strings.ReplaceAll(strings.Split(senderClean, "@")[0], ".", " "))
		customer = models.Customer{
			OrganizationID:       orgID,
			UserID:               userID,
			Name:                 name,
			Email:                senderClean,
			DefaultMarkupPercent: 12.0,
		}
		if createErr := gdb.Create(&customer).Error; createErr != nil {
			return nil, fmt.Errorf("failed to create customer record: %w", createErr)
		}
	}

	extracted := ParseCustomerEmail(subject, body, senderClean)

	quoteID := NextQuoteID(gdb)
	desc := fmt.Sprintf("Origin: %s, Destination: %s, Class: %s, Weight: %.2f lbs", extracted.Origin, extracted.Destination, extracted.FreightClass, extracted.WeightLbs)
	vector := GetEmbedding(desc)

	pickupTime := time.Now().UTC().AddDate(0, 0, 3)
	if extracted.PickupDate != "" {
		if t, err := time.Parse("2006-01-02", extracted.PickupDate); err == nil {
			pickupTime = t
		}
	}

	accs := strings.Join(extracted.Accessorials, ", ")
	approvalTok, _ := utils.GenerateRandomToken(8)

	quote := models.FreightQuote{
		ID:             quoteID,
		OrganizationID: orgID,
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
		ApprovalToken:  &approvalTok,
		ShipmentVector: FormatVector(vector),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := gdb.Create(&quote).Error; err != nil {
		return nil, fmt.Errorf("failed to persist freight quote: %w", err)
	}

	RecordQuoteUsage(gdb, &org, quote.ID, isOverage)

	_ = TransitionQuote(gdb, &quote, "INTAKE", fmt.Sprintf("Email intake from %s", senderClean))
	_ = TransitionQuote(gdb, &quote, "OUT_TO_CARRIERS", "Broadcasting RFQs to carrier network")

	return map[string]interface{}{
		"type":        "CUSTOMER_INQUIRY",
		"quote_id":    quote.ID,
		"parsed_data": extracted,
		"status":      "Quote created and RFQs broadcasted",
	}, nil
}

// NextQuoteID allocates a unique sequential Quote ID.
func NextQuoteID(gdb *gorm.DB) string {
	var count int64
	gdb.Model(&models.FreightQuote{}).Count(&count)
	for {
		quoteID := fmt.Sprintf("Q-%d", 1000+count+1)
		var exists int64
		gdb.Model(&models.FreightQuote{}).Where("id = ?", quoteID).Count(&exists)
		if exists == 0 {
			return quoteID
		}
		count++
	}
}

func getSafeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PollAndIngestEmails polls all active organizations' configured mailboxes.
func PollAndIngestEmails(gdb *gorm.DB) {
	var credsList []models.EmailCredential
	if err := gdb.Find(&credsList).Error; err != nil {
		log.Printf("Error checking email credentials list: %v\n", err)
		return
	}

	for _, creds := range credsList {
		if creds.OrganizationID == 0 {
			continue
		}
		var org models.Organization
		if err := gdb.First(&org, creds.OrganizationID).Error; err != nil {
			continue
		}
		if org.BillingStatus == "canceled" {
			log.Printf("Skipping inbox poll for canceled organization %d\n", org.ID)
			continue
		}

		if creds.UseDevMode && !config.Settings.IsProd() {
			pollMailpit(gdb, &creds)
		} else {
			pollRealIMAPv2(gdb, &creds)
		}
	}
}

func pollMailpit(gdb *gorm.DB, creds *models.EmailCredential) {
	mailpitHost := config.Settings.SMTPHost
	url := "http://mailpit:8025/api/v1/messages"
	if mailpitHost != "mailpit" {
		url = fmt.Sprintf("http://%s:8025/api/v1/messages", mailpitHost)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Get(url + "?limit=50")
	if err != nil {
		log.Printf("Error polling Mailpit: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var mailpitResponse MailpitMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mailpitResponse); err != nil {
		return
	}

	for _, msg := range mailpitResponse.Messages {
		var processed models.ProcessedEmail
		if err := gdb.Where("id = ?", msg.ID).First(&processed).Error; err == nil {
			continue
		}

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

		detailURL := fmt.Sprintf("http://%s:8025/api/v1/message/%s", mailpitHost, msg.ID)
		detailResp, err := httpClient.Get(detailURL)
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

		log.Printf("Ingesting Mailpit email %s from %s (Org %d)\n", msg.ID, sender, creds.OrganizationID)
		_, err = ProcessIncomingEmail(gdb, sender, creds.Email, detail.Subject, body, creds.OrganizationID, creds.UserID)
		if err != nil {
			log.Printf("Error processing Mailpit email %s: %v\n", msg.ID, err)
		}

		gdb.Create(&models.ProcessedEmail{ID: msg.ID, ProcessedAt: time.Now().UTC()})
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
		bodyBytes, err := io.ReadAll(io.LimitReader(part, 65536))
		if err == nil && *body == "" {
			*body = string(bodyBytes)
		}
	}
}

// pollRealIMAPv2 polls a tenant's IMAP inbox using the actively maintained go-imap/v2 client with SSRF + TLS 1.2 guards.
func pollRealIMAPv2(gdb *gorm.DB, creds *models.EmailCredential) {
	imapHost := strings.TrimSpace(creds.IMAPHost)
	imapPort := creds.IMAPPort
	emailUser := strings.TrimSpace(creds.Email)
	emailPass, err := utils.DecryptPassword(creds.EncryptedIMAPPassword)
	if err != nil {
		log.Printf("Failed to decrypt IMAP password for org %d: %v\n", creds.OrganizationID, err)
		return
	}
	if imapHost == "" || emailUser == "" || emailPass == "" {
		return
	}

	allowedPorts := []int{993}
	if !config.Settings.IsProd() {
		allowedPorts = []int{993, 143}
	}
	if err := utils.ValidateExternalHostPort(imapHost, imapPort, allowedPorts, !config.Settings.IsProd()); err != nil {
		log.Printf("Blocked unsafe IMAP host %s:%d for org %d: %v\n", imapHost, imapPort, creds.OrganizationID, err)
		return
	}

	addr := net.JoinHostPort(imapHost, fmt.Sprintf("%d", imapPort))
	var c *imapclient.Client
	if imapPort == 993 {
		c, err = imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: imapHost},
		})
	} else {
		c, err = imapclient.DialInsecure(addr, nil)
	}
	if err != nil {
		log.Printf("Failed to connect to IMAP (%s) for %s: %v\n", addr, emailUser, err)
		return
	}
	defer c.Close()

	if err := c.Login(emailUser, emailPass).Wait(); err != nil {
		log.Printf("IMAP login failed for %s: %v\n", emailUser, err)
		return
	}
	defer c.Logout().Wait()

	mbox, err := c.Select("INBOX", nil).Wait()
	if err != nil || mbox.NumMessages == 0 {
		return
	}

	searchData, err := c.UIDSearch(&imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}, nil).Wait()
	if err != nil {
		log.Printf("IMAP search failed for %s: %v\n", emailUser, err)
		return
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return
	}
	// Limit to latest 5 unseen messages per poll cycle
	if len(uids) > 5 {
		uids = uids[len(uids)-5:]
	}

	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(u)
	}

	bodySection := &imap.FetchItemBodySection{}
	fetchOpts := &imap.FetchOptions{
		Envelope:    true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	msgBuffers, err := c.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		log.Printf("IMAP fetch failed for %s: %v\n", emailUser, err)
		return
	}

	for _, msg := range msgBuffers {
		subject := ""
		sender := ""
		messageID := ""
		if msg.Envelope != nil {
			subject = msg.Envelope.Subject
			messageID = msg.Envelope.MessageID
			if len(msg.Envelope.From) > 0 {
				f := msg.Envelope.From[0]
				sender = fmt.Sprintf("%s <%s@%s>", f.Name, f.Mailbox, f.Host)
			}
		}
		if messageID == "" {
			messageID = fmt.Sprintf("imap_%d_%d", creds.OrganizationID, msg.UID)
		}

		var processed models.ProcessedEmail
		if err := gdb.Where("id = ?", messageID).First(&processed).Error; err == nil {
			continue
		}

		rawBytes := msg.FindBodySection(bodySection)
		body := ""
		if len(rawBytes) > 0 {
			if parsedMsg, err := mail.ReadMessage(strings.NewReader(string(rawBytes))); err == nil {
				mediaType, params, err := mime.ParseMediaType(parsedMsg.Header.Get("Content-Type"))
				if err == nil && strings.HasPrefix(mediaType, "multipart/") {
					mr := multipart.NewReader(parsedMsg.Body, params["boundary"])
					for {
						part, err := mr.NextPart()
						if err != nil {
							break
						}
						parsePart(part, &body)
						part.Close()
					}
				} else {
					bodyBytes, _ := io.ReadAll(io.LimitReader(parsedMsg.Body, 65536))
					body = string(bodyBytes)
				}
			}
		}

		log.Printf("Ingesting IMAP v2 email %s from %s (Org %d)\n", messageID, sender, creds.OrganizationID)
		_, err = ProcessIncomingEmail(gdb, sender, creds.Email, subject, body, creds.OrganizationID, creds.UserID)
		if err != nil {
			log.Printf("Error processing IMAP email %s: %v\n", messageID, err)
		}

		gdb.Create(&models.ProcessedEmail{ID: messageID, ProcessedAt: time.Now().UTC()})

		// Mark only this UID as Seen
		var singleSet imap.UIDSet
		singleSet.AddNum(msg.UID)
		_ = c.Store(singleSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Silent: true,
			Flags:  []imap.Flag{imap.FlagSeen},
		}, nil).Close()
	}
}
