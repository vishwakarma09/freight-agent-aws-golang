package routes

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap/client"
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

func checkSMTP(host string, port int, email, password string) (bool, string) {
	host = cleanStr(host)
	email = cleanStr(email)
	password = cleanStr(password)
	addr := fmt.Sprintf("%s:%d", host, port)

	// Validate ASCII password encoding
	isASCII := true
	for i := 0; i < len(password); i++ {
		if password[i] > 127 {
			isASCII = false
			break
		}
	}
	if !isASCII {
		return false, "Password or email contains invalid non-ASCII characters. Please re-type it manually."
	}

	var err error
	if port == 465 {
		tlsConfig := &tls.Config{InsecureSkipVerify: true, ServerName: host}
		var conn *tls.Conn
		conn, err = tls.Dial("tcp", addr, tlsConfig)
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
		c, err := smtp.Dial(addr)
		if err != nil {
			return false, err.Error()
		}
		defer c.Close()

		if port == 587 {
			tlsConfig := &tls.Config{InsecureSkipVerify: true, ServerName: host}
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

func checkIMAP(host string, port int, email, password string) (bool, string) {
	host = cleanStr(host)
	email = cleanStr(email)
	password = cleanStr(password)
	addr := fmt.Sprintf("%s:%d", host, port)

	var c *client.Client
	var err error
	if port == 993 {
		c, err = client.DialTLS(addr, nil)
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		return false, err.Error()
	}
	defer c.Logout()

	if err := c.Login(email, password); err != nil {
		return false, err.Error()
	}

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
	currentUser, _ := GetCurrentUser(c)
	var creds []models.EmailCredential
	if err := db.DB.Where("user_id = ?", currentUser.ID).Find(&creds).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to query credentials"})
		return
	}
	c.JSON(http.StatusOK, creds)
}

func getEnvMode(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"env": config.Settings.Env})
}

func saveCredentials(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var payload EmailCredentialCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	effectiveUseDevMode := payload.UseDevMode
	if config.Settings.Env == "prod" {
		effectiveUseDevMode = false
	}

	var creds models.EmailCredential
	err := db.DB.Where("user_id = ?", currentUser.ID).First(&creds).Error

	if err == nil {
		// Update
		creds.EmailProvider = payload.EmailProvider
		creds.Email = cleanStr(payload.Email)
		creds.SMTPHost = cleanStr(payload.SMTPHost)
		creds.SMTPPort = payload.SMTPPort
		if payload.SMTPPassword != "••••••••••••" {
			encrypted, _ := utils.EncryptPassword(cleanStr(payload.SMTPPassword))
			creds.EncryptedSMTPPassword = encrypted
		}
		creds.IMAPHost = cleanStr(payload.IMAPHost)
		creds.IMAPPort = payload.IMAPPort
		if payload.IMAPPassword != "••••••••••••" {
			encrypted, _ := utils.EncryptPassword(cleanStr(payload.IMAPPassword))
			creds.EncryptedIMAPPassword = encrypted
		}
		creds.UseDevMode = effectiveUseDevMode
		creds.UserEmail = currentUser.Email
		db.DB.Save(&creds)
	} else {
		// Create new
		smtpPwd := ""
		if payload.SMTPPassword != "••••••••••••" {
			smtpPwd = cleanStr(payload.SMTPPassword)
		}
		imapPwd := ""
		if payload.IMAPPassword != "••••••••••••" {
			imapPwd = cleanStr(payload.IMAPPassword)
		}

		encryptedSMTP, _ := utils.EncryptPassword(smtpPwd)
		encryptedIMAP, _ := utils.EncryptPassword(imapPwd)

		creds = models.EmailCredential{
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

	c.JSON(http.StatusOK, creds)
}

func deleteCredentials(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var creds models.EmailCredential
	if err := db.DB.Where("user_id = ?", currentUser.ID).First(&creds).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Email credentials not found"})
		return
	}

	db.DB.Delete(&creds)
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Email credentials disconnected"})
}

func testCredentials(c *gin.Context) {
	currentUser, _ := GetCurrentUser(c)

	var payload EmailCredentialCreate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	effectiveUseDevMode := payload.UseDevMode
	if config.Settings.Env == "prod" {
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
	db.DB.Where("user_id = ?", currentUser.ID).First(&existing)

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
	currentUser, _ := GetCurrentUser(c)

	var creds models.EmailCredential
	if err := db.DB.Where("user_id = ?", currentUser.ID).First(&creds).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Email credentials not found"})
		return
	}

	if creds.UseDevMode && config.Settings.Env != "prod" {
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

	smtpPassword, _ := utils.DecryptPassword(creds.EncryptedSMTPPassword)
	imapPassword, _ := utils.DecryptPassword(creds.EncryptedIMAPPassword)

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
