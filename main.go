package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http" // Needed for CMC URL encoding
	"os"
	"strconv" // Needed for Dexscreener price parsing
	"strings"

	"github.com/TeneoProtocolAI/teneo-agent-sdk/pkg/agent"
	"github.com/joho/godotenv"
	"golang.org/x/text/message"
)

// Agent Handler Struct
type PMOAgent struct{}

// --- CoinGecko Maps (Needed for CG Symbol resolution) ---
// This map helps convert simple symbols to CoinGecko's full ID string
var coinIDMap = map[string]string{
	"btc":   "bitcoin",
	"eth":   "ethereum",
	"sol":   "solana",
	"ada":   "cardano",
	"doge":  "dogecoin",
	"shib":  "shiba-inu",
	"pepe":  "pepe",
	"avax":  "avalanche",
	"link":  "chainlink",
	"uni":   "uniswap",
	"matic": "polygon",
	"ltc":   "litecoin",
	// Keep this list short, as we rely on CMC first
}

// --- CoinGecko Structs (For CG Failover) ---
type CoinGeckoResponse struct {
	ID         string `json:"id"`
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	MarketData struct {
		CurrentPrice             map[string]float64 `json:"current_price"`
		PriceChangePercentage24h float64            `json:"price_change_percentage_24h"`
		MarketCap                map[string]float64 `json:"market_cap"`
		CirculatingSupply        float64            `json:"circulating_supply"`
		TotalSupply              float64            `json:"total_supply"`
	} `json:"market_data"`
}

// --- CoinMarketCap (CMC) Structs (Primary CEX Lookup) ---
type CMCResponse struct {
	Status struct {
		ErrorCode    int    `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"status"`
	Data map[string]CMCData `json:"data"`
}

type CMCData struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	Symbol            string  `json:"symbol"`
	CirculatingSupply float64 `json:"circulating_supply"`
	TotalSupply       float64 `json:"total_supply"`
	Quote             struct {
		USD struct {
			Price            float64 `json:"price"`
			Volume24h        float64 `json:"volume_24h"`
			MarketCap        float64 `json:"market_cap"`
			PercentChange24h float64 `json:"percent_change_24h"`
		} `json:"USD"`
	} `json:"quote"`
}

// --- Dexscreener Structs (DEX Lookup) ---
type DexscreenerResponse struct {
	Pairs []DexPair `json:"pairs"`
}

type DexPair struct {
	ChainID     string  `json:"chainId"`
	PairAddress string  `json:"pairAddress"`
	BaseToken   Token   `json:"baseToken"`
	QuoteToken  Token   `json:"quoteToken"`
	PriceUsd    string  `json:"priceUsd"`
	Volume      Volume  `json:"volume"`
	FDV         float64 `json:"fdv"`
}

type Token struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
}

type Volume struct {
	H24 float64 `json:"h24"`
	H6  float64 `json:"h6"`
	H1  float64 `json:"h1"`
	M5  float64 `json:"m5"`
}

// --- Helper Functions ---

func getCoinID(input string) string {
	lowerInput := strings.ToLower(input)
	if id, ok := coinIDMap[lowerInput]; ok {
		return id
	}
	return lowerInput
}

func formatCurrency(amount float64) string {
	p := message.NewPrinter(message.MatchLanguage("en"))
	return p.Sprintf("$%.2f", amount)
}

func formatQuantity(quantity float64) string {
	if quantity == 0 {
		return "N/A"
	}
	p := message.NewPrinter(message.MatchLanguage("en"))
	return p.Sprintf("%.0f", quantity)
}

// --- NEW Helper Function ---

// formatOutput transforms the semicolon-separated response string into a readable message.
func formatOutput(rawOutput string) string {
	parts := make(map[string]string)

	// 1. Parse the semicolon string into a map
	pairs := strings.Split(rawOutput, ";")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, ":", 2)
		if len(kv) == 2 {
			parts[kv[0]] = kv[1]
		}
	}

	// 2. Identify the source and get key data points
	source := parts["token_source"]
	price := parts["current_price_usd"]
	change := parts["24h_change"]

	// The CMC response contains the full name, which is ideal
	tokenName := parts["name"]
	if tokenName == "" {
		tokenName = "Token" // Fallback if name is missing
	}

	// 3. Start building the human-readable response
	var responseBuilder strings.Builder

	// Add the source and token name
	responseBuilder.WriteString(fmt.Sprintf("💰 **%s Price & Market Overview**\n", tokenName))

	// Add current price
	responseBuilder.WriteString(fmt.Sprintf("- **Price (USD):** %s\n", price))

	// Add 24-hour change with proper color emoji
	changeFloat, err := strconv.ParseFloat(strings.TrimSuffix(change, "%"), 64)
	if err == nil {
		if changeFloat >= 0 {
			responseBuilder.WriteString(fmt.Sprintf("- **24h Change:** **🟢 +%s**\n", change))
		} else {
			responseBuilder.WriteString(fmt.Sprintf("- **24h Change:** **🔴 %s**\n", change))
		}
	} else if change != "" {
		// Fallback for unparseable change, just print the raw string
		responseBuilder.WriteString(fmt.Sprintf("- **24h Change:** %s\n", change))
	}

	// Add Market Cap (available from CEX APIs)
	if marketCap, ok := parts["market_cap_usd"]; ok && marketCap != "" {
		responseBuilder.WriteString(fmt.Sprintf("- **Market Cap:** %s\n", marketCap))
	}

	// Add Volume (available from Dexscreener)
	if volume, ok := parts["volume_24h"]; ok && volume != "" {
		responseBuilder.WriteString(fmt.Sprintf("- **24h Volume:** %s\n", volume))
	}

	// Add FDV (available from Dexscreener)
	if fdv, ok := parts["fdv"]; ok && fdv != "" {
		responseBuilder.WriteString(fmt.Sprintf("- **Fully Diluted Value (FDV):** %s\n", fdv))
	}

	// Add Circulating Supply
	if supply, ok := parts["circulating_supply"]; ok && supply != "N/A" && supply != "" {
		responseBuilder.WriteString(fmt.Sprintf("- **Circulating Supply:** %s\n", supply))
	}

	// Add Source Footer
	responseBuilder.WriteString(fmt.Sprintf("\n*(Data provided by %s)*", strings.ToUpper(source)))

	return responseBuilder.String()
}

