package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type TransactionRequest struct {
	UserID    string  `json:"user_id"`
	Amount    float64 `json:"amount"`
	Operation string  `json:"operation"`
}

type TransactionResponse struct {
	TransactionID string      `json:"transaction_id"`
	Status        string      `json:"status"`
	Timestamp     int64       `json:"timestamp"`
	Data          interface{} `json:"data,omitempty"`
	Error         string      `json:"error,omitempty"`
}



func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, using defaults")
	}

	// Get database service URL from environment
	dbServiceURL := os.Getenv("DB_SERVICE_URL")
	if dbServiceURL == "" {
		dbServiceURL = "http://127.0.0.1:8081"
	}

	// Start API service
	startCoreService(dbServiceURL)
}



func startCoreService(dbServiceURL string) {
	mux := http.NewServeMux()

	// HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	mux.HandleFunc("/api/transaction", func(w http.ResponseWriter, r *http.Request) {
		// Parse request
		var req TransactionRequest
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(TransactionResponse{
					Status: "error",
					Error:  "invalid request body",
				})
				return
			}
		} else {
			// Default values for GET requests
			req = TransactionRequest{
				UserID:    fmt.Sprintf("user_%d", rand.Intn(1000)),
				Amount:    rand.Float64() * 1000,
				Operation: "balance_check",
			}
		}

		transactionID := fmt.Sprintf("txn_%d_%d", time.Now().Unix(), rand.Intn(10000))

		slog.Info("Processing transaction", "transaction_id", transactionID, "user_id", req.UserID)

		// Business logic validation
		if req.Amount <= 0 {
			slog.Error("Invalid amount", "transaction_id", transactionID, "amount", req.Amount)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TransactionResponse{
				TransactionID: transactionID,
				Status:        "error",
				Error:         "amount must be positive",
				Timestamp:     time.Now().Unix(),
			})
			return
		}

		// Call database service
		dbResp, err := callDatabaseService(client, dbServiceURL, req)
		if err != nil {
			slog.Error("Database service call failed", "transaction_id", transactionID, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(TransactionResponse{
				TransactionID: transactionID,
				Status:        "failed",
				Error:         fmt.Sprintf("database service error: %v", err),
				Timestamp:     time.Now().Unix(),
			})
			return
		}

		// Success
		slog.Info("Transaction successful", "transaction_id", transactionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TransactionResponse{
			TransactionID: transactionID,
			Status:        "success",
			Timestamp:     time.Now().Unix(),
			Data:          dbResp,
		})
	})

	mux.HandleFunc("/api/user/{id}/balance", func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("id")
		if userID == "" {
			userID = "user_default"
		}

		// Call database service for balance
		req := TransactionRequest{
			UserID:    userID,
			Operation: "get_balance",
		}

		dbResp, err := callDatabaseService(client, dbServiceURL, req)
		if err != nil {
			slog.Error("Failed to get balance", "user_id", userID, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to get balance"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbResp)
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		// Check database service health
		resp, err := client.Get(dbServiceURL + "/db/health")
		dbHealthy := err == nil && resp != nil && resp.StatusCode == http.StatusOK
		if resp != nil {
			resp.Body.Close()
		}

		status := "healthy"
		if !dbHealthy {
			status = "unhealthy"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           status,
			"database_healthy": dbHealthy,
			"timestamp":        time.Now().Unix(),
		})
	})

	slog.Info("🚀 Core API Service running on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("Server failed", "error", err)
	}
}

func callDatabaseService(client *http.Client, dbServiceURL string, req TransactionRequest) (interface{}, error) {
	// Prepare request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make request to database service
	resp, err := client.Post(dbServiceURL+"/db/query", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("database service call failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("database service returned error: %s", string(body))
	}

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}
