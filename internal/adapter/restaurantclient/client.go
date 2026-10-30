// Package restaurantclient lists the active restaurants from franchise-service's
// public endpoint — the same list customers browse, so no auth is needed.
package restaurantclient

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

type quoteResponse struct {
	CanOrder                 bool     `json:"canOrder"`
	Refusal                  *string  `json:"refusal"`
	DistanceKm               *float64 `json:"distanceKm"`
	DeliveryFeeCents         int64    `json:"deliveryFeeCents"`
	MinimumOrderCents        int64    `json:"minimumOrderCents"`
	EstimatedDeliveryMinutes int      `json:"estimatedDeliveryMinutes"`
}

// Quote is the same check the cart makes before checkout (public endpoint).
func (c *Client) Quote(ctx context.Context, restaurantID, address string) (*domain.DeliveryQuote, error) {
	var q quoteResponse
	path := "/api/restaurants/" + url.PathEscape(restaurantID) + "/quote?address=" + url.QueryEscape(address)
	if err := c.get(ctx, path, &q); err != nil {
		return nil, err
	}
	out := &domain.DeliveryQuote{
		CanOrder: q.CanOrder, DistanceKm: q.DistanceKm, DeliveryFeeCents: q.DeliveryFeeCents,
		MinimumOrderCents: q.MinimumOrderCents, EstimatedMinutes: q.EstimatedDeliveryMinutes,
	}
	if q.Refusal != nil {
		out.Refusal = *q.Refusal
	}
	return out, nil
}

type suggestionResponse struct {
	Label string `json:"label"`
}

func (c *Client) SuggestAddresses(ctx context.Context, text, clientIP string) ([]string, error) {
	var list []suggestionResponse
	if err := c.get(ctx, "/api/restaurants/address-suggestions?limit=5&q="+url.QueryEscape(text), &list, clientIP); err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(list))
	for _, s := range list {
		labels = append(labels, s.Label)
	}
	return labels, nil
}

func (c *Client) get(ctx context.Context, path string, out any, clientIP ...string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if len(clientIP) > 0 && clientIP[0] != "" {
		req.Header.Set("X-Forwarded-For", clientIP[0])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("franchise-service unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("franchise-service returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("invalid franchise-service response: %w", err)
	}
	return nil
}
