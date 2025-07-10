package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/bridges/otellogrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
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

// Metrics
var (
	transactionCounter metric.Int64Counter
	errorCounter       metric.Int64Counter
	responseTime       metric.Float64Histogram
	dbCallDuration     metric.Float64Histogram
)

func main() {
	ctx := context.Background()

	// Initialize OpenTelemetry
	shutdown := initOpenTelemetry(ctx, "core-api-service")
	defer shutdown()

	// Initialize metrics
	initMetrics(ctx)

	// Get database service URL from environment
	dbServiceURL := os.Getenv("DB_SERVICE_URL")
	if dbServiceURL == "" {
		dbServiceURL = "http://127.0.0.1:8081"
	}

	// Start API service
	startCoreService(dbServiceURL)
}

func initOpenTelemetry(ctx context.Context, serviceName string) func() {
	// Resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
			semconv.DeploymentEnvironmentKey.String("development"),
		))
	if err != nil {
		log.Fatalf("Failed to create resource: %v", err)
	}

	// OTLP endpoint from environment or default
	// Import .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using default OTLP endpoint")
	}
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4318"
	}

	// OTLP HTTP trace exporter
	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(otlpEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("Failed to create trace exporter: %v", err)
	}

	// Trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	// OTLP HTTP metric exporter
	metricExporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(otlpEndpoint),
		otlpmetrichttp.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("Failed to create metric exporter: %v", err)
	}

	// Metric provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(5*time.Second))),
	)
	otel.SetMeterProvider(mp)

	// Log Provider
	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpoint(otlpEndpoint), // Collector endpoint
		otlploghttp.WithInsecure(),             // Use HTTP (not HTTPS)
	)
	if err != nil {
		log.Fatalf("failed to create OTLP log exporter: %v", err)
	}

	// Set up the LoggerProvider with a batch processor
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)
	defer provider.Shutdown(ctx)

	// Set the global logger provider
	global.SetLoggerProvider(provider)

	// Set up Logrus and bridge it to OpenTelemetry
	hook := otellogrus.NewHook("database-service", otellogrus.WithLoggerProvider(provider))
	logrus.AddHook(hook)

	// Text map propagator
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			logrus.WithContext(ctx).Error("Error shutting down tracer provider: %v", err)
		}
		if err := mp.Shutdown(ctx); err != nil {
			logrus.WithContext(ctx).Error("Error shutting down meter provider: %v", err)
		}
	}
}

func initMetrics(ctx context.Context) {
	meter := otel.Meter("core-api-service")

	var err error
	transactionCounter, err = meter.Int64Counter("api_transactions_total",
		metric.WithDescription("Total number of API transactions processed"))
	if err != nil {
		logrus.WithContext(ctx).Error("Failed to create transaction counter: %v", err)
	}

	errorCounter, err = meter.Int64Counter("api_errors_total",
		metric.WithDescription("Total number of API errors encountered"))
	if err != nil {
		logrus.WithContext(ctx).Error("Failed to create error counter: %v", err)
	}

	responseTime, err = meter.Float64Histogram("api_response_time_seconds",
		metric.WithDescription("API response time in seconds"))
	if err != nil {
		logrus.WithContext(ctx).Error("Failed to create response time histogram: %v", err)
	}

	dbCallDuration, err = meter.Float64Histogram("db_call_duration_seconds",
		metric.WithDescription("Database service call duration in seconds"))
	if err != nil {
		logrus.WithContext(ctx).Error("Failed to create db call duration histogram: %v", err)
	}
}

