#!/bin/bash

echo "🚀 Starting Auto-Instrumented Services with Grafana Beyla"

# Check if observability stack is running
if ! docker network ls | grep -q "otel_default"; then
    echo "⚠️  Observability stack not found. Starting it first..."
    cd ../infra/otel
    docker-compose up -d
    cd ../app-auto-instrumented
    echo "⏳ Waiting for observability stack to be ready..."
    sleep 10
fi

# Build and start the services
echo "🔨 Building and starting services..."
docker-compose up --build

echo "✅ Services started!"
echo ""
echo "📊 Access points:"
echo "  - Core API: http://localhost:8080"
echo "  - Database API: http://localhost:8081"
echo "  - Grafana: http://localhost:3000"
echo ""
echo "🧪 Test endpoints:"
echo "  curl http://localhost:8080/api/transaction"
echo "  curl http://localhost:8080/api/health"
echo "  curl http://localhost:8081/db/health"