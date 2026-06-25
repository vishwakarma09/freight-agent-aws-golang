package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
)

type InvoiceResponse struct {
	InvoiceID  string `json:"invoice_id"`
	InvoiceURL string `json:"invoice_url"`
	Status     string `json:"status"`
}

type BOLResponse struct {
	BOLID  string `json:"bol_id"`
	BOLURL string `json:"bol_url"`
	Status string `json:"status"`
}

// GenerateMockInvoice simulates calling PandaDoc API to create a draft invoice for the customer.
func GenerateMockInvoice(quoteID string, customerName string, amount float64) InvoiceResponse {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	invoiceID := fmt.Sprintf("INV-%s", strings.ToUpper(hex.EncodeToString(b)))
	log.Printf("PandaDoc draft invoice created: %s for %s of amount $%.2f\n", invoiceID, customerName, amount)
	return InvoiceResponse{
		InvoiceID:  invoiceID,
		InvoiceURL: fmt.Sprintf("https://pandadoc.mock/invoices/%s", invoiceID),
		Status:     "DRAFT",
	}
}

// GenerateMockBOL simulates receiving a Bill of Lading (BOL) from the winning carrier.
func GenerateMockBOL(quoteID string, carrierName string) BOLResponse {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	bolID := fmt.Sprintf("BOL-%s", strings.ToUpper(hex.EncodeToString(b)))
	log.Printf("BOL received from carrier %s: %s\n", carrierName, bolID)
	return BOLResponse{
		BOLID:  bolID,
		BOLURL: fmt.Sprintf("https://carrier.mock/bol/%s", bolID),
		Status: "ISSUED",
	}
}
