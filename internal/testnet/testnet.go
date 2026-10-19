// Package testnet keeps tests off the network. A test binary calls [Main] from its TestMain: afterwards every
// request that goes through http.DefaultTransport and is not for a loopback address fails, and no GitHub token
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
	Guard()
	os.Exit(m.Run())
}

// Guard replaces http.DefaultTransport with a transport that refuses any host that is not loopback, and removes
// GITHUB_TOKEN and GH_TOKEN from the environment. Clients read http.DefaultTransport when they are built, so the
// guard must be installed before any test runs. Calling it twice is harmless.
func Guard() {
	_ = os.Unsetenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GH_TOKEN")
	if _, ok := http.DefaultTransport.(*guard); ok {
		return
	}
	http.DefaultTransport = &guard{base: http.DefaultTransport}
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
