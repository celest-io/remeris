package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// sleeps records the requested waits instead of waiting.
type sleeps struct{ got []time.Duration }

func (s *sleeps) sleep(_ context.Context, d time.Duration) error {
	s.got = append(s.got, d)
	return nil
}

func newTestTransport(s *sleeps) *Transport {
	tr := NewRetryTransport(nil)
	tr.Sleep = s.sleep
	tr.Now = func() time.Time { return fixedNow }
	tr.Jitter = func(_, hi time.Duration) time.Duration { return hi }
	return tr
}

// scripted serves the given statuses in order, repeating the last one, and counts the requests.
func scripted(t *testing.T, statuses []int, header http.Header) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(hits.Add(1)) - 1
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(statuses[min(n, len(statuses)-1)])
		_, _ = io.WriteString(w, "body")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func get(t *testing.T, tr http.RoundTripper, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tr.RoundTrip(req)
}

// captureLogs replaces the default logger and returns the messages logged at warn level.
func captureLogs(t *testing.T) func() []string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []string {
		var out []string
		for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
			if line != "" {
				out = append(out, line)
			}
		}
		return out
	}
}

func TestStatusRetries(t *testing.T) {
	tests := map[string]struct {
		status      int
		wantHits    int32
		wantRetried bool
	}{
		"200 is final":             {http.StatusOK, 1, false},
		"201 is final":             {http.StatusCreated, 1, false},
		"301 is final":             {http.StatusMovedPermanently, 1, false},
		"400 is final":             {http.StatusBadRequest, 1, false},
		"401 is final":             {http.StatusUnauthorized, 1, false},
		"403 is final":             {http.StatusForbidden, 1, false},
		"404 is final":             {http.StatusNotFound, 1, false},
		"422 is final":             {http.StatusUnprocessableEntity, 1, false},
		"408 is retried":           {http.StatusRequestTimeout, 6, true},
		"429 is retried":           {http.StatusTooManyRequests, 6, true},
		"500 is retried":           {http.StatusInternalServerError, 6, true},
		"502 is retried":           {http.StatusBadGateway, 6, true},
		"503 is retried":           {http.StatusServiceUnavailable, 6, true},
		"599 is retried":           {599, 6, true},
		"retries stop after five":  {http.StatusGatewayTimeout, 6, true},
		"501 is retried like 5xx":  {http.StatusNotImplemented, 6, true},
		"507 is retried like 5xx ": {http.StatusInsufficientStorage, 6, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var s sleeps
			srv, hits := scripted(t, []int{tt.status}, nil)
			resp, err := get(t, newTestTransport(&s), srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if got := hits.Load(); got != tt.wantHits {
				t.Errorf("hits = %d, want %d", got, tt.wantHits)
			}
			if body, _ := io.ReadAll(resp.Body); string(body) != "body" {
				t.Errorf("final response body = %q, want it left readable", body)
			}
			if retried := len(s.got) > 0; retried != tt.wantRetried {
				t.Errorf("retried = %v, want %v", retried, tt.wantRetried)
			}
		})
	}
}

