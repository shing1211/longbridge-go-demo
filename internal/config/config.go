// Package config loads Longbridge OpenAPI credentials and demo safety switches
// from environment variables or an optional YAML file, validates them, and
// produces a ready-to-use *config.Config for the official SDK.
//
// It deliberately owns three concerns the raw SDK does not:
//
//  1. Friendly diagnostics. The SDK aborts on the first missing credential.
//     This package collects *all* missing variables and reports them at once.
//  2. The simulated-vs-live switch (see Mode below). The Go SDK has no
//     "paper" flag at all; the distinction is entirely in which credentials you
//     use. This package makes that choice explicit and prints it loudly.
//  3. A dry-run gate for every order-writing path. Default is dry run.
package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/longbridge/openapi-go/config"
)

// Mode selects which Longbridge environment the credentials belong to.
//
// IMPORTANT / VERIFIED: the Go SDK (github.com/longbridge/openapi-go v0.25.2)
// has no paper-trading flag. Grepping the entire module for
// "paper", "simulat", "sandbox", "demo" returns zero hits in non-test Go
// source. The only environment selector the SDK exposes is
// LONGBRIDGE_ENV=staging, which merely points at *.longbridge.xyz hosts —
// it does NOT create a simulated account.
//
// Longbridge issues a *separate* App Key / App Secret / Access Token triple
// for a simulated account. Simulated and live are distinguished solely by
// which credentials you supply, not by a base URL. So this package cannot
// verify the switch; it can only record the operator's stated intent and
// enforce the safety gate. See Mode's doc comment and the README.
type Mode string

const (
	// ModeSimulated is the DEFAULT. Expects a simulated-account credential set.
	ModeSimulated Mode = "simulated"
	// ModeLive requires an explicit opt-in and a non-dry-run flag.
	ModeLive Mode = "live"
)

// Config is the validated demo configuration.
type Config struct {
	// SDK is the official SDK config, ready for quote/trade/market NewFromCfg.
	SDK *sdk.Config

	AppKey      string
	AppSecret   string
	AccessToken string

	// Mode is ModeSimulated or ModeLive.
	Mode Mode
	// DryRun blocks all order writes when true. Defaults to true.
	DryRun bool

	// Source describes where credentials came from, for logging.
	Source string
}

// MissingCredentialError reports every missing credential at once.
type MissingCredentialError struct {
	Missing []string
	Source  string
}

func (e *MissingCredentialError) Error() string {
	var b strings.Builder
	b.WriteString("missing Longbridge credentials")
	if e.Source != "" {
		fmt.Fprintf(&b, " (checked via %s)", e.Source)
	}
	fmt.Fprintf(&b, ":\n\n")
	for _, v := range e.Missing {
		fmt.Fprintf(&b, "  - %s\n", v)
	}
	b.WriteString("\nHow to fix:\n")
	b.WriteString("  1. Sign in at https://open.longbridge.com/ -> User Center ->\n")
	b.WriteString("     \"Application credential\" and copy App Key, App Secret and\n")
	b.WriteString("     Access Token. For a SIMULATED account use that account's own\n")
	b.WriteString("     credential triple, not the live one.\n")
	b.WriteString("  2. Export the variables (see .env.example), e.g.\n")
	b.WriteString("       export LONGBRIDGE_APP_KEY=<your app key>\n")
	b.WriteString("       export LONGBRIDGE_APP_SECRET=<your app secret>\n")
	b.WriteString("       export LONGBRIDGE_ACCESS_TOKEN=<your access token>\n")
	b.WriteString("  3. Or copy config.example.yaml to config.yaml and fill it in.\n")
	b.WriteString("\nDo NOT commit either file; both are in .gitignore.")
	return b.String()
}

// credSpec documents one credential for collection and for help output.
type credSpec struct {
	envNames  []string // in priority order
	yamlKey   string
	docEnv    string // the canonical name shown to the user
	secret    bool
	descr     string
	sourceDoc string
}

var credSpecs = []credSpec{
	{
		envNames: []string{"LONGBRIDGE_APP_KEY", "LONGPORT_APP_KEY"},
		yamlKey:  "app_key",
		docEnv:   "LONGBRIDGE_APP_KEY",
		descr:    "App Key from the Longbridge developer User Center",
	},
	{
		envNames:  []string{"LONGBRIDGE_APP_SECRET", "LONGPORT_APP_SECRET"},
		yamlKey:   "app_secret",
		docEnv:    "LONGBRIDGE_APP_SECRET",
		secret:    true,
		descr:     "App Secret from the Longbridge developer User Center",
		sourceDoc: "same",
	},
	{
		envNames: []string{"LONGBRIDGE_ACCESS_TOKEN", "LONGPORT_ACCESS_TOKEN"},
		yamlKey:  "access_token",
		docEnv:   "LONGBRIDGE_ACCESS_TOKEN",
		secret:   true,
		descr:    "legacy Access Token from User Center (NOT the OAuth token)",
	},
}

