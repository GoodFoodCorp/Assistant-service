// Package paymentclient talks to payment-service to fetch the calling
// customer's default saved card — informational only, so an OrderProposal
// can name it exactly like the cart's checkout summary would. The customer's
// own JWT is forwarded, never a service account.
package paymentclient

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

type paymentMethodResponse struct {
	Brand     string `json:"brand"`
	Last4     string `json:"last4"`
	IsDefault bool   `json:"is_default"`
}

// DefaultPaymentMethod returns "" (no error) when the customer has no saved
// card — a proposal without a named card is still perfectly valid.
func (c *Client) DefaultPaymentMethod(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/payments/methods", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("payment-service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("payment-service returned %d", resp.StatusCode)
	}

	var methods []paymentMethodResponse
	if err := json.NewDecoder(resp.Body).Decode(&methods); err != nil {
		return "", fmt.Errorf("invalid payment-service response: %w", err)
	}
	if len(methods) == 0 {
		return "", nil
	}

	chosen := methods[0]
	for _, m := range methods {
		if m.IsDefault {
			chosen = m
			break
		}
	}
	return chosen.Brand + " •••• " + chosen.Last4, nil
}
