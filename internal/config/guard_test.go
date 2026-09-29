package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestBlockedf_FormatsAndCarriesTheSentinel(t *testing.T) {
	err := Blockedf("refusing to %s because %d", "buy AAPL", 3)
	if got, want := err.Error(), "refusing to buy AAPL because 3"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatal("every refusal must satisfy errors.Is(err, ErrBlocked)")
	}
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatal("Blockedf must produce a *BlockedError")
	}
	if be.Reason != err.Error() {
		t.Errorf("Reason = %q, Error() = %q; they must be the same string", be.Reason, err.Error())
	}
}

func TestBlockedError_UnwrapIsExactlyErrBlocked(t *testing.T) {
	// errors.Is works via the chain, but the contract here is stronger: the
	// sentinel is the single shared instance, so a caller can compare it
	// directly without allocating a comparison wrapper.
	e := &BlockedError{Reason: "nope"}
	if e.Unwrap() != ErrBlocked {
		t.Fatalf("Unwrap() = %v, want the ErrBlocked singleton", e.Unwrap())
	}
	if !errors.Is(e, ErrBlocked) {
		t.Fatal("errors.Is must be true")
	}
	if errors.Is(e, nil) {
		t.Fatal("a refusal must not match a nil target")
	}
}

func TestBlockedError_EmptyReasonIsStillARefusal(t *testing.T) {
	// An empty message is a cosmetic bug, not a permission slip: the error
	// must still be classified as blocked so the exit code stays 3.
	e := &BlockedError{}
	if e.Error() != "" {
		t.Fatalf("Error() = %q, want empty", e.Error())
	}
	if !errors.Is(error(e), ErrBlocked) {
		t.Fatal("even an empty refusal must wrap ErrBlocked")
	}
}

func TestBlockedError_SurvivesWrapping(t *testing.T) {
	// Commands add context with %w on the way up to cli.Fail; the sentinel has
	// to survive that or the exit code regresses to 1.
	inner := Blockedf("stop DCA plan %d", 42)
	wrapped := fmt.Errorf("dca stop: %w", inner)
	if !errors.Is(wrapped, ErrBlocked) {
		t.Fatal("the sentinel must survive an outer %w wrap")
	}
	if !strings.Contains(wrapped.Error(), "stop DCA plan 42") {
		t.Errorf("context and reason must both survive, got %q", wrapped)
	}
	var be *BlockedError
	if !errors.As(wrapped, &be) {
		t.Fatal("errors.As must still find the *BlockedError through the wrap")
	}
}

func TestExitBlocked_IsThree(t *testing.T) {
	// 0 is success, 1 is a generic failure, and 2 is missing credentials and
	// nothing else — a *MissingCredentialError. A usage error is NOT 2: the flag
	// package returns a plain *flag.error, which exits 1. A refusal must be
	// distinguishable from all three, or a wrapper script cannot tell "declined"
	// from "ordered".
	if ExitBlocked != 3 {
		t.Fatalf("ExitBlocked = %d, want 3", ExitBlocked)
	}
	if ExitBlocked == 0 || ExitBlocked == 1 || ExitBlocked == 2 {
		t.Fatal("ExitBlocked collides with a status that already means something else")
	}
}

func TestWatchlistDryRun_TruthTable(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		val  string
		want bool
		why  string
	}{
		{name: "unset defaults to blocking", set: false, want: true,
			why: "a watchlist edit must never happen without an explicit opt-in"},
		{name: "empty value fails safe", set: true, val: "", want: true},
		{name: "1 blocks", set: true, val: "1", want: true},
		{name: "true blocks", set: true, val: "true", want: true},
		{name: "0 clears", set: true, val: "0", want: false},
		{name: "false clears", set: true, val: "false", want: false},
		{name: "padded 0 is trimmed then cleared", set: true, val: " 0 ", want: false},
		{name: "unparseable yes fails safe", set: true, val: "yes", want: true},
		{name: "unparseable off fails safe", set: true, val: "off", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			setDryRunEnv(t, "LONGPORT_WATCHLIST_DRY_RUN", tt.val, tt.set)
			if got := WatchlistDryRun(); got != tt.want {
				t.Fatalf("WatchlistDryRun() = %v, want %v (set=%v val=%q): %s", got, tt.want, tt.set, tt.val, tt.why)
			}
		})
	}
}

func TestWatchlistDryRun_OnlyExactFalsyClearsIt(t *testing.T) {
	for _, v := range boolishValues {
		t.Run(fmt.Sprintf("value=%q", v), func(t *testing.T) {
			sandbox(t)
			t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", v)
			want := !clearsDryRun(v)
			if got := WatchlistDryRun(); got != want {
				t.Fatalf("WatchlistDryRun() = %v, want %v for %q", got, want, v)
			}
		})
	}
}

