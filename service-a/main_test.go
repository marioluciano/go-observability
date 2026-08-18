package main

import (
	"encoding/json"
	"testing"
)

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
			var req InputRequest
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

func TestIsValidCEP(t *testing.T) {
	valid := []string{"01310100", "00000000", "29902555"}
	invalid := []string{"", "123", "0131010", "013101000", "0131010a", "01310-100", " 29902555"}

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
