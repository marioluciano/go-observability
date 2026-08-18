package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

type InputRequest struct {
	// Raw so that a non-string cep can be rejected as an invalid zipcode
	// rather than as a malformed body. See extractCEP.
	CEP json.RawMessage `json:"cep"`
}

type ErrorResponse struct {
	Message string `json:"message"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdown, err := initTracing("service-a")
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

	handler := otelhttp.NewHandler(mux, "service-a")
	server := &http.Server{
		Addr:    ":8080",
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

	log.Printf("Service A listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func handleWeatherRequest(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("service-a").Start(r.Context(), "handleWeatherRequest")
	defer span.End()

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Message: "method not allowed"})
		return
	}

	// Any input problem is an invalid zipcode: the contract defines only 422
	// and 404 as failures, so an unreadable body must not produce a 400.
	var req InputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidZipCode(w)
		return
	}

	cep, ok := extractCEP(req.CEP)
	if !ok || !isValidCEP(cep) {
		writeInvalidZipCode(w)
		return
	}

	response, err := forwardToServiceB(ctx, cep)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Message: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	io.Copy(w, response.Body)
	response.Body.Close()
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

func forwardToServiceB(ctx context.Context, cep string) (*http.Response, error) {
	tracer := otel.Tracer("service-a")
	ctx, span := tracer.Start(ctx, "forwardToServiceB")
	defer span.End()

	serviceBURL := os.Getenv("SERVICE_B_URL")
	if serviceBURL == "" {
		serviceBURL = "http://service-b:8081"
	}

	payload := map[string]string{"cep": cep}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceBURL+"/weather", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request to service-b: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call service-b: %w", err)
	}

	return resp, nil
}
