# Weather by Postal Code with Observability

Distributed system in Go with two microservices, instrumented with OpenTelemetry and traced in Zipkin. Service A validates a Brazilian postal code (CEP) and forwards it to Service B, which resolves the city via [ViaCEP](https://viacep.com.br/), fetches the temperature from [WeatherAPI](https://www.weatherapi.com/), and returns it in Celsius, Fahrenheit and Kelvin.

## Architecture

```
client → service-a :8080 → service-b :8081 → ViaCEP + WeatherAPI
              ↓                  ↓
         OTLP/gRPC → otel-collector :4317 → zipkin :9411
```

| Component | Port | Role |
|---|---|---|
| `service-a` | 8080 | Input — validates the CEP, forwards to Service B |
| `service-b` | 8081 | Orchestration — city lookup, temperature, conversions |
| `otel-collector` | 4317 | Receives OTLP spans, exports to Zipkin |
| `zipkin` | 9411 | Trace UI |

## Setup

Get a free API key at [weatherapi.com](https://www.weatherapi.com/), then:

```bash
cp .env.sample .env    # set WEATHER_API_KEY
docker compose up -d --build
```

All four containers start together. Verify with `docker compose ps`.

## Making a request to Service A

```bash
# 200 OK
curl -i -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"01310100"}'
```

```json
{
  "city": "São Paulo",
  "temp_C": 28.5,
  "temp_F": 83.3,
  "temp_K": 301.5
}
```

```bash
# 422 — invalid format
curl -i -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" -d '{"cep":"123"}'

# 404 — valid format, nonexistent CEP
curl -i -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" -d '{"cep":"99999999"}'
```

| Status | Condition | Body |
|---|---|---|
| `200` | Success | `{"city":…,"temp_C":…,"temp_F":…,"temp_K":…}` |
| `422` | CEP is not a string of exactly 8 digits | `{"message":"invalid zipcode"}` |
| `404` | Well-formed CEP that does not exist | `{"message":"can not find zipcode"}` |

Service B exposes the same `POST /weather` contract on port 8081 and can be called directly for debugging.

## Viewing traces in Zipkin

Open **http://localhost:9411**, click **Run Query**, and select a trace. Spans are batched, so allow a few seconds after a request.

A single trace spans both services under one trace ID:

```
service-a  POST /weather
  └─ handleWeatherRequest
     └─ forwardToServiceB
        └─ service-b  POST /weather
           └─ handleWeatherRequest
              ├─ getCityByCEP           ← ViaCEP latency
              └─ getTemperatureByCity   ← WeatherAPI latency
```

`getCityByCEP` and `getTemperatureByCity` are manual spans measuring each external API call. Quick check from the shell:

```bash
curl -s http://localhost:9411/api/v2/services | jq .   # ["service-a","service-b"]
```

## Conversions

`F = C × 1.8 + 32` · `K = C + 273`

## Project structure

```
├── service-a/                  # input service (main.go, tracing.go, Dockerfile)
├── service-b/                  # orchestration service (main.go, tracing.go, Dockerfile)
├── otel-collector-config.yaml  # OTLP receiver → Zipkin exporter
├── docker-compose.yml          # service-a, service-b, otel-collector, zipkin
└── Makefile                    # make up / down / logs / test-valid / test-invalid / test-notfound
```

Tracing is configured in each service's `tracing.go`: OTLP/gRPC exporter to `OTEL_EXPORTER_OTLP_ENDPOINT` (`http://otel-collector:4317`), batch span processor, and W3C trace-context propagation so the trace ID crosses the A → B boundary.