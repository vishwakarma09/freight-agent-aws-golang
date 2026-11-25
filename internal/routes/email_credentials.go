package routes

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/gin-gonic/gin"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/db"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/models"
	"github.com/vishwakarma09/freight-agent-aws-golang/internal/utils"
)

type EmailCredentialCreate struct {
	EmailProvider string `json:"email_provider" binding:"required"`
	Email         string `json:"email" binding:"required,email"`
	SMTPHost      string `json:"smtp_host" binding:"required"`
	SMTPPort      int    `json:"smtp_port" binding:"required"`
	SMTPPassword  string `json:"smtp_password" binding:"required"`
	IMAPHost      string `json:"imap_host" binding:"required"`
	IMAPPort      int    `json:"imap_port" binding:"required"`
	IMAPPassword  string `json:"imap_password" binding:"required"`
	UseDevMode    bool   `json:"use_dev_mode"`
}

func cleanStr(val string) string {
	val = strings.ReplaceAll(val, " ", "")
	val = strings.ReplaceAll(val, "\t", "")
	val = strings.ReplaceAll(val, "\n", "")
	val = strings.ReplaceAll(val, "\r", "")
	return val
}

func checkMailpitService() bool {
	if config.Settings.IsProd() {
		return false
	}
	mailpitHost := config.Settings.SMTPHost
	url := "http://mailpit:8025/api/v1/messages"
	if mailpitHost != "mailpit" {
		url = fmt.Sprintf("http://%s:8025/api/v1/messages", mailpitHost)
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// checkSMTP verifies SMTP connectivity with SSRF protection, 8s dial timeout, and strict TLS 1.2 verification.
func checkSMTP(host string, port int, email, password string) (bool, string) {
	host = cleanStr(host)
	email = cleanStr(email)
	password = cleanStr(password)

	allowedPorts := []int{465, 587}
	if !config.Settings.IsProd() {
		allowedPorts = []int{465, 587, 1025}
	}
	if err := utils.ValidateExternalHostPort(host, port, allowedPorts, !config.Settings.IsProd()); err != nil {
		return false, err.Error()
	}

	for i := 0; i < len(password); i++ {
		if password[i] > 127 {
			return false, "Password contains invalid non-ASCII characters. Please re-type it manually."
		}
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}

	if port == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if err != nil {
			return false, err.Error()
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, host)
		if err != nil {
			return false, err.Error()
		}
		defer c.Close()

		auth := smtp.PlainAuth("", email, password, host)
		if err := c.Auth(auth); err != nil {
			return false, err.Error()
		}
	} else {
		conn, err := dialer.Dial("tcp", addr)
		if err != nil {
			return false, err.Error()
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, host)
		if err != nil {
			return false, err.Error()
		}
		defer c.Close()

		if port == 587 {
			if err := c.StartTLS(tlsConfig); err != nil {
				return false, err.Error()
			}
		}

		auth := smtp.PlainAuth("", email, password, host)
		if err := c.Auth(auth); err != nil {
			return false, err.Error()
		}
	}

	return true, "SMTP connection OK"
}

// checkIMAP verifies IMAP connectivity using go-imap/v2 with SSRF protection and strict TLS 1.2 verification.
func checkIMAP(host string, port int, email, password string) (bool, string) {
	host = cleanStr(host)
	email = cleanStr(email)
	password = cleanStr(password)

	allowedPorts := []int{993}
	if !config.Settings.IsProd() {
		allowedPorts = []int{993, 143}
	}
	if err := utils.ValidateExternalHostPort(host, port, allowedPorts, !config.Settings.IsProd()); err != nil {
		return false, err.Error()
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	var c *imapclient.Client
	var err error
	if port == 993 {
		c, err = imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host},
		})
	} else {
		c, err = imapclient.DialInsecure(addr, nil)
	}
	if err != nil {
		return false, err.Error()
	}
	defer c.Close()

	if err := c.Login(email, password).Wait(); err != nil {
		return false, err.Error()
	}
	_ = c.Logout().Wait()

	return true, "IMAP connection OK"
}

func RegisterEmailCredentialRoutes(rg *gin.RouterGroup) {
	creds := rg.Group("/email-credentials", AuthMiddleware())
	{
		creds.GET("", getCredentials)
		creds.GET("/env", getEnvMode)
		creds.POST("", saveCredentials)
		creds.DELETE("", deleteCredentials)
		creds.POST("/test", testCredentials)
		creds.POST("/test-existing", testExistingCredentials)
	}
}

