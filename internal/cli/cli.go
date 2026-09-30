// Package cli provides the argument parsing and credential-error rendering
// shared by every demo binary, so that all four behave identically at the
// edges: -h always works without credentials, and missing credentials always
// produce a readable message plus a non-zero exit.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/shopspring/decimal"

	"github.com/longbridge/openapi-go/oauth"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// Fail prints a problem to stderr and exits. Three statuses are distinguished,
// and the distinction matters to anyone scripting this repo:
//
//	0  success — the command did what it was asked
//	2  missing credentials, and nothing else — a *MissingCredentialError
//	3  BLOCKED — a safety guard refused a write; nothing was sent
//	1  anything else, including failures from the real API, and usage
//	   errors — an unknown flag, or a flag value the command cannot use
//
// A guard refusal must never collapse into 0: the whole point of the gate is
// that a script wrapping it can tell "I placed the order" from "the demo
// declined to place it".
//
// WHY A USAGE ERROR IS NOT 2: the flag package returns a plain *flag.error,
// which wraps neither sentinel, so it lands on 1. Reserving 2 for
// *MissingCredentialError alone is what lets a script read it as "go and fix
// your environment" without also having to handle "go and fix your flags".
func Fail(err error) {
	if err == nil {
		return
	}
	// Checked before the generic path, since BlockedError is also a plain
	// error and would otherwise be reported as an ordinary failure (1).
	if errors.Is(err, appcfg.ErrBlocked) {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(appcfg.ExitBlocked)
	}
	var missing *appcfg.MissingCredentialError
	if errors.As(err, &missing) {
		// err, not missing: a caller that wrapped the error for context
		// ("loading config from %s") would otherwise lose that prefix, since
		// %v on the wrapper already spells out the reason.
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// AssertReadOnly states the invariant every read-only binary shares — the order
// gate must still be closed at startup — and enforces it. name is the binary's
// own name as it appears in the refusal; action is the phrase GuardWrite would
// have printed had a write been attempted. A binary that issues no writes has
// no guard of its own to catch a mistake, so if the gate would let it through
// then the environment is wrong and the process stops before it can run.
//
// # WHY A HELPER INSTEAD OF EIGHT COPIES
//
// The eight read-only binaries that existed when this helper was written each
// inlined the same four lines, and the copies had already drifted: cmd/quote
// said "the write gate is open" where the other seven, and the contract in
// README.md, say "the order gate". Nothing noticed,
// because no test reached main() — with the inline copies put back and that
// "write gate" wording restored, the whole suite still passed. The message now
// exists once, so the noun cannot drift again, and
// test/readonly_invariant_test.go fails if a read-only binary stops calling this
// at all.
//
// WHY action STAYS A PARAMETER RATHER THAN BEING DERIVED FROM name: the nine
// action strings genuinely differ ("run the market reader" is not "quote a
// symbol"), and GuardWrite prints the action verbatim in its own refusal text.
// Deriving it from name would rewrite what nine binaries say when a real write
// is refused, and that message belongs to GuardWrite, not to this helper.
//
// # WHY A VIOLATION EXITS 1 AND NOT 3
//
// 3 means a guard refused a write and nothing was sent. Here the opposite
// happened: nothing was refused, the gate was open. A read-only binary finding
// an open gate is a misconfiguration, and README.md promises exit 1 for it. Do
// not wrap this error in appcfg.Blockedf — that would report 3 and break the
// documented contract.
//
// # TWO EDGES, BOTH PINNED BY TEST
//
// An empty action makes GuardWrite return its own "action description is
// required" error, which this function reads as a closed gate, so the check
// passes vacuously; callers must pass the real action string. A nil cfg has no
// gate state at all and panics inside GuardWrite rather than passing silently;
// every call site gets cfg from Usage.Load, which has already failed out if the
// config did not load, so a nil there is a programming error worth the stack
// trace.
func AssertReadOnly(cfg *appcfg.Config, name, action string) error {
	if err := cfg.GuardWrite(action); err == nil {
		// Fail does not return for a non-nil error, but returning it keeps the
		// function total: a caller that wanted to handle the violation itself
		// rather than exit would not have to work around a missing value.
		err := fmt.Errorf("internal invariant violated: %s is read-only but the order gate is open", name)
		Fail(err)
		return err
	}
	return nil
}

// Run executes fn, converting panics from the SDK into a normal error report
// rather than a stack trace, and returning a process exit code.
func Run(fn func(ctx context.Context) error) {
	// Catch SDK/library panics so the user never sees a raw trace.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "error: internal panic: %v\n", r)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := fn(ctx); err != nil {
		Fail(err)
	}
}

// Usage is a flag.FlagSet plus the standard -h behaviour, built here so each
// command can declare flags and get a consistent envelope.
type Usage struct {
	FS     *flag.FlagSet
	Config string
	Help   bool

	// headers holds the raw -header arguments, in the order they were given.
	// They are stored raw and validated inside appcfg.Load, so that the flag,
	// the environment and the YAML file go through one parser and one set of
	// rules — the reserved-name check above all.
	headers []string
	// headerErr is the first rejection among them. See Parse.
	headerErr error

	// logLevel is the SDK log level, empty when the flag was not passed.
	logLevel string
}

// NewUsage creates a FlagSet named after the binary with a one-line summary
// and a trailing credential block that lists every supported variable.
//
// The -header and -log-level flags are registered here rather than by each
// command, for the same reason the credential block is: they are cross-cutting,
// and a flag sixteen commands each had to remember to declare would be a flag
// sixteen commands could each get subtly wrong.
func NewUsage(name, summary string) *Usage {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintf(out, "%s - %s\n\n", name, summary)
		fmt.Fprintf(out, "Usage:\n  %s [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
		fmt.Fprintf(out, "\nCredentials (env or config.yaml `longbridge:` block):\n")
		for _, l := range appcfg.EnvDocLines() {
			fmt.Fprintln(out, l)
		}
		fmt.Fprintf(out, "\nExtra headers (every request the SDK makes):\n")
		fmt.Fprintf(out, "  -header NAME=VALUE             repeatable; also %s<NAME> or\n", appcfg.HeaderEnvPrefix)
		fmt.Fprintf(out, "                              config.yaml `longbridge: headers:`, in that\n")
		fmt.Fprintf(out, "                              order of precedence.\n")
		fmt.Fprintf(out, "  Reserved, and refused: %s.\n", strings.Join(appcfg.ReservedHeaderNames(), ", "))
		fmt.Fprintf(out, "  The SDK attaches those itself and applies extra headers\n")
		fmt.Fprintf(out, "  afterwards, so one would silently REPLACE the credential.\n")
		fmt.Fprintf(out, "\nSafety:\n")
		fmt.Fprintf(out, "  LONGPORT_DRY_RUN            1/true (default) blocks all order writes.\n")
		fmt.Fprintf(out, "  LONGPORT_MODE                simulated (default), paper or live.\n")
		fmt.Fprintf(out, "  LONGPORT_WATCHLIST_DRY_RUN   1/true (default) blocks watchlist writes\n")
		fmt.Fprintf(out, "                              (a separate gate; see cmd/watchlist).\n")
		fmt.Fprintf(out, "  LONGPORT_DCA_DRY_RUN          1/true (default) blocks DCA plan writes.\n")
		fmt.Fprintf(out, "                              Needs LONGPORT_MODE=live or =paper; see cmd/dca.\n")
		fmt.Fprintf(out, "  LONGPORT_ALERT_DRY_RUN        1/true (default) blocks price-alert writes.\n")
		fmt.Fprintf(out, "                              Needs LONGPORT_MODE=live or =paper; see cmd/alert.\n")
		fmt.Fprintf(out, "  LONGPORT_SHARELIST_DRY_RUN   1/true (default) blocks sharelist writes.\n")
		fmt.Fprintf(out, "                              Needs LONGPORT_MODE=live or =paper; see cmd/sharelist.\n")
		fmt.Fprintf(out, "  LONGPORT_CONTENT_DRY_RUN     1/true (default) blocks content publishes.\n")
		fmt.Fprintf(out, "                              Needs LONGPORT_MODE=live or =paper; see cmd/content.\n")
		fmt.Fprintf(out, "\nExit codes: 0 ok, 1 error, 2 missing credentials, 3 BLOCKED by a guard.\n")
		fmt.Fprintf(out, "\nSee README.md for the simulated-account walkthrough.\n")
	}

	u := &Usage{FS: fs}
	fs.StringVar(&u.Config, "config", "", "path to a config.yaml holding credentials (default: auto-detect config.yaml)")
	// WHY THIS RETURNS NIL INSTEAD OF ITS ERROR: the flag package wraps a Set
	// error as `invalid value %q for flag -%s: %v`, which would print the whole
	// argument — and the argument holds the value, which may be a credential.
	// So the rejection is recorded here and reported by Parse with a message
	// that names the header and quotes no value. The same masking rule the
	// startup banner applies is applied to the error path here.
	u.FS.Func("header", "extra HTTP header, NAME=VALUE; repeatable, and never one of the "+
		"credential headers the SDK sets itself", func(spec string) error {
		u.headers = append(u.headers, spec)
		if u.headerErr != nil {
			return nil
		}
		if _, err := appcfg.ParseHeaderSpec("-header", spec); err != nil {
			u.headerErr = err
		}
		return nil
	})
	fs.StringVar(&u.logLevel, "log-level", "",
		"route the SDK's own logging to stderr at debug|info|warn|error (default: none, so the SDK keeps its own logger)")
	return u
}

// Parse handles -h/--help and then parses the rest.
func (u *Usage) Parse(args []string) {
	// flag package handles -h itself via fs.Usage when defined; we only need to
	// let it exit cleanly. ContinueOnError means it returns ErrHelp.
	if err := u.FS.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		Fail(err)
	}
	// Reported here rather than from the flag's own Set, for the reason given
	// where the flag is declared: no value, not even in an error message.
	if u.headerErr != nil {
		Fail(u.headerErr)
	}
	// The level is checked at parse time as well as inside appcfg.Load, so that
	// a typo is a plain usage error — exit 1, naming the flag — even on a
	// machine with no credentials, where Load would otherwise have reported the
	// missing credential first.
	if strings.TrimSpace(u.logLevel) != "" {
		if _, err := appcfg.ParseLogLevel(u.logLevel); err != nil {
			Fail(fmt.Errorf("-log-level: %w", err))
		}
	}
}