// EnvDocLines renders the credential table used by -h output and the README.
func EnvDocLines() []string {
	lines := make([]string, 0, len(credSpecs))
	for _, s := range credSpecs {
		tag := ""
		if s.secret {
			tag = " (secret, never logged)"
		}
		lines = append(lines, fmt.Sprintf("  %-28s %s%s", s.docEnv, s.descr, tag))
	}
	return lines
}

// Load reads configuration and validates it.
//
// Precedence for each credential: env var, then YAML file (if one is found).
// When the file argument is "" the conventional file names config.yaml,
// config.local.yaml, config.yml and config.toml are tried in that order, and
// their absence is not an error.
//
// It returns a *MissingCredentialError listing every absent credential, or a
// plain error describing a bad value. It never panics and never returns nil.
func Load(file string) (*Config, error) {
	vals := make(map[string]string, len(credSpecs)) // yamlKey -> value
	yamlKeys := make([]string, 0, len(credSpecs))

	// Pass 1: env wins.
	for _, s := range credSpecs {
		if v := lookupEnv(s.envNames); v != "" {
			vals[s.yamlKey] = v
		}
		yamlKeys = append(yamlKeys, s.yamlKey)
	}

	// Pass 2: fill gaps from a YAML/TOML file.
	usedFile, err := loadFileInto(vals, yamlKeys, file)
	if err != nil {
		return nil, err
	}

	// Collect all missing credentials before erroring out.
	var missing []string
	for _, s := range credSpecs {
		if strings.TrimSpace(vals[s.yamlKey]) == "" {
			missing = append(missing, s.docEnv)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		src := "environment"
		if usedFile != "" {
			src = "environment and " + usedFile
		}
		return nil, &MissingCredentialError{Missing: missing, Source: src}
	}

	mode, dryRun, err := loadModeAndDryRun()
	if err != nil {
		return nil, err
	}
	// Same for the independent watchlist switch: fail at startup, not at the
	// moment a write is silently refused.
	if err := ValidateWatchlistDryRun(); err != nil {
		return nil, err
	}

	opts := []sdk.Option{}
	if usedFile != "" {
		opts = append(opts, sdk.WithFilePath(usedFile))
	}
	// WithConfigKey is applied last so it overrides anything the file supplied,
	// which matches the "env wins" precedence above.
	opts = append(opts, sdk.WithConfigKey(
		vals["app_key"], vals["app_secret"], vals["access_token"],
	))

	// Remaining tunables are env-only; the SDK has no struct field setters.
	applyEnvOverrides()

	sdkCfg, err := sdk.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("building SDK config: %w", err)
	}

	return &Config{
		SDK:         sdkCfg,
		AppKey:      vals["app_key"],
		AppSecret:   vals["app_secret"],
		AccessToken: vals["access_token"],
		Mode:        mode,
		DryRun:      dryRun,
		Source:      usedFile,
	}, nil
}

// Redact returns a safe-to-print form of a secret: at most 4 leading and 2
// trailing characters are kept, the middle is replaced by a fixed-length mask
// so that the string length itself does not leak the secret.
func Redact(s string) string {
	if s == "" {
		return "<unset>"
	}
	r := []rune(s)
	switch {
	case len(r) <= 6:
		return strings.Repeat("*", len(r))
	case len(r) <= 10:
		return string(r[:2]) + strings.Repeat("*", len(r)-2)
	default:
		return string(r[:4]) + strings.Repeat("*", 6) + string(r[len(r)-2:])
	}
}

// effectiveEndpoints returns the URLs the SDK will actually use. The SDK only
// populates these fields for the cn and staging regions; in the default
// region it leaves them empty and the underlying HTTP client substitutes the
// production hosts. Report the real value rather than an empty string.
func effectiveEndpoints(c *sdk.Config) (httpURL, quoteURL, tradeURL string) {
	httpURL, quoteURL, tradeURL = c.HttpURL, c.QuoteUrl, c.TradeUrl
	if httpURL == "" {
		httpURL = "https://openapi.longbridge.com (SDK default)"
	}
	if quoteURL == "" {
		quoteURL = "wss://openapi-quote.longbridge.com/v2 (SDK default)"
	}
	if tradeURL == "" {
		tradeURL = "wss://openapi-trade.longbridge.com/v2 (SDK default)"
	}
	return
}

