package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/quote"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// quote.WatchlistUpdateMode is a string enum, so a wrong mapping is not a wrong
// number but a wrong HTTP body field: "replace" sent where "add" was asked for
// silently deletes the symbols the user did not name. Each accepted word is
// pinned to its own SDK constant and the constants' own values are pinned too.

// The SDK constants are themselves the words, so a swap in the switch is
// invisible to a "no error" assertion. Asserted against the constants, not
// against string literals.
func TestParseUpdateMode_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want quote.WatchlistUpdateMode
	}{
		{"add", quote.AddWatchlist},
		{"remove", quote.RemoveWatchlist},
		{"replace", quote.ReplaceWatchlist},
		// ToLower+TrimSpace.
		{"ADD", quote.AddWatchlist},
		{"Remove", quote.RemoveWatchlist},
		{"REPLACE", quote.ReplaceWatchlist},
		{"  add  ", quote.AddWatchlist},
		{"\tremove\n", quote.RemoveWatchlist},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseUpdateMode(tt.in)
			if err != nil {
				t.Fatalf("parseUpdateMode(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseUpdateMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The three constants must be mutually distinct, otherwise two of the three
// switch cases could be swapped and every test above would still pass.
func TestParseUpdateMode_TheSDKConstantsAreDistinct(t *testing.T) {
	seen := map[quote.WatchlistUpdateMode]string{}
	for in, want := range map[string]quote.WatchlistUpdateMode{
		"add": quote.AddWatchlist, "remove": quote.RemoveWatchlist, "replace": quote.ReplaceWatchlist,
	} {
		if want == "" {
			t.Fatalf("the SDK constant for %q is the empty string, so an unknown value would look valid", in)
		}
		if prev, dup := seen[want]; dup {
			t.Errorf("%q and %q both map to %q", prev, in, want)
		}
		seen[want] = in
	}
	// The wire values are the plain lowercase words.
	if quote.AddWatchlist != "add" || quote.RemoveWatchlist != "remove" || quote.ReplaceWatchlist != "replace" {
		t.Errorf("SDK wire values changed: add=%q remove=%q replace=%q",
			quote.AddWatchlist, quote.RemoveWatchlist, quote.ReplaceWatchlist)
	}
}

func TestParseUpdateMode_UnknownValueIsAnErrorNotAnEmptyMode(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string; the flag default is \"add\", so an empty value is a mistake"},
		{"   ", "whitespace only"},
		{"insert", "a plausible synonym"},
		{"delete", "what -action delete does, but not an update mode"},
		{"append", "a plausible synonym"},
		{"subtract", "a plausible synonym"},
		{"addremove", "concatenation of two valid words"},
		{"replaced", "past tense"},
		{"remove_all", "underscore form"},
		{"overwrite", "a plausible synonym"},
		{"none", "a word the SDK does not define"},
		{"0", "numeric id where a word belongs"},
		{"1", "numeric id where a word belongs"},
		// Words that are valid in other commands of this repo must not be
		// accepted here: -pin-mode uses "add"/"remove", not "replace".
		{"pin", "a -pin-mode value"},
		{"unpin", "a -pin-mode value"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseUpdateMode(tt.in)
			if err == nil {
				t.Fatalf("parseUpdateMode(%q) = %q with no error, want an error", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseUpdateMode(%q) = %q alongside the error, want the zero value \"\"", tt.in, got)
			}
		})
	}
}

// "-pin-mode" is a separate switch in the same file with a different vocabulary.
// A value valid there must be rejected here and vice versa, so the two can
// never be merged by accident.
func TestParseUpdateMode_PinModeVocabularyIsNotUpdateModeVocabulary(t *testing.T) {
	for _, in := range []string{"pin", "unpin"} {
		if got, err := parseUpdateMode(in); err == nil {
			t.Errorf("parseUpdateMode(%q) = %q with no error, want an error", in, got)
		}
	}
	// "add" and "remove" are shared, which is why the pin switch and this one
	// are separate functions rather than one.
	for _, in := range []string{"add", "remove"} {
		if _, err := parseUpdateMode(in); err != nil {
			t.Errorf("parseUpdateMode(%q) = error %v, want it accepted", in, err)
		}
	}
}

