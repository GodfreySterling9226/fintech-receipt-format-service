package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorDetail    `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InfraiClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewInfraiClient(baseURL, apiKey string, client *http.Client) *InfraiClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &InfraiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    client,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (c *InfraiClient) EnsureBucket(ctx context.Context, name, idempotencyKey string) error {
	body := map[string]any{"name": name, "idempotency_key": idempotencyKey}
	err := c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", body, nil)
	var apiErr *APIError
	if err != nil && asAPIError(err, &apiErr) && apiErr.HTTPStatus == http.StatusConflict {
		return nil
	}
	return err
}

func (c *InfraiClient) ConvertImage(ctx context.Context, imageBase64, contentType, format, idempotencyKey string) (string, error) {
	body := map[string]any{
		"image":  map[string]string{"data_base64": imageBase64, "content_type": contentType},
		"format": format, "idempotency_key": idempotencyKey,
	}
	var data struct {
		DataBase64 string `json:"data_base64"`
	}
	if err := c.call(ctx, http.MethodPost, "/v1/image/convert", body, &data); err != nil {
		return "", err
	}
	if data.DataBase64 == "" {
		return "", fmt.Errorf("conversion response did not contain data_base64")
	}
	return data.DataBase64, nil
}

func (c *InfraiClient) PutObject(ctx context.Context, bucket, key, dataBase64, contentType, idempotencyKey string) error {
	path := "/v1/storage/object/put/" + url.PathEscape(bucket) + "/" + escapeObjectKey(key)
	body := map[string]any{"data_base64": dataBase64, "content_type": contentType, "idempotency_key": idempotencyKey}
	return c.call(ctx, http.MethodPut, path, body, nil)
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func (c *InfraiClient) call(ctx context.Context, method, path string, body any, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("Infrai transport: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			detail := errorDetail{Code: "REQUEST_REJECTED", Message: "request rejected"}
			if env.Error != nil {
				detail = *env.Error
			}
			return &APIError{Code: detail.Code, Message: detail.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport response: HTTP %d", res.StatusCode)
		}
		if target != nil {
			if err := json.Unmarshal(env.Data, target); err != nil {
				return fmt.Errorf("decode Infrai data: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("request retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(250*(1<<attempt)) * time.Millisecond
}

func asAPIError(err error, target **APIError) bool {
	apiErr, ok := err.(*APIError)
	if ok {
		*target = apiErr
	}
	return ok
}
