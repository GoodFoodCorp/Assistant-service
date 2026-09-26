// Package orderclient talks to order-service to fetch the calling customer's
// own recent orders. The customer's JWT is forwarded, never a service
// account — the assistant can only ever see what the customer themself
// could see.
package orderclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"goodfood/assistant-service/internal/domain"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

type orderResponse struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	TotalAmountCents int64  `json:"total_amount_cents"`
	PlacedAt         string `json:"placed_at"`
	Items            []struct {
		MenuItemName string `json:"menu_item_name"`
	} `json:"items"`
}

const maxOrders = 5

func (c *Client) RecentOrders(ctx context.Context, token string) ([]domain.OrderSummary, error) {
	if token == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/orders", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("order-service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("order-service returned %d", resp.StatusCode)
	}

	var orders []orderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		return nil, fmt.Errorf("invalid order-service response: %w", err)
	}

	if len(orders) > maxOrders {
		orders = orders[:maxOrders]
	}
	out := make([]domain.OrderSummary, 0, len(orders))
	for _, o := range orders {
		names := make([]string, 0, len(o.Items))
		for _, it := range o.Items {
			names = append(names, it.MenuItemName)
		}
		out = append(out, domain.OrderSummary{
			ID: o.ID, Status: o.Status, TotalAmountCents: o.TotalAmountCents,
			ItemNames: names, PlacedAt: o.PlacedAt,
		})
	}
	return out, nil
}