// --- API Logic Functions ---

// 1. CoinGecko API (Failover)
func getCoinGeckoData(coinID string) (string, error) {
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s?localization=false&tickers=false&market_data=true&community_data=false&developer_data=false&sparkline=false", coinID)
	apiKey := os.Getenv("COINGECKO_API_KEY")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating CG request: %v", err)
		return "Error creating HTTP request.", err
	}

	if apiKey != "" {
		req.Header.Set("x-cg-demo-api-key", apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return "Error contacting CoinGecko API.", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("CoinGecko API returned status: %d for ID: %s", resp.StatusCode, coinID)
		// Return a specific failure message that ProcessTask can check
		return fmt.Sprintf("Error: CoinGecko API returned status %d. Could not find data for %s.", resp.StatusCode, coinID), nil
	}

	var cryptoData CoinGeckoResponse
	if err := json.NewDecoder(resp.Body).Decode(&cryptoData); err != nil {
		return "Error processing CG API response.", err
	}

	// Format all data points
	priceUSD := formatCurrency(cryptoData.MarketData.CurrentPrice["usd"])
	priceEUR := formatCurrency(cryptoData.MarketData.CurrentPrice["eur"])
	change24h := fmt.Sprintf("%.2f%%", cryptoData.MarketData.PriceChangePercentage24h)
	marketCap := formatCurrency(cryptoData.MarketData.MarketCap["usd"])
	circulatingSupply := formatQuantity(cryptoData.MarketData.CirculatingSupply)
	totalSupply := formatQuantity(cryptoData.MarketData.TotalSupply)

	// Build the final response string
	responseString := fmt.Sprintf(
		"token_source:coingecko;current_price_usd:%s;current_price_eur:%s;24h_change:%s;market_cap_usd:%s;circulating_supply:%s;total_supply:%s",
		priceUSD,
		priceEUR,
		change24h,
		marketCap,
		circulatingSupply,
		totalSupply,
	)

	return responseString, nil
}

// 2. CoinMarketCap API (Primary CEX Lookup)
func getCMCData(symbol string) (string, error) {
	url := "https://pro-api.coinmarketcap.com/v1/cryptocurrency/quotes/latest"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating CMC request: %v", err)
		return "Error creating HTTP request.", err
	}

	q := req.URL.Query()
	q.Add("symbol", strings.ToUpper(symbol))
	q.Add("convert", "USD")
	req.URL.RawQuery = q.Encode()

	apiKey := os.Getenv("CMC_API_KEY")
	if apiKey == "" {
		return "Error: CMC_API_KEY not found in .env file.", nil
	}
	req.Header.Set("X-CMC_PRO_API_KEY", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return "Error contacting CoinMarketCap API.", err
	}
	defer resp.Body.Close()

	var cryptoData CMCResponse
	if err := json.NewDecoder(resp.Body).Decode(&cryptoData); err != nil {
		return "Error processing CMC API response.", err
	}

	// Check for API errors (e.g., Symbol not found)
	if cryptoData.Status.ErrorCode != 0 {
		log.Printf("CMC API Error: %s for symbol: %s", cryptoData.Status.ErrorMessage, symbol)
		// Return a specific failure message that ProcessTask can check
		return fmt.Sprintf("CMC could not find market data for symbol: %s. Error: %s", symbol, cryptoData.Status.ErrorMessage), nil
	}

	data, ok := cryptoData.Data[strings.ToUpper(symbol)]
	if !ok {
		// Return a specific failure message that ProcessTask can check
		return fmt.Sprintf("CMC could not find market data for symbol: %s. Try another symbol.", symbol), nil
	}

	// Format all data points
	priceUSD := formatCurrency(data.Quote.USD.Price)
	change24h := fmt.Sprintf("%.2f%%", data.Quote.USD.PercentChange24h)
	marketCap := formatCurrency(data.Quote.USD.MarketCap)
	circulatingSupply := formatQuantity(data.CirculatingSupply)
	totalSupply := formatQuantity(data.TotalSupply)

	// Build the final response string
	responseString := fmt.Sprintf(
		"token_source:coinmarketcap;current_price_usd:%s;24h_change:%s;market_cap_usd:%s;circulating_supply:%s;total_supply:%s",
		priceUSD,
		change24h,
		marketCap,
		circulatingSupply,
		totalSupply,
	)

	return responseString, nil
}