// ------------------------------------------------------------------- gate
//
// The four write actions in this binary mutate server-side state belonging to
// the account, and the package doc makes one promise about them: a refused
// write makes NO network request at all, because the quote context is only
// created after the gate passes. That promise is the whole reason each write
// function takes a `connect` function value — the SDK offers no seam to fake a
// *quote.QuoteContext — so every assertion below is built on whether connect
// was reached, and the error text alone could not tell a refusal from a dial.
//
// The gate has two independent refusals, and both are pinned: the package-level
// `confirm` var (--confirm), and GuardWatchlist, which reads
// LONGPORT_WATCHLIST_DRY_RUN. Each test asserts the exact refusal — a
// *config.BlockedError, so the process exits 3 rather than 0 — and "no error" is
// never the assertion.

// captureStderr redirects os.Stderr for the duration of fn and returns what was
// written to it. A temp file rather than a pipe: a refusal block is larger than
// a pipe buffer, and a blocked write with nobody draining the pipe would
// deadlock the test instead of failing it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q): %v", path, err)
	}
	orig := os.Stderr
	os.Stderr = f
	fn()
	os.Stderr = orig
	if err := f.Close(); err != nil {
		t.Fatalf("closing captured stderr: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return string(b)
}

// captureStdout is captureStderr for the dry-run request preview.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q): %v", path, err)
	}
	orig := os.Stdout
	os.Stdout = f
	fn()
	os.Stdout = orig
	if err := f.Close(); err != nil {
		t.Fatalf("closing captured stdout: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(b)
}

// unsetEnv removes a variable for the duration of the test and puts back
// whatever was there. t.Setenv cannot express "unset", and that is the state
// that matters: WatchlistDryRun defaults to dry run when the variable is
// ABSENT, which is the case an operator who has never heard of it is in.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	prev, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if wasSet {
			os.Setenv(key, prev)
			return
		}
		os.Unsetenv(key)
	})
}

// watchlistState is the package-level flag set the write paths read. main()
// never runs in a test, so whatever is installed here is the only state the
// write paths ever see, and a test that forgets a field is testing a zero value
// it did not mean to — groupID 0 means "missing -group-id", not "group zero".
type watchlistState struct {
	action     string
	groupName  string
	groupID    int64
	symbols    string
	updateMode string
	pinMode    string
	purge      bool
	confirm    bool
}

// setWatchlistFlags installs a complete flag set and restores the previous values
// when the test ends, so tests stay independent under -shuffle.
//
// Two of these fields are hazards rather than conveniences. confirm is read by
// gate() itself, so a leaked true would let every later test sail past the first
// branch and quietly end up testing GuardWatchlist instead; and timeout is a
// package var that main() overwrites from appcfg.Timeout() but a test never
// does, so without the line below every write path would build a
// context.WithTimeout(ctx, 0) — already expired.
func setWatchlistFlags(t *testing.T, s watchlistState) {
	t.Helper()
	prev := watchlistState{action, groupName, groupID, symbols, updateMode, pinMode, purge, confirm}
	prevTimeout := timeout
	action, groupName, groupID = s.action, s.groupName, s.groupID
	symbols, updateMode, pinMode = s.symbols, s.updateMode, s.pinMode
	purge, confirm = s.purge, s.confirm
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		action, groupName, groupID = prev.action, prev.groupName, prev.groupID
		symbols, updateMode, pinMode = prev.symbols, prev.updateMode, prev.pinMode
		purge, confirm = prev.purge, prev.confirm
		timeout = prevTimeout
	})
}

