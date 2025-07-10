#!/bin/bash

echo "📊 Importing Grafana Dashboard..."

# Wait for Grafana to be ready
until curl -s http://localhost:3000/api/health > /dev/null; do
    echo "Waiting for Grafana..."
    sleep 2
done

# Import the dashboard
curl -X POST \
  http://localhost:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d @grafana-dashboard.json

echo "✅ Dashboard imported successfully!"
echo "🌐 Access at: http://localhost:3000/d/beyla-red-metrics"