func TestSucceedsAfterRetries(t *testing.T) {
	var s sleeps
	srv, hits := scripted(t, []int{500, 503, 200}, nil)
	resp, err := get(t, newTestTransport(&s), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || hits.Load() != 3 {
		t.Errorf("status %d after %d hits, want 200 after 3", resp.StatusCode, hits.Load())
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	var s sleeps
	srv, _ := scripted(t, []int{500}, nil)
	tr := newTestTransport(&s) // Jitter returns the upper bound
	tr.MaxRetries = 7
	resp, err := get(t, tr, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	want := []time.Duration{1, 2, 4, 8, 16, 32, 64}
	if len(s.got) != len(want) {
		t.Fatalf("sleeps = %v, want %d of them", s.got, len(want))
	}
	for i, w := range want {
		if s.got[i] != w*time.Second {
			t.Errorf("sleep %d = %v, want %v", i, s.got[i], w*time.Second)
		}
	}
	if got := newTestTransport(&s).backoff(40); got != DefaultMaxDelay {
		t.Errorf("backoff(40) = %v, want the %v cap", got, DefaultMaxDelay)
	}
}

func TestDefaultJitterStaysInBounds(t *testing.T) {
	tr := NewRetryTransport(nil)
	for retry := range 12 {
		hi := min(DefaultMaxDelay, DefaultMinDelay<<retry)
		for range 50 {
			if d := tr.backoff(retry); d < DefaultMinDelay || d > hi {
				t.Fatalf("backoff(%d) = %v, want within [%v, %v]", retry, d, DefaultMinDelay, hi)
			}
		}
	}
}

func TestRetryAfter(t *testing.T) {
	date := fixedNow.Add(90 * time.Second).Format(http.TimeFormat)
	past := fixedNow.Add(-time.Hour).Format(http.TimeFormat)
	tests := map[string]struct {
		header string
		status int
		want   time.Duration
	}{
		"seconds":                 {"7", http.StatusTooManyRequests, 7 * time.Second},
		"zero seconds":            {"0", http.StatusTooManyRequests, 0},
		"seconds on 503":          {"3", http.StatusServiceUnavailable, 3 * time.Second},
		"longer than the backoff": {"3600", http.StatusTooManyRequests, time.Hour},
		"huge uint64 is clamped": {
			"18446744073709551615", http.StatusTooManyRequests, time.Duration(math.MaxInt64),
		},
		"above uint32 is kept":  {"4294967296", http.StatusTooManyRequests, 4294967296 * time.Second},
		"http date":             {date, http.StatusTooManyRequests, 90 * time.Second},
		"http date in the past": {past, http.StatusTooManyRequests, 0},
		"rfc 2822 date": {
			fixedNow.Add(time.Minute).Format(time.RFC1123Z),
			http.StatusTooManyRequests,
			time.Minute,
		},
		"invalid falls back":         {"soon", http.StatusTooManyRequests, DefaultMinDelay},
		"negative falls back":        {"-5", http.StatusTooManyRequests, DefaultMinDelay},
		"empty falls back":           {"", http.StatusTooManyRequests, DefaultMinDelay},
		"missing header on a 500":    {"", http.StatusInternalServerError, DefaultMinDelay},
		"invalid header on a 500":    {"later", http.StatusInternalServerError, DefaultMinDelay},
		"date form on a 500 is used": {date, http.StatusInternalServerError, 90 * time.Second},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var s sleeps
			header := http.Header{}
			if tt.header != "" {
				header.Set("Retry-After", tt.header)
			}
			srv, _ := scripted(t, []int{tt.status, http.StatusOK}, header)
			resp, err := get(t, newTestTransport(&s), srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if len(s.got) != 1 || s.got[0] != tt.want {
				t.Errorf("sleeps = %v, want [%v]", s.got, tt.want)
			}
		})
	}
}

func TestRateLimitWarnings(t *testing.T) {
	date := fixedNow.Add(time.Minute).Format(http.TimeFormat)
	tests := map[string]struct {
		status        int
		retryAfter    string
		authenticated bool
		want          []string
	}{
		"seconds, anonymous": {
			http.StatusTooManyRequests, "30", false,
			[]string{
				`level=WARN msg="API rate limited; retrying in 30 seconds"`,
				`level=WARN msg="To increase your rate limit, set the GITHUB_TOKEN environment variable"`,
			},
		},
		"seconds, authenticated": {
			http.StatusTooManyRequests, "30", true,
			[]string{`level=WARN msg="API rate limited; retrying in 30 seconds"`},
		},
		"date form, anonymous": {
			http.StatusTooManyRequests, date, false,
			[]string{
				`level=WARN msg="API rate limited; retrying after ` + date + `"`,
				`level=WARN msg="To increase your rate limit, set the GITHUB_TOKEN environment variable"`,
			},
		},
		"unparsable value is quoted as is": {
			http.StatusTooManyRequests, "soon", true,
			[]string{`level=WARN msg="API rate limited; retrying after soon"`},
		},
		"429 without Retry-After is silent": {http.StatusTooManyRequests, "", false, nil},
		"503 with Retry-After is silent":    {http.StatusServiceUnavailable, "5", false, nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			var s sleeps
			header := http.Header{}
			if tt.retryAfter != "" {
				header.Set("Retry-After", tt.retryAfter)
			}
			srv, _ := scripted(t, []int{tt.status, http.StatusOK}, header)
			tr := newTestTransport(&s)
			tr.Authenticated = tt.authenticated
			resp, err := get(t, tr, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			got := logs()
			if len(got) != len(tt.want) {
				t.Fatalf("logs = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("log %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRateLimitWarningOnEveryAttempt(t *testing.T) {
	logs := captureLogs(t)
	var s sleeps
	header := http.Header{"Retry-After": {"1"}}
	srv, _ := scripted(t, []int{http.StatusTooManyRequests}, header)
	tr := newTestTransport(&s)
	tr.Authenticated = true
	resp, err := get(t, tr, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	// Like Knope, the final 429 is warned about too, although no retry follows.
	if got := len(logs()); got != 6 {
		t.Errorf("warnings = %d, want 6", got)
	}
}

func TestUserAgent(t *testing.T) {
	tests := map[string]struct {
		set  string
		want string
	}{
		"added when missing": {"", "Remeris"},
		"kept when set":      {"Custom/1", "Custom/1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var seen []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = append(seen, r.Header.Get("User-Agent"))
				if len(seen) == 1 {
					w.WriteHeader(http.StatusBadGateway)
				}
			}))
			defer srv.Close()
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
			if tt.set != "" {
				req.Header.Set("User-Agent", tt.set)
			}
			var s sleeps
			resp, err := newTestTransport(&s).RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if len(seen) != 2 || seen[0] != tt.want || seen[1] != tt.want {
				t.Errorf("User-Agent seen = %q, want %q on both attempts", seen, tt.want)
			}
			if req.Header.Get("User-Agent") != tt.set {
				t.Errorf("the caller's request was modified: %q", req.Header.Get("User-Agent"))
			}
		})
	}
}

const testPayload = "payload"

func TestBodyReplay(t *testing.T) {
	post := func(t *testing.T, url string, body io.Reader) *http.Request {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, body)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	newServer := func(bodies *[]string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			*bodies = append(*bodies, string(b))
			if len(*bodies) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}))
	}

	t.Run("replayed with GetBody", func(t *testing.T) {
		var bodies []string
		srv := newServer(&bodies)
		defer srv.Close()
		var s sleeps
		resp, err := newTestTransport(&s).RoundTrip(post(t, srv.URL, strings.NewReader(testPayload)))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if len(bodies) != 3 || bodies[0] != testPayload || bodies[1] != testPayload || bodies[2] != testPayload {
			t.Errorf("bodies = %q, want payload three times", bodies)
		}
	})

	t.Run("not retried without GetBody", func(t *testing.T) {
		var bodies []string
		srv := newServer(&bodies)
		defer srv.Close()
		var s sleeps
		req := post(t, srv.URL, io.NopCloser(strings.NewReader(testPayload)))
		if req.GetBody != nil {
			t.Fatal("test setup: GetBody should be nil")
		}
		resp, err := newTestTransport(&s).RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || len(bodies) != 1 || len(s.got) != 0 {
			t.Errorf(
				"status %d, %d requests, %d sleeps; want 503 after exactly one request",
				resp.StatusCode,
				len(bodies),
				len(s.got),
			)
		}
	})

	t.Run("GetBody error ends the retries", func(t *testing.T) {
		var bodies []string
		srv := newServer(&bodies)
		defer srv.Close()
		var s sleeps
		req := post(t, srv.URL, strings.NewReader(testPayload))
		boom := errors.New("boom")
		req.GetBody = func() (io.ReadCloser, error) { return nil, boom }
		if _, err := newTestTransport(&s).RoundTrip(req); !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})
}

// trackedBody reports whether it was drained and closed.
type trackedBody struct {
	io.Reader
	closed *atomic.Bool
}

func (b trackedBody) Close() error { b.closed.Store(true); return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscardedResponsesAreDrainedAndClosed(t *testing.T) {
	var closed, last atomic.Bool
	calls := 0
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		flag := &closed
		status := http.StatusBadGateway
		if calls == 2 {
			flag, status = &last, http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{},
			Body:       trackedBody{strings.NewReader("unread"), flag},
			Request:    r,
		}, nil
	})
	var s sleeps
	tr := newTestTransport(&s)
	tr.Base = base
	resp, err := get(t, tr, "http://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	if !closed.Load() {
		t.Error("the discarded 502 response was not closed")
	}
	if last.Load() {
		t.Error("the returned response must stay open for the caller")
	}
	_ = resp.Body.Close()
}

func TestConnectErrorsAreRetried(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close() // nothing listens any more: connection refused

	var s sleeps
	tr := newTestTransport(&s)
	tr.Base = &http.Transport{}
	_, err = get(t, tr, "http://"+addr)
	if err == nil {
		t.Fatal("expected a connect error")
	}
	if len(s.got) != 5 {
		t.Errorf("retries = %d, want 5", len(s.got))
	}
}

func TestOtherErrorsAreNotRetried(t *testing.T) {
	boom := errors.New("tls: bad certificate")
	var s sleeps
	calls := 0
	tr := newTestTransport(&s)
	tr.Base = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, boom })
	if _, err := get(t, tr, "http://example.invalid/"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestResetsAreRetried(t *testing.T) {
	var s sleeps
	calls := 0
	tr := newTestTransport(&s)
	tr.Base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	})
	resp, err := get(t, tr, "http://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestIsTransientError(t *testing.T) {
	tests := map[string]struct {
		err  error
		want bool
	}{
		"connection reset":    {syscall.ECONNRESET, true},
		"connection refused":  {syscall.ECONNREFUSED, true},
		"connection aborted":  {syscall.ECONNABORTED, true},
		"broken pipe":         {syscall.EPIPE, true},
		"wrapped broken pipe": {&net.OpError{Op: "write", Err: os.NewSyscallError("write", syscall.EPIPE)}, true},
		"unexpected eof":      {io.ErrUnexpectedEOF, true},
		"bare eof":            {io.EOF, true},
		"wrapped eof":         {fmt.Errorf("read: %w", io.EOF), true},
		"other":               {errors.New("tls: bad certificate"), false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := isTransientError(tt.err); got != tt.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestContextCancellation(t *testing.T) {
	t.Run("while sleeping", func(t *testing.T) {
		srv, hits := scripted(t, []int{http.StatusServiceUnavailable}, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		tr := newTestTransport(&sleeps{})
		tr.Sleep = func(ctx context.Context, _ time.Duration) error {
			cancel()
			<-ctx.Done()
			return ctx.Err()
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		if _, err := tr.RoundTrip(req); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if hits.Load() != 1 {
			t.Errorf("hits = %d, want 1", hits.Load())
		}
	})

	t.Run("real sleep returns when the context is done", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if err := (&Transport{}).sleep(ctx, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
		if err := (&Transport{}).sleep(t.Context(), 0); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("request error caused by a done context is final", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var s sleeps
		tr := newTestTransport(&s)
		tr.Base = &http.Transport{}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1", nil)
		if _, err := tr.RoundTrip(req); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if len(s.got) != 0 {
			t.Errorf("sleeps = %v, want none", s.got)
		}
	})
}

func TestDefaultsAndNilBase(t *testing.T) {
	tr := NewRetryTransport(nil)
	if tr.MaxRetries != 5 || tr.RateLimitEnvVar != "GITHUB_TOKEN" || tr.Authenticated {
		t.Errorf("unexpected defaults: %+v", tr)
	}
	if tr.base() != http.DefaultTransport {
		t.Error("a nil Base should use http.DefaultTransport")
	}
	if tr.now().IsZero() {
		t.Error("the default clock returned the zero time")
	}
}