// String renders a log-safe summary. It never includes the app secret or the
// access token, not even redacted, because they are pure credentials with no
// diagnostic value.
func (c *Config) String() string {
	mode := string(c.Mode)
	if c.Mode == ModeSimulated {
		mode += "  (expected: credentials from a SIMULATED account)"
	}
	httpURL, quoteURL, tradeURL := effectiveEndpoints(c.SDK)
	return fmt.Sprintf(
		"mode=%s dry_run=%v app_key=%s http=%s quote_ws=%s trade_ws=%s",
		mode, c.DryRun, Redact(c.AppKey), httpURL, quoteURL, tradeURL,
	)
}

// LoadModeAndDryRun exposes the safety switches for commands that parse flags
// before loading credentials.
func LoadModeAndDryRun() (Mode, bool, error) { return loadModeAndDryRun() }

func loadModeAndDryRun() (Mode, bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("LONGPORT_MODE")))
	mode := ModeSimulated
	switch raw {
	case "", "simulated", "sim", "paper":
		mode = ModeSimulated
	case "live":
		mode = ModeLive
	default:
		return "", false, fmt.Errorf(
			"invalid LONGPORT_MODE=%q: want \"simulated\" (default) or \"live\"", raw)
	}

	// Dry run defaults to true. It is only disabled by an explicit, exact value.
	dryRun := true
	if v, ok := lookupEnvOk([]string{"LONGPORT_DRY_RUN"}); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return "", false, fmt.Errorf(
				"invalid LONGPORT_DRY_RUN=%q: want 1, true, 0 or false", v)
		}
		dryRun = b
	}

	// Live + dry run is a legitimate combination: it means "these are real
	// credentials, but I still do not want anything sent". GuardWrite will
	// refuse the write, and say why.
	return mode, dryRun, nil
}

// applyEnvOverrides maps demo-level env vars onto SDK process env so the SDK's
// own env loader picks them up. The SDK reads these at config.New time, and
// WithConfigKey is applied after, so credential precedence is unaffected.
func applyEnvOverrides() {
	for env, dest := range map[string]string{
		"LONGBRIDGE_HTTP_URL":       "LONGBRIDGE_HTTP_URL",
		"LONGBRIDGE_QUOTE_URL":      "LONGBRIDGE_QUOTE_URL",
		"LONGBRIDGE_TRADE_URL":      "LONGBRIDGE_TRADE_URL",
		"LONGBRIDGE_LANGUAGE":       "LONGPORT_LANGUAGE",
		"LONGPORT_REGION":           "LONGPORT_REGION",
		"LONGPORT_ENABLE_OVERNIGHT": "LONGPORT_ENABLE_OVERNIGHT",
		"LONGBRIDGE_LOG_LEVEL":      "LONGBRIDGE_LOG_LEVEL",
		"LONGBRIDGE_TIMEOUT":        "LONGBRIDGE_TIMEOUT",
		"LONGBRIDGE_AUTH_TIMEOUT":   "LONGBRIDGE_AUTH_TIMEOUT",
		"LONGBRIDGE_HTTP_TIMEOUT":   "LONGBRIDGE_HTTP_TIMEOUT",
	} {
		if v, ok := os.LookupEnv(env); ok && v != "" {
			_ = os.Setenv(dest, v)
		}
	}
	if v, ok := os.LookupEnv("LONGBRIDGE_ENV"); ok {
		_ = os.Setenv("LONGBRIDGE_ENV", v)
	}
}

// Timeout returns a sane HTTP timeout for demo commands.
func Timeout() time.Duration {
	if v, ok := os.LookupEnv("LONGBRIDGE_HTTP_TIMEOUT"); ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 15 * time.Second
}

// GuardWrite is the hard safety gate for every order-writing call.
//
// It must be consulted immediately before SubmitOrder, ReplaceOrder and
// CancelOrder. It returns nil only when the operator has disabled dry run.
// The caller must treat any error from this function as "do not call the SDK".
func (c *Config) GuardWrite(action string) error {
	if action == "" {
		return errors.New("GuardWrite: action description is required")
	}
	if c.DryRun {
		return fmt.Errorf(
			"refusing to %s: DRY RUN is active (mode=%s).\n"+
				"No order was sent. To actually submit orders you must set\n"+
				"  LONGPORT_DRY_RUN=0\n"+
				"and pass --confirm-live.\n"+
				"Both are required; either one alone still blocks the write.",
			action, c.Mode)
	}
	if c.Mode == ModeSimulated {
		return fmt.Errorf(
			"refusing to %s: mode is \"simulated\" but dry run is disabled.\n"+
				"No order was sent. Either restore LONGPORT_DRY_RUN=1, or set\n"+
				"  LONGPORT_MODE=live\n"+
				"to state that these are real-money credentials.",
			action)
	}
	return nil
}

func lookupEnv(names []string) string {
	v, _ := lookupEnvOk(names)
	return v
}

func lookupEnvOk(names []string) (string, bool) {
	for _, n := range names {
		if v, ok := os.LookupEnv(n); ok {
			return v, true
		}
	}
	return "", false
}