func TestValidateWatchlistDryRun(t *testing.T) {
	tests := []struct {
		name    string
		set     bool
		val     string
		wantErr bool
	}{
		{"unset is accepted", false, "", false},
		{"0 is accepted", true, "0", false},
		{"1 is accepted", true, "1", false},
		{"false is accepted", true, "false", false},
		{"padded 1 is accepted", true, " 1 ", false},
		{"empty value is rejected", true, "", true},
		{"yes is rejected", true, "yes", true},
		{"off is rejected", true, "off", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			setDryRunEnv(t, "LONGPORT_WATCHLIST_DRY_RUN", tt.val, tt.set)
			err := ValidateWatchlistDryRun()
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidateWatchlistDryRun() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				want := fmt.Sprintf("invalid LONGPORT_WATCHLIST_DRY_RUN=%q: want 1, true, 0 or false", tt.val)
				if err.Error() != want {
					t.Errorf("message = %q, want %q", err, want)
				}
			}
		})
	}
}

func TestGuardWatchlist_EmptyActionIsNotABlockedError(t *testing.T) {
	sandbox(t)
	unsetEnv(t, "LONGPORT_WATCHLIST_DRY_RUN")
	err := (&Config{}).GuardWatchlist("")
	if err == nil {
		t.Fatal("empty action must be rejected")
	}
	if errors.Is(err, ErrBlocked) {
		t.Error("a caller bug must not be reported as a safety refusal")
	}
	if got, want := err.Error(), "GuardWatchlist: action description is required"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// TestGuardWatchlist_IsIndependentOfModeAndDryRun is the property the whole
// guard exists for: the watchlist gate is governed by exactly one switch.
// Folding it into the order gate would either demand LONGPORT_MODE=live for a
// saved ticker list, or let a simulated account edit server state silently.
func TestGuardWatchlist_IsIndependentOfModeAndDryRun(t *testing.T) {
	for _, mode := range []Mode{ModeSimulated, ModeLive} {
		for _, cfgDryRun := range []bool{true, false} {
			for _, env := range []struct {
				name string
				val  string
				set  bool
			}{
				{"unset", "", false},
				{"1", "1", true},
				{"0", "0", true},
			} {
				t.Run(fmt.Sprintf("mode=%s/cfgDryRun=%v/watchlistEnv=%s", mode, cfgDryRun, env.name), func(t *testing.T) {
					sandbox(t)
					setDryRunEnv(t, "LONGPORT_WATCHLIST_DRY_RUN", env.val, env.set)
					cfg := newTestConfig(mode, cfgDryRun)

					err := cfg.GuardWatchlist("delete watchlist group 1")

					if env.name == "0" {
						if err != nil {
							t.Fatalf("the watchlist gate must be open here, got %v", err)
						}
						return
					}
					if err == nil {
						t.Fatal("SAFETY BUG: watchlist mutation must be blocked here")
					}
					if !errors.Is(err, ErrBlocked) {
						t.Fatalf("want ErrBlocked, got %v", err)
					}
				})
			}
		}
	}
}

func TestGuardWatchlist_IgnoresAnUnrecognisedMode(t *testing.T) {
	sandbox(t)
	t.Setenv("LONGPORT_WATCHLIST_DRY_RUN", "0")
	cfg := newTestConfig(Mode("live-ish"), true)
	if err := cfg.GuardWatchlist("delete watchlist group 1"); err != nil {
		t.Fatalf("with dry run cleared the gate is open regardless of mode, got %v", err)
	}
}

func TestGuardWatchlist_RefusalMessage(t *testing.T) {
	sandbox(t)
	unsetEnv(t, "LONGPORT_WATCHLIST_DRY_RUN")
	err := newTestConfig(ModeSimulated, true).GuardWatchlist("delete watchlist group 1")
	if err == nil {
		t.Fatal("want a refusal")
	}
	msg := err.Error()
	for _, want := range []string{
		"refusing to delete watchlist group 1",
		"watchlist DRY RUN is active",
		"No change was sent",
		"LONGPORT_WATCHLIST_DRY_RUN=0",
		"--confirm",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q\n---\n%s", want, msg)
		}
	}
	// The message must not drag the operator into live trading: the watchlist
	// endpoints do not move money.
	if strings.Contains(msg, "LONGPORT_MODE=live") {
		t.Errorf("watchlist refusal must not demand live mode\n---\n%s", msg)
	}
}

