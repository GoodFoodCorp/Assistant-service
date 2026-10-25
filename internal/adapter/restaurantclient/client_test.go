package restaurantclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRestaurants_KeepsActiveOnes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/restaurants" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"id":"a","name":"Good Food République","address":"12 place de la République","city":"Paris","isActive":true},
			{"id":"b","name":"Fermé","city":"Lyon","isActive":false},
			{"id":"","name":"Sans id","isActive":true}
		]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).ListRestaurants(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" || got[0].City != "Paris" || got[0].Address != "12 place de la République" {
		t.Errorf("got %+v", got)
	}
}

func TestListRestaurants_Errors(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(502) }))
	defer down.Close()
	if _, err := New(down.URL).ListRestaurants(context.Background()); err == nil {
		t.Error("a 502 must be an error")
	}
	if _, err := New("http://127.0.0.1:1").ListRestaurants(context.Background()); err == nil {
		t.Error("unreachable must be an error")
	}
}