// 3. Dexscreener API (DEX Lookup)
func getDexData(tokenAddress string) (string, error) {
	url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", tokenAddress)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating Dexscreener request: %v", err)
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Dexscreener API returned status: %d for address: %s", resp.StatusCode, tokenAddress)
		return fmt.Sprintf("Dexscreener Error: API returned status %d.", resp.StatusCode), nil
	}

	var dexData DexscreenerResponse
	if err := json.NewDecoder(resp.Body).Decode(&dexData); err != nil {
		return "Error processing Dexscreener response.", err
	}

	if len(dexData.Pairs) == 0 {
		return "Dexscreener found no pairs for that token address.", nil
	}

	pair := dexData.Pairs[0]

	price, _ := strconv.ParseFloat(pair.PriceUsd, 64)

	responseString := fmt.Sprintf(
		"token_source:dexscreener;chain_id:%s;current_price_usd:%s;volume_24h:%s;fdv:%s;base_token:%s",
		pair.ChainID,
		formatCurrency(price),
		formatCurrency(pair.Volume.H24),
		formatCurrency(pair.FDV),
		pair.BaseToken.Symbol,
	)

	return responseString, nil
}

// --- Agent Handler (The Core Logic) ---

// ProcessTask uses the correct Teneo SDK signature and orchestrates the API calls.
func (a *PMOAgent) ProcessTask(ctx context.Context, input string) (string, error) {
	log.Printf("Processing task: %s", input)

	// 1. Command and Input Parsing
	parts := strings.Fields(input)
	if len(parts) < 2 {
		return "Please specify a command (/price or /market) and a token symbol or contract address.", nil
	}

	command := strings.ToLower(parts[0])
	if command != "/price" && command != "/market" {
		return fmt.Sprintf("Unknown command: %s. Use /price or /market.", command), nil
	}

	lookupTarget := parts[1]
	cleanInput := strings.ToLower(strings.TrimSpace(lookupTarget))

	// 2. Try DEX (Contract Address Lookup)
	if strings.HasPrefix(cleanInput, "0x") && len(cleanInput) >= 40 {
		log.Printf("Attempting Dexscreener lookup for address: %s", cleanInput)
		dexResponse, err := getDexData(cleanInput)
		if err != nil {
			return "Error fetching DEX data.", err
		}
		// --- FORMATTING CHANGE HERE ---
		return formatOutput(dexResponse), nil
	}

	// 3. Try CEX Primary (CoinMarketCap)
	log.Printf("Attempting CoinMarketCap lookup for symbol: %s", lookupTarget)
	cmcResponse, cmcErr := getCMCData(lookupTarget)

	// Check if CMC succeeded (no fatal error AND found data)
	if cmcErr == nil && !strings.Contains(cmcResponse, "CMC could not find market data") {
		// --- FORMATTING CHANGE HERE ---
		return formatOutput(cmcResponse), nil
	}

	// 4. Try CEX Failover (CoinGecko)
	log.Printf("CMC failed. Falling back to CoinGecko for symbol: %s", lookupTarget)
	coinID := getCoinID(lookupTarget)
	cgResponse, cgErr := getCoinGeckoData(coinID)

	// Check if CoinGecko succeeded (no fatal error AND found data)
	if cgErr == nil && !strings.Contains(cgResponse, "Could not find data for") {
		// --- FORMATTING CHANGE HERE ---
		return formatOutput(cgResponse), nil
	}

	// 5. Final Failure
	return fmt.Sprintf("Could not find market data for %s on CoinMarketCap or CoinGecko. Please ensure the symbol is correct or use a contract address for DEX listings.", lookupTarget), nil
}

// --- Main Function ---

func main() {
	godotenv.Load()

	config := agent.DefaultConfig()
	config.Name = "Price and Market Overview"
	config.Description = "Fetches comprehensive crypto market data from CoinMarketCap (Primary CEX), CoinGecko (CEX Failover), and Dexscreener (DEX)."
	config.Capabilities = []string{"fetch real-time cryptocurrency price and market data using multiple apis"}

	config.PrivateKey = os.Getenv("PRIVATE_KEY")
	config.NFTTokenID = os.Getenv("NFT_TOKEN_ID")
	config.OwnerAddress = os.Getenv("OWNER_ADDRESS")

	enhancedAgent, err := agent.NewEnhancedAgent(&agent.EnhancedAgentConfig{
		Config:       config,
		AgentHandler: &PMOAgent{},
	})

	if err != nil {
		log.Fatalf("Failed to initialize enhanced agent: %v", err)
	}

	log.Println("Starting Price and Market Overview Agent...")
	enhancedAgent.Run()
}
