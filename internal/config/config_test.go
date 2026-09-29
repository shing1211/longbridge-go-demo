package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/longbridge/openapi-go/config"
	"github.com/longbridge/openapi-go/oauth"
)

// ---------------------------------------------------------------- helpers --

// longbridgeEnv is every variable this package or the SDK reads. Clearing all
// of them at the start of a test makes the suite independent of the
// developer's shell, of a stray .env picked up by the SDK's godotenv autoload,
// and of anything a previous test's applyEnvOverrides left behind.
var longbridgeEnv = []string{
	// Credentials, canonical and deprecated spellings.
	"LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET", "LONGBRIDGE_ACCESS_TOKEN",
	"LONGPORT_APP_KEY", "LONGPORT_APP_SECRET", "LONGPORT_ACCESS_TOKEN",
	// Demo safety switches: one per declared gate, plus the order gate's pair.
	"LONGPORT_MODE", "LONGPORT_DRY_RUN",
	"LONGPORT_WATCHLIST_DRY_RUN", "LONGPORT_DCA_DRY_RUN", "LONGPORT_ALERT_DRY_RUN",
	"LONGPORT_SHARELIST_DRY_RUN", "LONGPORT_CONTENT_DRY_RUN",
	// Values applyEnvOverrides copies between the two spellings.
	"LONGPORT_REGION", "LONGPORT_ENABLE_OVERNIGHT", "LONGPORT_LANGUAGE",
	"LONGBRIDGE_LANGUAGE", "LONGBRIDGE_LOG_LEVEL", "LONGPORT_LOG_LEVEL",
	"LONGBRIDGE_ENV",
	// Endpoints and timeouts, both spellings.
	"LONGBRIDGE_HTTP_URL", "LONGBRIDGE_QUOTE_URL", "LONGBRIDGE_TRADE_URL",
	"LONGPORT_HTTP_URL", "LONGPORT_QUOTE_URL", "LONGPORT_TRADE_URL",
	"LONGBRIDGE_TIMEOUT", "LONGPORT_TIMEOUT",
	"LONGBRIDGE_AUTH_TIMEOUT", "LONGPORT_AUTH_TIMEOUT",
	"LONGBRIDGE_HTTP_TIMEOUT", "LONGPORT_HTTP_TIMEOUT",
	"LONGPORT_WRITE_QUEUE_SIZE", "LONGPORT_READ_QUEUE_SIZE",
	"LONGPORT_READ_BUFFER_SIZE", "LONGPORT_MIN_GZIP_SIZE",
}

// sandbox gives a test a private working directory with an empty Longbridge
// environment, and guarantees the environment is put back afterwards.
//
// It exists because two separate leaks reach these tests:
//   - Load and loadFileInto read the process working directory, so a developer's
//     real config.yaml would otherwise be picked up.
//   - applyEnvOverrides calls os.Setenv directly, so its writes outlive
//     t.Setenv's cleanup unless something restores them.
//
// t.Setenv and t.Chdir both refuse to run in parallel tests, so no test in
// this package may call t.Parallel().
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	restoreEnv(t, longbridgeEnv...)
	for _, k := range longbridgeEnv {
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("clearing %s: %v", k, err)
		}
	}
	return dir
}

// restoreEnv snapshots keys and puts their values back when the test ends.
// t.Setenv is not enough here: it only restores the variables the test itself
// set, not the ones production code set as a side effect.
func restoreEnv(t *testing.T, keys ...string) {
	t.Helper()
	type saved struct {
		key   string
		value string
		set   bool
	}
	snap := make([]saved, 0, len(keys))
	for _, k := range keys {
		key := k
		v, ok := os.LookupEnv(key)
		snap = append(snap, saved{key, v, ok})
	}
	t.Cleanup(func() {
		for _, s := range snap {
			if s.set {
				os.Setenv(s.key, s.value)
			} else {
				os.Unsetenv(s.key)
			}
		}
	})
}

// unsetEnv removes variables entirely. t.Setenv(k, "") is NOT the same thing:
// the guards use os.LookupEnv and treat "unset" and "set to empty" as
// different inputs to Validate*.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		key := k
		if old, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { os.Setenv(key, old) })
		} else {
			t.Cleanup(func() { os.Unsetenv(key) })
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetting %s: %v", key, err)
		}
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return name
}

func newTestConfig(mode Mode, dryRun bool) *Config {
	return &Config{SDK: &sdk.Config{}, Mode: mode, DryRun: dryRun}
}

// setCredentials fills the canonical credential variables.
func setCredentials(t *testing.T, key, secret, token string) {
	t.Helper()
	t.Setenv("LONGBRIDGE_APP_KEY", key)
	t.Setenv("LONGBRIDGE_APP_SECRET", secret)
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", token)
}

func requireMissingCredentials(t *testing.T, err error, want ...string) *MissingCredentialError {
	t.Helper()
	if err == nil {
		t.Fatal("want a *MissingCredentialError, got nil")
	}
	var mce *MissingCredentialError
	if !asMissingCredentialError(err, &mce) {
		t.Fatalf("want *MissingCredentialError, got %T: %v", err, err)
	}
	if len(mce.Missing) != len(want) {
		t.Fatalf("Missing = %q, want %q", mce.Missing, want)
	}
	for i := range want {
		if mce.Missing[i] != want[i] {
			t.Fatalf("Missing[%d] = %q, want %q (full list %q)", i, mce.Missing[i], want[i], mce.Missing)
		}
	}
	return mce
}

