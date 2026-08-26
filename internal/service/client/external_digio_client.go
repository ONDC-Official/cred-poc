package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DigioClient is the Digio KYC HTTP adapter. It implements provider.Caller.
type DigioClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type DigioConfig struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

func NewDigioClient(cfg DigioConfig) *DigioClient {
	return &DigioClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Call performs an authenticated Digio HTTP request. method is typically POST.
func (c *DigioClient) Call(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to encode request: %w", err)
	}

	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodPost
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("digio request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// Post is a convenience wrapper for Call with POST.
func (c *DigioClient) Post(ctx context.Context, path string, payload any) ([]byte, int, error) {
	return c.Call(ctx, http.MethodPost, path, payload)
}
