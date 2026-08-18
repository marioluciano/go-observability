package main

import (
	"encoding/json"
	"math"
	"testing"
)

func TestViaCEPNotFound(t *testing.T) {
	tests := []struct {
		name         string
		payload      string
		wantNotFound bool
	}{
		{
			name:         "erro as a string, the shape that broke",
			payload:      `{"erro": "true"}`,
			wantNotFound: true,
		},
		{
			name:         "erro as a boolean",
			payload:      `{"erro": true}`,
			wantNotFound: true,
		},
		{
			name:         "valid CEP",
			payload:      `{"cep":"01310-100","localidade":"São Paulo","uf":"SP"}`,
			wantNotFound: false,
		},
		{
			name:         "erro explicitly false with a city",
			payload:      `{"localidade":"Rio de Janeiro","erro":false}`,
			wantNotFound: false,
		},
		{
			name:         "erro explicitly false as a string with a city",
			payload:      `{"localidade":"Rio de Janeiro","erro":"false"}`,
			wantNotFound: false,
		},
		{
			name:         "no erro field and no city",
			payload:      `{}`,
			wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response ViaCEPResponse

			if err := json.Unmarshal([]byte(tt.payload), &response); err != nil {
				t.Fatalf("decoding %s failed: %v", tt.payload, err)
			}

			if got := response.NotFound(); got != tt.wantNotFound {
				t.Errorf("NotFound() = %v for %s, want %v", got, tt.payload, tt.wantNotFound)
			}
		})
	}
}

func TestConversions(t *testing.T) {
	const tolerance = 0.001

	tests := []struct {
		celsius    float64
		fahrenheit float64
		kelvin     float64
	}{
		{celsius: 0, fahrenheit: 32, kelvin: 273},
		{celsius: 28.5, fahrenheit: 83.3, kelvin: 301.5},
		{celsius: 100, fahrenheit: 212, kelvin: 373},
		{celsius: -40, fahrenheit: -40, kelvin: 233},
	}

	for _, tt := range tests {
		if got := celsiusToFahrenheit(tt.celsius); math.Abs(got-tt.fahrenheit) > tolerance {
			t.Errorf("celsiusToFahrenheit(%v) = %v, want %v", tt.celsius, got, tt.fahrenheit)
		}
		if got := celsiusToKelvin(tt.celsius); math.Abs(got-tt.kelvin) > tolerance {
			t.Errorf("celsiusToKelvin(%v) = %v, want %v", tt.celsius, got, tt.kelvin)
		}
	}
}

func TestIsValidCEP(t *testing.T) {
	valid := []string{"01310100", "00000000", "29902555"}
	invalid := []string{"", "123", "0131010", "013101000", "0131010a", "01310-100"}

	for _, cep := range valid {
		if !isValidCEP(cep) {
			t.Errorf("isValidCEP(%q) = false, want true", cep)
		}
	}

	for _, cep := range invalid {
		if isValidCEP(cep) {
			t.Errorf("isValidCEP(%q) = true, want false", cep)
		}
	}
}

// TestExtractCEP covers the specification rule that a cep which is not a
// string is an invalid zipcode (422), not a malformed request (400).
func TestExtractCEP(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantCEP string
		wantOK  bool
	}{
		{name: "string cep", payload: `{"cep":"29902555"}`, wantCEP: "29902555", wantOK: true},
		{name: "number cep", payload: `{"cep":29902555}`, wantOK: false},
		{name: "boolean cep", payload: `{"cep":true}`, wantOK: false},
		{name: "null cep", payload: `{"cep":null}`, wantOK: false},
		{name: "object cep", payload: `{"cep":{"value":"29902555"}}`, wantOK: false},
		{name: "array cep", payload: `{"cep":["29902555"]}`, wantOK: false},
		{name: "missing cep", payload: `{}`, wantOK: false},
		{name: "empty string cep", payload: `{"cep":""}`, wantCEP: "", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req WeatherRequest
			if err := json.Unmarshal([]byte(tt.payload), &req); err != nil {
				t.Fatalf("decoding %s failed: %v", tt.payload, err)
			}

			cep, ok := extractCEP(req.CEP)

			if ok != tt.wantOK {
				t.Fatalf("extractCEP ok = %v for %s, want %v", ok, tt.payload, tt.wantOK)
			}
			if ok && cep != tt.wantCEP {
				t.Errorf("extractCEP cep = %q, want %q", cep, tt.wantCEP)
			}

			// Whatever the shape, the request must never be accepted as a
			// valid zipcode unless it is a string of exactly 8 digits.
			if (ok && isValidCEP(cep)) && tt.payload != `{"cep":"29902555"}` {
				t.Errorf("payload %s should not pass validation", tt.payload)
			}
		})
	}
}
