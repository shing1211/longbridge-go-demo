// Command watchlist reads and, behind its own safety gate, edits the user's
// saved watchlist groups.
//
// # SAFETY — READ THIS BEFORE THE WRITE ACTIONS
//
// WatchedGroups is a pure read. The other four actions (create, delete,
// update, pin) MUTATE SERVER-SIDE STATE on the account: they add and remove
// saved symbol groups. They are not orders and they move no money, so they
// deliberately do NOT go through the order gate in internal/config.GuardWrite —
// that gate requires LONGPORT_MODE=live, which would be a misleading label for
// a cosmetic preference.
//
// Instead they sit behind GuardWatchlist (internal/config/guard.go), which
// requires ALL of:
//
//  1. LONGPORT_WATCHLIST_DRY_RUN=0 (or false), and
//  2. --confirm passed on the command line
//
// Dry run is ON by default. A blocked write prints the exact intended call and
// makes NO network request at all — the quote context is only created after
// the guard passes, so "dry" really does mean no round trip.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/quote"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action     string
	groupName  string
	groupID    int64
	symbols    string
	updateMode string
	pinMode    string
	purge      bool
	confirm    bool
	timeout    time.Duration
)

func main() {
	u := cli.NewUsage("watchlist", "read watchlist groups; gated edits of saved groups")
	u.FS.StringVar(&action, "action", "list",
		"list (read-only) | create | delete | update | pin")
	u.FS.StringVar(&groupName, "name", "", "group name (required for create)")
	u.FS.Int64Var(&groupID, "group-id", 0, "group id (required for update, delete and pin)")
	u.FS.StringVar(&symbols, "symbols", "", "comma-separated symbols (required for create, optional for update)")
	u.FS.StringVar(&updateMode, "update-mode", "add",
		"for -action update: add, remove or replace")
	u.FS.StringVar(&pinMode, "pin-mode", "add", "for -action pin: add or remove")
	u.FS.BoolVar(&purge, "purge", false,
		"for -action delete: also delete the group's symbols (otherwise keep them in the default list)")
	u.FS.BoolVar(&confirm, "confirm", false,
		"REQUIRED acknowledgement for any watchlist write; must be combined with LONGPORT_WATCHLIST_DRY_RUN=0")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	fmt.Fprintf(os.Stderr, "[config] watchlist_dry_run=%v (separate gate: LONGPORT_WATCHLIST_DRY_RUN + --confirm)\n",
		appcfg.WatchlistDryRun())

	cli.Run(func(ctx context.Context) error {
		// Lazily created, for the same reason the trade binary does it: a
		// blocked write must not authenticate or open a connection.
		var qc *quote.QuoteContext
		defer func() {
			if qc != nil {
				if err := qc.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "warning: closing quote context: %v\n", err)
				}
			}
		}()
		connect := func() (*quote.QuoteContext, error) {
			if qc != nil {
				return qc, nil
			}
			c, err := quote.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating quote context: %w", err)
			}
			qc = c
			return qc, nil
		}

		switch strings.ToLower(action) {
		case "list":
			// Read-only: no guard is consulted, by design.
			c, err := connect()
			if err != nil {
				return err
			}
			return printGroups(ctx, c)
		case "create":
			return doCreate(ctx, cfg, connect)
		case "delete":
			return doDelete(ctx, cfg, connect)
		case "update":
			return doUpdate(ctx, cfg, connect)
		case "pin":
			return doPin(ctx, cfg, connect)
		default:
			return fmt.Errorf("unknown -action %q: want list, create, delete, update or pin", action)
		}
	})
}

// ----------------------------------------------------------------- read path

func printGroups(ctx context.Context, qc *quote.QuoteContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Watchlist groups")
	groups, err := qc.WatchedGroups(c)
	if err != nil {
		return fmt.Errorf("watched groups: %w", err)
	}
	if len(groups) == 0 {
		fmt.Println("   (no groups saved on this account)")
		return nil
	}
	for _, g := range groups {
		fmt.Printf("\n-- group %d %q (%d symbols)\n", g.Id, g.Name, len(g.Securites))
		if len(g.Securites) == 0 {
			fmt.Println("   (empty)")
			continue
		}
		fmt.Printf("   %-14s %-8s %-9s %-20s %-8s %s\n",
			"SYMBOL", "MARKET", "PRICE", "NAME", "PINNED", "WATCHED_AT")
		for _, s := range g.Securites {
			fmt.Printf("   %-14s %-8s %-9s %-20s %-8s %s\n",
				s.Symbol, cli.OrDash(s.Market), cli.Dec4(s.Price),
				cli.Truncate(s.Name, 20), yesNo(s.IsPinned), cli.FmtTime(s.WatchedAt))
		}
	}
	return nil
}

// ---------------------------------------------------------------- write paths
//
// Every one of these follows the same shape: validate flags, PRINT the exact
// intended call, then ask the guard, and only then connect. If you add another
// mutation, copy this shape verbatim — print, gate, connect, call.

