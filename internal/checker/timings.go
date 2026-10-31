package checker

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

type timingsKey struct{}

// timings collects the phase durations of one check. httptrace calls the
// hooks from the transport's goroutines, hence the mutex.
//
// Every hop of a redirect chain opens its own connection (keep-alives are
// off); GetConn starts a hop and resets the phases, so the result describes
// the final request only. ResponseTimeMs covers the whole chain.
type timings struct {
	clock clock.Clock

	mu                         sync.Mutex
	dns, connect, tlsHandshake time.Duration
	hasDNS, hasConnect, hasTLS bool
	connectStart, tlsStart     time.Time
	wroteRequest, firstByte    time.Time
}

func timingsFrom(ctx context.Context) *timings {
	tm, _ := ctx.Value(timingsKey{}).(*timings)
	return tm
}

// setDNS records the lookup time measured by Checker.dial. A nil receiver
// (dial called without Check, which does not happen) is ignored.
func (t *timings) setDNS(d time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dns, t.hasDNS = d, true
}

// trace returns the hooks. DNS is not among them: Checker.dial resolves
// with the injected resolver and times the lookup itself.
func (t *timings) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn: func(string) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.dns, t.connect, t.tlsHandshake = 0, 0, 0
			t.hasDNS, t.hasConnect, t.hasTLS = false, false, false
			t.connectStart, t.tlsStart = time.Time{}, time.Time{}
			t.wroteRequest, t.firstByte = time.Time{}, time.Time{}
		},
		ConnectStart: func(string, string) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.connectStart = t.clock.Now()
		},
		ConnectDone: func(_, _ string, err error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if err == nil {
				t.connect, t.hasConnect = t.clock.Now().Sub(t.connectStart), true
			}
		},
		TLSHandshakeStart: func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.tlsStart = t.clock.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if err == nil {
				t.tlsHandshake, t.hasTLS = t.clock.Now().Sub(t.tlsStart), true
			}
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.wroteRequest = t.clock.Now()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.firstByte = t.clock.Now()
		},
	}
}

// fill copies the measured phases into res.
func (t *timings) fill(res *domain.CheckResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hasDNS {
		res.DNSMs = ptr(millis(t.dns))
	}
	if t.hasConnect {
		res.ConnectMs = ptr(millis(t.connect))
	}
	if t.hasTLS {
		res.TLSMs = ptr(millis(t.tlsHandshake))
	}
	if !t.wroteRequest.IsZero() && !t.firstByte.IsZero() {
		res.TTFBMs = ptr(millis(t.firstByte.Sub(t.wroteRequest)))
	}
}

func ptr[T any](v T) *T { return &v }
