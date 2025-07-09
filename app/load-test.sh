#!/bin/bash
echo "🔄 Starting load test..."

# Function to make requests
make_requests() {
    local endpoint=$1
    local count=$2
    local delay=$3
    
    for i in $(seq 1 $count); do
        curl -s -X POST $endpoint \
            -H "Content-Type: application/json" \
            -d '{"user_id":"user_'$((RANDOM % 100))'","amount":'$((RANDOM % 1000))'.50,"operation":"transfer"}' || true
        sleep $delay
    done
}

# Background load generation
make_requests "http://localhost:8080/api/transaction" 50 1 &
make_requests "http://localhost:8080/api/transaction" 30 2 &

# Wait for completion
wait
echo "✅ Load test completed"
