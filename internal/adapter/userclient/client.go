// Package userclient talks to user-service to fetch the calling customer's
// default saved address — so "chez moi" resolves to a real address instead
// of being passed through to order-service as free text. The customer's own
// JWT is forwarded, never a service account.
package userclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

type addressResponse struct {
	IsDefault bool   `json:"is_default"`
	Full      string `json:"full_address"`
}

// DefaultAddress returns "" (no error) when the customer has no saved
// address — the assistant just falls back to asking for one.
func (c *Client) DefaultAddress(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/users/me/addresses", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("user-service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("user-service returned %d", resp.StatusCode)
	}

	var addresses []addressResponse
	if err := json.NewDecoder(resp.Body).Decode(&addresses); err != nil {
		return "", fmt.Errorf("invalid user-service response: %w", err)
	}
	if len(addresses) == 0 {
		return "", nil
	}

	chosen := addresses[0]
	for _, a := range addresses {
		if a.IsDefault {
			chosen = a
			break
		}
	}
	return chosen.Full, nil
}
