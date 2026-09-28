// Command alert reads the account's price alerts and, behind its own dedicated
// safety gate, creates, changes and deletes them.
//
// # SAFETY — READ THIS BEFORE THE WRITE ACTIONS
//
// # WHAT IS GUARDED AND WHY
//
// Taken from the v0.25.2 source (alert/context.go), not inferred from names:
//
// READ-ONLY — no guard is consulted:
//
//	List  GET /v1/notify/reminders
//
// MUTATING — all three sit behind the alert gate:
//
//	Add    POST   /v1/notify/reminders
//	Update POST   /v1/notify/reminders
//	Delete DELETE /v1/notify/reminders
//
// # ADD AND UPDATE SHARE AN ENDPOINT
//
// Add and Update both POST to /v1/notify/reminders. The SDK tells them apart
// by whether the body carries an "id": Update takes an *AlertItem obtained
// from List and echoes it back, Add builds a body with no id at all. So there
// is no endpoint-level place to hang a guard on — the two calls are the same
// HTTP request shape. The guard therefore sits at the METHOD level, in the
// dispatch switch in this file, before any context is created.
//
// Update needs an AlertItem, and the SDK has no "fetch one alert by id" call.
// Rather than invent one, -action update calls List to resolve -id. The guard
// runs BEFORE that read, because a refusal that dials the API is not a
// refusal: the guarantee that a blocked write makes no network call has to
// hold here too. So the cost of resolving the body inside the request is paid
// only when the gate is already open — at which point a request was going out
// regardless.
//
// # THE GATE
//
// config.AlertGuard, built by the reusable config.WriteGuard helper, requires
// ALL THREE of:
//
//  1. LONGPORT_ALERT_DRY_RUN=0 (or false)
//  2. --confirm-live-alert
//  3. LONGPORT_MODE=live
//
// Dry run is ON by default, a refusal makes NO network call, and a refusal
// exits 3 (config.ExitBlocked).
//
// Note the independence: passing --confirm-live-alert does not open the gate
// on its own, and clearing the env does not either. All three are required.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/alert"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action         string
	symbol         string
	condition      string
	triggerValue   string
	frequency      string
	alertID        string
	enabled        bool
	confirmLiveAlt bool
	timeout        time.Duration
)

func main() {
	u := cli.NewUsage("alert", "price alerts; gated alert changes")
	u.FS.StringVar(&action, "action", "list",
		"list (read-only) | add | update | delete (gated)")
	u.FS.StringVar(&symbol, "symbol", "", "symbol, e.g. 700.HK (required for add)")
	u.FS.StringVar(&condition, "condition", "price-rise",
		"for add: price-rise | price-fall | percent-rise | percent-fall")
	u.FS.StringVar(&triggerValue, "value", "", "trigger threshold as a decimal string (required for add)")
	u.FS.StringVar(&frequency, "frequency", "once",
		"for add: daily | every-time | once (default once)")
	u.FS.StringVar(&alertID, "id", "", "alert id (required for update and delete)")
	u.FS.BoolVar(&enabled, "enabled", true,
		"for update: enable or disable the alert")
	u.FS.BoolVar(&confirmLiveAlt, "confirm-live-alert", false,
		"REQUIRED for any write; must be combined with LONGPORT_ALERT_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	fmt.Fprintf(os.Stderr, "[config] alert_dry_run=%v (dedicated gate: %s + %s + LONGPORT_MODE=live)\n",
		appcfg.AlertGuard.DryRun(), appcfg.AlertGuard.DryRunEnv, appcfg.AlertGuard.ConfirmFlag)
	fmt.Fprintf(os.Stderr, "[config] read-only actions: list\n")

	cli.Run(func(ctx context.Context) error {
		// Created lazily and only after a gate passes, so a refused write
		// never authenticates and never dials. AlertContext has no Close in
		// v0.25.2.
		var ac *alert.AlertContext
		connect := func() (*alert.AlertContext, error) {
			if ac != nil {
				return ac, nil
			}
			c, err := alert.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating alert context: %w", err)
			}
			ac = c
			return ac, nil
		}

		switch strings.ToLower(strings.TrimSpace(action)) {
		// ---- read-only: no guard is consulted, by design ----
		case "list":
			return doList(ctx, connect)
		// ---- mutating: all three behind the alert gate ----
		case "add":
			return doAdd(ctx, cfg, connect)
		case "update":
			return doUpdate(ctx, cfg, connect)
		case "delete":
			return doDelete(ctx, cfg, connect)
		default:
			return fmt.Errorf("unknown -action %q: want list, add, update or delete", action)
		}
	})
}