func asMissingCredentialError(err error, target **MissingCredentialError) bool {
	for e := err; e != nil; {
		if m, ok := e.(*MissingCredentialError); ok {
			*target = m
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// ------------------------------------------------------------------ Redact --

func TestRedact(t *testing.T) {
	// Three distinct shapes, each with its own rule:
	//   ""            -> the literal marker
	//   1..6 runes    -> all stars, so nothing is echoed
	//   7..10 runes   -> 2 leading characters survive
	//   >10 runes     -> 4 leading + 6 stars + 2 trailing
	tests := []struct {
		name  string
		in    string
		want  string
		runes int // -1 means "no fixed expectation"
	}{
		{name: "empty becomes the unset marker", in: "", want: "<unset>", runes: -1},
		{name: "one rune is fully masked", in: "a", want: "*", runes: 1},
		{name: "six runes are fully masked", in: "abcdef", want: "******", runes: 6},
		{name: "seven runes keep two", in: "abcdefg", want: "ab*****", runes: 7},
		{name: "ten runes keep two", in: "abcdefghij", want: "ab********", runes: 10},
		{name: "eleven runes switch to the wide form", in: "abcdefghijk", want: "abcd******jk", runes: 12},
		{name: "forty runes use the same twelve-rune form", in: strings.Repeat("x", 40), want: "xxxx******xx", runes: 12},
		{name: "multibyte runes are counted, not bytes", in: "中文密码", want: "****", runes: 4},
		{name: "multibyte long secret", in: "密钥密钥密钥密钥密钥密钥", want: "密钥密钥******密钥", runes: 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.in)
			if got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if tt.runes >= 0 && len([]rune(got)) != tt.runes {
				t.Fatalf("Redact(%q) has %d runes, want %d", tt.in, len([]rune(got)), tt.runes)
			}
		})
	}
}

func TestRedact_DoesNotLeakLengthForLongSecrets(t *testing.T) {
	// The whole point of the fixed six-star mask: two secrets of very
	// different lengths must be indistinguishable once redacted.
	short := Redact(strings.Repeat("a", 11))
	long := Redact(strings.Repeat("a", 512))
	if short != long {
		t.Fatalf("redaction leaks length: %q vs %q", short, long)
	}
	if len([]rune(short)) != 12 {
		t.Fatalf("wide form should be 12 runes, got %d", len([]rune(short)))
	}
}

func TestRedact_NeverEchoesTheWholeShortSecret(t *testing.T) {
	// Below the wide threshold the output length equals the input length, so
	// length is visible for secrets of 1-10 runes. What must never happen is
	// the whole value coming back.
	for n := 1; n <= 10; n++ {
		in := strings.Repeat("s", n)
		got := Redact(in)
		if got == in {
			t.Fatalf("Redact(%q) returned the secret unchanged", in)
		}
		if n <= 6 && strings.Trim(got, "*") != "" {
			t.Fatalf("Redact(%q) = %q; a short secret must be fully masked", in, got)
		}
	}
}

// ------------------------------------------------------------- Config.String --

func TestConfigString_NeverContainsTheSecretOrToken(t *testing.T) {
	const (
		key    = "appkey-ABCDEFGHIJKLMNOP"
		secret = "appsecret-DO-NOT-LOG-THIS"
		token  = "token-DO-NOT-LOG-EITHER"
	)
	for _, tc := range []struct {
		name   string
		mode   Mode
		dryRun bool
	}{
		{"simulated", ModeSimulated, true},
		{"live", ModeLive, false},
		{"live with dry run still on", ModeLive, true},
		{"empty mode", Mode(""), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(tc.mode, tc.dryRun)
			cfg.AppKey, cfg.AppSecret, cfg.AccessToken = key, secret, token
			msg := cfg.String()

			for _, leak := range []string{secret, token, key} {
				if strings.Contains(msg, leak) {
					t.Fatalf("String() leaked %q:\n%s", leak, msg)
				}
			}
			// The key is not a credential in the same sense, but it is still
			// redacted rather than printed.
			if !strings.Contains(msg, Redact(key)) {
				t.Errorf("String() should show the redacted app key:\n%s", msg)
			}
		})
	}
}

func TestConfigString_IncludesTheSafeFields(t *testing.T) {
	cfg := newTestConfig(ModeSimulated, true)
	cfg.AppKey = "abcdefghijklmnop"
	msg := cfg.String()
	for _, want := range []string{
		"mode=simulated",
		"(expected: credentials from a SIMULATED account)",
		"dry_run=true",
		"app_key=abcd******op",
		"http=",
		"quote_ws=",
		"trade_ws=",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("String() is missing %q\n---\n%s", want, msg)
		}
	}

	live := newTestConfig(ModeLive, false)
	live.AppKey = "abcdefghijklmnop"
	if msg := live.String(); strings.Contains(msg, "SIMULATED") {
		t.Errorf("live mode must not print the simulated warning:\n%s", msg)
	}
}