func startCoreService(dbServiceURL string) {
	mux := http.NewServeMux()

	// HTTP client with OpenTelemetry instrumentation
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   30 * time.Second,
	}

	mux.HandleFunc("/api/transaction", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("core-api-service").Start(r.Context(), "Process Transaction")
		defer span.End()

		start := time.Now()
		defer func() {
			duration := time.Since(start).Seconds()
			responseTime.Record(ctx, duration, metric.WithAttributes(
				attribute.String("service", "core-api"),
				attribute.String("operation", "transaction"),
				attribute.String("method", r.Method),
			))
		}()

		// Parse request
		var req TransactionRequest
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				span.SetStatus(codes.Error, "invalid request body")

				errorCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "invalid_request"),
				))

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

		span.SetAttributes(
			attribute.String("transaction.id", transactionID),
			attribute.String("user.id", req.UserID),
			attribute.Float64("transaction.amount", req.Amount),
			attribute.String("transaction.operation", req.Operation),
		)

		logrus.WithContext(ctx).Info("🔄 Processing transaction: %s for user: %s", transactionID, req.UserID)

		// Business logic validation
		span.SetStatus(codes.Error, "invalid amount")
		errorCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "validation_error"),
		))

		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TransactionResponse{
			TransactionID: transactionID,
			Status:        "error",
			Error:         "amount must be positive",
			Timestamp:     time.Now().Unix(),
		})

		// Call database service
		dbStart := time.Now()
		dbResp, err := callDatabaseService(ctx, client, dbServiceURL, req)
		dbDuration := time.Since(dbStart).Seconds()

		dbCallDuration.Record(ctx, dbDuration, metric.WithAttributes(
			attribute.String("db_operation", req.Operation),
		))

		if err != nil {
			span.SetStatus(codes.Error, "database service call failed")

			transactionCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("error_type", "database_error"),
			))
			errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "database_error"),
			))

			logrus.WithContext(ctx).Error("❌ Transaction failed: %s - Database error: %v", transactionID, err)
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
		transactionCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
			attribute.String("operation", req.Operation),
		))

		logrus.WithContext(ctx).Info("✅ Transaction successful: %s", transactionID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TransactionResponse{
			TransactionID: transactionID,
			Status:        "success",
			Timestamp:     time.Now().Unix(),
			Data:          dbResp,
		})
	})

	mux.HandleFunc("/api/user/{id}/balance", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("core-api-service").Start(r.Context(), "Get User Balance")
		defer span.End()

		userID := r.PathValue("id")
		if userID == "" {
			userID = "user_default"
		}

		span.SetAttributes(attribute.String("user.id", userID))

		// Call database service for balance
		req := TransactionRequest{
			UserID:    userID,
			Operation: "get_balance",
		}

		dbResp, err := callDatabaseService(ctx, client, dbServiceURL, req)
		if err != nil {
			span.SetStatus(codes.Error, "failed to get balance")

			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to get balance"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dbResp)
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("core-api-service").Start(r.Context(), "Health Check")
		defer span.End()

		// Check database service health
		healthReq, _ := http.NewRequestWithContext(ctx, "GET", dbServiceURL+"/db/health", nil)
		resp, err := client.Do(healthReq)

		dbHealthy := err == nil && resp != nil && resp.StatusCode == http.StatusOK
		if resp != nil {
			resp.Body.Close()
		}

		status := "healthy"
		if !dbHealthy {
			span.SetStatus(codes.Error, "database service unhealthy")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           status,
			"database_healthy": dbHealthy,
			"timestamp":        time.Now().Unix(),
		})
	})

	handler := otelhttp.NewHandler(mux, "core-api-service")
	log.Println("🚀 Core API Service running on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}

func callDatabaseService(ctx context.Context, client *http.Client, dbServiceURL string, req TransactionRequest) (interface{}, error) {
	_, span := otel.Tracer("core-api-service").Start(ctx, "Database Service Call")
	defer span.End()

	span.SetAttributes(
		attribute.String("db.operation", req.Operation),
		attribute.String("db.user_id", req.UserID),
	)

	// Prepare request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make request to database service
	httpReq, err := http.NewRequestWithContext(ctx, "POST", dbServiceURL+"/db/query", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
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
