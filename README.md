# Distributed Weather Service with Observability

This project demonstrates a distributed system in Go composed of two microservices with OpenTelemetry and Zipkin distributed tracing infrastructure. The system retrieves weather information for a city based on a Brazilian postal code (CEP).

## Architecture

The system consists of:

- **Service A (Input)**: Receives user requests, validates CEP, and forwards to Service B
- **Service B (Orchestration)**: Validates CEP, retrieves city info, fetches temperature, and performs conversions
- **OpenTelemetry + Zipkin**: Distributed tracing infrastructure for request tracking
- **Docker Compose**: Orchestrates all services and infrastructure

## Project Structure

```
go-observability/
├── service-a/                  # Input validation and forwarding service
│   ├── main.go                # API endpoint and request forwarding
│   ├── tracing.go             # OpenTelemetry initialization
│   ├── go.mod                 # Go module definition
│   └── Dockerfile             # Docker build configuration
├── service-b/                  # Weather orchestration service
│   ├── main.go                # CEP lookup and temperature retrieval
│   ├── tracing.go             # OpenTelemetry initialization
│   ├── go.mod                 # Go module definition
│   └── Dockerfile             # Docker build configuration
├── docker-compose.yaml         # Docker Compose configuration
└── README.md                   # This file
```

## Prerequisites

- Docker and Docker Compose
- Go 1.26+ (for local development)
- OpenTelemetry SDKs
- WeatherAPI account (for temperature data)

## Services Overview

### Service A (Input Validation)

**Port**: 8080

**Endpoint**: `POST /weather`

**Request Format**:
```json
{
  "cep": "29902555"
}
```

**Validation Rules**:
- CEP must be a string
- CEP must contain exactly 8 digits

**Response on Success**: Proxies Service B response (200 OK)

**Response on Invalid CEP** (422 Unprocessable Entity):
```json
{
  "message": "invalid zipcode"
}
```

### Service B (Weather Orchestration)

**Port**: 8081

**Endpoint**: `POST /weather`

**Request Format**:
```json
{
  "cep": "29902555"
}
```

**Successful Response** (200 OK):
```json
{
  "city": "São Paulo",
  "temp_C": 28.5,
  "temp_F": 83.3,
  "temp_K": 301.65
}
```

**Error Responses**:

Invalid CEP (422 Unprocessable Entity):
```json
{
  "message": "invalid zipcode"
}
```

CEP Not Found (404 Not Found):
```json
{
  "message": "can not find zipcode"
}
```

## Observability Features

### Distributed Tracing

The system uses OpenTelemetry with Zipkin backend to trace requests across both services:

1. **Automatic HTTP Tracing**: All HTTP requests and responses are automatically traced
2. **Manual Spans**: Custom spans for external API calls:
   - `getCityByCEP`: Measures ViaCEP API latency
   - `getTemperatureByCity`: Measures WeatherAPI latency
3. **Service Context**: Each service identifies itself with resource attributes
4. **Distributed Context**: Trace IDs propagate across service boundaries

### Zipkin Dashboard

Once running, access the Zipkin UI at:
```
http://localhost:9411
```

The dashboard displays:
- Trace timelines
- Service dependencies
- Span details and latencies
- Error tracking

## Setup and Execution

### Prerequisites