func TestConfigString_EndpointFallbacks(t *testing.T) {
	// The SDK leaves the URLs empty in the default region and lets the HTTP
	// client substitute production hosts; String() must report the real value
	// rather than an empty string.
	t.Run("empty SDK fields get the production defaults", func(t *testing.T) {
		cfg := newTestConfig(ModeSimulated, true)
		msg := cfg.String()
		for _, want := range []string{
			"https://openapi.longbridge.com (SDK default)",
			"wss://openapi-quote.longbridge.com/v2 (SDK default)",
			"wss://openapi-trade.longbridge.com/v2 (SDK default)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("missing %q\n---\n%s", want, msg)
			}
		}
	})

	t.Run("SDK fields are reported verbatim", func(t *testing.T) {
		cfg := newTestConfig(ModeSimulated, true)
		cfg.SDK.HttpURL = "https://openapi.longbridge.cn"
		cfg.SDK.QuoteUrl = "wss://openapi-quote.longbridge.cn"
		cfg.SDK.TradeUrl = "wss://openapi-trade.longbridge.cn"
		msg := cfg.String()
		if strings.Contains(msg, "SDK default") {
			t.Errorf("no field is empty, so no placeholder should appear:\n%s", msg)
		}
		for _, want := range []string{"openapi.longbridge.cn", "openapi-quote.longbridge.cn", "openapi-trade.longbridge.cn"} {
			if !strings.Contains(msg, want) {
				t.Errorf("missing %q\n---\n%s", want, msg)
			}
		}
	})
}

// ------------------------------------------------- MissingCredentialError --

func TestMissingCredentialError_ListsEveryMissingVariable(t *testing.T) {
	tests := []struct {
		name    string
		missing []string
		source  string
	}{
		{
			name:    "all three",
			missing: []string{"LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET"},
			source:  "environment",
		},
		{name: "just the key", missing: []string{"LONGBRIDGE_APP_KEY"}, source: "environment"},
		{
			name:    "a file was also consulted",
			missing: []string{"LONGBRIDGE_APP_SECRET", "LONGBRIDGE_ACCESS_TOKEN"},
			source:  "environment and config.yaml",
		},
		{name: "no source is omitted from the message", missing: []string{"LONGBRIDGE_APP_KEY"}, source: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := (&MissingCredentialError{Missing: tt.missing, Source: tt.source}).Error()

			if !strings.HasPrefix(msg, "missing Longbridge credentials") {
				t.Errorf("message must open with the summary:\n%s", msg)
			}
			if tt.source != "" && !strings.Contains(msg, "checked via "+tt.source) {
				t.Errorf("message must name the source %q:\n%s", tt.source, msg)
			}
			if tt.source == "" && strings.Contains(msg, "checked via") {
				t.Errorf("an empty source must be omitted, not printed as empty:\n%s", msg)
			}
			// Every missing variable is listed, not just the first: the whole
			// point of the type is one round trip instead of three.
			for _, v := range tt.missing {
				if !strings.Contains(msg, "  - "+v+"\n") {
					t.Errorf("message must list %q as a bullet:\n%s", v, msg)
				}
			}
			for _, want := range []string{"How to fix:", "LONGBRIDGE_APP_KEY=", "config.example.yaml", ".gitignore"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message is missing the fix text %q:\n%s", want, msg)
				}
			}
		})
	}
}

func TestMissingCredentialError_IsAnError(t *testing.T) {
	var err error = &MissingCredentialError{Missing: []string{"LONGBRIDGE_APP_KEY"}}
	if err.Error() == "" {
		t.Fatal("want a non-empty message")
	}
	// It must not masquerade as a guard refusal: a missing credential is
	// exit status 2, not the exit-3 "declined" status.
	if strings.Contains(err.Error(), "blocked by safety guard") {
		t.Error("a missing credential is not a guard refusal")
	}
}

// ------------------------------------------------------------- EnvDocLines --

func TestEnvDocLines(t *testing.T) {
	lines := EnvDocLines()
	if len(lines) != len(credSpecs) {
		t.Fatalf("got %d lines, want one per credential (%d)", len(lines), len(credSpecs))
	}
	wantNames := []string{"LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET", "LONGBRIDGE_ACCESS_TOKEN"}
	for i, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("line %d must be indented for -h output: %q", i, line)
		}
		if !strings.Contains(line, wantNames[i]) {
			t.Errorf("line %d = %q, want it to name %q", i, line, wantNames[i])
		}
		// The name column is 28 wide, so the description always starts at the
		// same offset; that alignment is the reason the format verb is there.
		if len([]rune(line)) < 31 {
			t.Errorf("line %d is too short to be padded: %q", i, line)
		} else if desc := line[31:]; strings.TrimSpace(desc) == "" {
			t.Errorf("line %d has no description after the padded name: %q", i, line)
		}
		// Deprecated spellings stay undocumented: help output shows one
		// canonical name per credential.
		if strings.Contains(line, "LONGPORT_") {
			t.Errorf("line %d must not advertise the deprecated name: %q", i, line)
		}
	}
	// Only the two pure secrets are tagged.
	if strings.Contains(lines[0], "never logged") {
		t.Errorf("the app key is not a secret and should not be tagged: %q", lines[0])
	}
	for _, i := range []int{1, 2} {
		if !strings.Contains(lines[i], "(secret, never logged)") {
			t.Errorf("line %d must be tagged as a secret: %q", i, lines[i])
		}
	}
}

func TestEnvDocLines_DoesNotChangeBetweenCalls(t *testing.T) {
	// It builds a fresh slice each time; a shared backing array would let one
	// caller's mutation corrupt another's help text.
	a, b := EnvDocLines(), EnvDocLines()
	a[0] = "clobbered"
	if b[0] == "clobbered" || EnvDocLines()[0] == "clobbered" {
		t.Fatal("EnvDocLines must return an independent slice on every call")
	}
}

// ------------------------------------------------------------------ Timeout --