func getCredentials(c *gin.Context) {
	org, _ := GetCurrentOrg(c)
	var creds []models.EmailCredential
	if err := db.DB.Where("organization_id = ?", org.ID).Find(&creds).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to query credentials"})
		return
	}
	for i := range creds {
		creds[i].PopulateMasks()
	}
	c.JSON(http.StatusOK, creds)
}

func getEnvMode(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"env": config.Settings.Env})
}

func saveCredentials(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)
	org, _ := GetCurrentOrg(c)

	var payload EmailCredentialCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	effectiveUseDevMode := payload.UseDevMode
	if config.Settings.IsProd() {
		effectiveUseDevMode = false
	}

	// Validate external hosts before saving to block SSRF in background cron too
	if !effectiveUseDevMode {
		smtpPorts := []int{465, 587}
		imapPorts := []int{993}
		if !config.Settings.IsProd() {
			smtpPorts = append(smtpPorts, 1025)
			imapPorts = append(imapPorts, 143)
		}
		if err := utils.ValidateExternalHostPort(cleanStr(payload.SMTPHost), payload.SMTPPort, smtpPorts, !config.Settings.IsProd()); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid SMTP host/port: " + err.Error()})
			return
		}
		if err := utils.ValidateExternalHostPort(cleanStr(payload.IMAPHost), payload.IMAPPort, imapPorts, !config.Settings.IsProd()); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid IMAP host/port: " + err.Error()})
			return
		}
	}

	var creds models.EmailCredential
	err := db.DB.Where("organization_id = ?", org.ID).First(&creds).Error

	if err == nil {
		creds.EmailProvider = payload.EmailProvider
		creds.Email = cleanStr(payload.Email)
		creds.SMTPHost = cleanStr(payload.SMTPHost)
		creds.SMTPPort = payload.SMTPPort
		if payload.SMTPPassword != "••••••••••••" {
			encrypted, encErr := utils.EncryptPassword(cleanStr(payload.SMTPPassword))
			if encErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to encrypt SMTP password"})
				return
			}
			creds.EncryptedSMTPPassword = encrypted
		}
		creds.IMAPHost = cleanStr(payload.IMAPHost)
		creds.IMAPPort = payload.IMAPPort
		if payload.IMAPPassword != "••••••••••••" {
			encrypted, encErr := utils.EncryptPassword(cleanStr(payload.IMAPPassword))
			if encErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to encrypt IMAP password"})
				return
			}
			creds.EncryptedIMAPPassword = encrypted
		}
		creds.UseDevMode = effectiveUseDevMode
		creds.UserEmail = currentUser.Email
		db.DB.Save(&creds)
	} else {
		// Enforce inbox limit for new credentials
		if org.InboxLimit > 0 {
			var inboxCount int64
			db.DB.Model(&models.EmailCredential{}).Where("organization_id = ?", org.ID).Count(&inboxCount)
			if int(inboxCount) >= org.InboxLimit {
				c.JSON(http.StatusPaymentRequired, gin.H{
					"detail": fmt.Sprintf("Plan inbox limit reached (%d/%d). Upgrade to Growth or Enterprise for additional mailboxes.", inboxCount, org.InboxLimit),
				})
				return
			}
		}

		smtpPwd := ""
		if payload.SMTPPassword != "••••••••••••" {
			smtpPwd = cleanStr(payload.SMTPPassword)
		}
		imapPwd := ""
		if payload.IMAPPassword != "••••••••••••" {
			imapPwd = cleanStr(payload.IMAPPassword)
		}

		encryptedSMTP, encErr := utils.EncryptPassword(smtpPwd)
		if encErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to encrypt SMTP password"})
			return
		}
		encryptedIMAP, encErr := utils.EncryptPassword(imapPwd)
		if encErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to encrypt IMAP password"})
			return
		}

		creds = models.EmailCredential{
			OrganizationID:        org.ID,
			UserID:                &currentUser.ID,
			UserEmail:             currentUser.Email,
			EmailProvider:         payload.EmailProvider,
			Email:                 cleanStr(payload.Email),
			SMTPHost:              cleanStr(payload.SMTPHost),
			SMTPPort:              payload.SMTPPort,
			EncryptedSMTPPassword: encryptedSMTP,
			IMAPHost:              cleanStr(payload.IMAPHost),
			IMAPPort:              payload.IMAPPort,
			EncryptedIMAPPassword: encryptedIMAP,
			UseDevMode:            effectiveUseDevMode,
		}
		db.DB.Create(&creds)
	}

	creds.PopulateMasks()
	c.JSON(http.StatusOK, creds)
}