// Load validates configuration and renders the non-secret startup banner.
func (u *Usage) Load() *appcfg.Config {
	cfg, err := appcfg.Load(u.Config, u.loadOptions()...)
	if err != nil {
		Fail(err)
	}
	return cfg
}

// LoadOAuth is Load for the one command that authenticates with OAuth 2.0
// rather than the app-key triple. It exists so that command's main() is shaped
// like the other eight (`cfg := u.LoadOAuth(o)`) instead of repeating the
// load-and-exit dance inline, which is exactly the kind of copy that drifts.
//
// The same Config comes back, so the command gets the same mode, dry-run state
// and banner — and can assert the read-only invariant against a configuration
// that was really loaded rather than one it assembled itself. The extra headers
// and the SDK log level travel with it, because they are properties of the
// configuration rather than of a particular credential.
func (u *Usage) LoadOAuth(o *oauth.OAuth) *appcfg.Config {
	cfg, err := appcfg.LoadOAuth(o, u.Config, u.loadOptions()...)
	if err != nil {
		Fail(err)
	}
	return cfg
}

// loadOptions is the one place the shared flags become loader options, so the two
// loaders cannot drift into applying them differently.
func (u *Usage) loadOptions() []appcfg.Option {
	return []appcfg.Option{
		appcfg.WithHeaders(u.headers...),
		appcfg.WithLogLevel(u.logLevel),
	}
}

// WithTimeout derives a request context bounded by the configured HTTP timeout.
func WithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, appcfg.Timeout())
}

// FmtTime renders a Unix timestamp (seconds) as local time, or "-" if unset.
func FmtTime(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// Dec renders an optional decimal at a fixed 2dp, or "-" when nil. Most SDK
// value fields are *decimal.Decimal precisely so that "absent" is
// distinguishable from zero, so a plain String() would print an empty cell.
func Dec(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(2)
}

// Dec4 is Dec at 4dp, for per-share and per-unit figures where 2dp loses
// meaningful precision (warrant premiums, greeks, conversion ratios).
func Dec4(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(4)
}

// Truncate shortens s to at most n runes, marking the cut with an ellipsis so
// a clipped cell is never mistaken for a complete one.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// OrDash returns s, or "-" when s is empty, for the many string fields the SDK
// models as plain strings with an absent-is-empty convention.
func OrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Section prints a top-level heading.
func Section(title string) { fmt.Printf("\n=== %s ===\n", title) }
