package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type PaymentEvent struct {
	PaymentID   string `json:"payment_id"`
	AmountCents int64  `json:"amount_cents"`
	RiskLevel   string `json:"risk_level"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ImageBase64 string `json:"image_base64"`
}

type ReceiptDecision struct {
	PaymentID    string `json:"payment_id"`
	Action       string `json:"action"`
	Format       string `json:"format"`
	ObjectKey    string `json:"object_key"`
	Notification string `json:"notification"`
}

type ReceiptGateway interface {
	ConvertImage(context.Context, string, string, string, string) (string, error)
	PutObject(context.Context, string, string, string, string, string) error
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func PlanReceipt(event PaymentEvent) (ReceiptDecision, error) {
	if !safeID.MatchString(event.PaymentID) {
		return ReceiptDecision{}, fmt.Errorf("payment_id must contain only letters, numbers, underscore, or hyphen")
	}
	if event.AmountCents <= 0 || event.ImageBase64 == "" {
		return ReceiptDecision{}, fmt.Errorf("amount_cents and image_base64 are required")
	}
	if event.ContentType != "image/jpeg" && event.ContentType != "image/png" {
		return ReceiptDecision{}, fmt.Errorf("content_type must be image/jpeg or image/png")
	}

	decision := ReceiptDecision{PaymentID: event.PaymentID, Format: "webp", Action: "archive"}
	if strings.EqualFold(event.RiskLevel, "high") {
		decision.Format = "avif"
		decision.Action = "manual_review"
	}
	decision.ObjectKey = "payments/" + event.PaymentID + "/receipt." + decision.Format
	decision.Notification = fmt.Sprintf("payment %s receipt stored; action=%s", event.PaymentID, decision.Action)
	return decision, nil
}

func ProcessReceipt(ctx context.Context, gateway ReceiptGateway, bucket string, event PaymentEvent) (ReceiptDecision, error) {
	decision, err := PlanReceipt(event)
	if err != nil {
		return ReceiptDecision{}, err
	}
	idempotencyKey := "receipt-" + event.PaymentID + "-" + decision.Format
	converted, err := gateway.ConvertImage(ctx, event.ImageBase64, event.ContentType, decision.Format, idempotencyKey+"-convert")
	if err != nil {
		return ReceiptDecision{}, err
	}
	if err := gateway.PutObject(ctx, bucket, decision.ObjectKey, converted, "image/"+decision.Format, idempotencyKey+"-store"); err != nil {
		return ReceiptDecision{}, err
	}
	return decision, nil
}