// validWatchlistState is the flag set main() would produce for a plain
// `-action create -name Tech -symbols 700.HK,9988.HK -group-id 12345` run.
// --confirm is left false because that is the flag's own default, so a blocked
// test is testing the state a user gets without asking for the gate.
func validWatchlistState() watchlistState {
	return watchlistState{
		action:     "create",
		groupName:  "Tech",
		groupID:    12345,
		symbols:    "700.HK,9988.HK",
		updateMode: "add",
		pinMode:    "add",
		purge:      false,
		confirm:    false,
	}
}

func validWatchlistFlags(t *testing.T) {
	t.Helper()
	setWatchlistFlags(t, validWatchlistState())
}

// optedInFlags is the same set with --confirm passed. Together with
// LONGPORT_WATCHLIST_DRY_RUN=0 that is the only combination entitled to reach
// the SDK, and these are the flags the tests that prove the gate can open use.
func optedInFlags(t *testing.T) {
	t.Helper()
	s := validWatchlistState()
	s.confirm = true
	setWatchlistFlags(t, s)
}

// blockedCfg is a Config that carries no credentials, so nothing in a test can
// reach the network through it. Mode and DryRun are deliberately the strictest
// pair: GuardWatchlist ignores both (see the truth table below), so this is the
// cell that would look wrong if the gate ever started consulting them.
func blockedCfg() *appcfg.Config {
	return &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
}

// isBlocked reports whether an error is a guard refusal rather than an ordinary
// failure. The distinction is the whole point of the gate: 3 means "declined to
// act", 1 means "tried and broke".
func isBlocked(err error) bool { return errors.Is(err, appcfg.ErrBlocked) }

// failConnect records an attempt to build an SDK context and hands back
// context.Canceled, so a write path that ignores the gate fails with an error
// that is neither a refusal nor a success. Any call to it while the gate is shut
// is a test failure: a blocked write that dials the API is not a refusal.
type failConnect struct{ calls int }

func (c *failConnect) connect() (*quote.QuoteContext, error) {
	c.calls++
	return nil, context.Canceled
}

func (c *failConnect) reached() bool { return c.calls > 0 }

// gatedWrite is one of the four mutations, with the SDK method its dry-run
// preview must name and the action description its refusal must quote.
//
// wantAction is pinned per function because that description is the only thing
// that tells the user which of the four stopped them: a copy-paste that handed
// doUpdate doDelete's wording would produce an identical-looking refusal and an
// identical exit status, and nothing else would show it.
type gatedWrite struct {
	name       string
	run        func(context.Context, *appcfg.Config, func() (*quote.QuoteContext, error)) error
	wantSDK    string
	wantAction string
}

func gatedWrites() []gatedWrite {
	return []gatedWrite{
		{"create", doCreate, "CreateWatchlistGroup", "create a watchlist group"},
		{"delete", doDelete, "DeleteWatchlistGroup", "delete watchlist group 12345"},
		{"update", doUpdate, "UpdateWatchlistGroup", "update watchlist group 12345"},
		{"pin", doPin, "UpdatePinned", "pin or unpin symbols"},
	}
}

// writeNamed joins a table that names a mutation to the entry describing it, and
// fails loudly rather than running a zero value that would call a nil function.
func writeNamed(t *testing.T, name string) gatedWrite {
	t.Helper()
	for _, w := range gatedWrites() {
		if w.name == name {
			return w
		}
	}
	t.Fatalf("no gated write named %q", name)
	return gatedWrite{}
}

// attempt is one captured run of a write function: the preview it printed, what
// the gate said on stderr, and how many times it tried to build a client.
type attempt struct {
	err     error
	preview string
	stderr  string
	calls   int
}

func (a attempt) reached() bool { return a.calls > 0 }

// try runs w with both streams captured. stderr is captured inside stdout
// because describe() prints the request preview to stdout and the gate prints
// its refusal to stderr; one call has to produce both.
func try(t *testing.T, w gatedWrite, cfg *appcfg.Config) attempt {
	t.Helper()
	c := &failConnect{}
	var a attempt
	a.preview = captureStdout(t, func() {
		a.stderr = captureStderr(t, func() {
			a.err = w.run(context.Background(), cfg, c.connect)
		})
	})
	a.calls = c.calls
	return a
}

