package orderclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The assistant answers "où en est ma commande ?" and "pareil que la dernière
// fois": it must see the customer's NEWEST orders first, whatever order
// order-service happens to list them in.
func TestRecentOrdersAreNewestFirstAndCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("the customer's own token must be forwarded, got %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`[
			{"id":"o1","status":"DELIVERED","total_amount_cents":100,"placed_at":"2026-10-01T10:00:00Z","items":[{"menu_item_name":"A"}]},
			{"id":"o7","status":"PLACED","total_amount_cents":700,"placed_at":"2026-10-25T10:00:00Z","items":[{"menu_item_name":"G"}]},
			{"id":"o3","status":"DELIVERED","total_amount_cents":300,"placed_at":"2026-10-10T10:00:00Z","items":[]},
			{"id":"o6","status":"CONFIRMED","total_amount_cents":600,"placed_at":"2026-10-24T10:00:00Z","items":[]},
			{"id":"o2","status":"DELIVERED","total_amount_cents":200,"placed_at":"2026-10-05T10:00:00Z","items":[]},
			{"id":"o5","status":"DELIVERED","total_amount_cents":500,"placed_at":"2026-10-20T10:00:00Z","items":[]},
			{"id":"o4","status":"CANCELLED","total_amount_cents":400,"placed_at":"2026-10-15T10:00:00Z","items":[]}
		]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).RecentOrders(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"o7", "o6", "o5", "o4", "o3"}
	if len(got) != len(want) {
		t.Fatalf("got %d orders, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d = %s, want %s (newest first)", i, got[i].ID, id)
		}
	}
	if got[0].ItemNames[0] != "G" {
		t.Errorf("item names lost: %+v", got[0])
	}
}

func TestRecentOrdersWithoutATokenIsEmpty(t *testing.T) {
	got, err := New("http://127.0.0.1:1").RecentOrders(context.Background(), "")
	if err != nil || got != nil {
		t.Errorf("got %v / %v", got, err)
	}
}
