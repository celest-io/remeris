package gitea

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

// TestGuardEndToEnd sends requests that carry an Authorization header through the transport of a real client. The
// header may reach the configured origin and no other.
func TestGuardEndToEnd(t *testing.T) {
	own := newServer(t, replying(http.StatusOK, `{}`))
	other := newServer(t, replying(http.StatusOK, `{}`))
	c := newClient(t, own.URL)
	const header = "token " + testToken

	tests := map[string]struct {
		target   *server
		wantAuth string
	}{
		"configured origin keeps the header": {target: own, wantAuth: header},
		"another loopback origin loses it":   {target: other, wantAuth: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.target.URL+"/x", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", header)

			resp, err := c.http.Transport.RoundTrip(req)

			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			got := tc.target.requests()
			if len(got) != 1 || got[0].auth != tc.wantAuth {
				t.Errorf("requests = %+v, want Authorization %q", got, tc.wantAuth)
			}
		})
	}
}

// TestNoRateLimitHintForGitea checks that the warning for a 429 of an unauthenticated client does not name
// GITHUB_TOKEN, which means nothing to Gitea: the transport is made unauthenticated on purpose.
func TestNoRateLimitHintForGitea(t *testing.T) {
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	f := newForge(srv.URL, tokenStore(t))
	f.tune = func(tr *httpx.Transport) {
		tr.Sleep = func(context.Context, time.Duration) error { return nil }
		tr.Authenticated = false
	}
	c, err := f.RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := c.do(context.Background(), http.MethodGet, "/user", nil, nil, nil, "checking"); err == nil {
		t.Fatal("want the 429")
	}

	got := logs.String()
	if !strings.Contains(got, "API rate limited; retrying in 3 seconds") {
		t.Errorf("logs = %q, want the rate limit warning", got)
	}
	for _, hint := range []string{"GITHUB_TOKEN", "To increase your rate limit"} {
		if strings.Contains(got, hint) {
			t.Errorf("logs hold %q: %q", hint, got)
		}
	}
}

// TestScrubKeepsEveryField checks that cleaning a diagnostic changes the token and nothing else.
func TestScrubKeepsEveryField(t *testing.T) {
	c := newClient(t, "https://gitea.example.com")
	nested := diag.New("nested " + testToken).WithCode("nested::code").WithHelp("nested help " + testToken)
	original := diag.New("message "+testToken).
		WithCode("some::code").
		WithHelp("help "+testToken).
		WithURL("https://docs.example/"+testToken).
		WithSource("source "+testToken, diag.Label{Start: 1, End: 3, Text: "label"}).
		WithRelated(nested, errors.New("plain "+testToken)).
		WithCause(context.DeadlineExceeded)

	got := c.scrub.Diag(original)

	if got.Error() != "message [REDACTED]" || got.Code() != "some::code" || got.Help() != "help [REDACTED]" ||
		got.URL() != "https://docs.example/[REDACTED]" || got.SourceCode() != "source [REDACTED]" {
		t.Errorf("scrubbed = %q %q %q %q %q", got.Error(), got.Code(), got.Help(), got.URL(), got.SourceCode())
	}
	if labels := got.Labels(); len(labels) != 1 || labels[0] != (diag.Label{Start: 1, End: 3, Text: "label"}) {
		t.Errorf("labels = %+v", labels)
	}
	rel := got.Related()
	if len(rel) != 2 || rel[0].Error() != "nested [REDACTED]" || rel[1].Error() != "plain [REDACTED]" {
		t.Fatalf("related = %v", rel)
	}
	if d, ok := rel[0].(diag.Diagnostic); !ok || d.Code() != "nested::code" || d.Help() != "nested help [REDACTED]" {
		t.Errorf("nested = %#v", rel[0])
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Error("the cause should be reachable with errors.Is")
	}
}

// TestCauseIsReachableButNeverPrinted pins the contract of the scrubbed cause: errors.Is sees it, and no way of
// printing the error shows its text.
func TestCauseIsReachableButNeverPrinted(t *testing.T) {
	c := newClient(t, "https://gitea.example.com")
	tests := map[string]error{
		"deadline": fmt.Errorf("%w: proxy said %s", context.DeadlineExceeded, testToken),
		"canceled": fmt.Errorf("%w: proxy said %s", context.Canceled, testToken),
	}
	for name, cause := range tests {
		t.Run(name, func(t *testing.T) {
			err := c.WrapError(activity, cause)

			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				t.Error("the cause should be reachable with errors.Is")
			}
			var rendered strings.Builder
			diag.Render(&rendered, err)
			for how, text := range map[string]string{
				"Render": rendered.String(), "Sprint": fmt.Sprint(err), "%v": fmt.Sprintf("%v", err),
				"%+v": fmt.Sprintf("%+v", err), "%s": fmt.Sprintf("%s", err), //nolint:perfsprint // the verb is the point
			} {
				if strings.Contains(text, testToken) {
					t.Errorf("%s shows the token: %q", how, text)
				}
			}
		})
	}
}
