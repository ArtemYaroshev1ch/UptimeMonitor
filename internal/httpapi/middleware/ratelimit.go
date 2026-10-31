package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

// RateLimiter allows each key (client IP) a number of requests per window,
// using a token bucket per key: the bucket holds up to limit tokens and
// refills at limit/window. A client may burst limit requests at once, then
// one more every window/limit.
//
// State is in memory, per process. With N API replicas a client effectively
// gets N times the limit; see docs/decisions.md.
type RateLimiter struct {
	limit  int
	window time.Duration
	clock  clock.Clock

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter returns a limiter allowing limit requests per window per key.
func NewRateLimiter(limit int, window time.Duration, clk clock.Clock) *RateLimiter {
	return &RateLimiter{
		limit:     limit,
		window:    window,
		clock:     clk,
		buckets:   make(map[string]*bucket),
		lastSweep: clk.Now(),
	}
}

// Allow takes a token for key. If none is left it returns false and how long
// until the next token arrives.
func (l *RateLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.clock.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)
	b, found := l.buckets[key]
	if !found {
		every := rate.Every(l.window / time.Duration(l.limit))
		b = &bucket{limiter: rate.NewLimiter(every, l.limit)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	// The *At/*N variants take the time explicitly, so tests can drive
	// the limiter with a fake clock.
	if b.limiter.AllowN(now, 1) {
		return true, 0
	}
	missing := 1 - b.limiter.TokensAt(now)
	return false, time.Duration(missing / float64(b.limiter.Limit()) * float64(time.Second))
}

// sweep drops buckets idle for a full window. Such a bucket has refilled
// completely, so forgetting it changes nothing for its client, and memory
// stays proportional to the number of clients seen in the last window. It
// runs inline, at most once per window, instead of in a goroutine that would
// need its own lifecycle.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) >= l.window {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

// RateLimit rejects requests over the limit with 429 and a Retry-After
// header. Requests are keyed by clientIP.
func RateLimit(l *RateLimiter, clientIP func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retryAfter := l.Allow(clientIP(r))
			if !ok {
				seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				response.WriteError(w, http.StatusTooManyRequests, response.CodeRateLimited, "Too many requests", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns a function that finds the IP of the client of a request.
//
// By default it is the TCP peer address. Behind a reverse proxy that address
// is the proxy's, so with trustProxy the IP is taken from the last entry of
// X-Forwarded-For: the one appended by the proxy itself. Earlier entries are
// whatever the client sent and cannot be trusted. Enable trustProxy only if
// a proxy that appends to X-Forwarded-For always sits in front of the API.
func ClientIP(trustProxy bool) func(*http.Request) string {
	return func(r *http.Request) string {
		if trustProxy {
			if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
				last := xff[len(xff)-1]
				if i := strings.LastIndexByte(last, ','); i >= 0 {
					last = last[i+1:]
				}
				if ip := net.ParseIP(strings.TrimSpace(last)); ip != nil {
					return ip.String()
				}
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
}
