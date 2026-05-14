// Package shared provides common utilities for plugin implementations.
package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AdminAPIClient performs HTTP-based admin API requests for overlay network plugins.
type AdminAPIClient struct {
	httpClient *http.Client
}

// NewAdminAPIClient creates a new AdminAPIClient with the provided HTTP client.
func NewAdminAPIClient(httpClient *http.Client) *AdminAPIClient {
	return &AdminAPIClient{httpClient: httpClient}
}

// SendRequest sends a JSON payload to the specified admin API URL.
func (c *AdminAPIClient) SendRequest(url string, payload interface{}) error {
	return c.SendRequestWithContext(context.Background(), url, payload)
}

// SendRequestWithContext sends a JSON payload to the specified admin API URL with context.
func (c *AdminAPIClient) SendRequestWithContext(ctx context.Context, url string, payload interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		return fmt.Errorf("admin API returned error: %s", errMsg)
	}

	return nil
}
