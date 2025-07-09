# ai-driven-observability

## Overview

This repository contains a sample application demonstrating AI-driven observability using OpenTelemetry and (OpenAI's GPT). This is a proof a concept to showcase how AI can enhance observability in distributed systems.

## In glance architecture

```mermaid
graph TD
    A[Service 1] -->|HTTP Request| B[Service 2]
    B -->|HTTP Call| C[Service 3]
    C -->|Database Query| D[(Database)]
    A -- Logs --> L1[Log Data 1]
    A -- Metrics --> M1[Metrics Data 1]
    A -- Traces --> T1[Trace Data 1]
    B -- Logs --> L2[Log Data 2]
    B -- Metrics --> M2[Metrics Data 2]
    B -- Traces --> T2[Trace Data 2]
    C -- Logs --> L3[Log Data 3]
    C -- Metrics --> M3[Metrics Data 3]
    C -- Traces --> T3[Trace Data 3]
    L1 --> OC[otel-collector]
    M1 --> OC
    T1 --> OC
    L2 --> OC
    M2 --> OC
    T2 --> OC
    L3 --> OC
    M3 --> OC
    T3 --> OC
    OC --> H[AI Analysis]
    H --> I[AI Insights]
````