// mustBeRefused is the load-bearing assertion, shared so all four mutations are
// held to the identical standard: a refusal is a *config.BlockedError (the
// process exits 3, not 0), it is explained on stderr, and it never dialled the
// API. The last is the one that matters most — the returned error would look the
// same whether or not a request went out.
func (a attempt) mustBeRefused(t *testing.T, w gatedWrite) {
	t.Helper()
	if a.err == nil {
		t.Fatalf("%s() = nil; a refused watchlist write would exit 0 and be "+
			"indistinguishable from one that reached Longbridge", w.name)
	}
	if !isBlocked(a.err) {
		t.Errorf("%s() = %v, want a *config.BlockedError so the exit status is %d",
			w.name, a.err, appcfg.ExitBlocked)
	}
	if a.reached() {
		t.Errorf("%s() called connect() %d time(s) while blocked; a blocked write that "+
			"dials the API is not a refusal", w.name, a.calls)
	}
	for _, want := range []string{"BLOCKED", "DRY RUN: nothing was sent to Longbridge."} {
		if !strings.Contains(a.stderr, want) {
			t.Errorf("%s() refusal is missing %q; a silent no-op looks like a crash.\nrefusal was:\n%s",
				w.name, want, a.stderr)
		}
	}
}

// The core of the file. Each of the four mutations, with the gate shut, must
// return a *config.BlockedError, must explain itself on stderr, and must never
// have called connect(). LONGPORT_WATCHLIST_DRY_RUN is pinned rather than
// inherited so a developer shell cannot open the gate underneath a test.
func TestGatedWrites_EveryMutationIsRefusedByTheGateWithNoNetworkCall(t *testing.T) {
	for _, w := range gatedWrites() {
		t.Run(w.name, func(t *testing.T) {
			validWatchlistFlags(t)
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "1")
			a := try(t, w, blockedCfg())
			a.mustBeRefused(t, w)
			// The preview is printed BEFORE the gate on purpose, so that a dry
			// run is informative rather than just a refusal — and it must name
			// the SDK method this function is wired to, so a dispatch that sent
			// a delete where an update was asked for shows up here.
			if !strings.Contains(a.preview, w.wantSDK) {
				t.Errorf("%s() preview does not name %q.\npreview was:\n%s",
					w.name, w.wantSDK, a.preview)
			}
		})
	}
}

// The first refusal branch: the --confirm var. GuardWatchlist is configured to
// authorise the write, so the only thing that can stop it is the flag check —
// and dropping that check, or making it read the parameter instead of the
// package var, is what these subtests exist to catch.
func TestGatedWrites_AMissingConfirmIsRefusedEvenWhenTheGuardWouldAllow(t *testing.T) {
	for _, w := range gatedWrites() {
		t.Run(w.name, func(t *testing.T) {
			validWatchlistFlags(t) // confirm is false
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "0")
			a := try(t, w, &appcfg.Config{Mode: appcfg.ModeLive, DryRun: false})
			a.mustBeRefused(t, w)
			if !strings.Contains(a.stderr, "missing --confirm") {
				t.Errorf("%s() refusal does not name the flag that is missing.\nrefusal was:\n%s",
					w.name, a.stderr)
			}
			// The refusal must not quote GuardWatchlist: that branch was never
			// reached, and blaming a variable the user did not set sends them
			// to edit the wrong one.
			if strings.Contains(a.stderr, "refusing to") {
				t.Errorf("%s() blamed the guard for a missing --confirm.\nrefusal was:\n%s",
					w.name, a.stderr)
			}
			for _, want := range []string{"LONGPORT_WATCHLIST_DRY_RUN=0", "guarded separately from orders"} {
				if !strings.Contains(a.stderr, want) {
					t.Errorf("%s() refusal is missing %q; both switches have to be named.\nrefusal was:\n%s",
						w.name, want, a.stderr)
				}
			}
		})
	}
}

