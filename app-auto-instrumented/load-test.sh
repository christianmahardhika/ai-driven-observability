#!/bin/bash

echo "🚦 Generating HTTP traffic for Beyla observability..."

for i in {1..20}; do
    # Health checks
    curl -s http://localhost:8080/api/health > /dev/null
    curl -s http://localhost:8081/db/health > /dev/null
    
    # GET transactions
    curl -s http://localhost:8080/api/transaction > /dev/null
    
    # POST transactions
    curl -s -X POST http://localhost:8080/api/transaction \
        -H "Content-Type: application/json" \
        -d "{\"user_id\":\"user_$i\",\"amount\":$((RANDOM % 1000 + 1)).50,\"operation\":\"transfer\"}" > /dev/null
    
    # Balance queries
    curl -s http://localhost:8080/api/user/$i/balance > /dev/null
    
    echo "Request batch $i/20 sent"
    sleep 1
done

echo "✅ Traffic generation complete. Check Grafana at http://localhost:3000"