func TestTimeout(t *testing.T) {
	const def = 15 * time.Second
	tests := []struct {
		name string
		set  bool
		val  string
		want time.Duration
	}{
		{name: "unset uses the default", set: false, want: def},
		{name: "a valid duration is honoured", set: true, val: "250ms", want: 250 * time.Millisecond},
		{name: "seconds are honoured", set: true, val: "3s", want: 3 * time.Second},
		{name: "garbage uses the default", set: true, val: "not-a-duration", want: def},
		{name: "a bare number is garbage", set: true, val: "30", want: def},
		{name: "an empty value uses the default", set: true, val: "", want: def},
		// Zero and negative parse fine but would mean "give up immediately" or
		// "fail instantly", so they are treated as no answer at all.
		{name: "zero uses the default", set: true, val: "0s", want: def},
		{name: "a bare zero uses the default", set: true, val: "0", want: def},
		{name: "a negative duration uses the default", set: true, val: "-5s", want: def},
		// Timeout is the one parser in the package that does not TrimSpace,
		// unlike DryRun(), WatchlistDryRun() and loadModeAndDryRun(). Asserted
		// as-is so the asymmetry stays visible.
		{name: "untrimmed value is garbage to the default", set: true, val: " 250ms ", want: def},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			if tt.set {
				t.Setenv("LONGBRIDGE_HTTP_TIMEOUT", tt.val)
			}
			if got := Timeout(); got != tt.want {
				t.Fatalf("Timeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ------------------------------------------------------- loadModeAndDryRun --

func TestLoadModeAndDryRun(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		modeSet  bool
		dryRun   string
		drySet   bool
		wantMode Mode
		wantDry  bool
		wantErr  string
	}{
		{name: "both unset means simulated and dry run", wantMode: ModeSimulated, wantDry: true},
		{name: "empty mode is the default", mode: "", modeSet: true, wantMode: ModeSimulated, wantDry: true},
		{name: "simulated", mode: "simulated", modeSet: true, wantMode: ModeSimulated, wantDry: true},
		{name: "sim is accepted", mode: "sim", modeSet: true, wantMode: ModeSimulated, wantDry: true},
		{name: "paper is accepted", mode: "paper", modeSet: true, wantMode: ModeSimulated, wantDry: true},
		{name: "mode is case insensitive", mode: "SiMuLaTeD", modeSet: true, wantMode: ModeSimulated, wantDry: true},
		{name: "mode is trimmed", mode: "  live  ", modeSet: true, wantMode: ModeLive, wantDry: true},
		{name: "live", mode: "live", modeSet: true, wantMode: ModeLive, wantDry: true},
		{name: "dry run on", dryRun: "1", drySet: true, wantMode: ModeSimulated, wantDry: true},
		{name: "dry run off", dryRun: "0", drySet: true, wantMode: ModeSimulated, wantDry: false},
		{name: "dry run false", dryRun: "false", drySet: true, wantMode: ModeSimulated, wantDry: false},
		{name: "dry run is trimmed", dryRun: " 0 ", drySet: true, wantMode: ModeSimulated, wantDry: false},
		// Live plus dry run is a legitimate state: real credentials, nothing
		// sent. GuardWrite refuses it and explains why.
		{name: "live with dry run on is allowed", mode: "live", modeSet: true, dryRun: "1", drySet: true, wantMode: ModeLive, wantDry: true},
		{name: "live with dry run off is the only live-write state", mode: "live", modeSet: true, dryRun: "0", drySet: true, wantMode: ModeLive, wantDry: false},
		{name: "unknown mode is rejected", mode: "sandbox", modeSet: true, wantErr: `invalid LONGPORT_MODE="sandbox"`},
		{name: "live with a typo is rejected", mode: "live!", modeSet: true, wantErr: `invalid LONGPORT_MODE="live!"`},
		{name: "demo is rejected", mode: "demo", modeSet: true, wantErr: `invalid LONGPORT_MODE="demo"`},
		// A dry-run value the user cannot have meant is a startup error, not a
		// silent "on": better to stop than to run with a switch nobody set.
		{name: "garbage dry run is rejected", dryRun: "yes", drySet: true, wantErr: `invalid LONGPORT_DRY_RUN="yes"`},
		{name: "empty dry run is rejected", dryRun: "", drySet: true, wantErr: `invalid LONGPORT_DRY_RUN=""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			if tt.modeSet {
				t.Setenv("LONGPORT_MODE", tt.mode)
			}
			if tt.drySet {
				t.Setenv("LONGPORT_DRY_RUN", tt.dryRun)
			}
			mode, dryRun, err := LoadModeAndDryRun()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want an error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				if mode != "" || dryRun != false {
					t.Errorf("a failed parse must return the zero value, got mode=%q dryRun=%v", mode, dryRun)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tt.wantMode || dryRun != tt.wantDry {
				t.Fatalf("got mode=%q dryRun=%v, want mode=%q dryRun=%v", mode, dryRun, tt.wantMode, tt.wantDry)
			}
		})
	}
}

func TestLoadModeAndDryRun_OnlyExactFalsyTurnsDryRunOff(t *testing.T) {
	for _, v := range boolishValues {
		t.Run(fmt.Sprintf("value=%q", v), func(t *testing.T) {
			sandbox(t)
			t.Setenv("LONGPORT_DRY_RUN", v)
			_, dryRun, err := LoadModeAndDryRun()
			if clearsDryRun(v) {
				if err != nil {
					t.Fatalf("%q must parse and disable dry run, got %v", v, err)
				}
				if dryRun {
					t.Fatalf("%q must disable dry run, got dryRun=true", v)
				}
				return
			}
			// Everything else is either an error or a safe "still on"; the one
			// outcome that must never occur is an unparseable value silently
			// disabling the dry run.
			if err == nil && !dryRun {
				t.Fatalf("SAFETY BUG: %q must not disable dry run", v)
			}
		})
	}
}

// --------------------------------------------------------------------- Load --

func TestLoad_AllCredentialsPresentFromTheEnvironment(t *testing.T) {
	sandbox(t)
	setCredentials(t, "key-1", "secret-1", "token-1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "key-1" || cfg.AppSecret != "secret-1" || cfg.AccessToken != "token-1" {
		t.Fatalf("credentials were not taken from the environment: %+v", cfg)
	}
	if cfg.Source != "" {
		t.Errorf("Source = %q, want empty when no file was used", cfg.Source)
	}
	if cfg.Mode != ModeSimulated {
		t.Errorf("Mode = %q, want the simulated default", cfg.Mode)
	}
	if !cfg.DryRun {
		t.Error("DryRun must default to true")
	}
	if cfg.SDK == nil {
		t.Fatal("SDK config must be built")
	}
	if cfg.SDK.AppKey != "key-1" || cfg.SDK.AppSecret != "secret-1" || cfg.SDK.AccessToken != "token-1" {
		t.Errorf("the SDK config must carry the same credentials: %+v", cfg.SDK)
	}
	if s := cfg.String(); strings.Contains(s, "secret-1") || strings.Contains(s, "token-1") {
		t.Errorf("String() leaked a credential:\n%s", s)
	}
}

func TestLoad_ReportsEveryMissingCredentialAtOnce(t *testing.T) {
	// Sorted output, all three listed: fixing credentials one run at a time is
	// the exact pain this package exists to remove.
	t.Run("all three missing", func(t *testing.T) {
		sandbox(t)
		_, err := Load("")
		requireMissingCredentials(t, err,
			"LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET")
	})
	t.Run("app key missing", func(t *testing.T) {
		sandbox(t)
		t.Setenv("LONGBRIDGE_APP_SECRET", "s")
		t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "t")
		_, err := Load("")
		requireMissingCredentials(t, err, "LONGBRIDGE_APP_KEY")
	})
	t.Run("app secret missing", func(t *testing.T) {
		sandbox(t)
		t.Setenv("LONGBRIDGE_APP_KEY", "k")
		t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "t")
		_, err := Load("")
		requireMissingCredentials(t, err, "LONGBRIDGE_APP_SECRET")
	})
	t.Run("access token missing", func(t *testing.T) {
		sandbox(t)
		t.Setenv("LONGBRIDGE_APP_KEY", "k")
		t.Setenv("LONGBRIDGE_APP_SECRET", "s")
		_, err := Load("")
		requireMissingCredentials(t, err, "LONGBRIDGE_ACCESS_TOKEN")
	})
	t.Run("two missing are both named", func(t *testing.T) {
		sandbox(t)
		t.Setenv("LONGBRIDGE_APP_KEY", "k")
		_, err := Load("")
		requireMissingCredentials(t, err, "LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_SECRET")
	})
	t.Run("a whitespace-only value counts as missing", func(t *testing.T) {
		sandbox(t)
		t.Setenv("LONGBRIDGE_APP_KEY", "k")
		t.Setenv("LONGBRIDGE_APP_SECRET", "   \t ")
		t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "t")
		_, err := Load("")
		requireMissingCredentials(t, err, "LONGBRIDGE_APP_SECRET")
	})
}

func TestLoad_DeprecatedLongportNamesStillWork(t *testing.T) {
	sandbox(t)
	t.Setenv("LONGPORT_APP_KEY", "old-key")
	t.Setenv("LONGPORT_APP_SECRET", "old-secret")
	t.Setenv("LONGPORT_ACCESS_TOKEN", "old-token")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("the deprecated LONGPORT_* spellings must still load: %v", err)
	}
	if cfg.AppKey != "old-key" || cfg.AppSecret != "old-secret" || cfg.AccessToken != "old-token" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestLoad_CanonicalNameBeatsTheDeprecatedOne(t *testing.T) {
	sandbox(t)
	t.Setenv("LONGBRIDGE_APP_KEY", "new-key")
	t.Setenv("LONGPORT_APP_KEY", "old-key")
	t.Setenv("LONGBRIDGE_APP_SECRET", "s")
	t.Setenv("LONGPORT_APP_SECRET", "old-secret")
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "t")
	t.Setenv("LONGPORT_ACCESS_TOKEN", "old-token")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "new-key" || cfg.AppSecret != "s" || cfg.AccessToken != "t" {
		t.Fatalf("LONGBRIDGE_* must win: %+v", cfg)
	}
}

func TestLoad_ModeAndDryRun(t *testing.T) {
	sandbox(t)
	setCredentials(t, "k", "s", "t")
	t.Setenv("LONGPORT_MODE", "live")
	t.Setenv("LONGPORT_DRY_RUN", "0")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeLive {
		t.Errorf("Mode = %q, want live", cfg.Mode)
	}
	if cfg.DryRun {
		t.Error("DryRun = true, want false")
	}
	// The only combination in which the order gate opens.
	if err := cfg.GuardWrite("submit buy 100 AAPL"); err != nil {
		t.Errorf("live + dry run off should allow the write, got %v", err)
	}
}

func TestLoad_BadSwitchesFailAtStartup(t *testing.T) {
	// Every switch is validated during Load, not at the moment a write is
	// refused, so a typo is found before anything has been attempted.
	tests := []struct {
		name    string
		env     string
		val     string
		wantErr string
	}{
		{"bad mode", "LONGPORT_MODE", "sandbox", `invalid LONGPORT_MODE="sandbox"`},
		{"bad dry run", "LONGPORT_DRY_RUN", "maybe", `invalid LONGPORT_DRY_RUN="maybe"`},
		{"empty dry run", "LONGPORT_DRY_RUN", "", `invalid LONGPORT_DRY_RUN=""`},
		{"bad watchlist dry run", "LONGPORT_WATCHLIST_DRY_RUN", "sure", `invalid LONGPORT_WATCHLIST_DRY_RUN="sure"`},
		{"empty watchlist dry run", "LONGPORT_WATCHLIST_DRY_RUN", "", `invalid LONGPORT_WATCHLIST_DRY_RUN=""`},
		{"bad dca dry run", "LONGPORT_DCA_DRY_RUN", "0 0", `invalid LONGPORT_DCA_DRY_RUN="0 0"`},
		{"empty dca dry run", "LONGPORT_DCA_DRY_RUN", "", `invalid LONGPORT_DCA_DRY_RUN=""`},
		{"bad alert dry run", "LONGPORT_ALERT_DRY_RUN", "nope", `invalid LONGPORT_ALERT_DRY_RUN="nope"`},
		{"bad sharelist dry run", "LONGPORT_SHARELIST_DRY_RUN", "0 0", `invalid LONGPORT_SHARELIST_DRY_RUN="0 0"`},
		{"empty sharelist dry run", "LONGPORT_SHARELIST_DRY_RUN", "", `invalid LONGPORT_SHARELIST_DRY_RUN=""`},
		{"bad content dry run", "LONGPORT_CONTENT_DRY_RUN", "maybe", `invalid LONGPORT_CONTENT_DRY_RUN="maybe"`},
		{"empty content dry run", "LONGPORT_CONTENT_DRY_RUN", "", `invalid LONGPORT_CONTENT_DRY_RUN=""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			setCredentials(t, "k", "s", "t")
			t.Setenv(tt.env, tt.val)

			cfg, err := Load("")
			if err == nil {
				t.Fatalf("want a startup error, got config %+v", cfg)
			}
			if cfg != nil {
				t.Errorf("no config may be returned alongside an error, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_ValidationOrderIsFixed(t *testing.T) {
	// The order is observable: an operator with several problems should be
	// told about them one at a time, in the order the code checks them.
	t.Run("credentials are checked before the switches", func(t *testing.T) {
		sandbox(t)
		// No credentials and a bad mode: the credential error wins, so the
		// user is told what to fix first.
		t.Setenv("LONGPORT_MODE", "sandbox")
		_, err := Load("")
		requireMissingCredentials(t, err, "LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET")
	})
	t.Run("mode is checked before the watchlist switch", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGPORT_MODE", "sandbox")
		t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "bad")
		_, err := Load("")
		if !strings.Contains(err.Error(), "LONGPORT_MODE") {
			t.Fatalf("want the mode error first, got %q", err)
		}
	})
	t.Run("the watchlist switch is checked before the write guards", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "bad")
		t.Setenv("LONGPORT_DCA_DRY_RUN", "bad")
		_, err := Load("")
		if !strings.Contains(err.Error(), "LONGPORT_WATCHLIST_DRY_RUN") {
			t.Fatalf("want the watchlist error first, got %q", err)
		}
	})
	t.Run("dca is checked before alert", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGPORT_DCA_DRY_RUN", "bad")
		t.Setenv("LONGPORT_ALERT_DRY_RUN", "bad")
		_, err := Load("")
		if !strings.Contains(err.Error(), "LONGPORT_DCA_DRY_RUN") {
			t.Fatalf("want the dca error first, got %q", err)
		}
		if strings.Contains(err.Error(), "LONGPORT_ALERT_DRY_RUN") {
			t.Fatalf("the alert error must not be reported yet, got %q", err)
		}
	})
	t.Run("sharelist is checked before content", func(t *testing.T) {
		// The two gates used to validate their own switches inside their own
		// command, so a bad value was found at different moments (and only in
		// that one command). Now every command validates the whole set during
		// Load, in this fixed order.
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGPORT_SHARELIST_DRY_RUN", "bad")
		t.Setenv("LONGPORT_CONTENT_DRY_RUN", "bad")
		_, err := Load("")
		if !strings.Contains(err.Error(), "LONGPORT_SHARELIST_DRY_RUN") {
			t.Fatalf("want the sharelist error first, got %q", err)
		}
		if strings.Contains(err.Error(), "LONGPORT_CONTENT_DRY_RUN") {
			t.Fatalf("the content error must not be reported yet, got %q", err)
		}
	})
}

func TestLoad_ValidSwitchesDoNotBlockStartup(t *testing.T) {
	sandbox(t)
	setCredentials(t, "k", "s", "t")
	for _, v := range []string{"0", "1", "true", "false", " 0 ", "TRUE"} {
		t.Run("value="+v, func(t *testing.T) {
			for _, g := range declaredGuards() {
				t.Setenv(g.DryRunEnv, v)
			}
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", v)
			if _, err := Load(""); err != nil {
				t.Fatalf("%q must be accepted, got %v", v, err)
			}
		})
	}
}

func TestLoad_RegionAndStagingSelectTheHosts(t *testing.T) {
	t.Run("the default region leaves the URLs to the SDK", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SDK.HttpURL != "" {
			t.Errorf("HttpURL = %q, want empty for the default region", cfg.SDK.HttpURL)
		}
		if !strings.Contains(cfg.String(), "openapi.longbridge.com (SDK default)") {
			t.Errorf("String() should fall back to the production hosts:\n%s", cfg.String())
		}
	})

	t.Run("the cn region gets the cn hosts", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGPORT_REGION", "cn")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SDK.HttpURL != "https://openapi.longbridge.cn" {
			t.Errorf("HttpURL = %q, want the cn host", cfg.SDK.HttpURL)
		}
		if strings.Contains(cfg.String(), "SDK default") {
			t.Errorf("String() must report the real hosts:\n%s", cfg.String())
		}
	})

	t.Run("staging wins over the region", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGPORT_REGION", "cn")
		t.Setenv("LONGBRIDGE_ENV", "staging")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SDK.HttpURL != "https://openapi.longbridge.xyz" {
			t.Errorf("HttpURL = %q, want the staging host", cfg.SDK.HttpURL)
		}
		// The doc comment is emphatic that staging is NOT a paper account.
		if !strings.Contains(cfg.String(), "SIMULATED") {
			t.Errorf("staging must not be presented as a simulated account:\n%s", cfg.String())
		}
	})

	t.Run("applyEnvOverrides maps the demo names onto the SDK names", func(t *testing.T) {
		sandbox(t)
		setCredentials(t, "k", "s", "t")
		t.Setenv("LONGBRIDGE_LANGUAGE", "en-US")
		t.Setenv("LONGPORT_ENABLE_OVERNIGHT", "true")
		t.Setenv("LONGBRIDGE_LOG_LEVEL", "debug")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := fmt.Sprint(cfg.SDK.Language); got != "en-US" {
			t.Errorf("Language = %q, want en-US", got)
		}
		if !cfg.SDK.EnableOvernight {
			t.Error("EnableOvernight should have been copied from LONGPORT_ENABLE_OVERNIGHT")
		}
		if cfg.SDK.LogLevel != "debug" {
			t.Errorf("LogLevel = %q, want debug", cfg.SDK.LogLevel)
		}
	})
}