// The second refusal branch: --confirm is passed and GuardWatchlist still says
// no. The refusal must quote this function's own action description — the one
// thing that tells the user which of the four was stopped — and must still make
// no request.
func TestGatedWrites_GuardWatchlistRefusalNamesTheActionAndStillMakesNoRequest(t *testing.T) {
	for _, w := range gatedWrites() {
		t.Run(w.name, func(t *testing.T) {
			optedInFlags(t)
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "1")
			a := try(t, w, blockedCfg())
			a.mustBeRefused(t, w)
			if !strings.Contains(a.stderr, w.wantAction) {
				t.Errorf("%s() refusal does not name the action it refused (%q), so the user "+
					"cannot tell which of the four was stopped.\nrefusal was:\n%s",
					w.name, w.wantAction, a.stderr)
			}
			if !strings.Contains(a.stderr, "LONGPORT_WATCHLIST_DRY_RUN=0") {
				t.Errorf("%s() refusal does not name the variable to change.\nrefusal was:\n%s",
					w.name, a.stderr)
			}
			// The watchlist endpoints do not move money, so the refusal must not
			// drag the operator into live trading to satisfy a gate that has
			// nothing to do with orders.
			if strings.Contains(a.stderr, "LONGPORT_MODE=live") {
				t.Errorf("%s() refusal demands live mode; this gate never consults it.\nrefusal was:\n%s",
					w.name, a.stderr)
			}
		})
	}
}

// The gate's truth table at the command level: {--confirm} ×
// {LONGPORT_WATCHLIST_DRY_RUN} × {cfg.Mode/DryRun}. The third column is here
// precisely because it must make no difference — GuardWatchlist is documented as
// independent of Mode, so each cell has to agree with the cell above it, and a
// gate that started demanding ModeLive would break the property this file
// depends on without failing any single-cell test.
//
// So of the 8 cells exactly one COMBINATION OF SWITCHES opens, and it opens
// twice — once per row of the third column. The tally at the end is what makes
// that literal: a gate that refused everything would pass every other test in
// this file, and a gate keyed on Mode as well would open four. The open cells
// are asserted as "opened AND printed no refusal", never as a bare nil error.
func TestGate_OnlyTheConfirmedAndDryRunOffCellOpensTheWatchlistGate(t *testing.T) {
	opens := 0
	cfgs := []struct {
		name string
		cfg  *appcfg.Config
	}{
		{"simulated+dryRun", &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}},
		{"live+dryRun=false", &appcfg.Config{Mode: appcfg.ModeLive, DryRun: false}},
	}
	for _, confirmOn := range []bool{false, true} {
		for _, env := range []string{"1", "0"} {
			for _, c := range cfgs {
				t.Run(fmt.Sprintf("confirm=%v/env=%s/%s", confirmOn, env, c.name), func(t *testing.T) {
					setWatchlistFlags(t, watchlistState{confirm: confirmOn})
					t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", env)
					var err error
					refusal := captureStderr(t, func() { err = gate(c.cfg, "create a watchlist group") })

					// The verdict is a function of the two switches alone, so
					// this expression is deliberately the same in both columns
					// of the table.
					wantOpen := confirmOn && env == "0"
					if wantOpen {
						opens++
						if err != nil {
							t.Fatalf("confirm with LONGPORT_WATCHLIST_DRY_RUN=0 must open the "+
								"gate whatever the Config says, got %v", err)
						}
						if strings.Contains(refusal, "BLOCKED") {
							t.Errorf("gate() opened but still printed a refusal:\n%s", refusal)
						}
						return
					}
					if err == nil {
						t.Fatal("SAFETY BUG: this cell must refuse a watchlist mutation")
					}
					if !isBlocked(err) {
						t.Errorf("gate() = %v, want a *config.BlockedError", err)
					}
					if !strings.Contains(refusal, "DRY RUN: nothing was sent to Longbridge.") {
						t.Errorf("refusal is missing the DRY RUN line.\nrefusal was:\n%s", refusal)
					}
				})
			}
		}
	}
	if opens != len(cfgs) {
		t.Errorf("%d of the 8 cells opened the gate, want %d: the two refusals are independent, so "+
			"only --confirm together with LONGPORT_WATCHLIST_DRY_RUN=0 may write, and cfg.Mode must "+
			"not add a condition of its own", opens, len(cfgs))
	}
}

