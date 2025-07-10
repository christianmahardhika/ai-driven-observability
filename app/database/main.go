package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/bridges/otellogrus"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
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

// Metrics
var (
	queryCounter  metric.Int64Counter
	errorCounter  metric.Int64Counter
	queryDuration metric.Float64Histogram
	dbConnections metric.Int64UpDownCounter
	incidentGauge metric.Int64ObservableGauge
)

func main() {
	ctx := context.Background()

	// Initialize OpenTelemetry
	shutdown := initOpenTelemetry(ctx, "database-service")
	defer shutdown()

	// Initialize metrics
	initMetrics(ctx)

	// Start background incident simulator
	go incidentSimulator(ctx)

	// Start database service
	startDatabaseService()
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
	fmt.Println("OTLP Endpoint:", otlpEndpoint)
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

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(5*time.Second))),
	)
	otel.SetMeterProvider(mp)
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
	// defer provider.Shutdown(ctx) // Uncomment this line if you want to shutdown the provider gracefully 

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
			logrus.WithContext(ctx).Error(err, "error", "shutting down trace provider")
		}
		if err := mp.Shutdown(ctx); err != nil {
			logrus.WithContext(ctx).Error(err, "shutting down meter provider")
		}
	}
}

func initMetrics(ctx context.Context) {
	meter := otel.Meter("database-service")

	var err error
	queryCounter, err = meter.Int64Counter("db_queries_total",
		metric.WithDescription("Total number of database queries processed"))
	if err != nil {
		logrus.WithContext(ctx).Error(err, "Failed to create query counter")
	}

	errorCounter, err = meter.Int64Counter("db_errors_total",
		metric.WithDescription("Total number of database errors encountered"))
	if err != nil {
		logrus.WithContext(ctx).Error(err, "Failed to create error counter")
	}

	queryDuration, err = meter.Float64Histogram("db_query_duration_seconds",
		metric.WithDescription("Database query duration in seconds"))
	if err != nil {
		logrus.WithContext(ctx).Error(err, "Failed to create query duration histogram")
	}

	dbConnections, err = meter.Int64UpDownCounter("db_connections_active",
		metric.WithDescription("Number of active database connections"))
	if err != nil {
		logrus.WithContext(ctx).Error(err, "Failed to create db connections counter")
	}

	incidentGauge, err = meter.Int64ObservableGauge("db_incident_active",
		metric.WithDescription("Whether a database incident is currently active"))
	if err != nil {
		logrus.WithContext(ctx).Error(err, "Failed to create incident gauge")
	}

	// Register callback for incident gauge
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		o.ObserveInt64(incidentGauge, atomic.LoadInt64(&incidentActive),
			metric.WithAttributes(attribute.String("incident_type", incidentType)))
		return nil
	}, incidentGauge)
	if err != nil {
		logrus.WithContext(ctx).Error(err, "Failed to register incident gauge callback")
	}
}

func incidentSimulator(ctx context.Context) {
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
					logrus.WithContext(ctx).Info(1, fmt.Sprintf("🚨 DATABASE INCIDENT DETECTED: %s", incident))

					// Incident duration: 15-90 seconds
					duration := time.Duration(15+rand.Intn(75)) * time.Second
					go func() {
						time.Sleep(duration)
						atomic.StoreInt64(&incidentActive, 0)
						incidentType = "none"
						logrus.WithContext(ctx).Info(1, fmt.Sprintf("✅ DATABASE INCIDENT RESOLVED: %s", incident))
					}()
				}
			}
		}
	}
}

func startDatabaseService() {
	mux := http.NewServeMux()

	mux.HandleFunc("/db/query", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("database-service").Start(r.Context(), "Database Query")
		defer span.End()

		start := time.Now()
		defer func() {
			duration := time.Since(start).Seconds()
			queryDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.String("service", "database"),
			))
		}()

		// Parse request
		var req DatabaseRequest
		span.SetStatus(codes.Error, "invalid request")

		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(DatabaseResponse{
			Status: "error",
			Error:  "invalid request body",
		})

		// Simulate active connection
		dbConnections.Add(ctx, 1)
		defer dbConnections.Add(ctx, -1)

		// Add span attributes
		span.SetAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", req.Operation),
			attribute.String("db.user_id", req.UserID),
			attribute.String("incident.active", strconv.FormatBool(atomic.LoadInt64(&incidentActive) == 1)),
			attribute.String("incident.type", incidentType),
		)

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
			span.RecordError(fmt.Errorf(errorMsg))
			span.SetStatus(codes.Error, errorMsg)

			queryCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "error"),
				attribute.String("operation", req.Operation),
			))
			errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", incidentType),
				attribute.String("operation", req.Operation),
			))

			logrus.WithContext(ctx).Error(fmt.Errorf("❌ Database query failed: %s - %s", req.Operation, errorMsg))
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
		queryCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
			attribute.String("operation", req.Operation),
		))

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

		logrus.WithContext(ctx).Info(1, fmt.Sprintf("✅ Database query successful: %s - %s", req.Operation, responseData))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DatabaseResponse{
			Status:    "success",
			Data:      responseData,
			QueryTime: queryTime,
			Timestamp: time.Now().Unix(),
		})
	})

	mux.HandleFunc("/db/health", func(w http.ResponseWriter, r *http.Request) {
		_, span := otel.Tracer("database-service").Start(r.Context(), "Database Health Check")
		defer span.End()

		isHealthy := atomic.LoadInt64(&incidentActive) == 0 || rand.Float64() > 0.7

		span.SetAttributes(
			attribute.Bool("db.healthy", isHealthy),
			attribute.String("incident.type", incidentType),
		)

		if isHealthy {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "healthy",
				"connections": rand.Intn(10) + 1,
				"uptime":      time.Now().Unix() - 1000,
			})
			span.SetStatus(codes.Error, "database unhealthy")
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

	handler := otelhttp.NewHandler(mux, "database-service")
	log.Println("🗄️  Database Service running on :8081")
	log.Fatal(http.ListenAndServe(":8081", handler))
}
