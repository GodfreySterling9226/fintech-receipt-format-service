package main

import (
	"context"
	"testing"
)

type recordingGateway struct {
	format      string
	objectKey   string
	contentType string
}

func (g *recordingGateway) ConvertImage(_ context.Context, _, _, format, _ string) (string, error) {
	g.format = format
	return "converted-image", nil
}

func (g *recordingGateway) PutObject(_ context.Context, _, key, _, contentType, _ string) error {
	g.objectKey = key
	g.contentType = contentType
	return nil
}

func TestReceiptRiskDecisionAndRequestBoundary(t *testing.T) {
	tests := []struct {
		name        string
		risk        string
		wantFormat  string
		wantAction  string
		wantKey     string
		wantContent string
	}{
		{name: "routine payment", risk: "low", wantFormat: "webp", wantAction: "archive", wantKey: "payments/pay_101/receipt.webp", wantContent: "image/webp"},
		{name: "high risk payment", risk: "high", wantFormat: "avif", wantAction: "manual_review", wantKey: "payments/pay_101/receipt.avif", wantContent: "image/avif"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			event := PaymentEvent{PaymentID: "pay_101", AmountCents: 4200, RiskLevel: test.risk, Filename: "receipt.png", ContentType: "image/png", ImageBase64: "aW1hZ2U="}
			got, err := ProcessReceipt(context.Background(), gateway, "receipts", event)
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != test.wantFormat || got.Action != test.wantAction {
				t.Fatalf("decision = %s/%s, want %s/%s", got.Format, got.Action, test.wantFormat, test.wantAction)
			}
			if gateway.format != test.wantFormat || gateway.objectKey != test.wantKey || gateway.contentType != test.wantContent {
				t.Fatalf("boundary = %s/%s/%s", gateway.format, gateway.objectKey, gateway.contentType)
			}
		})
	}
}
