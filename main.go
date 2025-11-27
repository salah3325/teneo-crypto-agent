package main

import (
	"context"
	"encoding/json" // Standard Go Library: Required for JSON
	"fmt"
	"log"
	"net/http" // Standard Go Library: Required for API calls
	"os"
	"strings"
	"time"

	"golang.org/x/text/message"
	
	"github.com/TeneoProtocolAI/teneo-agent-sdk/pkg/agent" // External Dependency 1
	"github.com/joho/godotenv"                             // External Dependency 2
)

// coinIDMap maps common symbols (BTC, SOL) to their CoinGecko IDs (bitcoin, solana)
var coinIDMap = map[string]string{
	"btc": "bitcoin",
	"eth": "ethereum",
	"sol": "solana",
	"ada": "cardano",
	"dot": "polkadot",
	"xrp": "ripple", // Note: The ID for XRP is often just "ripple" or "xrp" depending on the endpoint/version. Using ripple for wider compatibility.
}

// --- CoinGecko API Structures ---

// Simple structure to decode only the price (used by handlePriceCommand)
type PriceResponse map[string]map[string]float64

// Structure for the detailed market data (used by handleMarketCommand)
type MarketDataResponse []struct {
	Name                     string  `json:"name"`
	CurrentPrice             float64 `json:"current_price"`
	MarketCap                float64 `json:"market_cap"`
	PriceChangePercentage24H float64 `json:"price_change_percentage_24h"`
	LastUpdatedTimestamp     int64   `json:"last_updated_timestamp"`
}

// PMOAgent implements the Teneo Agent Handler interface
type PMOAgent struct{}

// ProcessTask handles user commands from the Teneo network
func (a *PMOAgent) ProcessTask(ctx context.Context, task string) (string, error) {
	log.Printf("Processing task: %s", task)

	task = strings.TrimSpace(task)
	task = strings.TrimPrefix(task, "/")
	taskLower := strings.ToLower(task)

	parts := strings.Fields(taskLower)
	if len(parts) == 0 {
		return "No command provided. Available commands: price [symbol], market [symbol]", nil
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "price":
		return handlePriceCommand(args)
	case "market":
		return handleMarketCommand(args)
	default:
		return fmt.Sprintf("Unknown command '%s'. Available commands: price [symbol], market [symbol]", command), nil
	}
}

// handlePriceCommand fetches the current price using a direct HTTP request with API Key.
func handlePriceCommand(args []string) (string, error) {
	if len(args) == 0 {
		return "Please specify a cryptocurrency symbol (e.g., price bitcoin)", nil
	}
	coinID := getCoinID(args[0])
	currency := "usd"

	// API Endpoint for simple price data
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=%s", coinID, currency)

	// 1. Get the API key from environment variables
	apiKey := os.Getenv("COINGECKO_API_KEY")

	// 2. Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return "Error creating HTTP request.", err
	}

	// 3. Set the required header for CoinGecko's API key
	if apiKey != "" {
		req.Header.Set("x-cg-demo-api-key", apiKey)
	}

	// 4. Execute the request using a client
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return "Error contacting CoinGecko API.", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("CoinGecko API returned status: %d for URL: %s", resp.StatusCode, url)
		return fmt.Sprintf("Error: CoinGecko API returned status %d. Rate limit likely exceeded.", resp.StatusCode), nil
	}

	var data PriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "Error processing API response.", err
	}

	priceData, ok := data[coinID]
	if !ok || len(priceData) == 0 {
		return fmt.Sprintf("Could not find price data for symbol: %s. Please check the spelling.", coinID), nil
	}

	price := priceData[currency]

	response := fmt.Sprintf("%s Price: $%.2f", strings.ToTitle(coinID), price)
	return response, nil
}

// handleMarketCommand fetches detailed market data using a direct HTTP request with API Key.
func handleMarketCommand(args []string) (string, error) {
	if len(args) == 0 {
		return "Please specify a cryptocurrency symbol (e.g., market ethereum)", nil
	}
	coinID := getCoinID(args[0])

	// API Endpoint for comprehensive market data
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&ids=%s&price_change_percentage=24h", coinID)

	// 1. Get the API key from environment variables
	apiKey := os.Getenv("COINGECKO_API_KEY")

	// 2. Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return "Error creating HTTP request.", err
	}

	// 3. Set the required header for CoinGecko's API key
	if apiKey != "" {
		req.Header.Set("x-cg-demo-api-key", apiKey)
	}

	// 4. Execute the request using a client
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return "Error contacting CoinGecko API.", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("CoinGecko API returned status: %d for URL: %s", resp.StatusCode, url)
		return fmt.Sprintf("Error: CoinGecko API returned status %d. Rate limit likely exceeded.", resp.StatusCode), nil
	}

	var marketDataList MarketDataResponse
	if err := json.NewDecoder(resp.Body).Decode(&marketDataList); err != nil {
		return "Error processing API response.", err
	}

	if len(marketDataList) == 0 {
		return fmt.Sprintf("Could not find market data for symbol: %s. Please check the spelling.", coinID), nil
	}

	data := marketDataList[0]

	response := fmt.Sprintf(`
%s Market Overview:
----------------------
Current Price: $%.2f
Market Cap: $%.2f
24h Change: %.2f%%
Last Updated: %s
`,
		data.Name,
		data.CurrentPrice,
		data.MarketCap,
		data.PriceChangePercentage24H,
		time.Unix(data.LastUpdatedTimestamp, 0).Format("2006-01-02 15:04:05 MST"),
	)

	return response, nil
}

// getCoinID converts user input (symbol or full name) to the CoinGecko ID.
func getCoinID(input string) string {
	// 1. Convert the input to lowercase for consistency
	lowerInput := strings.ToLower(input)

	// 2. Check the map for a common symbol conversion (e.g., "btc" -> "bitcoin")
	if id, ok := coinIDMap[lowerInput]; ok {
		return id
	}

	// 3. If not in the map, assume the user provided the full ID (e.g., "bitcoin" -> "bitcoin")
	return lowerInput
}

// Main function (TEST MODE) - This is the entry point that executes the tests
// Main function (Agent Setup) to initialize and run the agent
func main() {
	// 1. Load configuration from .env file
	godotenv.Load()

	// 2. Configure the Teneo Agent
	config := agent.DefaultConfig()
	config.Name = "Price and Market Overview"
	config.Description = "Fetches real-time price, market cap, and 24h change for crypto symbols using CoinGecko (via direct HTTP)."
	config.Capabilities = []string{"fetch real-time cryptocurrency price and market data using the coingecko api"}

	// Environment variables loaded from .env are required here
	config.PrivateKey = os.Getenv("PRIVATE_KEY")
	config.NFTTokenID = os.Getenv("NFT_TOKEN_ID")
	config.OwnerAddress = os.Getenv("OWNER_ADDRESS")

	// 3. Create and run the Enhanced Agent
	enhancedAgent, err := agent.NewEnhancedAgent(&agent.EnhancedAgentConfig{
		Config:       config,
		AgentHandler: &PMOAgent{},
	})

	if err != nil {
		// This error will occur if your .env file keys are blank or invalid
		log.Fatalf("Failed to initialize enhanced agent: %v", err)
	}

	log.Println("Starting Price and Market Overview Agent...")
	enhancedAgent.Run()
}