1. **Get a WeatherAPI Key**:
   - Visit [https://www.weatherapi.com/](https://www.weatherapi.com/)
   - Sign up (free tier available)
   - Copy your API key

### Running with Docker Compose

1. **Update WeatherAPI Key**:
   
   Edit `docker-compose.yaml` and replace `YOUR_WEATHER_API_KEY_HERE` with your actual API key:
   
   ```yaml
   service-b:
     environment:
       - WEATHER_API_KEY=your_actual_api_key_here
   ```

2. **Start Services**:
   
   ```bash
   docker-compose up --build
   ```

   This command:
   - Builds Docker images for both services
   - Starts all containers (services, Zipkin)
   - Creates a shared network for inter-service communication

3. **Verify Services are Running**:
   
   ```bash
   docker-compose ps
   ```

4. **Stop Services**:
   
   ```bash
   docker-compose down
   ```

### Local Development

To build and run locally without Docker:

```bash
# Build Service A
cd service-a
go mod download
go build -o service-a
./service-a

# In another terminal, build Service B
cd service-b
go mod download
go build -o service-b
./service-b

# Zipkin still needs Docker or local installation
docker run -d -p 9411:9411 openzipkin/zipkin
```

## Usage Examples

### Test with cURL

```bash
# Valid CEP request
curl -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"01310100"}'

# Invalid CEP (wrong format)
curl -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"123"}'

# Invalid CEP (non-existent)
curl -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"99999999"}'
```

### Monitor Traces

1. Open Zipkin UI: http://localhost:9411
2. Click "Find Traces"
3. Select "service-a" from the dropdown
4. Click "Run Query"
5. Click on a trace to see detailed span information

## Temperature Conversion

The system automatically converts temperature to three scales:

- **Celsius** (°C): Original value from WeatherAPI
- **Fahrenheit** (°F): C × 1.8 + 32
- **Kelvin** (K): C + 273

Example:
- If current temp is 25°C
- Then: 25°F = 77°F, 25K = 298.15K

## External APIs Used

### ViaCEP
- **URL**: https://viacep.com.br/
- **Purpose**: Retrieve city name from Brazilian CEP
- **Rate Limit**: No authentication required
- **Response**: JSON with `localidade` field containing city name

### WeatherAPI
- **URL**: https://api.weatherapi.com/
- **Purpose**: Retrieve current temperature and weather data
- **Authentication**: Requires API key
- **Response**: JSON with current temperature in Celsius

## Configuration

### Environment Variables

**Service A**:
- `SERVICE_B_URL`: URL of Service B (default: `http://service-b:8081`)

**Service B**:
- `WEATHER_API_KEY`: WeatherAPI authentication key (required)
- `OTEL_EXPORTER_ZIPKIN_ENDPOINT`: Zipkin endpoint (default: `http://zipkin:9411/api/v2/spans`)

### OpenTelemetry Configuration

Both services initialize OpenTelemetry with:
- **Exporter**: Zipkin (batched span processor)
- **Resource**: Service name and version attributes
- **Sampler**: Default sampler (all spans recorded)

## Error Handling

The system implements comprehensive error handling:

1. **CEP Validation**: Validated in both services independently
2. **API Failures**: Graceful error messages if external APIs fail
3. **Invalid Responses**: Detected and reported with appropriate HTTP status codes
4. **Network Issues**: Timeouts and connection errors handled with context-aware messages

## Logs

Both services output:
- Startup confirmation with listening port
- Tracing initialization status
- Graceful shutdown notifications

Example Service A startup:
```
2024-04-13T10:00:00Z Tracing initialized for service: service-a
Service A listening on :8080
```

## Testing Scenarios

### Scenario 1: Valid CEP with Available Data
```bash
curl -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"01310100"}'
```
Expected: 200 OK with temperature data

### Scenario 2: Invalid CEP Format
```bash
curl -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"123"}'
```
Expected: 422 Unprocessable Entity with "invalid zipcode"

### Scenario 3: Valid Format but Non-existent CEP
```bash
curl -X POST http://localhost:8080/weather \
  -H "Content-Type: application/json" \
  -d '{"cep":"99999999"}'
```
Expected: 404 Not Found with "can not find zipcode"

### Scenario 4: View Traces in Zipkin
1. Make a request as above
2. Open http://localhost:9411
3. Select "service-a" service
4. Click "Run Query"
5. Click the trace to see:
   - Service A → Service B call
   - External API calls to ViaCEP and WeatherAPI
   - Total latency for each span

## Troubleshooting

### Services Won't Start
- Ensure Docker and Docker Compose are running
- Check port conflicts (8080, 8081, 9411)
- Run `docker-compose logs` to see error messages

### Invalid API Key
- Verify WeatherAPI key in docker-compose.yaml
- Ensure no extra spaces or quotes in the key
- Test key validity at weatherapi.com

### No Traces Appearing in Zipkin
- Verify all services are healthy: `docker-compose ps`
- Check network connectivity: `docker-compose logs`
- Ensure Zipkin endpoint is accessible from services

### CEP Returns "Not Found"
- Verify CEP is valid and exists in Brazil
- Check ViaCEP is accessible (check network)
- Try another known CEP (e.g., "01310100" for São Paulo)