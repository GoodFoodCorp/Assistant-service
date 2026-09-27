// Package menuclient talks to menu-service's public catalog endpoint to
// fetch a restaurant's menu — no auth required, it's the same public catalog
// customers browse.
package menuclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

type menuItemResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int64  `json:"price_cents"`
	Category    string `json:"category"`
	Available   bool   `json:"available"`
}

const maxMenuItems = 30

func (c *Client) MenuForRestaurant(ctx context.Context, restaurantID string) ([]domain.MenuItemSummary, error) {
	q := url.Values{"restaurantId": {restaurantID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/menu?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("menu-service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("menu-service returned %d", resp.StatusCode)
	}

	var items []menuItemResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("invalid menu-service response: %w", err)
	}

	if len(items) > maxMenuItems {
		items = items[:maxMenuItems]
	}
	out := make([]domain.MenuItemSummary, 0, len(items))
	for _, it := range items {
		out = append(out, domain.MenuItemSummary{
			ID: it.ID, Name: it.Name, Description: it.Description, PriceCents: it.PriceCents,
			Category: it.Category, Available: it.Available,
		})
	}
	return out, nil
}
