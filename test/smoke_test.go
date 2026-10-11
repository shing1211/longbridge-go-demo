package test

import (
	"strings"
	"testing"

	"github.com/longbridge/openapi-go/market"
	"github.com/longbridge/openapi-go/quote"
	"github.com/longbridge/openapi-go/trade"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// TestSmokeWiring constructs an SDK configuration from dummy credentials and
// creates one client per context family. It catches a dependency upgrade that
// silently breaks the wiring between config.Load and the SDK constructors —
// the same class of break that a protobuf or websocket transport migration
// would introduce.
//
// quote and trade constructors make a live OTP round-trip during NewFromCfg.
// With dummy credentials that round-trip returns a 401; this test treats an
// auth rejection as success (the wiring reached the network correctly) and
// fails on anything else. market.NewFromCfg makes no network call and must
// succeed outright.
//
// It deliberately uses dummy credentials rather than real ones: the test must
// pass in CI with no secrets configured.
func TestSmokeWiring(t *testing.T) {
	t.Setenv("LONGBRIDGE_APP_KEY", "smoke-test-key")
	t.Setenv("LONGBRIDGE_APP_SECRET", "smoke-test-secret")
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "smoke-test-token")

	cfg, err := appcfg.Load("")
	if err != nil {
		t.Fatalf("config.Load with dummy credentials: %v", err)
	}
	if cfg.SDK == nil {
		t.Fatal("config.Load returned a nil SDK config; the demo cannot construct any client")
	}

	t.Run("quote", func(t *testing.T) {
		qc, err := quote.NewFromCfg(cfg.SDK)
		if err != nil {
			if isAuthRejection(err) {
				t.Logf("quote.NewFromCfg returned auth error (expected with dummy creds): %v", err)
				return
			}
			t.Fatalf("quote.NewFromCfg: %v", err)
		}
		if qc == nil {
			t.Fatal("quote.NewFromCfg returned a nil context")
		}
		if err := qc.Close(); err != nil {
			t.Fatalf("closing quote context: %v", err)
		}
	})

	t.Run("trade", func(t *testing.T) {
		tc, err := trade.NewFromCfg(cfg.SDK)
		if err != nil {
			if isAuthRejection(err) {
				t.Logf("trade.NewFromCfg returned auth error (expected with dummy creds): %v", err)
				return
			}
			t.Fatalf("trade.NewFromCfg: %v", err)
		}
		if tc == nil {
			t.Fatal("trade.NewFromCfg returned a nil context")
		}
		if err := tc.Close(); err != nil {
			t.Fatalf("closing trade context: %v", err)
		}
	})

	t.Run("market", func(t *testing.T) {
		mc, err := market.NewFromCfg(cfg.SDK)
		if err != nil {
			t.Fatalf("market.NewFromCfg: %v", err)
		}
		if mc == nil {
			t.Fatal("market.NewFromCfg returned a nil context")
		}
	})
}

// isAuthRejection reports whether err is an HTTP 401 from the Longbridge
// gateway, which is the expected outcome when dummy credentials are used.
func isAuthRejection(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "401") || strings.Contains(msg, "token invalid")
}