func TestLoad_NeverTouchesTheNetwork(t *testing.T) {
	// Nothing here can prove the absence of a call, but it can pin that Load
	// completes with no credentials-shaped network requirement: it only builds
	// an *sdk.Config, it does not create a client connection.
	sandbox(t)
	setCredentials(t, "k", "s", "t")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SDK.Client != nil {
		t.Error("Load must not install an HTTP client; that is the caller's job")
	}
	if cfg.SDK.OAuthClient != nil {
		t.Error("Load must not build an OAuth client")
	}
}

// ---------------------------------------------------------------- LoadOAuth --

// The premise of LoadOAuth: an operator must be able to obtain an OAuth token
// BEFORE they have an app-key credential, because the OAuth client ID is
// issued on the same User Center page and not from the same generated triple.
// Requiring the triple here would invert that order for no reason — the SDK's
// own check() returns early once OAuthClient is set.
func TestLoadOAuth_NeedsNoAppKeyCredentialTriple(t *testing.T) {
	sandbox(t)
	o := oauth.New("client-id")

	cfg, err := LoadOAuth(o, "")
	if err != nil {
		t.Fatalf("LoadOAuth with no credentials at all = %v, want it to succeed", err)
	}
	if cfg.SDK == nil {
		t.Fatal("the SDK config must be built")
	}
	if cfg.SDK.OAuthClient != o {
		t.Error("the SDK config must carry the OAuth client, or the token is never used")
	}
	// Not a *MissingCredentialError: nothing is missing. Reporting it as one
	// would exit 2 and print remedy text about the app-key triple.
	var mce *MissingCredentialError
	if asMissingCredentialError(err, &mce) {
		t.Errorf("err = %T, want nil", err)
	}
}

