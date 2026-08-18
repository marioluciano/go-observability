package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

type WeatherRequest struct {
	// Raw so that a non-string cep can be rejected as an invalid zipcode
	// rather than as a malformed body. See extractCEP.
	CEP json.RawMessage `json:"cep"`
}

type WeatherResponse struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_C"`
	TempF float64 `json:"temp_F"`
	TempK float64 `json:"temp_K"`
}

type ErrorResponse struct {
	Message string `json:"message"`
}

var ErrZipCodeNotFound = errors.New("zipcode not found")

type ViaCEPResponse struct {
	Localidade string          `json:"localidade"`
	Erro       json.RawMessage `json:"erro"`
}

func (r ViaCEPResponse) NotFound() bool {
	if r.Localidade == "" {
		return true
	}

	raw := strings.Trim(strings.TrimSpace(string(r.Erro)), `"`)

	return raw != "" && raw != "false"
}

type WeatherAPIResponse struct {
	Current struct {
		TempC float64 `json:"temp_c"`
	} `json:"current"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdown, err := initTracing("service-b")
	if err != nil {
		log.Fatalf("Failed to initialize tracing: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/weather", handleWeatherRequest)

	handler := otelhttp.NewHandler(mux, "service-b")
	server := &http.Server{
		Addr:    ":8081",
		Handler: handler,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutdown signal received")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Fatalf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Service B listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func handleWeatherRequest(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("service-b").Start(r.Context(), "handleWeatherRequest")
	defer span.End()

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Message: "method not allowed"})
		return
	}

	// Any input problem is an invalid zipcode: the contract defines only 422
	// and 404 as failures, so an unreadable body must not produce a 400.
	var req WeatherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidZipCode(w)
		return
	}

	cep, ok := extractCEP(req.CEP)
	if !ok || !isValidCEP(cep) {
		writeInvalidZipCode(w)
		return
	}

	city, err := getCityByCEP(ctx, cep)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, ErrZipCodeNotFound) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Message: "can not find zipcode"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Message: err.Error()})
		}
		return
	}

	tempC, err := getTemperatureByCity(ctx, city)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Message: err.Error()})
		return
	}

	tempF := celsiusToFahrenheit(tempC)
	tempK := celsiusToKelvin(tempC)

	response := WeatherResponse{
		City:  city,
		TempC: math.Round(tempC*10) / 10,
		TempF: math.Round(tempF*10) / 10,
		TempK: math.Round(tempK*10) / 10,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// extractCEP pulls the cep out of the request payload.
//
// The field is decoded as raw JSON rather than straight into a string because
// the specification treats a non-string cep as an invalid zipcode (422), not
// as a malformed request. Decoding into a string directly would fail before
// that distinction could be made, turning {"cep": 29902555} into a 400.
func extractCEP(raw json.RawMessage) (string, bool) {
	// The cep must be a JSON string. A number, boolean, null, object, array
	// or a missing field is an invalid zipcode (422), not a malformed request.
	// Unmarshalling straight into a string would not do: it fails on a number
	// (which would become a 400) and silently accepts null (which would pass
	// as an empty string).
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}

	cep, ok := value.(string)

	return cep, ok
}

// writeInvalidZipCode emits the 422 response defined by the specification.
func writeInvalidZipCode(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	json.NewEncoder(w).Encode(ErrorResponse{Message: "invalid zipcode"})
}

func isValidCEP(cep string) bool {
	matched, _ := regexp.MatchString(`^\d{8}$`, cep)
	return matched
}

func getCityByCEP(ctx context.Context, cep string) (string, error) {
	tracer := otel.Tracer("service-b")
	ctx, span := tracer.Start(ctx, "getCityByCEP")
	defer span.End()

	url := fmt.Sprintf("https://viacep.com.br/ws/%s/json/", cep)

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call ViaCEP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
		return "", ErrZipCodeNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ViaCEP returned status %d", resp.StatusCode)
	}

	var viaCEPResp ViaCEPResponse
	if err := json.NewDecoder(resp.Body).Decode(&viaCEPResp); err != nil {
		return "", fmt.Errorf("failed to parse ViaCEP response: %w", err)
	}

	if viaCEPResp.NotFound() {
		return "", ErrZipCodeNotFound
	}

	return viaCEPResp.Localidade, nil
}

func getTemperatureByCity(ctx context.Context, city string) (float64, error) {
	tracer := otel.Tracer("service-b")
	ctx, span := tracer.Start(ctx, "getTemperatureByCity")
	defer span.End()

	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		return 0, fmt.Errorf("WEATHER_API_KEY not set")
	}

	url := fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=%s&q=%s&aqi=no", apiKey, url.QueryEscape(city))

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to call WeatherAPI: %w", err)
	}
	defer resp.Body.Close()

	var weatherResp WeatherAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&weatherResp); err != nil {
		return 0, fmt.Errorf("failed to parse WeatherAPI response: %w", err)
	}

	return weatherResp.Current.TempC, nil
}

func celsiusToFahrenheit(c float64) float64 {
	return c*1.8 + 32
}

func celsiusToKelvin(c float64) float64 {
	return c + 273
}