// ----------------------------------------------------------------- read path

func doList(ctx context.Context, connect func() (*alert.AlertContext, error)) error {
	ac, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Price alerts")
	list, err := ac.List(c)
	if err != nil {
		return fmt.Errorf("listing price alerts: %w", err)
	}
	if len(list.Lists) == 0 {
		fmt.Println("   (no price alerts on this account)")
		return nil
	}
	for _, g := range list.Lists {
		fmt.Printf("\n-- %s %s  price=%s chg=%s\n",
			g.Symbol, cli.Truncate(g.Name, 20), cli.Dec4(g.Price), cli.Dec(g.Chg))
		if len(g.Indicators) == 0 {
			fmt.Println("   (no alert rules)")
			continue
		}
		fmt.Printf("   %-22s %-8s %-6s %-10s %s\n",
			"ALERT_ID", "ENABLED", "FREQ", "TRIGGER", "TEXT")
		for _, it := range g.Indicators {
			fmt.Printf("   %-22s %-8s %-6d %-10s %s\n",
				it.ID, yesNo(it.Enabled), it.Frequency,
				cli.Truncate(prettyValueMap(it.ValueMap), 10), cli.OrDash(it.Text))
		}
	}
	return nil
}

// ---------------------------------------------------------------- write paths

func doAdd(ctx context.Context, cfg *appcfg.Config, connect func() (*alert.AlertContext, error)) error {
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action add")
	}
	if triggerValue == "" {
		return fmt.Errorf("-value is required for -action add")
	}
	cond, err := parseCondition(condition)
	if err != nil {
		return err
	}
	freq, err := parseFrequency(frequency)
	if err != nil {
		return err
	}

	// Mirrors the body the SDK builds in alert.Add, so the dry run shows the
	// user the real request rather than a summary of it. "price" vs "chg" is
	// chosen by the SDK from the condition, so it is reproduced here.
	key := "price"
	switch cond {
	case alert.AlertConditionPercentRise, alert.AlertConditionPercentFall:
		key = "chg"
	}
	describe(map[string]string{
		"endpoint":     "POST /v1/notify/reminders",
		"method":       "Add (no \"id\" in body => create, not update)",
		"symbol":       symbol,
		"indicator_id": strconv.Itoa(int(cond)),
		"value_map":    fmt.Sprintf("{%q:%q}", key, triggerValue),
		"frequency":    strconv.Itoa(int(freq)),
		"enabled":      "true",
		"scope":        "0",
		"state":        "[1]",
	})
	if err := gate(cfg, fmt.Sprintf("add a %s alert on %s at %s", condition, symbol, triggerValue)); err != nil {
		return err
	}

	ac, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := ac.Add(c, symbol, cond, triggerValue, freq); err != nil {
		return fmt.Errorf("adding %s alert on %s: %w", condition, symbol, err)
	}
	fmt.Printf("price alert added on %s (%s %s)\n", symbol, condition, triggerValue)
	return nil
}

func doUpdate(ctx context.Context, cfg *appcfg.Config, connect func() (*alert.AlertContext, error)) error {
	if alertID == "" {
		return fmt.Errorf("-id is required for -action update")
	}

	// GATE FIRST, before any request at all.
	//
	// The SDK's Update needs a whole *AlertItem and there is no get-by-id
	// call, so resolving -id means calling List. Doing that read before the
	// gate would mean a refused update still dials the API, which breaks the
	// "a refusal makes no network call" guarantee that every other guard in
	// this repo makes. The gate therefore runs first, and only once it is open
	// do we read to resolve the id.
	//
	// The cost is that the dry-run preview cannot show the resolved body,
	// because resolving the body is itself a request. That is the right
	// trade: a blocked write sends nothing, and when the gate is open the
	// request really is sent anyway.
	if err := gate(cfg, fmt.Sprintf("update price alert %s (enabled=%v)", alertID, enabled)); err != nil {
		return err
	}

	ac, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	list, err := ac.List(c)
	if err != nil {
		return fmt.Errorf("listing price alerts (needed to resolve -id %s): %w", alertID, err)
	}
	item, sym, err := findAlert(list, alertID)
	if err != nil {
		return err
	}
	item.Enabled = enabled

	describe(map[string]string{
		"endpoint":     "POST /v1/notify/reminders",
		"method":       "Update (id present in body => update, not create)",
		"symbol":       sym,
		"id":           item.ID,
		"indicator_id": item.IndicatorID,
		"value_map":    prettyValueMap(item.ValueMap),
		"frequency":    strconv.Itoa(item.Frequency),
		"enabled":      strconv.FormatBool(item.Enabled),
		"scope":        strconv.Itoa(item.Scope),
	})

	c2, cancel2 := context.WithTimeout(ctx, timeout)
	defer cancel2()
	if err := ac.Update(c2, item); err != nil {
		return fmt.Errorf("updating price alert %s: %w", alertID, err)
	}
	fmt.Printf("price alert %s updated (enabled=%v)\n", alertID, item.Enabled)
	return nil
}

