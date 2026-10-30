package http

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_BurstThenSteadyRate(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewRateLimiter(12, 3)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.allow("u1"); !ok {
			t.Fatalf("message %d of the burst refused", i+1)
		}
	}
	if ok, wait := l.allow("u1"); ok || wait <= 0 {
		t.Fatalf("4th message at once: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.allow("u2"); !ok {
		t.Fatal("another customer is not limited by u1")
	}
	now = now.Add(5 * time.Second) // 12/min = one every 5 s
	if ok, _ := l.allow("u1"); !ok {
		t.Fatal("a token is back after 5 s")
	}
}

func TestClientIP_TakesWhatTheProxyAppended(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Errorf("got %s", got)
	}
	r.Header.Del("X-Forwarded-For")
	if got := clientIP(r); got != "10.0.0.5" {
		t.Errorf("got %s", got)
	}
}
