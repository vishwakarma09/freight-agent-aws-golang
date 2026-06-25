package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type ChatResponseChoice struct {
	Message ChatMessage `json:"message"`
}

type ChatResponse struct {
	Choices []ChatResponseChoice `json:"choices"`
}

type ExtractedCustomerInquiry struct {
	CustomerNameGuess string   `json:"customer_name_guess"`
	Origin            string   `json:"origin"`
	Destination       string   `json:"destination"`
	WeightLbs         float64  `json:"weight_lbs"`
	Dimensions        string   `json:"dimensions"`
	FreightClass      string   `json:"freight_class"`
	Hazmat            bool     `json:"hazmat"`
	Accessorials      []string `json:"accessorials"`
	PickupDate        string   `json:"pickup_date"`
}

type ExtractedCarrierBid struct {
	BidAmount        float64 `json:"bid_amount"`
	TransitTimeDays  int     `json:"transit_time_days"`
	PickupWindow     string  `json:"pickup_window"`
	AccessorialsText string  `json:"accessorials_text"`
	ServiceLevel     string  `json:"service_level"`
	Notes            string  `json:"notes"`
}

func CallCerebrasChat(messages []ChatMessage, temperature float64) (string, error) {
	apiKey := config.Settings.CerebrasAPIKey
	if apiKey == "" || apiKey == "YOUR_CEREBRAS_API_KEY" || apiKey == "test" {
		return "", errors.New("cerebras API key is not configured")
	}

	url := "https://api.cerebras.ai/v1/chat/completions"
	reqBody := ChatRequest{
		Model:       "llama3.1-70b",
		Messages:    messages,
		Temperature: temperature,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("cerebras API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}

	if len(chatResp.Choices) == 0 {
		return "", errors.New("empty choices from Cerebras API")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func cleanJSON(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		parts := strings.Split(content, "```")
		if len(parts) > 1 {
			content = parts[1]
			if strings.HasPrefix(content, "json") {
				content = content[4:]
			}
		}
	}
	return strings.Trim(content, "` \n")
}

func ParseCustomerEmail(subject string, body string, senderEmail string) ExtractedCustomerInquiry {
	combinedText := fmt.Sprintf("Subject: %s\nSender: %s\nBody:\n%s", subject, senderEmail, body)

	prompt := fmt.Sprintf(
		"You are an expert freight logistics assistant. Your job is to extract structured shipment parameters "+
			"from the following customer email. Identify the origin city/state, destination city/state, weight in lbs, "+
			"dimensions, freight class, hazmat flag, requested accessorials (like liftgate, residential delivery, inside pickup, etc.), "+
			"and the estimated/required pickup date.\n\n"+
			"Email Content:\n%s\n\n"+
			"Return the result ONLY as a raw JSON object with the following schema, containing no extra text or markdown formatting:\n"+
			"{\n"+
			"  \"customer_name_guess\": \"string or null\",\n"+
			"  \"origin\": \"City, State (e.g. Los Angeles, CA)\",\n"+
			"  \"destination\": \"City, State (e.g. Chicago, IL)\",\n"+
			"  \"weight_lbs\": float_number,\n"+
			"  \"dimensions\": \"string (e.g. 48x48x50) or null\",\n"+
			"  \"freight_class\": \"string (e.g. 70) or null\",\n"+
			"  \"hazmat\": boolean_flag,\n"+
			"  \"accessorials\": [\"list\", \"of\", \"strings\"],\n"+
			"  \"pickup_date\": \"YYYY-MM-DD (estimate based on text, or null)\"\n"+
			"}",
		combinedText,
	)

	messages := []ChatMessage{
		{Role: "system", Content: "You are a precise data extraction bot. Output only raw JSON."},
		{Role: "user", Content: prompt},
	}

	content, err := CallCerebrasChat(messages, 0.1)
	if err == nil {
		content = cleanJSON(content)
		var result ExtractedCustomerInquiry
		if err := json.Unmarshal([]byte(content), &result); err == nil {
			return result
		}
	}

	log.Printf("Cerebras customer parsing failed or bypassed: %v. Running regex-based fallback...\n", err)
	return RunCustomerRegexFallback(subject, body)
}

func ParseCarrierBidEmail(body string) ExtractedCarrierBid {
	prompt := fmt.Sprintf(
		"You are a logistics coordinator. Extract the carrier bid details from the following email reply. "+
			"Find the total bid amount (cost), estimated transit time in days, pickup window/schedule details, "+
			"service level (e.g. Standard LTL, Guaranteed, Expedited), and any accessorials included or extra notes.\n\n"+
			"Email Reply:\n%s\n\n"+
			"Return the result ONLY as a raw JSON object with the following schema, containing no extra text or markdown formatting:\n"+
			"{\n"+
			"  \"bid_amount\": float_number,\n"+
			"  \"transit_time_days\": integer_number_or_null,\n"+
			"  \"pickup_window\": \"string or null\",\n"+
			"  \"accessorials_text\": \"string or null\",\n"+
			"  \"service_level\": \"string or null (e.g. Standard LTL, Guaranteed)\",\n"+
			"  \"notes\": \"string or null\"\n"+
			"}",
		body,
	)

	messages := []ChatMessage{
		{Role: "system", Content: "You are a precise data extraction bot. Output only raw JSON."},
		{Role: "user", Content: prompt},
	}

	content, err := CallCerebrasChat(messages, 0.1)
	if err == nil {
		content = cleanJSON(content)
		var result ExtractedCarrierBid
		if err := json.Unmarshal([]byte(content), &result); err == nil {
			return result
		}
	}

	log.Printf("Cerebras bid parsing failed or bypassed: %v. Running regex-based fallback...\n", err)
	return RunCarrierRegexFallback(body)
}

func RunCustomerRegexFallback(subject string, body string) ExtractedCustomerInquiry {
	text := strings.ToLower(subject + " " + body)

	origin := "Los Angeles, CA"
	destination := "Chicago, IL"

	// Try from...to... pattern first
	fromToRe := regexp.MustCompile(`from\s+([a-zA-Z\s,]+?)\s+to\s+([a-zA-Z\s,]+?)(?:\.|\n|class|weight|\d|$)`)
	fromToMatch := fromToRe.FindStringSubmatch(text)
	if len(fromToMatch) == 3 {
		origin = strings.Title(strings.TrimSpace(fromToMatch[1]))
		destination = strings.Title(strings.TrimSpace(fromToMatch[2]))
	} else {
		// Origin extraction
		originRe := regexp.MustCompile(`(?:origin|from):\s*([a-zA-Z\s,]+(?:\b[a-zA-Z]{2}\b)?)`)
		originMatch := originRe.FindStringSubmatch(text)
		if len(originMatch) > 1 {
			origin = strings.Title(strings.TrimSpace(originMatch[1]))
		} else {
			for _, city := range []string{"los angeles", "new york", "chicago", "dallas", "miami", "atlanta", "seattle"} {
				if strings.Contains(text, city) {
					origin = strings.Title(city) + ", USA"
					break
				}
			}
		}

		// Destination extraction
		destRe := regexp.MustCompile(`(?:destination|to):\s*([a-zA-Z\s,]+(?:\b[a-zA-Z]{2}\b)?)`)
		destMatch := destRe.FindStringSubmatch(text)
		if len(destMatch) > 1 {
			destination = strings.Title(strings.TrimSpace(destMatch[1]))
		} else {
			for _, city := range []string{"chicago", "dallas", "new york", "houston", "phoenix", "philadelphia", "seattle"} {
				if strings.Contains(text, city) && !strings.Contains(strings.ToLower(origin), city) {
					destination = strings.Title(city) + ", USA"
					break
				}
			}
		}
	}

	// Weight extraction
	weight := 1500.0
	weightRe := regexp.MustCompile(`(\d{2,6})\s*(?:lbs|lb|weight|pounds|kg)`)
	weightMatch := weightRe.FindStringSubmatch(text)
	if len(weightMatch) > 1 {
		if w, err := strconv.ParseFloat(weightMatch[1], 64); err == nil {
			weight = w
		}
	}

	// Freight class
	freightClass := "70"
	classRe := regexp.MustCompile(`(?:class|freight class)\s*#?\s*(\d{2,3})`)
	classMatch := classRe.FindStringSubmatch(text)
	if len(classMatch) > 1 {
		freightClass = classMatch[1]
	}

	// Hazmat
	hazmat := strings.Contains(text, "hazmat") || strings.Contains(text, "hazardous") || strings.Contains(text, "class 9")

	// Accessorials
	var accessorials []string
	for _, acc := range []string{"liftgate", "residential", "inside delivery", "inside pickup", "appointment"} {
		if strings.Contains(text, acc) {
			accessorials = append(accessorials, strings.Title(acc))
		}
	}

	// Pickup Date
	pickupDate := ""
	// Look for dates like 2026-06-25 or 06/25/2026
	dateRe := regexp.MustCompile(`(\d{4}[-/]\d{2}[-/]\d{2})`)
	dateMatch := dateRe.FindStringSubmatch(text)
	if len(dateMatch) > 1 {
		pickupDate = dateMatch[1]
	} else {
		dateReAlt := regexp.MustCompile(`(\d{2}[-/]\d{2}[-/]\d{4})`)
		dateMatchAlt := dateReAlt.FindStringSubmatch(text)
		if len(dateMatchAlt) > 1 {
			parts := regexp.MustCompile(`[-/]`).Split(dateMatchAlt[1], -1)
			if len(parts) == 3 {
				pickupDate = fmt.Sprintf("%s-%s-%s", parts[2], parts[0], parts[1])
			}
		}
	}

	if pickupDate == "" {
		pickupDate = time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	}

	return ExtractedCustomerInquiry{
		CustomerNameGuess: "Dispatch Customer",
		Origin:            origin,
		Destination:       destination,
		WeightLbs:         weight,
		Dimensions:        "48x48x48",
		FreightClass:      freightClass,
		Hazmat:            hazmat,
		Accessorials:      accessorials,
		PickupDate:        pickupDate,
	}
}

func RunCarrierRegexFallback(body string) ExtractedCarrierBid {
	text := strings.ToLower(body)

	// Bid amount extraction
	bidAmount := 1000.0
	moneyRe := regexp.MustCompile(`\$\s*(\d{1,4}(?:,\d{3})*(?:\.\d{2})?)`)
	moneyMatches := moneyRe.FindStringSubmatch(text)
	if len(moneyMatches) > 1 {
		cleanMoney := strings.ReplaceAll(moneyMatches[1], ",", "")
		if val, err := strconv.ParseFloat(cleanMoney, 64); err == nil {
			bidAmount = val
		}
	} else {
		numRe := regexp.MustCompile(`(?:quote|price|rate|cost|for|is)\s*:?\s*(\d{3,4})`)
		numMatches := numRe.FindStringSubmatch(text)
		if len(numMatches) > 1 {
			if val, err := strconv.ParseFloat(numMatches[1], 64); err == nil {
				bidAmount = val
			}
		}
	}

	// Transit time
	transitTimeDays := 3
	transitRe := regexp.MustCompile(`(\d+)\s*(?:day|days|transit|business days)`)
	transitMatches := transitRe.FindStringSubmatch(text)
	if len(transitMatches) > 1 {
		if val, err := strconv.Atoi(transitMatches[1]); err == nil {
			transitTimeDays = val
		}
	}

	accessorialsText := "Standard accessorials"
	if strings.Contains(text, "liftgate") {
		accessorialsText = "Liftgate included"
	}

	serviceLevel := "Standard LTL"
	if strings.Contains(text, "expedited") || strings.Contains(text, "guaranteed") {
		serviceLevel = "Guaranteed LTL"
	}

	return ExtractedCarrierBid{
		BidAmount:        bidAmount,
		TransitTimeDays:  transitTimeDays,
		PickupWindow:     "09:00 AM - 05:00 PM",
		AccessorialsText: accessorialsText,
		ServiceLevel:     serviceLevel,
		Notes:            "Extracted via regex fallback.",
	}
}