// Dry run is ON when the variable is absent, which is the state of every user
// who has never heard of LONGPORT_WATCHLIST_*. Even with --confirm passed, that
// operator must be refused — this is the "cannot mutate by accident" half of
// the gate's contract, and it is invisible to a test that always sets the var.
func TestGate_WithNoWatchlistEnvSetTheDefaultIsStillARefusal(t *testing.T) {
	unsetEnv(t, "LONGPORT_WATCHLIST_DRY_RUN")
	optedInFlags(t)
	if !appcfg.WatchlistDryRun() {
		t.Fatal("WatchlistDryRun() = false with the variable unset; the default is dry run")
	}
	var err error
	refusal := captureStderr(t, func() {
		err = gate(&appcfg.Config{Mode: appcfg.ModeLive, DryRun: false}, "delete watchlist group 12345")
	})
	if err == nil || !isBlocked(err) {
		t.Fatalf("gate() with --confirm and no env var = %v, want a *config.BlockedError", err)
	}
	if !strings.Contains(refusal, "DRY RUN: nothing was sent to Longbridge.") {
		t.Errorf("refusal is missing the DRY RUN line.\nrefusal was:\n%s", refusal)
	}
}

// The connect seam is live, so "connect was never called" is not vacuous: with
// both switches passed each of the four MUST reach connect() exactly once and
// then fail with the fake's error — a plain context.Canceled, not a refusal,
// which also distinguishes "the gate let it through" from "the function bailed
// out early for some other reason".
func TestGatedWrites_OnlyAFullyOptedInRunReachesTheSDK(t *testing.T) {
	open := &appcfg.Config{Mode: appcfg.ModeLive, DryRun: false}
	for _, w := range gatedWrites() {
		t.Run(w.name, func(t *testing.T) {
			optedInFlags(t)
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "0")
			a := try(t, w, open)
			if !a.reached() {
				t.Fatalf("%s() did not call connect() with --confirm and LONGPORT_WATCHLIST_DRY_RUN=0; "+
					"the gate is refusing a write it should have allowed", w.name)
			}
			if a.calls != 1 {
				t.Errorf("%s() called connect() %d time(s), want exactly 1", w.name, a.calls)
			}
			if a.err == nil {
				t.Fatalf("%s() = nil although the fake connect failed", w.name)
			}
			if isBlocked(a.err) {
				t.Errorf("%s() = %v, a refusal, with the gate open", w.name, a.err)
			}
			if !errors.Is(a.err, context.Canceled) {
				t.Errorf("%s() = %v, want the fake connect's context.Canceled", w.name, a.err)
			}
			if strings.Contains(a.stderr, "BLOCKED") {
				t.Errorf("%s() printed a refusal with the gate open:\n%s", w.name, a.stderr)
			}
		})
	}
}

