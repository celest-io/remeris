// Package testnet keeps tests off the network. A test binary calls [Main] from its TestMain: afterwards every
// request that goes through http.DefaultTransport and is not for a loopback address fails, and no GitHub or Gitea token
// is left in the environment. Tests talk to httptest servers, which listen on loopback.
//
// Only tests import this package.
package testnet

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Main installs the guard, runs the tests and exits. Use it as the body of TestMain.
func Main(m *testing.M) {
	home := Guard()
	code := m.Run()
	if home != "" {
		_ = os.RemoveAll(home)
	}
	os.Exit(code)
}

// Guard replaces http.DefaultTransport with a transport that refuses any host that is not loopback, removes
// GITHUB_TOKEN and GH_TOKEN from the environment, and points HOME, XDG_CONFIG_HOME and APPDATA at a new empty
// directory, so that no test reads or writes the user's real config (stored tokens, the Knope credentials). It
// returns that directory, or "" when the guard was already installed. Clients read http.DefaultTransport when
// they are built, so the guard must be installed before any test runs. Calling it twice is harmless.
func Guard() string {
	if !GuardNetwork() {
		return ""
	}
	home, err := os.MkdirTemp("", "remeris-test-home-")
	if err != nil {
		panic("testnet: cannot create a test home: " + err.Error())
	}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
		_ = os.Setenv(name, home)
	}
	return home
}

// GuardNetwork is Guard without the new home directory: it installs the transport and removes the GitHub and Gitea tokens.
// It is for the binary that testscript runs, whose scripts set their own HOME. It reports whether it installed the
// transport (false when it was already there).
func GuardNetwork() bool {
	_ = os.Unsetenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GH_TOKEN")
	_ = os.Unsetenv("GITEA_TOKEN")
	if _, ok := http.DefaultTransport.(*guard); ok {
		return false
	}
	http.DefaultTransport = &guard{base: http.DefaultTransport}
	return true
}

type guard struct{ base http.RoundTripper }

// RoundTrip fails unless the request is for a loopback host.
func (g *guard) RoundTrip(r *http.Request) (*http.Response, error) {
	if !loopback(r.URL.Hostname()) {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, fmt.Errorf("test tried to reach %s: tests must use httptest", r.URL.Host)
	}
	return g.base.RoundTrip(r)
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