// TestGuardWrite_FourCellMatrix pins the order gate: only "dry run off AND
// live" lets an order through. Dry run on is blocked whatever the mode, and
// simulated is blocked even with dry run explicitly disabled.
func TestGuardWrite_FourCellMatrix(t *testing.T) {
	for _, mode := range []Mode{ModeSimulated, ModeLive} {
		for _, dryRun := range []bool{true, false} {
			t.Run(fmt.Sprintf("mode=%s/dryRun=%v", mode, dryRun), func(t *testing.T) {
				sandbox(t)
				cfg := newTestConfig(mode, dryRun)

				err := cfg.GuardWrite("submit buy 100 AAPL")

				if mode == ModeLive && !dryRun {
					if err != nil {
						t.Fatalf("the only open cell is live+dryRun=false, got %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("SAFETY BUG: this cell must block an order")
				}
				if !errors.Is(err, ErrBlocked) {
					t.Fatalf("want ErrBlocked, got %T: %v", err, err)
				}
				var be *BlockedError
				if !errors.As(err, &be) {
					t.Fatalf("want a *BlockedError, got %T", err)
				}
			})
		}
	}
}

func TestGuardWrite_EmptyActionIsNotABlockedError(t *testing.T) {
	sandbox(t)
	// Checked before the dry-run branch, so a caller bug is reported as a
	// caller bug even in the most-blocked configuration.
	err := newTestConfig(ModeSimulated, true).GuardWrite("")
	if err == nil {
		t.Fatal("empty action must be rejected")
	}
	if errors.Is(err, ErrBlocked) {
		t.Error("must not be reported as a safety refusal")
	}
	if got, want := err.Error(), "GuardWrite: action description is required"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestGuardWrite_RefusalMessages(t *testing.T) {
	t.Run("dry run branch names the mode", func(t *testing.T) {
		sandbox(t)
		for _, mode := range []Mode{ModeSimulated, ModeLive} {
			err := newTestConfig(mode, true).GuardWrite("submit buy 100 AAPL")
			if err == nil {
				t.Fatalf("mode=%s dryRun=true must block", mode)
			}
			msg := err.Error()
			for _, want := range []string{
				"refusing to submit buy 100 AAPL",
				"DRY RUN is active",
				"No order was sent",
				"LONGPORT_DRY_RUN=0",
				"Both are required",
				"mode=" + string(mode),
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("mode=%s: message is missing %q\n---\n%s", mode, want, msg)
				}
			}
		}
	})

	t.Run("simulated branch demands a mode change", func(t *testing.T) {
		sandbox(t)
		err := newTestConfig(ModeSimulated, false).GuardWrite("submit buy 100 AAPL")
		if err == nil {
			t.Fatal("dryRun=false in simulated mode must block")
		}
		msg := err.Error()
		for _, want := range []string{
			"mode is \"simulated\" but dry run is disabled",
			"No order was sent",
			"LONGPORT_MODE=live",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message is missing %q\n---\n%s", want, msg)
			}
		}
		if strings.Contains(msg, "DRY RUN is active") {
			t.Errorf("dry run is off in this branch, so the message must not blame it\n---\n%s", msg)
		}
	})
}

// TestGuardWrite_UnknownModeIsBlocked asserts the fail-closed rule: an order is
// allowed only when Mode is EXACTLY ModeLive. Every other value — the empty
// zero value, a typo, a different case, an invented mode — blocks.
//
// This is a deliberate design decision, not an accident of the current inputs,
// and it is asserted that way on purpose. The previous implementation refused
// only ModeSimulated, so anything else ("", "LIVE", "paper") fell through to
// "allow" and a hand-built or future Config could place a real order; that also
// disagreed with WriteGuard.Unsatisfied in this same package, which has always
// used the deny-unless-live form. If someone "simplifies" this back to
// `c.Mode == ModeSimulated`, this test fails — which is the point. Do not
// delete it to make a new mode work; add the mode to the switch and to
// loadModeAndDryRun instead.
func TestGuardWrite_UnknownModeIsBlocked(t *testing.T) {
	sandbox(t)
	for _, mode := range []Mode{
		Mode(""), Mode("live-ish"), Mode("LIVE"), Mode("Live"), Mode("paper"),
		Mode("simulated "), Mode(" live"), Mode("sandbox"),
	} {
		t.Run("mode="+fmt.Sprintf("%q", mode), func(t *testing.T) {
			err := newTestConfig(mode, false).GuardWrite("submit buy 100 AAPL")
			if err == nil {
				t.Fatalf("SAFETY BUG: mode %q with dry run off must be blocked, but the order was allowed", mode)
			}
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("want a refusal wrapping ErrBlocked, got %T: %v", err, err)
			}
			// The message has to say WHICH value was wrong, or the operator
			// cannot tell a typo from a deliberate setting.
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", mode)) {
				t.Errorf("message must quote the offending mode %q:\n%s", mode, err)
			}
			if !strings.Contains(err.Error(), "LONGPORT_MODE=live") {
				t.Errorf("message must name the one value that opens the gate:\n%s", err)
			}
		})
	}
}

// TestGuardWrite_DefaultDeniesAnUnrecognisedMode is the assertion the
// WriteGuard gate does satisfy, and the one GuardWrite ought to as well.
func TestWriteGuard_UnsatisfiedAlreadyDeniesUnknownModes(t *testing.T) {
	sandbox(t)
	t.Setenv(DCAGuard.DryRunEnv, "0")
	for _, mode := range []Mode{Mode(""), Mode("live-ish"), Mode("LIVE"), Mode("paper"), Mode("simulated ")} {
		if err := DCAGuard.Check(newTestConfig(mode, false), true, "pause DCA plan 1"); err == nil {
			t.Errorf("WriteGuard.Check must deny mode %q", mode)
		}
	}
}
