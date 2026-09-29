package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ExitBlocked is the process exit status used when a safety guard refuses a
// write. It is the ONLY status that means "your write did not happen".
//
// # WHY 3
//
// 0 and 1 are already taken: 0 is success, 1 is a generic failure — which
// includes a usage error such as an unknown flag, because the flag package
// returns a plain *flag.error that wraps neither sentinel — and 2 is the
// missing-credentials status, reserved for a *MissingCredentialError and
// nothing else (see cli.Fail). A refusal is none of those: the command did not
// fail, it correctly declined to act. Before this constant existed, blocked
// writes returned nil and therefore exited 0, which made a refusal
// indistinguishable from a completed order to any wrapper script. 3 is also the
// convention already used by the sibling Tiger project, so the two repos agree.
//
// Any command that guards a write MUST return an error wrapping ErrBlocked
// when the guard refuses, so that cli.Fail turns it into this status.
const ExitBlocked = 3

// ErrBlocked is the sentinel that every guard refusal wraps. Call it with
// Blockedf, which produces both the human-readable reason and the sentinel.
var ErrBlocked = errors.New("blocked by safety guard")

// BlockedError is a guard refusal: a write that was deliberately not sent.
type BlockedError struct {
	Reason string
}

func (e *BlockedError) Error() string { return e.Reason }

// Unwrap ties every refusal to ErrBlocked, so a caller can use errors.Is
// instead of matching on message text.
func (e *BlockedError) Unwrap() error { return ErrBlocked }

// Blockedf builds a guard refusal. The caller must treat the result as
// "make no network call" and propagate it rather than swallowing it.
func Blockedf(format string, args ...any) error {
	return &BlockedError{Reason: fmt.Sprintf(format, args...)}
}

// GuardWatchlist is the safety gate for watchlist MUTATIONS.
//
// # WHY THIS IS SEPARATE FROM GuardWrite
//
// GuardWrite governs orders, and it deliberately requires LONGPORT_MODE=live
// because "live" means real money. The watchlist endpoints
// (CreateWatchlistGroup, DeleteWatchlistGroup, UpdateWatchlistGroup,
// UpdatePinned) do not move money, so folding them into the order gate would
// mean either (a) letting a simulated account edit its watchlist without
// opting in, which is needlessly annoying, or (b) labelling a harmless
// preference change as "live trading", which is dishonest. They are neither
// orders nor reads: they mutate server-side state belonging to the account
// and are not idempotent, so they need a real guard of their own.
//
// # THE CONTRACT
//
// A watchlist mutation proceeds only when BOTH hold:
//
//  1. LONGPORT_WATCHLIST_DRY_RUN=0 (or false), and
//  2. the caller passed --confirm (checked by the command, not here)
//
// Dry run is ON by default and, unlike the order gate, is NOT influenced by
// LONGPORT_MODE. A refusal here makes no network call at all: the caller must
// treat any error from this function as "do not call the SDK".
//
// The trade-off is deliberate: a user who never touches LONGPORT_WATCHLIST_*
// can never mutate a watchlist by accident, but a user who does opt in can do
// so regardless of which account type they are on.
func (c *Config) GuardWatchlist(action string) error {
	if action == "" {
		return errors.New("GuardWatchlist: action description is required")
	}
	if WatchlistDryRun() {
		return Blockedf(
			"refusing to %s: watchlist DRY RUN is active.\n"+
				"No change was sent. Watchlist writes mutate your account's\n"+
				"saved groups, so they are guarded separately from orders.\n"+
				"To allow them set\n"+
				"  LONGPORT_WATCHLIST_DRY_RUN=0\n"+
				"and pass --confirm. Both are required.",
			action)
	}
	return nil
}

// WatchlistDryRun reports whether watchlist mutations are blocked. It is
// exported so that commands can print the effective state in their banner
// without duplicating the lookup.
func WatchlistDryRun() bool {
	v, ok := os.LookupEnv("LONGPORT_WATCHLIST_DRY_RUN")
	if !ok {
		return true
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		// An unparseable value must fail safe: block, do not guess.
		return true
	}
	return b
}

// ValidateWatchlistDryRun reports a bad LONGPORT_WATCHLIST_DRY_RUN value as a
// startup error rather than silently treating it as "on". Commands call this
// during config load so the operator finds out at once rather than at the
// moment a write is silently blocked.
func ValidateWatchlistDryRun() error {
	v, ok := os.LookupEnv("LONGPORT_WATCHLIST_DRY_RUN")
	if !ok {
		return nil
	}
	if _, err := strconv.ParseBool(strings.TrimSpace(v)); err != nil {
		return fmt.Errorf(
			"invalid LONGPORT_WATCHLIST_DRY_RUN=%q: want 1, true, 0 or false", v)
	}
	return nil
}
