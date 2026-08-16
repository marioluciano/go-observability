package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

type WeatherRequest struct {
	CEP string `json:"cep"`
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

type ViaCEPResponse struct {
	Localidade string `json:"localidade"`
	Erro       bool   `json:"erro"`
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

	var req WeatherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Message: "invalid request format"})
		return
	}

	if !isValidCEP(req.CEP) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(ErrorResponse{Message: "invalid zipcode"})
		return
	}

	city, err := getCityByCEP(ctx, req.CEP)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err.Error() == "zipcode not found" {
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

	var viaCEPResp ViaCEPResponse
	if err := json.NewDecoder(resp.Body).Decode(&viaCEPResp); err != nil {
		return "", fmt.Errorf("failed to parse ViaCEP response: %w", err)
	}

	if viaCEPResp.Erro {
		return "", fmt.Errorf("zipcode not found")
	}

	if viaCEPResp.Localidade == "" {
		return "", fmt.Errorf("zipcode not found")
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
