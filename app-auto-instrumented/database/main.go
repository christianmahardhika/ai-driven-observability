package main

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
)

// Global incident state
var (
	incidentActive int64  = 0
	incidentType   string = "none"
)

type DatabaseRequest struct {
	UserID    string  `json:"user_id"`
	Amount    float64 `json:"amount"`
	Operation string  `json:"operation"`
}

type DatabaseResponse struct {
	Status    string      `json:"status"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	QueryTime float64     `json:"query_time_ms"`
	Timestamp int64       `json:"timestamp"`
}



func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, using defaults")
	}

	// Start background incident simulator
	go incidentSimulator()

	// Start database service
	startDatabaseService()
}



func incidentSimulator() {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	incidents := []string{"connection_timeout", "high_latency", "connection_refused", "deadlock", "disk_full"}

	for {
		select {
		case <-ticker.C:
			if atomic.LoadInt64(&incidentActive) == 0 {
				// Start incident (25% chance)
				if rand.Float64() < 0.25 {
					incident := incidents[rand.Intn(len(incidents))]
					atomic.StoreInt64(&incidentActive, 1)
					incidentType = incident
					slog.Info("🚨 DATABASE INCIDENT DETECTED", "incident", incident)

					// Incident duration: 15-90 seconds
					duration := time.Duration(15+rand.Intn(75)) * time.Second
					go func() {
						time.Sleep(duration)
						atomic.StoreInt64(&incidentActive, 0)
						incidentType = "none"
						slog.Info("✅ DATABASE INCIDENT RESOLVED", "incident", incident)
					}()
				}
			}
		}
	}
}

func startDatabaseService() {
	mux := http.NewServeMux()

	mux.HandleFunc("/db/query", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Parse request
		var req DatabaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(DatabaseResponse{
				Status: "error",
				Error:  "invalid request body",
			})
			return
		}

		// Simulate different scenarios based on incident type
		isIncident := atomic.LoadInt64(&incidentActive) == 1
		var errorRate float64 = 0.02 // Base 2% error rate
		var baseLatency time.Duration = 50 * time.Millisecond

		if isIncident {
			switch incidentType {
			case "connection_timeout":
				errorRate = 0.85
				baseLatency = 5 * time.Second
				time.Sleep(baseLatency + time.Duration(rand.Intn(3000))*time.Millisecond)
			case "high_latency":
				errorRate = 0.15
				baseLatency = 2 * time.Second
				time.Sleep(baseLatency + time.Duration(rand.Intn(1000))*time.Millisecond)
			case "connection_refused":
				errorRate = 0.95
				baseLatency = 100 * time.Millisecond
			case "deadlock":
				errorRate = 0.40
				baseLatency = 1 * time.Second
				time.Sleep(baseLatency + time.Duration(rand.Intn(2000))*time.Millisecond)
			case "disk_full":
				errorRate = 0.70
				baseLatency = 3 * time.Second
				time.Sleep(baseLatency)
			}
		} else {
			// Normal operation latency
			time.Sleep(baseLatency + time.Duration(rand.Intn(100))*time.Millisecond)
		}

		queryTime := time.Since(start).Seconds() * 1000 // Convert to milliseconds

		if rand.Float64() < errorRate {
			var errorMsg string
			switch incidentType {
			case "connection_timeout":
				errorMsg = "connection timeout after 30 seconds"
			case "connection_refused":
				errorMsg = "connection refused by database server"
			case "deadlock":
				errorMsg = "deadlock detected in database transaction"
			case "disk_full":
				errorMsg = "insufficient disk space for database operation"
			default:
				errorMsg = "database connection error"
			}

			slog.Error("Database query failed", "operation", req.Operation, "error", errorMsg)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(DatabaseResponse{
				Status:    "error",
				Error:     errorMsg,
				QueryTime: queryTime,
				Timestamp: time.Now().Unix(),
			})
			return
		}

		// Successful response

		var responseData interface{}
		switch req.Operation {
		case "get_balance":
			responseData = map[string]interface{}{
				"user_id":  req.UserID,
				"balance":  rand.Float64() * 10000,
				"currency": "USD",
			}
		case "balance_check":
			responseData = map[string]interface{}{
				"user_id":           req.UserID,
				"balance":           rand.Float64() * 10000,
				"available_balance": rand.Float64() * 8000,
				"currency":          "USD",
			}
		default:
			responseData = map[string]interface{}{
				"user_id":       req.UserID,
				"result":        "success",
				"affected_rows": rand.Intn(5) + 1,
			}
		}

		slog.Info("Database query successful", "operation", req.Operation)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DatabaseResponse{
			Status:    "success",
			Data:      responseData,
			QueryTime: queryTime,
			Timestamp: time.Now().Unix(),
		})
	})

	mux.HandleFunc("/db/health", func(w http.ResponseWriter, r *http.Request) {
		isHealthy := atomic.LoadInt64(&incidentActive) == 0 || rand.Float64() > 0.7

		if isHealthy {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "healthy",
				"connections": rand.Intn(10) + 1,
				"uptime":      time.Now().Unix() - 1000,
			})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":        "unhealthy",
				"incident_type": incidentType,
				"error":         "database service degraded",
			})
		}
	})

	mux.HandleFunc("/db/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incident_active":    atomic.LoadInt64(&incidentActive) == 1,
			"incident_type":      incidentType,
			"active_connections": rand.Intn(20) + 1,
			"timestamp":          time.Now().Unix(),
		})
	})

	slog.Info("🗄️  Database Service running on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		slog.Error("Server failed", "error", err)
	}
}
