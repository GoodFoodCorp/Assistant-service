package http

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clientIP is the address the edge proxy (nginx locally, Traefik in the
// cluster) saw: the LAST X-Forwarded-For entry, the one it appended itself.
// Earlier entries come from the client and can be anything.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimiter caps how many chat messages one customer sends: every message
// costs a model call. A token bucket per user: Burst messages at once, then
// PerMinute. Kept in memory: a pod's own share is enough to stop a script.
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	perSecond float64
	burst     float64
	now       func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// maxTrackedUsers bounds memory: idle buckets (full again) are dropped past it.
const maxTrackedUsers = 50_000

func NewRateLimiter(perMinute, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: map[string]*bucket{}, perSecond: float64(perMinute) / 60, burst: float64(burst), now: time.Now,
	}
}

// allow takes one token for key, or says how long to wait for the next one.
func (l *RateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxTrackedUsers {
			l.dropIdle(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.perSecond)
	b.last = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / l.perSecond * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

func (l *RateLimiter) dropIdle(now time.Time) {
	refill := time.Duration(l.burst / l.perSecond * float64(time.Second))
	for k, b := range l.buckets {
		if now.Sub(b.last) > refill {
			delete(l.buckets, k)
		}
	}
}

// Limit answers 429 once the authenticated customer is over their share.
func (l *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, wait := l.allow(actorFrom(r).UserID)
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, r, http.StatusTooManyRequests, "Vous envoyez beaucoup de messages : patientez quelques secondes avant de réessayer.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
