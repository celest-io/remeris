package httpx

import (
	"testing"

	"github.com/celest-io/remeris/internal/testnet"
)

// TestMain keeps the tests off the network: only loopback (httptest) is reachable, and no token is inherited.
func TestMain(m *testing.M) { testnet.Main(m) }