func doCreate(ctx context.Context, cfg *appcfg.Config, connect func() (*quote.QuoteContext, error)) error {
	if groupName == "" {
		return fmt.Errorf("-name is required for -action create")
	}
	syms := splitList(symbols)

	describe("CreateWatchlistGroup", map[string]string{
		"name":    groupName,
		"symbols": strings.Join(syms, ","),
	})
	if err := gate(cfg, "create a watchlist group"); err != nil {
		return err
	}

	qc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := qc.CreateWatchlistGroup(c, groupName, syms)
	if err != nil {
		return fmt.Errorf("creating watchlist group %q: %w", groupName, err)
	}
	fmt.Printf("watchlist group created, group_id=%d\n", id)
	return nil
}

func doDelete(ctx context.Context, cfg *appcfg.Config, connect func() (*quote.QuoteContext, error)) error {
	if groupID == 0 {
		return fmt.Errorf("-group-id is required for -action delete")
	}

	describe("DeleteWatchlistGroup", map[string]string{
		"group_id": strconv.FormatInt(groupID, 10),
		"purge":    strconv.FormatBool(purge),
	})
	if err := gate(cfg, fmt.Sprintf("delete watchlist group %d", groupID)); err != nil {
		return err
	}

	qc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := qc.DeleteWatchlistGroup(c, groupID, purge); err != nil {
		return fmt.Errorf("deleting watchlist group %d: %w", groupID, err)
	}
	fmt.Printf("watchlist group %d deleted\n", groupID)
	return nil
}

func doUpdate(ctx context.Context, cfg *appcfg.Config, connect func() (*quote.QuoteContext, error)) error {
	if groupID == 0 {
		return fmt.Errorf("-group-id is required for -action update")
	}
	mode, err := parseUpdateMode(updateMode)
	if err != nil {
		return err
	}
	syms := splitList(symbols)

	describe("UpdateWatchlistGroup", map[string]string{
		"group_id": strconv.FormatInt(groupID, 10),
		"name":     groupName,
		"mode":     string(mode),
		"symbols":  strings.Join(syms, ","),
	})
	if err := gate(cfg, fmt.Sprintf("update watchlist group %d", groupID)); err != nil {
		return err
	}

	qc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := qc.UpdateWatchlistGroup(c, groupID, groupName, syms, mode); err != nil {
		return fmt.Errorf("updating watchlist group %d: %w", groupID, err)
	}
	fmt.Printf("watchlist group %d updated (%s)\n", groupID, mode)
	return nil
}

func doPin(ctx context.Context, cfg *appcfg.Config, connect func() (*quote.QuoteContext, error)) error {
	syms := splitList(symbols)
	if len(syms) == 0 {
		return fmt.Errorf("-symbols is required for -action pin")
	}
	var mode quote.PinnedMode
	switch strings.ToLower(pinMode) {
	case "add", "pin":
		mode = quote.PinnedModeAdd
	case "remove", "unpin":
		mode = quote.PinnedModeRemove
	default:
		return fmt.Errorf("unknown -pin-mode %q: want add or remove", pinMode)
	}

	describe("UpdatePinned", map[string]string{
		"mode":    mode.String(),
		"symbols": strings.Join(syms, ","),
	})
	if err := gate(cfg, "pin or unpin symbols"); err != nil {
		return err
	}

	qc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := qc.UpdatePinned(c, mode, syms); err != nil {
		return fmt.Errorf("updating pinned symbols: %w", err)
	}
	fmt.Printf("pinned symbols updated (mode=%s, %d symbols)\n", mode, len(syms))
	return nil
}

// ------------------------------------------------------------------- helpers

// gate is the watchlist choke point. It returns nil only when the caller may
// proceed; on refusal it explains why, guarantees no request was made, and
// returns a *BlockedError so the process exits 3 rather than 0.
//
// The reason goes to stderr deliberately: a silent no-op looks like a crash,
// and "nothing happened" is exactly the message a safety gate must never leave
// unexplained.
func gate(cfg *appcfg.Config, action string) error {
	if !confirm {
		fmt.Fprintf(os.Stderr, "\nBLOCKED: missing --confirm\n")
		fmt.Fprintf(os.Stderr, "DRY RUN: nothing was sent to Longbridge.\n")
		fmt.Fprintf(os.Stderr, "Watchlist writes are guarded separately from orders; pass\n")
		fmt.Fprintf(os.Stderr, "  --confirm   together with   LONGPORT_WATCHLIST_DRY_RUN=0\n")
		return appcfg.Blockedf("missing --confirm")
	}
	if err := cfg.GuardWatchlist(action); err != nil {
		fmt.Fprintf(os.Stderr, "\nBLOCKED: %v\n", err)
		fmt.Fprintf(os.Stderr, "DRY RUN: nothing was sent to Longbridge.\n")
		return err
	}
	return nil
}

// describe prints the exact call that would be made, in deterministic key
// order, so a dry run is informative rather than just a refusal.
func describe(op string, fields map[string]string) {
	fmt.Printf("\n--- %s request ---\n", op)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-12s %s\n", k, cli.OrDash(fields[k]))
	}
}

func parseUpdateMode(s string) (quote.WatchlistUpdateMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "add":
		return quote.AddWatchlist, nil
	case "remove":
		return quote.RemoveWatchlist, nil
	case "replace":
		return quote.ReplaceWatchlist, nil
	}
	return "", fmt.Errorf("unknown -update-mode %q: want add, remove or replace", s)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
