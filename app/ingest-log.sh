TIME_UNIX_NANO=$(($(date -u +%s) * 1000000000))

# Build the JSON payload
cat <<EOF > payload.json
{
  "resourceLogs": [
    {
      "resource": {
        "attributes": [
          { "key": "service.name", "value": { "stringValue": "manual-otlp-test" } }
        ]
      },
      "scopeLogs": [
        {
          "scope": { "name": "manual-test" },
          "logRecords": [
            {
              "timeUnixNano": "$TIME_UNIX_NANO",
              "severityNumber": 9,
              "severityText": "INFO",
              "body": { "stringValue": "Hello from manual OTLP POST" }
            }
          ]
        }
      ]
    }
  ]
}
EOF

# Send the payload with curl
curl -X POST -H "Content-Type: application/json" -d @payload.json http://100.83.50.92:4318/v1/logs