// The rest of the contract: same mode, same dry-run state, same banner, so the
// read-only assertion this config is used with is evaluated against a real
// configuration rather than a stand-in.
func TestLoadOAuth_LoadsTheSameGateStateAsLoad(t *testing.T) {
	sandbox(t)
	cfg, err := LoadOAuth(oauth.New("client-id"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeSimulated {
		t.Errorf("Mode = %q, want the simulated default", cfg.Mode)
	}
	if !cfg.DryRun {
		t.Error("DryRun must default to true, or AssertReadOnly has no gate to check")
	}
	if err := cfg.GuardWrite("run the OAuth login"); err == nil {
		t.Error("GuardWrite admitted the default config, so the order gate is not " +
			"closed and AssertReadOnly would exit 1 on a normal run")
	}
	if !strings.Contains(cfg.String(), "dry_run=true") {
		t.Errorf("the banner must show the gate state:\n%s", cfg.String())
	}
	if !strings.Contains(cfg.String(), "app_key="+Redact("")) {
		t.Errorf("the banner must still show the app key field, masked as unset:\n%s", cfg.String())
	}
}

func TestLoadOAuth_CarriesTheAppKeyForTheBannerOnly(t *testing.T) {
	sandbox(t)
	t.Setenv("LONGBRIDGE_APP_KEY", "abcdefghijklmnop")

	cfg, err := LoadOAuth(oauth.New("client-id"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "abcdefghijklmnop" {
		t.Errorf("AppKey = %q, want the key from the environment so the banner is not a lie", cfg.AppKey)
	}
	// The env wins over the file, as it does in Load.
	if err := os.WriteFile("config.yaml", []byte("longbridge:\n  app_key: from-file\n"), 0o600); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}
	cfg, err = LoadOAuth(oauth.New("client-id"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "abcdefghijklmnop" {
		t.Errorf("AppKey = %q, want the environment to win over the file", cfg.AppKey)
	}
	// And a key that exists only in the file is picked up too, so the same
	// precedence holds in both directions.
	if err := os.Unsetenv("LONGBRIDGE_APP_KEY"); err != nil {
		t.Fatalf("clearing LONGBRIDGE_APP_KEY: %v", err)
	}
	if err := os.Unsetenv("LONGPORT_APP_KEY"); err != nil {
		t.Fatalf("clearing LONGPORT_APP_KEY: %v", err)
	}
	cfg, err = LoadOAuth(oauth.New("client-id"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "from-file" {
		t.Errorf("AppKey = %q, want the key from config.yaml", cfg.AppKey)
	}
	if cfg.Source != "config.yaml" {
		t.Errorf("Source = %q, want the file it was read from", cfg.Source)
	}
}

// The secret and the access token are left empty on purpose: neither exists on
// the OAuth path, and asking for them would report a missing credential that is
// not one.
func TestLoadOAuth_LeavesTheSecretAndTokenEmpty(t *testing.T) {
	sandbox(t)
	cfg, err := LoadOAuth(oauth.New("client-id"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppSecret != "" || cfg.AccessToken != "" {
		t.Errorf("AppSecret = %q, AccessToken = %q, want both empty: the OAuth "+
			"flow is authorised by the client ID", cfg.AppSecret, cfg.AccessToken)
	}
}

// The dry-run switches are validated here even though this loader's only
// consumer issues no writes, because every other binary validates them and a
// bad value should fail at startup rather than in an unrelated command later.
func TestLoadOAuth_ValidatesTheDryRunSwitches(t *testing.T) {
	sandbox(t)
	t.Setenv("LONGPORT_CONTENT_DRY_RUN", "bad")
	if _, err := LoadOAuth(oauth.New("client-id"), ""); err == nil {
		t.Fatal("LoadOAuth accepted a malformed LONGPORT_CONTENT_DRY_RUN; a value " +
			"that cannot be parsed is a startup failure everywhere else")
	}
}

// Every startup failure Load reports, LoadOAuth reports too. The switches are
// validated before the SDK is asked for anything, so a typo in one is a plain
// error naming the variable rather than something that surfaces later.
func TestLoadOAuth_EveryStartupFailureLoadReportsItReportsToo(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		val     string
		wantErr string
	}{
		{"bad mode", "LONGPORT_MODE", "sandbox", `invalid LONGPORT_MODE="sandbox"`},
		{"bad dry run", "LONGPORT_DRY_RUN", "maybe", `invalid LONGPORT_DRY_RUN="maybe"`},
		{"bad watchlist dry run", "LONGPORT_WATCHLIST_DRY_RUN", "sure", `invalid LONGPORT_WATCHLIST_DRY_RUN="sure"`},
		{"bad dca dry run", "LONGPORT_DCA_DRY_RUN", "0 0", `invalid LONGPORT_DCA_DRY_RUN="0 0"`},
		{"bad alert dry run", "LONGPORT_ALERT_DRY_RUN", "nope", `invalid LONGPORT_ALERT_DRY_RUN="nope"`},
		{"bad sharelist dry run", "LONGPORT_SHARELIST_DRY_RUN", "0 0", `invalid LONGPORT_SHARELIST_DRY_RUN="0 0"`},
		{"bad content dry run", "LONGPORT_CONTENT_DRY_RUN", "maybe", `invalid LONGPORT_CONTENT_DRY_RUN="maybe"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			t.Setenv(tt.env, tt.val)

			cfg, err := LoadOAuth(oauth.New("client-id"), "")
			if err == nil {
				t.Fatalf("want a startup error, got config %+v", cfg)
			}
			if cfg != nil {
				t.Errorf("no config may be returned alongside an error, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	t.Run("a config file the SDK cannot read fails the load", func(t *testing.T) {
		// The demo loader treats a file with no longbridge: block as "no
		// credentials in it" and carries on; the SDK's own reader is the one
		// that rejects it, and that rejection has to reach the user.
		sandbox(t)
		t.Setenv("LONGBRIDGE_APP_KEY", "k")
		writeFile(t, "config.yaml", "something_else:\n  key: value\n")

		if _, err := LoadOAuth(oauth.New("client-id"), ""); err == nil {
			t.Error("a config.yaml with no longbridge: block must fail the load, " +
				"as it does for Load")
		}
	})

	t.Run("a config file that is not YAML is rejected", func(t *testing.T) {
		sandbox(t)
		if _, err := LoadOAuth(oauth.New("client-id"), "config.toml"); err == nil {
			t.Error("a config file that is not YAML must be rejected rather than " +
				"silently ignored, as it is by Load")
		}
	})
}

func TestLoadOAuth_NeverTouchesTheNetworkOrAClient(t *testing.T) {
	// Nothing here proves the absence of a call; it pins that LoadOAuth only
	// builds an *sdk.Config and that it does not install an HTTP client or an
	// OAuth client of its own — the caller owns both.
	sandbox(t)
	cfg, err := LoadOAuth(oauth.New("client-id"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SDK.Client != nil {
		t.Error("LoadOAuth must not install an HTTP client; that is the caller's job")
	}
	if cfg.SDK.OAuthClient.ClientID() != "client-id" {
		t.Errorf("the SDK config must carry the caller's OAuth client, got %q",
			cfg.SDK.OAuthClient.ClientID())
	}
}
