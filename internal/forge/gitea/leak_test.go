package gitea

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

// syncBuffer collects log output from the goroutines of the HTTP client.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// failsToDecode is a response target whose error repeats the token, as a careless decoder could.
type failsToDecode struct{}

func (*failsToDecode) UnmarshalJSON([]byte) error { return errors.New("cannot read " + testToken) }

// TestTokenNeverLeaks makes every kind of failure happen with a sentinel token and looks for the sentinel in the
// error, in the way the error is rendered for the terminal and in the logs.
func TestTokenNeverLeaks(t *testing.T) {
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	closed := newServer(t, replying(http.StatusOK, ""))
	closedURL := closed.URL
	closed.Close()

	tests := map[string]struct {
		host      string
		handler   func(http.ResponseWriter, *http.Request)
		transport http.RoundTripper
		out       any
		wantLog   string
	}{
		"500 that echoes the token": {
			handler: replying(http.StatusInternalServerError, `{"message":"invalid token `+testToken+`"}`),
		},
		"429 with Retry-After": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("slow down, " + testToken))
			},
			wantLog: "API rate limited; retrying in 7 seconds",
		},
		"refused redirect whose Location echoes the token": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "http://user:"+testToken+"@127.0.0.1:1/x/"+testToken+"?t="+testToken)
				w.WriteHeader(http.StatusTemporaryRedirect)
			},
		},
		"closed server": {host: closedURL},
		"transport error that echoes the token": {
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("proxy rejected token " + testToken)
			}),
		},
		"decoder error that echoes the token": {
			handler: replying(http.StatusOK, `{}`), out: &failsToDecode{},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			host := tc.host
			if host == "" {
				host = newServer(t, tc.handler).URL
			}
			c := newClient(t, host)
			if tc.transport != nil {
				c.http.Transport = tc.transport
			}

			err := c.do(context.Background(), http.MethodPost, c.cfg.releasesURL(), nil, map[string]string{"a": "b"}, tc.out,
				activity)

			if err == nil {
				t.Fatal("want an error")
			}
			var rendered strings.Builder
			diag.Render(&rendered, err)
			for where, text := range map[string]string{
				"message": err.Error(), "help": helpOf(err), "rendering": rendered.String(), "logs": logs.String(),
			} {
				if strings.Contains(text, testToken) {
					t.Errorf("the token is in the %s: %q", where, text)
				}
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Errorf("logs = %q, want %q", logs.String(), tc.wantLog)
			}
			if strings.Contains(logs.String(), "GITHUB_TOKEN") {
				t.Errorf("logs hint at GITHUB_TOKEN for Gitea: %q", logs.String())
			}
		})
	}
}

func TestScrubReplacesTheToken(t *testing.T) {
	c := newClient(t, "https://gitea.example.com")

	err := c.WrapError(activity, errors.New("a "+testToken+" b"))

	if want := "Trouble communicating with https://gitea.example.com while creating a release: a [REDACTED] b"; err.Error() != want {
		t.Errorf("message = %q, want %q", err, want)
	}
}
