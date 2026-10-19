// Package httpx holds remeris' HTTP behaviour (spec F15.1): the User-Agent, a retrying
// http.RoundTripper with Retry-After support and rate-limit warnings, and the diagnostics for failed
// API calls. It uses the standard library only.
package httpx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// Defaults taken from Knope: reqwest-retry's ExponentialBackoff::builder() with 5 retries.
const (
	// UserAgent is sent on every request that has none.
	UserAgent = "Remeris"
	// DefaultMaxRetries is the number of retries after the first attempt.
	DefaultMaxRetries = 5
	// DefaultMinDelay is the first backoff and the lower bound of every jittered delay.
	DefaultMinDelay = time.Second
	// DefaultMaxDelay caps the exponential backoff.
	DefaultMaxDelay = 30 * time.Minute

	drainLimit = 64 << 10
	// maxShift keeps DefaultMinDelay<<retry from overflowing.
	maxShift = 30
)

// Transport is an http.RoundTripper that sets the User-Agent and retries transient failures.
//
// A request is retried up to MaxRetries times when the connection fails (not when its context is
// done), or the response status is 5xx, 408 or 429. The wait before a retry is the Retry-After header
// when the response has a valid one (delta-seconds or an HTTP date, never capped, like Knope), else
// exponential backoff with full jitter: a random time between DefaultMinDelay and
// min(DefaultMaxDelay, DefaultMinDelay*2^retry). A request with a body but without GetBody is sent once.
//
// Create one with NewRetryTransport. The injectable fields exist for tests.
type Transport struct {
	// Base performs the requests. Nil means http.DefaultTransport.
	Base http.RoundTripper
	// Authenticated is false when the client sends no credentials. Rate-limit warnings then add a hint
	// to set RateLimitEnvVar.
	Authenticated bool
	// RateLimitEnvVar names the variable in that hint. NewRetryTransport sets GITHUB_TOKEN.
	RateLimitEnvVar string
	// MaxRetries is the number of retries after the first attempt.
	MaxRetries int
	// Sleep waits for d or returns the context's error. Nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock used to read an HTTP date in Retry-After. Nil means time.Now.
	Now func() time.Time
	// Jitter picks the delay in [lo, hi]. Nil picks uniformly at random.
	Jitter func(lo, hi time.Duration) time.Duration
}

// NewRetryTransport wraps base (nil means http.DefaultTransport) with the F15.1 policy. The result is
// unauthenticated; set Authenticated when base adds credentials.
func NewRetryTransport(base http.RoundTripper) *Transport {
	return &Transport{Base: base, RateLimitEnvVar: "GITHUB_TOKEN", MaxRetries: DefaultMaxRetries}
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	for attempt := 0; ; attempt++ {
		out := req.Clone(ctx)
		if out.Header.Get("User-Agent") == "" {
			out.Header.Set("User-Agent", UserAgent)
		}
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			out.Body = body
		}

		resp, err := t.base().RoundTrip(out)
		var retryAt time.Time
		switch {
		case err != nil:
			if ctx.Err() != nil || !isTransientError(err) {
				return resp, err
			}
		case isTransientStatus(resp.StatusCode):
			t.warnRateLimit(ctx, resp)
			retryAt, _ = t.retryAfter(resp.Header.Get("Retry-After"))
		default:
			return resp, nil
		}
		if !replayable || attempt >= t.MaxRetries {
			return resp, err
		}

		wait := t.backoff(attempt)
		if !retryAt.IsZero() {
			wait = max(retryAt.Sub(t.now()), 0)
		}
		if resp != nil {
			drain(resp)
		}
		if serr := t.sleep(ctx, wait); serr != nil {
			return nil, serr
		}
	}
}

func (t *Transport) base() http.RoundTripper {
	if t.Base == nil {
		return http.DefaultTransport
	}
	return t.Base
}

func (t *Transport) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

func (t *Transport) sleep(ctx context.Context, d time.Duration) error {
	if t.Sleep != nil {
		return t.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// backoff is reqwest-retry's ExponentialBackoff with Jitter::Full and base 2.
func (t *Transport) backoff(retry int) time.Duration {
	hi := DefaultMaxDelay
	if retry < maxShift {
		hi = min(DefaultMaxDelay, DefaultMinDelay<<retry)
	}
	if t.Jitter != nil {
		return t.Jitter(DefaultMinDelay, hi)
	}
	// Retry timing needs no cryptographic randomness.
	return DefaultMinDelay + rand.N(hi-DefaultMinDelay+1) //nolint:gosec // jitter only
}

// retryAfter parses a Retry-After value as delta-seconds or an HTTP date.
func (t *Transport) retryAfter(v string) (time.Time, bool) {
	if secs, err := strconv.ParseUint(v, 10, 32); err == nil {
		return t.now().Add(time.Duration(secs) * time.Second), true
	}
	if date, err := http.ParseTime(v); err == nil {
		return date, true
	}
	if date, err := time.Parse(time.RFC1123Z, v); err == nil {
		return date, true
	}
	return time.Time{}, false
}

// warnRateLimit logs Knope's rate-limit warnings for a 429 that carries Retry-After.
func (t *Transport) warnRateLimit(ctx context.Context, resp *http.Response) {
	value := resp.Header.Get("Retry-After")
	if resp.StatusCode != http.StatusTooManyRequests || value == "" {
		return
	}
	if secs, err := strconv.ParseUint(value, 10, 64); err == nil {
		slog.WarnContext(ctx, "API rate limited; retrying in "+strconv.FormatUint(secs, 10)+" seconds")
	} else {
		slog.WarnContext(ctx, "API rate limited; retrying after "+value)
	}
	if !t.Authenticated && t.RateLimitEnvVar != "" {
		slog.WarnContext(ctx, "To increase your rate limit, set the "+t.RateLimitEnvVar+" environment variable")
	}
}

func isTransientStatus(code int) bool {
	return code >= http.StatusInternalServerError || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
}

// isTransientError reports connection-level failures: dial errors, resets, timeouts and connections
// cut mid-response. The caller has already excluded a done context.
func isTransientError(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, io.ErrUnexpectedEOF)
}

// drain reads what is left of a discarded response, so the connection can be reused, and closes it.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
	_ = resp.Body.Close()
}
