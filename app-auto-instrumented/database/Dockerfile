# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o database-service main.go

# Get Beyla binary
FROM grafana/beyla:latest AS beyla

# Runtime stage
FROM alpine:latest

# Install ca-certificates
RUN apk --no-cache add ca-certificates

# Copy the built application
COPY --from=builder /app/database-service /usr/local/bin/database-service

# Copy Beyla binary
COPY --from=beyla /beyla /usr/local/bin/beyla

# Copy Beyla configuration
COPY beyla-config.yml /etc/beyla/config.yml

# Copy startup script
COPY start.sh /start.sh
RUN chmod +x /start.sh

# Set environment variables
ENV BEYLA_CONFIG_PATH=/etc/beyla/config.yml
ENV BEYLA_SERVICE_NAME=database-service
ENV BEYLA_SERVICE_VERSION=1.0.0
ENV BEYLA_SERVICE_NAMESPACE=ai-observability
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://alloy:4318

# Expose the application port
EXPOSE 8081

# Start with the startup script
CMD ["/start.sh"]