// A bad flag is reported as a bad flag even while the gate is shut: a BLOCKED
// message names the switches and not the thing the user actually got wrong. The
// preview must be absent too, so nothing claims a request would be sent for
// input that cannot be sent.
func TestGatedWrites_AMissingOrBadFlagIsAFlagErrorNotAGateRefusal(t *testing.T) {
	tests := []struct {
		name    string
		write   string
		mutate  func()
		wantErr string
	}{
		{"create without -name", "create", func() { groupName = "" }, "-name"},
		{"delete without -group-id", "delete", func() { groupID = 0 }, "-group-id"},
		{"update without -group-id", "update", func() { groupID = 0 }, "-group-id"},
		{"update with an unknown -update-mode", "update", func() { updateMode = "insert" }, "-update-mode"},
		{"pin without -symbols", "pin", func() { symbols = "" }, "-symbols"},
		{"pin with a whitespace-only -symbols", "pin", func() { symbols = " , " }, "-symbols"},
		{"pin with an unknown -pin-mode", "pin", func() { pinMode = "sideways" }, "-pin-mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validWatchlistFlags(t)
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "0")
			tt.mutate()
			w := writeNamed(t, tt.write)
			a := try(t, w, blockedCfg())
			if a.err == nil {
				t.Fatalf("%s: %s() = nil; the input is unusable", tt.name, w.name)
			}
			if isBlocked(a.err) {
				t.Errorf("%s: %s() = %v, a gate refusal; want the flag error, because the "+
					"refusal names switches and not the flag that is wrong", tt.name, w.name, a.err)
			}
			if !strings.Contains(a.err.Error(), tt.wantErr) {
				t.Errorf("%s: error %q does not name %q", tt.name, a.err, tt.wantErr)
			}
			if a.reached() {
				t.Errorf("%s: %s() called connect() %d time(s) during flag validation",
					tt.name, w.name, a.calls)
			}
			if strings.Contains(a.preview, w.wantSDK) {
				t.Errorf("%s: the dry-run preview was printed for input that cannot be sent.\npreview was:\n%s",
					tt.name, a.preview)
			}
		})
	}
}

// The two hazards of driving a command from its package vars, asserted rather
// than assumed: confirm is read by gate() itself, so a test that leaves it true
// silently converts every later test into a GuardWatchlist test, and timeout is
// zero in a test but non-zero in main(), so a path that reached
// context.WithTimeout would be handed an already-expired context.
func TestSetWatchlistFlags_ConfirmAndTimeoutAreRestoredWhenTheTestEnds(t *testing.T) {
	origConfirm, origTimeout := confirm, timeout
	t.Cleanup(func() { confirm, timeout = origConfirm, origTimeout })
	confirm, timeout = true, 7*time.Second

	for _, want := range []bool{true, false} {
		t.Run(fmt.Sprintf("confirm=%v", want), func(t *testing.T) {
			setWatchlistFlags(t, watchlistState{confirm: want})
			if confirm != want {
				t.Errorf("confirm = %v inside the test, want %v", confirm, want)
			}
			if timeout != 50*time.Millisecond {
				t.Errorf("timeout = %v inside the test, want the non-zero value main() would set", timeout)
			}
		})
		// The subtest has finished, so its cleanups have run.
		if !confirm {
			t.Errorf("confirm = %v after a subtest that set it to %v; a leaked true would let "+
				"every later test sail past the first branch of the gate", confirm, want)
		}
		if timeout != 7*time.Second {
			t.Errorf("timeout = %v after a subtest, want the value that was there before it", timeout)
		}
	}

	// The blocked tests rely on the valid set leaving --confirm at its flag
	// default. If that ever changed, every refusal above would be coming from
	// the wrong branch and none of them would say so.
	validWatchlistFlags(t)
	if confirm {
		t.Error("validWatchlistFlags installed --confirm; the blocked tests would be " +
			"exercising GuardWatchlist instead of the flag check")
	}
}

// gate must not turn a caller bug into a safety refusal: GuardWatchlist rejects
// an empty action description with a plain error, and that has to arrive
// unchanged. Reporting it as a BlockedError would claim the user's write was
// deliberately declined when in fact the command passed the gate a blank string.
func TestGate_AnEmptyActionDescriptionIsNotASafetyRefusal(t *testing.T) {
	optedInFlags(t)
	t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "0")
	var err error
	_ = captureStderr(t, func() { err = gate(blockedCfg(), "") })
	if err == nil {
		t.Fatal("gate(cfg, \"\") = nil; GuardWatchlist requires an action description")
	}
	if isBlocked(err) {
		t.Errorf("gate(cfg, \"\") = %v, a safety refusal; a blank action is a caller bug", err)
	}
	// Passed through unchanged, so the message still says what is wrong with it.
	if !strings.Contains(err.Error(), "action description is required") {
		t.Errorf("gate() reworded the guard's own error as %q", err)
	}
}
