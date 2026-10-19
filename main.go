package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
)

const receiptBucket = "fintech-receipts"

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	// The same credential and base URL serve image conversion and object storage.
	infrai := NewInfraiClient(defaultBaseURL, apiKey, nil)
	if err := infrai.EnsureBucket(contextBackground(), receiptBucket, "bucket-fintech-receipts"); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /payment-events", paymentEventHandler(infrai))
	address := ":8080"
	log.Printf("receipt converter listening on %s", address)
	log.Fatal(http.ListenAndServe(address, mux))
}

func paymentEventHandler(gateway ReceiptGateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
		decoder.DisallowUnknownFields()
		var event PaymentEvent
		if err := decoder.Decode(&event); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid payment event"})
			return
		}
		decision, err := ProcessReceipt(r.Context(), gateway, receiptBucket, event)
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				status = apiErr.HTTPStatus
			} else if !errors.As(err, &apiErr) {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": decision})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func contextBackground() context.Context { return context.Background() }
