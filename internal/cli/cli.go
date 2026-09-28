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
	"syscall"
	"time"

	"github.com/shopspring/decimal"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// Fail prints a credential/configuration problem to stderr with usage and
// exits with status 2. Anything that is not a usage problem exits 1.
func Fail(err error) {
	if err == nil {
		return
	}
	var missing *appcfg.MissingCredentialError
	if errors.As(err, &missing) {
		fmt.Fprintf(os.Stderr, "error: %s\n", missing.Error())
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
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
}

// NewUsage creates a FlagSet named after the binary with a one-line summary
// and a trailing credential block that lists every supported variable.
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
		fmt.Fprintf(out, "\nSafety:\n")
		fmt.Fprintf(out, "  LONGPORT_DRY_RUN            1/true (default) blocks all order writes.\n")
		fmt.Fprintf(out, "  LONGPORT_MODE                simulated (default) or live.\n")
		fmt.Fprintf(out, "  LONGPORT_WATCHLIST_DRY_RUN   1/true (default) blocks watchlist writes\n")
		fmt.Fprintf(out, "                              (a separate gate; see cmd/watchlist).\n")
		fmt.Fprintf(out, "\nSee README.md for the simulated-account walkthrough.\n")
	}

	u := &Usage{FS: fs}
	fs.StringVar(&u.Config, "config", "", "path to a config.yaml holding credentials (default: auto-detect config.yaml)")
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
}

// Load validates configuration and renders the non-secret startup banner.
func (u *Usage) Load() *appcfg.Config {
	cfg, err := appcfg.Load(u.Config)
	if err != nil {
		Fail(err)
	}
	return cfg
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
