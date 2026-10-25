// Package restaurantclient lists the active restaurants from franchise-service's
// public endpoint — the same list customers browse, so no auth is needed.
package restaurantclient

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

type restaurantResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	City     string `json:"city"`
	IsActive bool   `json:"isActive"`
}

// maxRestaurants bounds the list that ends up in the model's prompt.
const maxRestaurants = 20

func (c *Client) ListRestaurants(ctx context.Context) ([]domain.RestaurantSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/restaurants", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("franchise-service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("franchise-service returned %d", resp.StatusCode)
	}

	var list []restaurantResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("invalid franchise-service response: %w", err)
	}
	out := make([]domain.RestaurantSummary, 0, len(list))
	for _, r := range list {
		if !r.IsActive || r.ID == "" || r.Name == "" {
			continue
		}
		out = append(out, domain.RestaurantSummary{ID: r.ID, Name: r.Name, Address: r.Address, City: r.City})
		if len(out) == maxRestaurants {
			break
		}
	}
	return out, nil
}
