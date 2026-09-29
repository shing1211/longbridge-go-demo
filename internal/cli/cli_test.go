package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/longbridge/openapi-go/oauth"
	"github.com/shopspring/decimal"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

func mustDec(t *testing.T, s string) *decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal.NewFromString(%q): %v", s, err)
	}
	return &d
}

// ---------------------------------------------------------------------------
// Dec / Dec4
// ---------------------------------------------------------------------------

func TestDec(t *testing.T) {
	tests := []struct {
		name string
		in   *decimal.Decimal
		want string
	}{
		{"nil is absent", nil, "-"},
		{"zero renders as a real figure", mustDec(t, "0"), "0.00"},
		{"explicit zero with scale", mustDec(t, "0.00"), "0.00"},
		{"trailing zeros padded", mustDec(t, "1234.5"), "1234.50"},
		{"negative", mustDec(t, "-1.5"), "-1.50"},
		{"rounds to 2dp", mustDec(t, "12345.6789"), "12345.68"},
		{"rounds down to zero", mustDec(t, "0.001"), "0.00"},
		{"rounds up from below", mustDec(t, "0.009"), "0.01"},
		{"large magnitude", mustDec(t, "1234567.891"), "1234567.89"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dec(tt.in); got != tt.want {
				t.Errorf("Dec() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDec4(t *testing.T) {
	tests := []struct {
		name string
		in   *decimal.Decimal
		want string
	}{
		{"nil is absent", nil, "-"},
		{"zero renders as a real figure", mustDec(t, "0"), "0.0000"},
		{"keeps 4dp precision", mustDec(t, "1.23456789"), "1.2346"},
		{"rounds to 4dp", mustDec(t, "0.00005"), "0.0001"},
		{"rounds down at 5dp", mustDec(t, "0.00004"), "0.0000"},
		{"negative", mustDec(t, "-1.2345"), "-1.2345"},
		{"conversion ratio shape", mustDec(t, "1.085"), "1.0850"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dec4(tt.in); got != tt.want {
				t.Errorf("Dec4() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The nil/zero split is the entire reason these helpers exist: a missing figure
// must never render as 0.00, or a caller reading the output cannot tell "the
// field was absent" from "the field was zero".
func TestDec_NilAndZeroAreNeverTheSameString(t *testing.T) {
	zero := mustDec(t, "0")
	for _, tc := range []struct {
		fn   string
		rend func(*decimal.Decimal) string
		want string
	}{
		{fn: "Dec", rend: Dec, want: "0.00"},
		{fn: "Dec4", rend: Dec4, want: "0.0000"},
	} {
		t.Run(tc.fn, func(t *testing.T) {
			absent, got := tc.rend(nil), tc.rend(zero)
			if absent == got {
				t.Errorf("%s(nil) and %s(zero) both render as %q; an absent figure "+
					"would be indistinguishable from a real zero", tc.fn, tc.fn, absent)
			}
			if absent != "-" {
				t.Errorf("%s(nil) = %q, want %q", tc.fn, absent, "-")
			}
			if got != tc.want {
				t.Errorf("%s(zero) = %q, want %q", tc.fn, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Truncate
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter than the limit is untouched", "abc", 5, "abc"},
		{"exactly the limit is untouched", "abcde", 5, "abcde"},
		{"one over the limit is clipped", "abcdef", 5, "abcd…"},
		{"much longer keeps the prefix", "abcdefghij", 4, "abc…"},
		{"limit of 2", "abcdef", 2, "a…"},
		{"limit of 1 collapses to the ellipsis", "ab", 1, "…"},
		{"limit of 0 collapses to the ellipsis", "abc", 0, "…"},
		{"negative limit never panics", "abc", -5, "…"},
		{"empty input below the limit", "", 3, ""},
		{"empty input with limit 0", "", 0, ""},
		{"empty input with limit 1", "", 1, ""},
		// A single rune at limit 1 hits the "short enough" branch first, so it
		// is returned whole rather than replaced by the ellipsis.
		{"single rune at limit 1 is not marked", "a", 1, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.in, tt.n)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestTruncate_TruncatesOnRunesNotBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"cjk 4 runes", "恒生指数", 3, "恒生…"},
		{"cjk longer than the limit", "恒生指数科技", 5, "恒生指数…"},
		{"emoji are one rune each", "🙂🙃😀", 2, "🙂…"},
		{"mixed ascii and cjk", "HSI恒生", 4, "HSI…"},
		{"combining accents count as runes", "ééé", 2, "é…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.in, tt.n)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("Truncate(%q, %d) = %q is not valid UTF-8; it clipped on bytes",
					tt.in, tt.n, got)
			}
		})
	}
}

// A byte-wise implementation would emit invalid UTF-8 here: 3 runes of CJK
// clipped to 2 runes is still 6 bytes, so the width is only right if the
// slice is taken over runes.
func TestTruncate_RuneWidthIsIndependentOfByteLength(t *testing.T) {
	cjk := "中文字元"       // 4 runes, 12 bytes
	ascii := "abcdefgh" // 8 runes, 8 bytes

	cjkGot := Truncate(cjk, 3)
	asciiGot := Truncate(ascii, 3)

	if utf8.RuneCountInString(cjkGot) != 3 {
		t.Errorf("Truncate(%q, 3) = %q has %d runes, want 3",
			cjk, cjkGot, utf8.RuneCountInString(cjkGot))
	}
	if utf8.RuneCountInString(asciiGot) != 3 {
		t.Errorf("Truncate(%q, 3) = %q has %d runes, want 3",
			ascii, asciiGot, utf8.RuneCountInString(asciiGot))
	}
	if !strings.HasSuffix(cjkGot, "…") || !strings.HasSuffix(asciiGot, "…") {
		t.Error("a clipped cell must be marked with an ellipsis so it is not " +
			"mistaken for a complete one")
	}
}

// ---------------------------------------------------------------------------
// OrDash
// ---------------------------------------------------------------------------

func TestOrDash(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty becomes a dash", "", "-"},
		{"plain value is untouched", "AAPL", "AAPL"},
		{"a space is a value, not an absence", " ", " "},
		{"dash-like input is not rewritten", "-", "-"},
		{"multibyte is untouched", "恒生", "恒生"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OrDash(tt.in); got != tt.want {
				t.Errorf("OrDash(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FmtTime
// ---------------------------------------------------------------------------

const timeLayout = "2006-01-02 15:04:05"

func TestFmtTime(t *testing.T) {
	t.Run("zero means unset", func(t *testing.T) {
		if got := FmtTime(0); got != "-" {
			t.Errorf("FmtTime(0) = %q, want %q", got, "-")
		}
	})

	tests := []struct {
		name string
		ts   int64
	}{
		{"unix epoch", 1},
		{"a plausible order timestamp", 1700000000},
		{"far future", 4102444800},
		{"before the epoch", -86400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := time.Unix(tt.ts, 0).Format(timeLayout)
			got := FmtTime(tt.ts)
			if got != want {
				t.Errorf("FmtTime(%d) = %q, want %q", tt.ts, got, want)
			}
			// Round-tripping proves the value is local time, not a UTC
			// rendering: parsing in Local must land on the same instant.
			parsed, err := time.ParseInLocation(timeLayout, got, time.Local)
			if err != nil {
				t.Fatalf("FmtTime(%d) = %q does not parse as %q: %v",
					tt.ts, got, timeLayout, err)
			}
			if parsed.Unix() != tt.ts {
				t.Errorf("FmtTime(%d) = %q round-trips to %d",
					tt.ts, got, parsed.Unix())
			}
		})
	}
}

func TestFmtTime_AlwaysRendersTheSameWidth(t *testing.T) {
	// Every rendered value must be the same width, or a table column built
	// from FmtTime will not line up.
	for _, ts := range []int64{1, 1700000000, 4102444800} {
		if n := len(FmtTime(ts)); n != len("2006-01-02 15:04:05") {
			t.Errorf("FmtTime(%d) has width %d, want %d",
				ts, n, len("2006-01-02 15:04:05"))
		}
	}
}

// ---------------------------------------------------------------------------
// Section
// ---------------------------------------------------------------------------

// captureStdout swaps the process stdout for a pipe, runs fn, and returns
// whatever fn printed. Not parallel-safe: it mutates os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("draining pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("closing pipe reader: %v", err)
	}
	return buf.String()
}

func TestSection(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"plain title", "Positions", "\n=== Positions ===\n"},
		{"empty title", "", "\n===  ===\n"},
		{"multibyte title", "持仓 Positions", "\n=== 持仓 Positions ===\n"},
		{"percent signs are not format verbs", "100% done", "\n=== 100% done ===\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := captureStdout(t, func() { Section(tt.title) }); got != tt.want {
				t.Errorf("Section(%q) wrote %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WithTimeout
// ---------------------------------------------------------------------------

func TestWithTimeout(t *testing.T) {
	t.Run("cancel func cancels the returned context", func(t *testing.T) {
		ctx, cancel := WithTimeout(context.Background())
		if err := ctx.Err(); err != nil {
			t.Fatalf("ctx already done before cancel: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
		default:
			t.Fatal("ctx.Done() did not close after cancel()")
		}
		if err := ctx.Err(); err != context.Canceled {
			t.Errorf("ctx.Err() = %v, want %v", err, context.Canceled)
		}
	})

	t.Run("deadline matches config.Timeout", func(t *testing.T) {
		want := appcfg.Timeout()
		ctx, cancel := WithTimeout(context.Background())
		defer cancel()
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("WithTimeout returned a context with no deadline")
		}
		if d := time.Until(dl) - want; d > time.Second || d < -time.Second {
			t.Errorf("deadline is %v away, want about %v (config.Timeout() = %v)",
				time.Until(dl), want, d)
		}
	})

	t.Run("default is 15s when LONGBRIDGE_HTTP_TIMEOUT is unusable", func(t *testing.T) {
		t.Setenv("LONGBRIDGE_HTTP_TIMEOUT", "not-a-duration")
		if got := appcfg.Timeout(); got != 15*time.Second {
			t.Fatalf("config.Timeout() = %v, want 15s (precondition)", got)
		}
		ctx, cancel := WithTimeout(context.Background())
		defer cancel()
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("WithTimeout returned a context with no deadline")
		}
		if d := time.Until(dl) - 15*time.Second; d > time.Second || d < -time.Second {
			t.Errorf("deadline is %v away, want about 15s", time.Until(dl))
		}
	})

	t.Run("honours LONGBRIDGE_HTTP_TIMEOUT", func(t *testing.T) {
		t.Setenv("LONGBRIDGE_HTTP_TIMEOUT", "90s")
		ctx, cancel := WithTimeout(context.Background())
		defer cancel()
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("WithTimeout returned a context with no deadline")
		}
		if d := time.Until(dl) - 90*time.Second; d > time.Second || d < -time.Second {
			t.Errorf("deadline is %v away, want about 90s", time.Until(dl))
		}
	})

	t.Run("derives from the parent context", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		ctx, cancel := WithTimeout(parent)
		defer cancel()
		cancelParent()
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("child context did not follow its parent")
		}
		if err := ctx.Err(); err != context.Canceled {
			t.Errorf("ctx.Err() = %v, want %v", err, context.Canceled)
		}
	})
}

// ---------------------------------------------------------------------------
// NewUsage / Parse
// ---------------------------------------------------------------------------

func TestNewUsage_RegistersConfigFlag(t *testing.T) {
	u := NewUsage("demo", "a summary line")
	if u.FS == nil {
		t.Fatal("NewUsage returned a Usage with no FlagSet")
	}
	if got := u.FS.Name(); got != "demo" {
		t.Errorf("FlagSet name = %q, want %q (it appears in the usage line)", got, "demo")
	}

	f := u.FS.Lookup("config")
	if f == nil {
		t.Fatal(`-config is not registered; commands rely on it to find config.yaml`)
	}
	if f.DefValue != "" {
		t.Errorf("-config default = %q, want %q (auto-detect when empty)", f.DefValue, "")
	}
	if f.Value.String() != "" || u.Config != "" {
		t.Errorf("u.Config = %q before parsing, want empty", u.Config)
	}

	// -h and -help are not registered as flags; the flag package handles them.
	if u.FS.Lookup("h") != nil || u.FS.Lookup("help") != nil {
		t.Error("-h/-help must stay unregistered so the flag package can answer them")
	}
}

func TestNewUsage_ConfigFlagIsParsed(t *testing.T) {
	u := NewUsage("demo", "a summary line")
	var buf bytes.Buffer
	u.FS.SetOutput(&buf)
	u.Parse([]string{"-config", "/tmp/does-not-exist.yaml"})
	if u.Config != "/tmp/does-not-exist.yaml" {
		t.Errorf("u.Config = %q after parsing -config, want the flag value", u.Config)
	}
	if buf.Len() != 0 {
		t.Errorf("a successful parse wrote %q, want nothing", buf.String())
	}
}

func TestNewUsage_IsIndependentPerCall(t *testing.T) {
	// Each command must get its own FlagSet, or one command's flags leak
	// into the next parse.
	a := NewUsage("alpha", "first")
	b := NewUsage("beta", "second")
	var buf bytes.Buffer
	a.FS.SetOutput(&buf)
	b.FS.SetOutput(&buf)
	a.Parse([]string{})
	if b.Config != "" || b.Help {
		t.Error("a second Usage was mutated by parsing the first")
	}
	if a.FS.Name() == b.FS.Name() {
		t.Errorf("both Usage objects are named %q; the name drives the usage line", a.FS.Name())
	}
}

func TestParse_UnknownFlagFails(t *testing.T) {
	res := runHelper(t, "parse-unknown-flag")
	if res.code == 0 {
		t.Errorf("unknown flag exited 0; a usage error must never look like success.\nstderr:\n%s", res.stderr)
	}
	if res.code != 1 {
		t.Errorf("unknown flag exited %d, want 1.\n"+
			"NOTE: only a *MissingCredentialError reaches 2; a flag error wraps "+
			"neither sentinel, so a bad flag belongs on the generic branch.",
			res.code)
	}
	for _, want := range []string{
		"flag provided but not defined: -nope",
		"Usage:",
		"demo [flags]",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not contain %q; the user must be told what went wrong.\nstderr:\n%s", want, res.stderr)
		}
	}
}

func TestParse_HelpExitsZeroWithoutCredentials(t *testing.T) {
	for _, flag := range []string{"-h", "-help"} {
		t.Run(flag, func(t *testing.T) {
			res := runHelper(t, "parse-help", "HELPER_FLAG="+flag)
			if res.code != 0 {
				t.Errorf("%s exited %d, want 0: help must always work, even with "+
					"no credentials present.\nstderr:\n%s", flag, res.code, res.stderr)
			}
			for _, want := range []string{
				"demo - a summary line",
				"Usage:",
				"-config",
				"Credentials (env or config.yaml",
				"Exit codes:",
			} {
				if !strings.Contains(res.stderr, want) {
					t.Errorf("%s output does not contain %q.\nstderr:\n%s", flag, want, res.stderr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The exit-code boundary the Fail comment draws
// ---------------------------------------------------------------------------

// A usage error and a missing-credential error are the pair that must never be
// confused: only the second is 2. Pinned from both sides through the re-exec
// harness, because the doc bug this guards against is precisely the claim that
// "a usage error" also exits 2 — which the code has never done.
func TestExitCode_UsageErrorIsOneAndMissingCredentialsIsTwo(t *testing.T) {
	usage := runHelper(t, "parse-unknown-flag")
	if usage.code != 1 {
		t.Errorf("a usage error (unknown flag) exited %d, want 1; only a "+
			"*MissingCredentialError may exit 2.\nstderr:\n%s", usage.code, usage.stderr)
	}

	credentials := runHelper(t, "fail-missing-credentials")
	if credentials.code != 2 {
		t.Errorf("a missing-credential error exited %d, want 2.\nstderr:\n%s",
			credentials.code, credentials.stderr)
	}

	if usage.code == credentials.code {
		t.Errorf("both exit %d, so a wrapper script cannot tell \"fix your flags\" "+
			"from \"fix your environment\"", usage.code)
	}
}

// A flag the command cannot use is the same class of problem as an unknown
// flag, and Fail classifies by type rather than by message, so a bad value can
// never be routed to the credential branch by saying something credential-ish.
func TestExitCode_AnUnusableFlagValueIsNotACredentialError(t *testing.T) {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(new(int), "count", 0, "an int flag")

	err := fs.Parse([]string{"-count", "not-a-number"})
	if err == nil {
		t.Fatal(`parsing -count not-a-number succeeded; the premise of this test is gone`)
	}

	var missing *appcfg.MissingCredentialError
	if errors.As(err, &missing) {
		t.Errorf("a flag error is a *MissingCredentialError (%T), so Fail would exit 2 "+
			"for what is only a usage problem", err)
	}
	if errors.Is(err, appcfg.ErrBlocked) {
		t.Errorf("a flag error wraps ErrBlocked (%v), so Fail would exit 3", err)
	}
}

// ---------------------------------------------------------------------------
// AssertReadOnly
// ---------------------------------------------------------------------------

// readOnlyBinaries is the nine binaries that issue no writes, each with the
// action string its own GuardWrite refusal would print if a write were
// attempted. The same nine (name, action) pairs are pinned against the source
// in test/readonly_invariant_test.go, so a call site that drifts from this
// table fails there rather than leaving the table testing fiction.
//
// auth is here because it authenticates rather than reads market data, and
// because it is the first entry that writes anything at all: the SDK files its
// token under $HOME when the flow completes. That file is not an API write, and
// AssertReadOnly only asks that GuardWrite refuses, so the gate state these
// cases drive is the whole of what it checks.
var readOnlyBinaries = []struct{ name, action string }{
	{"quote", "quote a symbol"},
	{"watch", "run the watch streamer"},
	{"warrant", "run the warrant reader"},
	{"reference", "run the reference reader"},
	{"portfolio", "run the portfolio reader"},
	{"fundamentals", "run the fundamentals reader"},
	{"market", "run the market reader"},
	{"screener", "run the screener reader"},
	{"auth", "run the OAuth login"},
}

// closedGates are the configurations in which GuardWrite refuses, i.e. every
// state a read-only binary can be started in except the misconfigured one. They
// refuse for different reasons and say so in different text, so all three are
// exercised rather than only the default.
var closedGates = []struct {
	label string
	cfg   *appcfg.Config
}{
	{"dry run on, the default", &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}},
	{"dry run off but mode not live", &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: false}},
	{"mode never set", &appcfg.Config{}},
}

// openGate is the one configuration GuardWrite admits. Naming it as a single
// shared value keeps the subprocess scenario that asserts the violation from
// drifting away from what the guard actually does.
var openGate = &appcfg.Config{Mode: appcfg.ModeLive, DryRun: false}

// The invariant holds — silently — in every closed-gate configuration, and it
// must hold for all nine binaries, not just the one the helper was written
// for. The violated branch is deliberately absent here: Fail exits, so that
// half of the helper is pinned from a child process instead
// (TestAssertReadOnly_OpenGateExitsOneMisconfiguration).
func TestAssertReadOnly_ClosedGateReturnsNil(t *testing.T) {
	for _, gate := range closedGates {
		for _, bin := range readOnlyBinaries {
			t.Run(gate.label+"/"+bin.name, func(t *testing.T) {
				// Precondition, so the loop above cannot pass vacuously if
				// GuardWrite ever stops refusing in one of these states.
				if err := gate.cfg.GuardWrite(bin.action); err == nil {
					t.Fatalf("GuardWrite(%q) = nil in %s; the premise that this "+
						"gate is closed is gone", bin.action, gate.label)
				}
				if err := AssertReadOnly(gate.cfg, bin.name, bin.action); err != nil {
					t.Errorf("AssertReadOnly(%s, %q) = %v, want nil: the gate is "+
						"closed, so the invariant holds", bin.name, bin.action, err)
				}
			})
		}
	}
}

// Pin the premise the violation rests on. GuardWrite refuses everything except
// live-with-dry-run-off, so that single cell is the only way a read-only binary
// can find its own gate open — and the only one worth reporting. What it cannot
// do in-process is assert the report, since Fail exits; that half is the child
// process's job.
func TestAssertReadOnly_OpenGateIsTheOnlyCellGuardWriteAdmits(t *testing.T) {
	if err := openGate.GuardWrite("run the market reader"); err != nil {
		t.Fatalf("GuardWrite refused in %+v, so the open gate no longer exists "+
			"and nothing can violate the invariant: %v", openGate, err)
	}
	// Also the other direction: a mode spelled but not live must not slip
	// through. GuardWrite uses `!= live` rather than `== simulated` for
	// exactly this reason, and the read-only assertion inherits that choice.
	for _, mode := range []appcfg.Mode{appcfg.ModeSimulated, "", appcfg.Mode("LIVE")} {
		cfg := &appcfg.Config{Mode: mode, DryRun: false}
		if err := cfg.GuardWrite("run the market reader"); err == nil {
			t.Errorf("GuardWrite admitted mode %q with dry run off; the gate is "+
				"meant to be closed unless LONGPORT_MODE is exactly live", mode)
		}
	}
}

func TestAssertReadOnly_EdgesArePinned(t *testing.T) {
	t.Run("an empty action makes the check pass vacuously", func(t *testing.T) {
		// GuardWrite refuses an empty action with its own plain error ("action
		// description is required"), and a refusal is exactly what this helper
		// reads as "the gate is closed". So an empty action is a silent no-op
		// even with the gate open, which is why every call site passes the
		// action it would have handed to GuardWrite. Pinned because the failure
		// it invites is invisible: nothing errors, the binary just runs.
		if err := openGate.GuardWrite(""); err == nil {
			t.Fatal(`GuardWrite("") = nil; the empty-action behaviour this test ` +
				"pins has changed")
		}
		if err := AssertReadOnly(openGate, "market", ""); err != nil {
			t.Errorf("AssertReadOnly with an empty action = %v, want nil, because "+
				"GuardWrite's own complaint is read as a closed gate", err)
		}
	})

	t.Run("a nil config panics instead of passing silently", func(t *testing.T) {
		// GuardWrite dereferences c.DryRun, so a nil *Config has no gate state
		// to read. That panic is kept deliberately: returning nil would let a
		// read-only binary sail past the one assertion that is supposed to
		// prove it cannot write, which is the failure this helper exists to
		// prevent. Every call site gets cfg from Usage.Load, which has already
		// failed out if the config did not load, so no call site can reach it.
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("AssertReadOnly(nil, …) returned instead of panicking; a " +
					"silent return would read as \"the gate is closed\", which is " +
					"the one answer that must never come from an unread config")
			}
			if msg := fmt.Sprint(r); !strings.Contains(msg, "nil pointer") {
				t.Errorf("panicked with %q, want a nil-pointer dereference", msg)
			}
		}()
		_ = AssertReadOnly(nil, "market", "run the market reader")
	})
}

// ---------------------------------------------------------------------------
// LoadOAuth
// ---------------------------------------------------------------------------

// oauthEnv is every variable the two loaders read. Clearing them makes a
// loader test independent of the developer's shell and of a stray .env the SDK
// autoloads, which is the same trap internal/config's sandbox exists for.
var oauthEnv = []string{
	"LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET", "LONGBRIDGE_ACCESS_TOKEN",
	"LONGPORT_APP_KEY", "LONGPORT_APP_SECRET", "LONGPORT_ACCESS_TOKEN",
	"LONGBRIDGE_CLIENT_ID",
	"LONGPORT_MODE", "LONGPORT_DRY_RUN", "LONGPORT_WATCHLIST_DRY_RUN",
	"LONGPORT_DCA_DRY_RUN", "LONGPORT_ALERT_DRY_RUN",
	"LONGPORT_SHARELIST_DRY_RUN", "LONGPORT_CONTENT_DRY_RUN",
	"LONGBRIDGE_ENV", "LONGPORT_REGION", "LONGBRIDGE_HTTP_URL",
	"LONGBRIDGE_QUOTE_URL", "LONGBRIDGE_TRADE_URL", "LONGBRIDGE_TIMEOUT",
	"LONGBRIDGE_AUTH_TIMEOUT", "LONGBRIDGE_HTTP_TIMEOUT",
}

// emptyOAuthEnv clears them and puts them back afterwards. t.Setenv is not
// enough on its own: config.LoadOAuth calls os.Setenv itself (applyEnvOverrides
// copies between the two spellings), and those writes would outlive the test.
func emptyOAuthEnv(t *testing.T) {
	t.Helper()
	type saved struct {
		key, value string
		set        bool
	}
	snap := make([]saved, 0, len(oauthEnv))
	for _, k := range oauthEnv {
		v, ok := os.LookupEnv(k)
		snap = append(snap, saved{key: k, value: v, set: ok})
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("clearing %s: %v", k, err)
		}
	}
	t.Cleanup(func() {
		for _, s := range snap {
			if s.set {
				_ = os.Setenv(s.key, s.value)
			} else {
				_ = os.Unsetenv(s.key)
			}
		}
	})
}

// The success path is the whole point of having a second loader: cmd/auth must
// be able to reach a configuration with no app-key triple in the environment at
// all, and the gate state it comes back with has to be the one the read-only
// assertion is checked against.
func TestUsage_LoadOAuthNeedsNoAppKeyCredential(t *testing.T) {
	emptyOAuthEnv(t)

	cfg := NewUsage("demo", "s").LoadOAuth(oauth.New("client-id"))
	if cfg == nil {
		t.Fatal("LoadOAuth returned nil without exiting")
	}
	if cfg.SDK == nil {
		t.Fatal("the SDK config must be built")
	}
	if cfg.SDK.OAuthClient == nil {
		t.Fatal("the SDK config must carry the OAuth client")
	}
	if cfg.Mode != appcfg.ModeSimulated || !cfg.DryRun {
		t.Errorf("Mode/DryRun = %q/%v, want the simulated dry-run default, so the "+
			"read-only assertion has a closed gate to check", cfg.Mode, cfg.DryRun)
	}
	if err := cfg.GuardWrite("run the OAuth login"); err == nil {
		t.Error("GuardWrite admitted the loaded config, so AssertReadOnly would " +
			"exit 1 on a normal run")
	}
}

// The two loaders are not interchangeable, and the difference has to be visible
// in what they ask for: Load demands three credentials and LoadOAuth demands
// none, so on a machine with the triple set both succeed and only LoadOAuth
// still works once it is gone.
func TestUsage_LoadStillDemandsTheTripleThatLoadOAuthDoesNot(t *testing.T) {
	emptyOAuthEnv(t)

	// LoadOAuth first: with nothing in the environment at all it succeeds.
	if cfg := NewUsage("demo", "s").LoadOAuth(oauth.New("client-id")); cfg == nil {
		t.Fatal("LoadOAuth failed with no credentials in the environment")
	}
	// Load cannot be driven in-process here, because it exits. What can be
	// asserted is the shape of the difference: its missing set is the triple.
	_, err := appcfg.Load("")
	var missing *appcfg.MissingCredentialError
	if !errors.As(err, &missing) {
		t.Fatalf("appcfg.Load(\"\") = %v, want a *MissingCredentialError", err)
	} else if len(missing.Missing) != 3 {
		t.Errorf("Missing = %v, want all three credentials", missing.Missing)
	}
}

// ---------------------------------------------------------------------------
// Usage text (documentation contract)
// ---------------------------------------------------------------------------

func usageText(t *testing.T, name, summary string) string {
	t.Helper()
	u := NewUsage(name, summary)
	var buf bytes.Buffer
	u.FS.SetOutput(&buf)
	u.FS.Usage()
	return buf.String()
}

func TestUsageText_Envelope(t *testing.T) {
	got := usageText(t, "demo", "a summary line")
	for _, want := range []string{
		"demo - a summary line\n\n",        // binary name and summary
		"Usage:\n  demo [flags]\n\n",       // invocation shape
		"Flags:\n",                         // flag section
		"-config",                          // the flag itself
		"path to a config.yaml",            // its help string
		"default: auto-detect config.yaml", // documents the empty default
		"\nCredentials (env or config.yaml `longbridge:` block):\n",
		"\nSafety:\n",
		"\nSee README.md for the simulated-account walkthrough.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("usage text does not contain %q.\nfull text:\n%s", want, got)
		}
	}
}

// The credential block is generated from config.EnvDocLines, so comparing
// against that same list is drift-proof: a credential added to config without
// a description here would still show up, and one described there but not in
// EnvDocLines would fail.
func TestUsageText_ListsEveryCredentialFromConfig(t *testing.T) {
	got := usageText(t, "demo", "s")
	lines := appcfg.EnvDocLines()
	if len(lines) == 0 {
		t.Fatal("config.EnvDocLines() is empty; the credential block would be a lie")
	}
	for _, line := range lines {
		if !strings.Contains(got, line) {
			t.Errorf("usage text is missing credential line %q.\nfull text:\n%s", line, got)
		}
	}
	for _, env := range []string{
		"LONGBRIDGE_APP_KEY",
		"LONGBRIDGE_APP_SECRET",
		"LONGBRIDGE_ACCESS_TOKEN",
	} {
		if !strings.Contains(got, env) {
			t.Errorf("usage text never names %s.\nfull text:\n%s", env, got)
		}
	}
	if !strings.Contains(got, "secret, never logged") {
		t.Error("usage text does not mark the secret variables; a reader must know " +
			"which values must not be pasted into a bug report")
	}
}

type safetySwitch struct {
	env    string
	readBy string // what in the code actually reads this variable
}

// safetySwitchesReadByCode is the set of environment variables that gate a
// write somewhere in this repo. Deriving the guard names from the guard values
// themselves means the usage text and the code cannot disagree about which
// variable is authoritative.
func safetySwitchesReadByCode() []safetySwitch {
	return []safetySwitch{
		{env: "LONGPORT_DRY_RUN", readBy: "config.loadModeAndDryRun / Config.DryRun"},
		{env: "LONGPORT_MODE", readBy: "config.loadModeAndDryRun / Config.Mode"},
		{env: "LONGPORT_WATCHLIST_DRY_RUN", readBy: "config.WatchlistDryRun"},
		{env: appcfg.DCAGuard.DryRunEnv, readBy: "config.DCAGuard"},
		{env: appcfg.AlertGuard.DryRunEnv, readBy: "config.AlertGuard"},
		{env: appcfg.SharelistGuard.DryRunEnv, readBy: "config.SharelistGuard"},
		{env: appcfg.ContentGuard.DryRunEnv, readBy: "config.ContentGuard"},
	}
}

func TestUsageText_DocumentsEverySafetySwitch(t *testing.T) {
	got := usageText(t, "demo", "s")
	for _, sw := range safetySwitchesReadByCode() {
		t.Run(sw.env, func(t *testing.T) {
			if !strings.Contains(got, sw.env) {
				t.Fatalf("usage text does not document %s, but %s reads it. "+
					"Every gate the user can trip must be named in -h.\nfull text:\n%s",
					sw.env, sw.readBy, got)
			}
			// Documented is not enough: a name with no explanation is a trap.
			idx := strings.Index(got, sw.env)
			window := got[idx:min(idx+120, len(got))]
			if !strings.Contains(window, "default") {
				t.Errorf("%s is named but its default state is not stated.\ncontext:\n%s",
					sw.env, window)
			}
		})
	}
}

// The reverse direction: a switch the usage text advertises that no code reads
// would send an operator to set a variable that does nothing.
func TestUsageText_MentionsNoSwitchThatCodeIgnores(t *testing.T) {
	got := usageText(t, "demo", "s")
	readByCode := map[string]bool{}
	for _, sw := range safetySwitchesReadByCode() {
		readByCode[sw.env] = true
	}

	// Matches bare names only, so the prose "LONGPORT_MODE=live" counts as
	// LONGPORT_MODE rather than as a variable of its own.
	for _, name := range distinctEnvNames(got) {
		if !strings.HasPrefix(name, "LONGPORT_") {
			continue
		}
		// The extra-header prefix is a documented source that is not a safety
		// gate, so it is not in the switch table above — and the name it is
		// documented under is a PREFIX, so no table could enumerate it.
		//
		// What keeps it honest is that the usage text is generated from
		// appcfg.HeaderEnvPrefix, the same exported constant internal/config
		// scans the environment with: remove the reader and the constant stops
		// existing, so the line advertising it cannot survive. Allow-listing a
		// prefix here therefore cannot rot into a permanent exemption the way a
		// bare name could.
		if strings.HasPrefix(name, appcfg.HeaderEnvPrefix) {
			continue
		}
		if !readByCode[name] {
			t.Errorf("usage text advertises %s, but no code in this repo reads "+
				"it; setting it would silently do nothing.\nfull text:\n%s", name, got)
		}
	}
}

var envNameRE = regexp.MustCompile(`\b[A-Z][A-Z0-9]*_[A-Z0-9_]+\b`)

// distinctEnvNames returns every environment-variable-shaped token in s, in
// order of first appearance.
func distinctEnvNames(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range envNameRE.FindAllString(s, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

func TestUsageText_ExitCodeLine(t *testing.T) {
	got := usageText(t, "demo", "s")
	const want = "Exit codes: 0 ok, 1 error, 2 missing credentials, 3 BLOCKED by a guard."
	if !strings.Contains(got, want) {
		t.Errorf("usage text does not contain the exit-code line %q.\nfull text:\n%s", want, got)
	}
	// The line is the user-facing half of the exit-code contract; every status
	// cli.Fail can produce has to appear.
	for _, code := range []string{"0 ok", "1 error", "2 missing credentials", "3 BLOCKED"} {
		if !strings.Contains(got, code) {
			t.Errorf("exit-code line omits %q; all four statuses must be documented", code)
		}
	}
}

// Ties the printed number to the constant, so a change to config.ExitBlocked
// that does not reach the usage text fails here instead of in a bug report.
func TestUsageText_ExitCodeLineAgreesWithExitBlockedConstant(t *testing.T) {
	got := usageText(t, "demo", "s")
	want := fmt.Sprintf("Exit codes: 0 ok, 1 error, 2 missing credentials, %d BLOCKED by a guard.",
		appcfg.ExitBlocked)
	if !strings.Contains(got, want) {
		t.Errorf("usage text disagrees with config.ExitBlocked = %d.\nwant substring: %q\nfull text:\n%s",
			appcfg.ExitBlocked, want, got)
	}
}

func TestUsageText_SafetySectionWarnsThatDryRunIsTheDefault(t *testing.T) {
	got := usageText(t, "demo", "s")
	safety := got[strings.Index(got, "\nSafety:\n"):]
	safety = safety[:strings.Index(safety, "Exit codes:")]

	if n := strings.Count(safety, "(default)"); n < 5 {
		t.Errorf("Safety section states a default for only %d switches, want at least 5 "+
			"(DRY_RUN, MODE, WATCHLIST, DCA, ALERT).\nsection:\n%s", n, safety)
	}
	if !strings.Contains(safety, "blocks all order writes") {
		t.Error("Safety section does not say what LONGPORT_DRY_RUN blocks")
	}
	if !strings.Contains(safety, "LONGPORT_MODE") || !strings.Contains(safety, "live") {
		t.Error("Safety section does not explain that live mode is a real opt-in")
	}
	for _, gate := range []string{"watchlist writes", "DCA plan writes", "price-alert writes"} {
		if !strings.Contains(safety, gate) {
			t.Errorf("Safety section does not say that %q are gated", gate)
		}
	}
	if strings.Count(safety, "LONGPORT_MODE=live") < 2 {
		t.Error("Safety section does not say which gates additionally require LONGPORT_MODE=live")
	}
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

func TestRun_NilErrorReturnsWithoutExiting(t *testing.T) {
	called := false
	Run(func(ctx context.Context) error {
		called = true
		if ctx == nil {
			t.Fatal("Run passed a nil context to fn")
		}
		// Checked here, not after Run returns: Run defers stop(), so by the
		// time the call returns the signal context is already cancelled.
		if err := ctx.Err(); err != nil {
			t.Errorf("the context handed to fn is already done: %v", err)
		}
		if _, ok := ctx.Deadline(); ok {
			t.Error("Run's context carries a deadline; WithTimeout exists for " +
				"that, so a command that forgets it would silently time out")
		}
		if ctx.Done() == nil {
			t.Error("Run's context is not cancellable; it should be a signal context")
		}
		return nil
	})
	if !called {
		t.Fatal("Run did not invoke fn")
	}
}

func TestRun_PropagatesTheErrorToFail(t *testing.T) {
	// Run's whole job for a non-nil error is to hand it to Fail, so the exit
	// status is whatever Fail decides. Pin each of the three.
	for _, tc := range []struct {
		name     string
		scenario string
		want     int
		env      []string
	}{
		{name: "guard refusal exits 3", scenario: "run-blocked", want: 3},
		{name: "ordinary failure exits 1", scenario: "run-generic", want: 1},
		{
			name: "panic exits 1", scenario: "run-panic-string", want: 1,
			env: []string{"HELPER_PANIC_VALUE=string"},
		},
		{
			name: "panic of an error value exits 1", scenario: "run-panic-error", want: 1,
			env: []string{"HELPER_PANIC_VALUE=error"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runHelper(t, tc.scenario, tc.env...)
			if res.code != tc.want {
				t.Errorf("Run exited %d, want %d.\nstdout:\n%s\nstderr:\n%s",
					res.code, tc.want, res.stdout, res.stderr)
			}
		})
	}
}

// The contract at cli.go:55 is that a panic becomes a normal error report, not
// a stack trace. Anything resembling a trace on stderr is a regression.
func TestRun_PanicIsReportedWithoutAStackTrace(t *testing.T) {
	for _, panicValue := range []string{"string", "error"} {
		t.Run("panic of "+panicValue, func(t *testing.T) {
			res := runHelper(t, "run-panic-"+panicValue)
			if res.code != 1 {
				t.Errorf("a panicking command exited %d, want 1", res.code)
			}
			want := "error: internal panic: "
			if !strings.HasPrefix(res.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", res.stderr, want)
			}
			if !strings.Contains(res.stderr, "the SDK exploded") {
				t.Errorf("stderr = %q, want the panic value included", res.stderr)
			}
			if !strings.HasSuffix(res.stderr, "\n") {
				t.Errorf("stderr = %q, want a trailing newline", res.stderr)
			}
			if traces := goTraceMarkers(res.stderr); len(traces) > 0 {
				t.Errorf("stderr contains trace marker(s) %v, which means a raw "+
					"panic reached the user.\nstderr:\n%s", traces, res.stderr)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want empty: a panic report belongs on stderr", res.stdout)
			}
		})
	}
}

// goTraceMarkers returns the runtime stack-trace signatures present in s. The
// words "panic:" and ".go" appear in the intended one-line report too, so the
// patterns are anchored to the shapes the runtime actually prints.
func goTraceMarkers(s string) []string {
	patterns := map[string]*regexp.Regexp{
		"goroutine header":   regexp.MustCompile(`(?m)^goroutine \d+`),
		"panic header":       regexp.MustCompile(`(?m)^panic: `),
		"file:line frame":    regexp.MustCompile(`(?m)^\s+\S+\.go:\d+`),
		"runtime frame":      regexp.MustCompile(`\bruntime\.\w`),
		"created by frame":   regexp.MustCompile(`(?m)^created by `),
		"exit status prefix": regexp.MustCompile(`^exit status `),
	}
	var found []string
	for name, re := range patterns {
		if re.MatchString(s) {
			found = append(found, name)
		}
	}
	sort.Strings(found)
	return found
}

// ---------------------------------------------------------------------------
// -header and -log-level
// ---------------------------------------------------------------------------

// clearHeaderEnv unsets every LONGPORT_HEADER_* variable for one test.
//
// The list above is a fixed enumeration of variable names, which cannot cover a
// prefix-scanned source: a developer who exported LONGPORT_HEADER_X_TRACE_ID to
// try the feature would otherwise fail the "no headers configured" cases here.
func clearHeaderEnv(t *testing.T) {
	t.Helper()
	var names []string
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, appcfg.HeaderEnvPrefix) {
			names = append(names, name)
		}
	}
	for _, n := range names {
		name := n
		old, wasSet := os.LookupEnv(name)
		t.Cleanup(func() {
			if wasSet {
				os.Setenv(name, old)
				return
			}
			os.Unsetenv(name)
		})
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
	}
}

// The flag reaches the SDK, and it is repeatable. Both matter: a single-use flag
// would force a comma-separated value with its own escaping rules, and a flag
// that stopped at the Config without reaching the SDK config would be decoration.
//
// The credentials are set here rather than skipped because Load is the thing
// under test and it will not return without them. Nothing in this path opens a
// connection: Load only builds a configuration.
func TestUsage_HeaderFlagIsRepeatableAndReachesTheSDKConfig(t *testing.T) {
	clearHeaderEnv(t)
	t.Setenv("LONGBRIDGE_APP_KEY", "key-1")
	t.Setenv("LONGBRIDGE_APP_SECRET", "secret-1")
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "token-1")

	u := NewUsage("demo", "s")
	u.Parse([]string{"-header", "x-trace-id=abc123", "-header", "x-tenant=demo"})
	cfg := u.Load()

	want := map[string]string{"x-trace-id": "abc123", "x-tenant": "demo"}
	if len(cfg.SDK.ExtraHeaders) != len(want) {
		t.Fatalf("SDK.ExtraHeaders = %v, want %v; every -header has to survive to the "+
			"SDK's own map, which is what its request loop iterates", cfg.SDK.ExtraHeaders, want)
	}
	for k, v := range want {
		if cfg.SDK.ExtraHeaders[k] != v {
			t.Errorf("SDK.ExtraHeaders[%q] = %q, want %q", k, cfg.SDK.ExtraHeaders[k], v)
		}
	}
	if len(cfg.Headers) != len(want) {
		t.Errorf("Config.Headers = %+v, want %d entries", cfg.Headers, len(want))
	}
}

// The last spelling of a name wins, and a spelling that differs only in case is
// the same name — the two rules together are what stop the SDK's map iteration
// from choosing the winner.
func TestUsage_HeaderFlagFoldsSpellingsAndTakesTheLast(t *testing.T) {
	clearHeaderEnv(t)
	t.Setenv("LONGBRIDGE_APP_KEY", "key-1")
	t.Setenv("LONGBRIDGE_APP_SECRET", "secret-1")
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "token-1")

	u := NewUsage("demo", "s")
	u.Parse([]string{"-header", "x-a=first", "-header", "X-A=second"})
	cfg := u.Load()

	if len(cfg.SDK.ExtraHeaders) != 1 {
		t.Fatalf("SDK.ExtraHeaders = %v, want exactly one entry", cfg.SDK.ExtraHeaders)
	}
	if got := cfg.SDK.ExtraHeaders["x-a"]; got != "second" {
		t.Errorf("x-a = %q, want %q", got, "second")
	}
}

// -log-level is what installs the adapter, and leaving it off installs nothing.
func TestUsage_LogLevelFlagInstallsTheAdapterAndItsAbsenceDoesNot(t *testing.T) {
	clearHeaderEnv(t)
	t.Setenv("LONGBRIDGE_APP_KEY", "key-1")
	t.Setenv("LONGBRIDGE_APP_SECRET", "secret-1")
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "token-1")

	quiet := NewUsage("demo", "s")
	quiet.Parse(nil)
	if cfg := quiet.Load(); cfg.Logger != nil {
		t.Errorf("no -log-level installed %v; the flag is what turns this on", cfg.Logger)
	}

	loud := NewUsage("demo", "s")
	loud.Parse([]string{"-log-level", "debug"})
	cfg := loud.Load()
	if cfg.Logger == nil {
		t.Fatal("-log-level debug installed no adapter")
	}
	if got := cfg.Logger.LevelName(); got != "debug" {
		t.Errorf("level = %s, want debug", got)
	}
	if cfg.SDK.Logger() == nil {
		t.Error("the adapter was not handed to the SDK config, so the SDK's own " +
			"logging would not be routed through it")
	}
}

// The -h block has to teach the whole rule, because a reader who cannot see that
// the credential headers are reserved will try them and lose a credential.
func TestUsageText_DocumentsTheHeaderAndLogLevelFlags(t *testing.T) {
	got := usageText(t, "demo", "s")
	for _, want := range []string{
		"-header",              // the flag itself
		"NAME=VALUE",           // its shape
		"repeatable",           // that it may be given more than once
		"-log-level",           // the other new flag
		"debug",                // with a level it accepts
		"stderr",               // and where the output goes
		appcfg.HeaderEnvPrefix, // the environment source, spelled from the constant
		"config.yaml",          // the file source
		"precedence",           // and the order of the three
	} {
		if !strings.Contains(got, want) {
			t.Errorf("usage text does not contain %q.\nfull text:\n%s", want, got)
		}
	}
	// Every reserved name is listed, so the refusal is never a surprise.
	for _, name := range appcfg.ReservedHeaderNames() {
		if !strings.Contains(got, name) {
			t.Errorf("usage text does not name the reserved header %q.\nfull text:\n%s",
				name, got)
		}
	}
	// And the consequence is stated, not just the list.
	if !strings.Contains(got, "REPLACE the credential") {
		t.Errorf("usage text lists the reserved headers without saying what happens if "+
			"one is set.\nfull text:\n%s", got)
	}
}

// The flag's own help string is what a reader sees in the flags block, so the
// reserved names have to be reachable from there too rather than only from the
// block printed underneath.
func TestUsageText_HeaderFlagHelpMentionsRepeatableAndReserved(t *testing.T) {
	got := usageText(t, "demo", "s")
	start := strings.Index(got, "-header")
	if start < 0 {
		t.Fatal("no -header flag in the usage text")
	}
	window := got[start:min(start+400, len(got))]
	if !strings.Contains(window, "repeatable") {
		t.Errorf("the -header help does not say it is repeatable:\n%s", window)
	}
	if !strings.Contains(window, "credential") {
		t.Errorf("the -header help does not warn that the credential headers are "+
			"refused:\n%s", window)
	}
}