func doDelete(ctx context.Context, cfg *appcfg.Config, connect func() (*alert.AlertContext, error)) error {
	if alertID == "" {
		return fmt.Errorf("-id is required for -action delete")
	}
	describe(map[string]string{
		"endpoint": "DELETE /v1/notify/reminders",
		"ids":      alertID,
		"WARNING":  "IRREVERSIBLE: there is no undelete endpoint. Recreating the alert gets a new id and loses its trigger state.",
	})
	if err := gate(cfg, fmt.Sprintf("delete price alert %s", alertID)); err != nil {
		return err
	}

	ac, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := ac.Delete(c, []string{alertID}); err != nil {
		return fmt.Errorf("deleting price alert %s: %w", alertID, err)
	}
	fmt.Printf("price alert %s deleted\n", alertID)
	return nil
}

// ------------------------------------------------------------------- helpers

// gate is the alert choke point. On refusal it prints the full explanation
// under a [DRY-RUN] prefix and returns a compact *config.BlockedError so the
// process exits 3, and guarantees no request was made. See cmd/dca for why the
// returned error is short and why the prefix is there.
func gate(cfg *appcfg.Config, action string) error {
	if err := appcfg.AlertGuard.Check(cfg, confirmLiveAlt, action); err != nil {
		fmt.Fprintf(os.Stderr, "\n[DRY-RUN] BLOCKED: %v\n", err)
		return appcfg.Blockedf(
			"%s BLOCKED by the price-alert safety gate. Nothing was sent to Longbridge.\n"+
				"See the [DRY-RUN] output above for the request and the full list\n"+
				"of unsatisfied conditions. To perform it:\n"+
				"  %s=0  +  %s  +  LONGPORT_MODE=live",
			action, appcfg.AlertGuard.DryRunEnv, appcfg.AlertGuard.ConfirmFlag)
	}
	return nil
}

// describe prints the request that WOULD be sent, in deterministic key order.
// Every line carries the [DRY-RUN] prefix so a refused transcript can never
// be mistaken for a live one.
func describe(fields map[string]string) {
	fmt.Printf("\n[DRY-RUN] request that would be sent:\n")
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("[DRY-RUN]   %-18s %s\n", k, cli.OrDash(fields[k]))
	}
}

func findAlert(list *alert.AlertList, id string) (*alert.AlertItem, string, error) {
	for _, g := range list.Lists {
		for _, it := range g.Indicators {
			if it.ID == id {
				return it, g.Symbol, nil
			}
		}
	}
	return nil, "", fmt.Errorf(
		"no price alert with id %q on this account; run -action list to see the ids", id)
}

func parseCondition(s string) (alert.AlertCondition, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "price-rise", "price_rise", "rise":
		return alert.AlertConditionPriceRise, nil
	case "price-fall", "price_fall", "fall":
		return alert.AlertConditionPriceFall, nil
	case "percent-rise", "percent_rise":
		return alert.AlertConditionPercentRise, nil
	case "percent-fall", "percent_fall":
		return alert.AlertConditionPercentFall, nil
	}
	return 0, fmt.Errorf(
		"unknown -condition %q: want price-rise, price-fall, percent-rise or percent-fall", s)
}

func parseFrequency(s string) (alert.AlertFrequency, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "daily":
		return alert.AlertFrequencyDaily, nil
	case "every-time", "everytime", "always":
		return alert.AlertFrequencyEveryTime, nil
	case "once", "":
		return alert.AlertFrequencyOnce, nil
	}
	return 0, fmt.Errorf("unknown -frequency %q: want daily, every-time or once", s)
}

// prettyValueMap renders the opaque json.RawMessage ValueMap. The SDK exposes
// it as raw JSON precisely because the shape is not stable, so this is a
// pretty-printer and not a field mapping.
func prettyValueMap(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