func deleteCredentials(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var creds models.EmailCredential
	if err := db.DB.Where("organization_id = ?", org.ID).First(&creds).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Email credentials not found"})
		return
	}

	db.DB.Delete(&creds)
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Email credentials disconnected"})
}

func testCredentials(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var payload EmailCredentialCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	effectiveUseDevMode := payload.UseDevMode
	if config.Settings.IsProd() {
		effectiveUseDevMode = false
	}

	if effectiveUseDevMode {
		mailpitOK := checkMailpitService()
		var errMsg *string
		if !mailpitOK {
			msg := "Mailpit service is unreachable inside the network."
			errMsg = &msg
		}
		c.JSON(http.StatusOK, gin.H{
			"smtp_connected": mailpitOK,
			"smtp_error":     errMsg,
			"imap_connected": mailpitOK,
			"imap_error":     errMsg,
			"success":        mailpitOK,
		})
		return
	}

	var existing models.EmailCredential
	db.DB.Where("organization_id = ?", org.ID).First(&existing)

	smtpPassword := payload.SMTPPassword
	if smtpPassword == "••••••••••••" || smtpPassword == "" {
		if existing.ID > 0 && existing.EncryptedSMTPPassword != "" {
			smtpPassword, _ = utils.DecryptPassword(existing.EncryptedSMTPPassword)
		} else {
			smtpPassword = ""
		}
	}

	imapPassword := payload.IMAPPassword
	if imapPassword == "••••••••••••" || imapPassword == "" {
		if existing.ID > 0 && existing.EncryptedIMAPPassword != "" {
			imapPassword, _ = utils.DecryptPassword(existing.EncryptedIMAPPassword)
		} else {
			imapPassword = ""
		}
	}

	smtpOK, smtpErr := checkSMTP(payload.SMTPHost, payload.SMTPPort, payload.Email, smtpPassword)
	imapOK, imapErr := checkIMAP(payload.IMAPHost, payload.IMAPPort, payload.Email, imapPassword)

	var smtpErrPtr, imapErrPtr *string
	if !smtpOK {
		smtpErrPtr = &smtpErr
	}
	if !imapOK {
		imapErrPtr = &imapErr
	}

	c.JSON(http.StatusOK, gin.H{
		"smtp_connected": smtpOK,
		"smtp_error":     smtpErrPtr,
		"imap_connected": imapOK,
		"imap_error":     imapErrPtr,
		"success":        smtpOK && imapOK,
	})
}

func testExistingCredentials(c *gin.Context) {
	org, _ := GetCurrentOrg(c)

	var creds models.EmailCredential
	if err := db.DB.Where("organization_id = ?", org.ID).First(&creds).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Email credentials not found"})
		return
	}

	if creds.UseDevMode && !config.Settings.IsProd() {
		mailpitOK := checkMailpitService()
		var errMsg *string
		if !mailpitOK {
			msg := "Mailpit service is unreachable inside the network."
			errMsg = &msg
		}
		c.JSON(http.StatusOK, gin.H{
			"smtp_connected": mailpitOK,
			"smtp_error":     errMsg,
			"imap_connected": mailpitOK,
			"imap_error":     errMsg,
			"success":        mailpitOK,
		})
		return
	}

	smtpPassword, err1 := utils.DecryptPassword(creds.EncryptedSMTPPassword)
	imapPassword, err2 := utils.DecryptPassword(creds.EncryptedIMAPPassword)
	if err1 != nil || err2 != nil {
		msg := "Stored credentials require re-entry after encryption upgrade"
		c.JSON(http.StatusOK, gin.H{
			"smtp_connected": false,
			"smtp_error":     &msg,
			"imap_connected": false,
			"imap_error":     &msg,
			"success":        false,
		})
		return
	}

	smtpOK, smtpErr := checkSMTP(creds.SMTPHost, creds.SMTPPort, creds.Email, smtpPassword)
	imapOK, imapErr := checkIMAP(creds.IMAPHost, creds.IMAPPort, creds.Email, imapPassword)

	var smtpErrPtr, imapErrPtr *string
	if !smtpOK {
		smtpErrPtr = &smtpErr
	}
	if !imapOK {
		imapErrPtr = &imapErr
	}

	c.JSON(http.StatusOK, gin.H{
		"smtp_connected": smtpOK,
		"smtp_error":     smtpErrPtr,
		"imap_connected": imapOK,
		"imap_error":     imapErrPtr,
		"success":        smtpOK && imapOK,
	})
}
