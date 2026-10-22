package testnet_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/testnet"
)

func TestMain(m *testing.M) { testnet.Main(m) }

func TestGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL) //nolint:noctx // a loopback request must pass
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	for _, u := range []string{"https://api.github.com/", "http://192.0.2.1/", "http://[2001:db8::1]/"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatalf("%s was reached", u)
		}
		if !strings.Contains(err.Error(), "tests must use httptest") {
			t.Errorf("err = %v", err)
		}
	}
}

// No forge token of the developer's environment reaches a test.
func TestGuardRemovesTokens(t *testing.T) {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN", "GITEA_TOKEN"} {
		if v := os.Getenv(name); v != "" {
			t.Errorf("%s is set", name)
		}
	}
}

// The guard gives the tests an empty home, so no test reads the user's stored tokens.
func TestGuardIsolatesHome(t *testing.T) {
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
		if v := os.Getenv(name); v == "" || !strings.Contains(v, "remeris-test-home-") {
			t.Errorf("%s = %q, want the test home", name, v)
		}
	}
}
