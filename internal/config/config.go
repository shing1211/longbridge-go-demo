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
	"github.com/longbridge/openapi-go/oauth"
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
	// It also blocks the RequireLive gates — a stale or unset value protects
	// against accidental writes.
	ModeSimulated Mode = "simulated"
	// ModePaper is for Longbridge's simulated/paper-trading account. It is
	// distinct from ModeSimulated because it satisfies the RequireLive gate:
	// a paper account cannot move real money, so the explicit assertion the
	// gate demands is satisfied by naming it. Set LONGPORT_MODE=paper when
	// running against simulated credentials and you want the write gates open.
	ModePaper Mode = "paper"
	// ModeLive requires an explicit opt-in and a non-dry-run flag.
	ModeLive Mode = "live"
)

// allowsWrites is the single allowlist for the RequireLive gates.
// It preserves default-deny: an empty or unrecognised value blocks.
func (m Mode) allowsWrites() bool { return m == ModeLive || m == ModePaper }

// Config is the validated demo configuration.
type Config struct {
	// SDK is the official SDK config, ready for quote/trade/market NewFromCfg.
	SDK *sdk.Config

	// AppKey, AppSecret and AccessToken are empty in a Config from LoadOAuth
	// except for the app key, which that loader carries for the startup banner
	// alone: it authenticates by OAuth client ID, and the SDK's own check()
	// returns early when OAuthClient is set. Nothing may treat the empty
	// secret or token there as a missing credential. See LoadOAuth.
	AppKey      string
	AppSecret   string
	AccessToken string

	// Mode is ModeSimulated, ModePaper, or ModeLive. Use ModePaper when
	// running against a simulated account and you want the RequireLive gates
	// open; ModeSimulated blocks them.
	Mode Mode
	// DryRun blocks all order writes when true. Defaults to true.
	DryRun bool

	// Headers are the extra HTTP headers applied to every request the SDK makes,
	// in the order they were resolved (file, then environment, then flag) and
	// sorted by name. Empty when none were configured, which is the usual case.
	// See header.go for the sources and the precedence between them.
	Headers []Header

	// Logger is the adapter handed to the SDK when the SDK log-level flag was
	// passed, and nil when it was not. A nil here means the SDK is still using
	// the logger it installs for itself, which writes to stderr and prints
	// error, warn and info with nothing set — see installLogger.
	Logger *SDKLogger

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

// Option is one caller-supplied setting that neither the environment nor a YAML
// file can express on its own — currently the extra HTTP headers and the SDK log
// level, both of which arrive from the shared command-line flag set.
//
// It is a variadic parameter on Load rather than a field of the callers, so that
// a command gets these by calling Load and there is no second way to load a
// configuration that has them.
type Option func(*loadOptions)

// loadOptions is the resolved set of caller-supplied settings. Kept unexported
// so a new one cannot be conjured by a command; it arrives through Option and
// nothing else.
type loadOptions struct {
	// headers are raw `-header NAME=VALUE` arguments, in the order given. They
	// are parsed and validated inside Load rather than at the flag layer, so
	// that the environment and file sources go through exactly the same rules.
	headers []string
	// logLevel is the SDK log level, or "" for "not requested".
	logLevel string
}

// WithHeaders supplies extra HTTP headers as `NAME=VALUE` arguments, one per
// header. It is the flag side of the same set that LONGPORT_HEADER_<NAME> and
// the YAML `headers:` block contribute to, and it has the highest precedence of
// the three.
//
// The arguments are validated here rather than where the flag is declared, on
// purpose: a rule that only the command line went through would be a rule with
// a hole in it, and the hole is exactly the interesting part — the credential
// headers. See ParseHeaderSpec and header.go.
func WithHeaders(specs ...string) Option {
	return func(o *loadOptions) { o.headers = append(o.headers, specs...) }
}

// WithLogLevel asks for the SDK's own logging to be routed to standard error
// through this package's adapter, at the named level. An empty name — the
// default, and what leaving the flag off means — changes nothing at all, so a
// run that did not ask for logs does not get this package's logger.
func WithLogLevel(name string) Option {
	return func(o *loadOptions) { o.logLevel = name }
}

// loadHeaders resolves the three header sources into the set to apply, in
// precedence order: the YAML file first, then the environment, then the flag.
//
// It is called after the credentials and the switches have been checked, which
// is where every other "this value cannot be used" error in this package is
// raised. A malformed header therefore reports itself as a plain error, exactly
// like a malformed LONGPORT_MODE, and never as a missing credential.
func loadHeaders(lo loadOptions, fileHeaders []Header) ([]Header, error) {
	envHeaders, err := headersFromEnv()
	if err != nil {
		return nil, err
	}
	var flagHeaders []Header
	for _, spec := range lo.headers {
		h, err := ParseHeaderSpec("-header", spec)
		if err != nil {
			return nil, err
		}
		flagHeaders = append(flagHeaders, h)
	}
	return resolveHeaders(fileHeaders, envHeaders, flagHeaders), nil
}

// applyHeaders installs the resolved set on the built SDK config.
//
// This is the only place the SDK's Config.WithHeader is called, which is the
// point: the extra-header feature has to exist for every command that loads a
// configuration, and the only way to guarantee that is to make it part of
// loading one. A per-command call would be sixteen chances to forget and would
// put the reserved-name check behind whatever the command felt like doing.
//
// VERIFIED in openapi-go v0.25.2 config/config.go: WithHeader lazily creates
// ExtraHeaders and stores the pair, so a run that passed no headers leaves the
// field nil and the SDK's own request loop skips the extra-header loop entirely.
func applyHeaders(sdkCfg *sdk.Config, headers []Header) {
	for _, h := range headers {
		sdkCfg.WithHeader(h.Key, h.Value)
	}
}

// installLogger routes the SDK's logging to standard error, but only if the
// operator asked for it.
//
// # Config.SetLogger IS PROCESS-WIDE, NOT PER-CONFIGURATION
//
// openapi-go v0.25.2 config/config.go, SetLogger, does two things:
//
//	func (c *Config) SetLogger(l log.Logger) {
//	    if l != nil {
//	        l.SetLevel(c.LogLevel)
//	        c.logger = l
//	        log.SetLogger(l)      // <- the package-global default
//	    }
//	}
//
// The second line replaces openapi-go/log's package-level defaultLogger, which
// is the logger every other part of the library reaches through the log.Info /
// log.Debug helpers — including the websocket protocol layer in
// quote/core.go and trade/core.go, which log from their read loops. So calling
// SetLogger on one *sdk.Config changes how the WHOLE process logs, and any
// other configuration in the same process now shares this logger and its
// threshold. Nothing in this repo builds a second configuration, so the hazard
// is contained, but it is the reason the call lives in one place behind one
// flag rather than being available to every command to sprinkle around.
//
// # WHY AN UNSET FLAG CALLS NOTHING AT ALL
//
// Leaving the SDK's own DefaultLogger in place is not a fallback, it is the
// better default: it writes through the standard library log package, which is
// stderr, and it needs no configuration to be correct.
//
// It is worth knowing what "no level set" means there, because it is not quiet.
// MEASURED with fake credentials and no request: the library filters with
// `severity >= threshold` and starts at threshold zero, which is *below* info —
// so with nothing set it prints error, warn and info and suppresses only debug.
// A -log-level of warn or error is therefore quieter than passing no flag at
// all, which is the intended effect rather than a surprise.
//
// Installing an adapter that behaved identically would change the output format
// of every run in the repo for no gain.
//
// The flag beats LONGBRIDGE_LOG_LEVEL, and the reason it takes a second
// SetLevel call is that SetLogger applies the SDK's own resolved value first —
// an explicit request on the command line should not be undone by a variable
// that was left in a .env from another session.
func installLogger(sdkCfg *sdk.Config, name string) (*SDKLogger, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	// Re-validated here even though the flag layer validates it too: Load is a
	// package API, and the SDK must never be handed a level this package has not
	// checked.
	level, err := ParseLogLevel(name)
	if err != nil {
		return nil, err
	}
	logger := NewSDKLogger(stderrLogger, level)
	sdkCfg.SetLogger(logger)
	logger.SetLevel(name)
	return logger, nil
}

// Load reads configuration and validates it.
//
// Precedence for each credential: env var, then YAML file (if one is found).
// When the file argument is "" the conventional file names config.yaml and
// config.local.yaml are tried in that order, and their absence is not an error.
// An explicit file argument must name a .yaml file: this loader parses YAML
// only, and the SDK's own TOML reader is not wired up here.
//
// The extra headers and the SDK log level are caller-supplied through Option.
// They are resolved, validated and applied here rather than by each command, so
// that a command cannot forget them and so that the reserved-name and masking
// rules apply identically whatever the source was.
//
// It returns a *MissingCredentialError listing every absent credential, or a
// plain error describing a bad value. It never panics and never returns nil.
func Load(file string, extra ...Option) (*Config, error) {
	lo := loadOptions{}
	for _, opt := range extra {
		opt(&lo)
	}

	vals := make(map[string]string, len(credSpecs)) // yamlKey -> value

	// Pass 1: env wins.
	for _, s := range credSpecs {
		if v := lookupEnv(s.envNames); v != "" {
			vals[s.yamlKey] = v
		}
	}

	// Pass 2: fill gaps from a YAML file. The file's `headers:` block is
	// collected at the same time, because it is the same read of the same file
	// and a second parse would exist only to avoid widening this signature.
	var fileHeaders []Header
	usedFile, err := loadFileInto(vals, &fileHeaders, file)
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
	// And for the reusable write guard (dca, alert, sharelist, content).
	// declaredGuards() is the single list, so a new gate is added in one place.
	if err := ValidateAllDryRunEnvs(declaredGuards()...); err != nil {
		return nil, err
	}
	// And for the headers, which have their own reserved names and their own
	// masking rules, from whichever of the three sources supplied them.
	headers, err := loadHeaders(lo, fileHeaders)
	if err != nil {
		return nil, err
	}

	sdkOpts := []sdk.Option{}
	if usedFile != "" {
		sdkOpts = append(sdkOpts, sdk.WithFilePath(usedFile))
	}
	// WithConfigKey is applied last so it overrides anything the file supplied,
	// which matches the "env wins" precedence above.
	sdkOpts = append(sdkOpts, sdk.WithConfigKey(
		vals["app_key"], vals["app_secret"], vals["access_token"],
	))

	// Remaining tunables are env-only; the SDK has no struct field setters.
	applyEnvOverrides()

	sdkCfg, err := sdk.New(sdkOpts...)
	if err != nil {
		return nil, fmt.Errorf("building SDK config: %w", err)
	}
	applyHeaders(sdkCfg, headers)
	logger, err := installLogger(sdkCfg, lo.logLevel)
	if err != nil {
		return nil, err
	}

	return &Config{
		SDK:         sdkCfg,
		AppKey:      vals["app_key"],
		AppSecret:   vals["app_secret"],
		AccessToken: vals["access_token"],
		Mode:        mode,
		DryRun:      dryRun,
		Headers:     headers,
		Logger:      logger,
		Source:      usedFile,
	}, nil
}

// LoadOAuth builds the demo configuration for a command that authenticates
// with OAuth 2.0 instead of the app-key triple, which is what cmd/auth needs.
//
// It is Load with one requirement removed, not a second opinion about the
// same one. Load demands all three credentials because every other binary
// signs its requests with them; the OAuth flow is authorised by the OAuth
// client ID alone, and the SDK's own config check returns early when
// OAuthClient is set (VERIFIED in openapi-go v0.25.2 config/config.go, check():
// `if c.OAuthClient != nil { return nil }`). Requiring the triple here would
// mean an operator could not obtain a token before creating an app-key
// credential, which is the opposite of the intended order.
//
// Everything else is deliberately identical to Load, so a command that
// authenticates this way still sees the same mode, the same dry-run state and
// the same startup banner as the rest of the repo — which is what lets it
// assert the read-only invariant against a genuinely loaded configuration
// rather than a hand-built stand-in. That includes validating the dry-run
// switches this command will never use: they are validated at startup by
// every binary, so that a bad value fails immediately instead of minutes later
// in an unrelated command.
//
// What is NOT loaded, and why:
//   - the app secret and the access token. Neither exists on the OAuth path;
//     asking for them here would report a missing credential that is not one.
//   - the client ID. It is not one of the SDK's YAML fields, so there is
//     nowhere in config.yaml to read it from; cmd/auth resolves it from the
//     flag and the environment instead.
//
// AppKey is still carried, purely so the banner reads the same as it does
// for every other command rather than reporting <unset> for a key the
// operator has in fact set. It signs nothing here: cmd/auth falls back to the
// app key as a source for the OAuth client ID, which is a different value
// wearing the same name.
func LoadOAuth(o *oauth.OAuth, file string, extra ...Option) (*Config, error) {
	// Named opt rather than o in the loop below: o is the OAuth client, and a
	// loop variable that shadows the parameter a reader came here for is a trap
	// in a function that takes three of them.
	lo := loadOptions{}
	for _, opt := range extra {
		opt(&lo)
	}
	vals := map[string]string{
		"app_key": lookupEnv([]string{"LONGBRIDGE_APP_KEY", "LONGPORT_APP_KEY"}),
	}
	// Same precedence as Load — env first, then the YAML file — so a key that
	// only exists in config.yaml still shows up in the banner.
	var fileHeaders []Header
	usedFile, err := loadFileInto(vals, &fileHeaders, file)
	if err != nil {
		return nil, err
	}

	mode, dryRun, err := loadModeAndDryRun()
	if err != nil {
		return nil, err
	}
	if err := ValidateWatchlistDryRun(); err != nil {
		return nil, err
	}
	if err := ValidateAllDryRunEnvs(declaredGuards()...); err != nil {
		return nil, err
	}
	// Headers and the SDK log level are resolved here exactly as in Load, and
	// for the same reason: an OAuth login is still a program that talks to the
	// API, and the reserved-name rule must not depend on how the command
	// authenticated.
	headers, err := loadHeaders(lo, fileHeaders)
	if err != nil {
		return nil, err
	}

	opts := []sdk.Option{sdk.WithOAuthClient(o)}
	if usedFile != "" {
		opts = append(opts, sdk.WithFilePath(usedFile))
	}
	applyEnvOverrides()

	sdkCfg, err := sdk.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("building SDK config: %w", err)
	}
	applyHeaders(sdkCfg, headers)
	logger, err := installLogger(sdkCfg, lo.logLevel)
	if err != nil {
		return nil, err
	}

	return &Config{
		SDK:     sdkCfg,
		AppKey:  vals["app_key"],
		Mode:    mode,
		DryRun:  dryRun,
		Headers: headers,
		Logger:  logger,
		Source:  usedFile,
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
//
// It does report the extra headers, because a header nobody expected is the
// first thing worth knowing when a request behaves oddly, and because a header
// that is going out on every request deserves to be visible in the one line
// every command prints before it does anything. Each one goes through
// RedactHeaderValue, so a name that looks credential-bearing is shown masked
// and any other name is shown in full; see that function for the exact rule.
func (c *Config) String() string {
	mode := string(c.Mode)
	if c.Mode == ModeSimulated {
		mode += "  (expected: credentials from a SIMULATED account)"
	} else if c.Mode == ModePaper {
		mode += "  (PAPER / simulated account — write gates are open)"
	}
	httpURL, quoteURL, tradeURL := effectiveEndpoints(c.SDK)
	var b strings.Builder
	fmt.Fprintf(&b,
		"mode=%s dry_run=%v app_key=%s http=%s quote_ws=%s trade_ws=%s",
		mode, c.DryRun, Redact(c.AppKey), httpURL, quoteURL, tradeURL)
	for _, h := range c.Headers {
		fmt.Fprintf(&b, " header[%s]=%s", h.Key, RedactHeaderValue(h.Key, h.Value))
		if h.Origin != "" {
			fmt.Fprintf(&b, "(from %s)", h.Origin)
		}
	}
	// Only when one was installed: the SDK's own logger is in place otherwise,
	// and saying "sdk_log=error" for a logger this package did not install
	// would be claiming something about behaviour it does not control.
	if c.Logger != nil {
		fmt.Fprintf(&b, " sdk_log=%s", c.Logger.LevelName())
	}
	return b.String()
}

// LoadModeAndDryRun exposes the safety switches for commands that parse flags
// before loading credentials.
func LoadModeAndDryRun() (Mode, bool, error) { return loadModeAndDryRun() }

func loadModeAndDryRun() (Mode, bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("LONGPORT_MODE")))
	mode := ModeSimulated
	switch raw {
	case "":
		mode = ModeSimulated
	case "simulated", "sim":
		mode = ModeSimulated
	case "paper":
		mode = ModePaper
	case "live":
		mode = ModeLive
	default:
		return "", false, fmt.Errorf(
			"invalid LONGPORT_MODE=%q: want \"simulated\", \"paper\" or \"live\"", raw)
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
//
// # WHY THE MODE TEST USES allowsWrites(), NOT `== ModeLive`
//
// The rule is "allow only exactly ModeLive or ModePaper", never "refuse only
// exactly ModeSimulated". The permissive form lets a Config whose Mode was never
// set (""), mistyped ("LIVE") or invented ("sandbox") place a real order, and
// it disagreed with WriteGuard.Unsatisfied in this same package, which has
// always used the fail-closed form. Load cannot currently produce such a Config,
// so the difference was latent rather than a live leak — but a latent hole in
// the one function that guards money is still a hole, so the deny-by-default
// form wins. Do not "simplify" this back to `c.Mode == ModeSimulated`.
func (c *Config) GuardWrite(action string) error {
	if action == "" {
		return errors.New("GuardWrite: action description is required")
	}
	if c.DryRun {
		return Blockedf(
			"refusing to %s: DRY RUN is active (mode=%s).\n"+
				"No order was sent. To actually submit orders you must set\n"+
				"  LONGPORT_DRY_RUN=0\n"+
				"and pass --confirm-live.\n"+
				"Both are required; either one alone still blocks the write.",
			action, c.Mode)
	}
	if !c.Mode.allowsWrites() {
		var hint string
		switch c.Mode {
		case ModeSimulated:
			hint = "set LONGPORT_MODE=paper to use a simulated account, or LONGPORT_MODE=live to assert these are real-money credentials"
		case Mode(""):
			hint = "set LONGPORT_MODE=paper or LONGPORT_MODE=live"
		default:
			hint = fmt.Sprintf("set LONGPORT_MODE=paper (simulated account) or LONGPORT_MODE=live (real-money account); %q is not recognised", c.Mode)
		}
		return Blockedf(
			"refusing to %s: mode is %q but dry run is disabled.\n"+
				"No order was sent. %s.",
			action, c.Mode, hint)
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
