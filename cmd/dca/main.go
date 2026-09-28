// Command dca reads Longbridge's dollar-cost-averaging plans and, behind its
// own dedicated safety gate, creates and changes them.
//
// # SAFETY — READ THIS BEFORE THE WRITE ACTIONS
//
// # WHAT IS GUARDED AND WHY
//
// The split below is taken from the v0.25.2 source (dca/context.go), not from
// method names. It is a source fact: the endpoint each method calls, and
// whether that endpoint mutates.
//
// READ-ONLY — no guard is consulted, by design:
//
//	List         GET  /v1/dailycoins/query
//	History      GET  /v1/dailycoins/query-records
//	Stats        GET  /v1/dailycoins/statistic
//	CheckSupport POST /v1/dailycoins/batch-check-support   <- POST, non-mutating
//	CalcDate     POST /v1/dailycoins/calc-trd-date         <- POST, non-mutating
//
// CheckSupport and CalcDate are POSTs and it would be easy to guard them "to
// be safe", but they compute an answer and change nothing server-side. Guarding
// a pure computation behind three switches is theatre, and it would make
// `calc-date` — the one call you want in order to sanity-check a schedule
// before you commit to it — awkward to use. They are reads wearing a POST verb.
//
// MUTATING — all six sit behind the DCA gate:
//
//	Create      POST /v1/dailycoins/create
//	Update      POST /v1/dailycoins/update
//	Pause       POST /v1/dailycoins/toggle   status=Suspended
//	Resume      POST /v1/dailycoins/toggle   status=Active
//	Stop        POST /v1/dailycoins/toggle   status=Finished
//	SetReminder POST /v1/dailycoins/update-alter-hours
//
// Pause, Resume and Stop share ONE endpoint and differ only by a status
// string. That is exactly why the guard here sits at the METHOD level — the
// dispatch happens in this file, in a switch, before any context is created.
// There is no single "toggle" call to hang a guard off.
//
// # THERE IS NO PREVIEW MODE
//
// The SDK has no plan/preview method: Create creates the plan directly. So
// there is no "what would this cost me" endpoint to show you first. The only
// preview available is this command's own dry run, which prints the exact
// request body it would send and sends nothing. That is why the gate is not
// optional friction — it is the only thing standing between a typo and a
// recurring standing order.
//
// # THE GATE
//
// config.DCAGuard, built by the reusable config.WriteGuard helper, requires ALL
// THREE of:
//
//  1. LONGPORT_DCA_DRY_RUN=0 (or false)
//  2. --confirm-live-dca
//  3. LONGPORT_MODE=live
//
// Dry run is ON by default, a refusal makes NO network call at all (the DCA
// context is created lazily, after the gate passes), and a refusal exits 3 —
// see config.ExitBlocked.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/dca"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action         string
	symbol         string
	amount         string
	frequency      string
	dayOfWeek      string
	dayOfMonth     uint
	allowMargin    bool
	planID         string
	reminderHours  string
	limit          int
	page           int
	confirmLiveDCA bool
	timeout        time.Duration
)

func main() {
	u := cli.NewUsage("dca", "dollar-cost-averaging plans; gated plan changes")
	u.FS.StringVar(&action, "action", "list",
		"list | history | stats | check-support | calc-date (read-only) | "+
			"create | update | pause | resume | stop | set-reminder (gated)")
	u.FS.StringVar(&symbol, "symbol", "", "symbol, e.g. 700.HK (required for create, calc-date; optional filter elsewhere)")
	u.FS.StringVar(&amount, "amount", "", "per-investment amount as a decimal string (required for create; optional for update)")
	u.FS.StringVar(&frequency, "frequency", "monthly",
		"daily | weekly | fortnightly | monthly (default monthly)")
	u.FS.StringVar(&dayOfWeek, "day-of-week", "", "weekday name, required for weekly/fortnightly (e.g. Monday)")
	u.FS.UintVar(&dayOfMonth, "day-of-month", 0, "day of month 1-31, required for monthly")
	u.FS.BoolVar(&allowMargin, "allow-margin", false, "for create/update: enable margin financing (real leverage)")
	u.FS.StringVar(&planID, "plan-id", "", "plan id (required for update, pause, resume, stop, history)")
	u.FS.StringVar(&reminderHours, "reminder-hours", "", "for set-reminder: 1, 6 or 12 hours before execution")
	u.FS.IntVar(&limit, "limit", 20, "for history: max records")
	u.FS.IntVar(&page, "page", 1, "for history: page index, 1-based")
	u.FS.BoolVar(&confirmLiveDCA, "confirm-live-dca", false,
		"REQUIRED for any write; must be combined with LONGPORT_DCA_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	fmt.Fprintf(os.Stderr, "[config] dca_dry_run=%v (dedicated gate: %s + %s + LONGPORT_MODE=live)\n",
		appcfg.DCAGuard.DryRun(), appcfg.DCAGuard.DryRunEnv, appcfg.DCAGuard.ConfirmFlag)
	fmt.Fprintf(os.Stderr, "[config] read-only actions: list, history, stats, check-support, calc-date\n")

	cli.Run(func(ctx context.Context) error {
		// Created lazily and only after a gate passes, so a refused write never
		// authenticates and never dials.
		// DCAContext has no Close in v0.25.2 (unlike the websocket-backed
		// quote/trade contexts), so there is nothing to release.
		var dc *dca.DCAContext
		connect := func() (*dca.DCAContext, error) {
			if dc != nil {
				return dc, nil
			}
			c, err := dca.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating dca context: %w", err)
			}
			dc = c
			return dc, nil
		}

		switch strings.ToLower(strings.TrimSpace(action)) {
		// ---- read-only: no guard is consulted, by design ----
		case "list":
			return doList(ctx, connect)
		case "history":
			return doHistory(ctx, connect)
		case "stats":
			return doStats(ctx, connect)
		case "check-support":
			return doCheckSupport(ctx, connect)
		case "calc-date":
			return doCalcDate(ctx, connect)
		// ---- mutating: all six behind the DCA gate ----
		case "create":
			return doCreate(ctx, cfg, connect)
		case "update":
			return doUpdate(ctx, cfg, connect)
		case "pause":
			return doToggle(ctx, cfg, connect, dca.DCAStatusSuspended, "pause")
		case "resume":
			return doToggle(ctx, cfg, connect, dca.DCAStatusActive, "resume")
		case "stop":
			return doToggle(ctx, cfg, connect, dca.DCAStatusFinished, "stop")
		case "set-reminder":
			return doSetReminder(ctx, cfg, connect)
		default:
			return fmt.Errorf(
				"unknown -action %q: want list, history, stats, check-support, calc-date, "+
					"create, update, pause, resume, stop or set-reminder", action)
		}
	})
}

// ----------------------------------------------------------------- read path

func doList(ctx context.Context, connect func() (*dca.DCAContext, error)) error {
	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("DCA plans")
	list, err := dc.List(c, nil, nil)
	if err != nil {
		return fmt.Errorf("listing dca plans: %w", err)
	}
	if len(list.Plans) == 0 {
		fmt.Println("   (no DCA plans on this account)")
		return nil
	}
	fmt.Printf("\n%-20s %-10s %-11s %-10s %-6s %-5s %-10s %s\n",
		"PLAN_ID", "SYMBOL", "PER_INVEST", "FREQUENCY", "DAY", "ISSUE", "NEXT_TRD", "STATUS")
	for _, p := range list.Plans {
		fmt.Printf("%-20s %-10s %-11s %-10s %-6s %-5d %-10s %s\n",
			p.PlanID, p.Symbol, p.PerInvestAmount.String(),
			p.Frequency.String(), p.DayOfMonth, p.IssueNumber,
			cli.OrDash(p.NextTrdDate), p.Status.String())
	}
	return nil
}

func doHistory(ctx context.Context, connect func() (*dca.DCAContext, error)) error {
	if planID == "" {
		return fmt.Errorf("-plan-id is required for -action history")
	}
	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("DCA plan %s history", planID))
	h, err := dc.History(c, planID, page, limit)
	if err != nil {
		return fmt.Errorf("dca history for plan %s: %w", planID, err)
	}
	if len(h.Records) == 0 {
		fmt.Println("   (no executions recorded)")
		return nil
	}
	fmt.Printf("\n%-10s %-24s %-12s %-10s %-10s %-11s %s\n",
		"ORDER_ID", "CREATED_AT", "SYMBOL", "ACTION", "QTY", "AMOUNT", "STATUS")
	for _, r := range h.Records {
		fmt.Printf("%-10s %-24s %-12s %-10s %-10s %-11s %s\n",
			cli.OrDash(r.OrderID), cli.OrDash(r.CreatedAt), r.Symbol,
			cli.OrDash(r.Action), cli.Dec(r.ExecutedQty),
			cli.Dec(r.ExecutedAmount), cli.OrDash(r.Status))
	}
	if h.HasMore {
		fmt.Println("(more records available; raise -page or -limit)")
	}
	return nil
}

func doStats(ctx context.Context, connect func() (*dca.DCAContext, error)) error {
	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var sym *string
	if symbol != "" {
		sym = &symbol
	}
	cli.Section("DCA statistics")
	s, err := dc.Stats(c, sym)
	if err != nil {
		return fmt.Errorf("dca statistics: %w", err)
	}
	fmt.Printf("  active            %s\n", cli.OrDash(s.ActiveCount))
	fmt.Printf("  finished          %s\n", cli.OrDash(s.FinishedCount))
	fmt.Printf("  suspended         %s\n", cli.OrDash(s.SuspendedCount))
	fmt.Printf("  rest days         %s\n", cli.OrDash(s.RestDays))
	fmt.Printf("  total amount      %s\n", cli.Dec(s.TotalAmount))
	fmt.Printf("  total profit      %s\n", cli.Dec(s.TotalProfit))

	if len(s.NearestPlans) == 0 {
		return nil
	}
	fmt.Printf("\n%-20s %-10s %-11s %-10s %s\n", "PLAN_ID", "SYMBOL", "PER_INVEST", "FREQUENCY", "STATUS")
	for _, p := range s.NearestPlans {
		fmt.Printf("%-20s %-10s %-11s %-10s %s\n",
			p.PlanID, p.Symbol, p.PerInvestAmount.String(),
			p.Frequency.String(), p.Status.String())
	}
	return nil
}

// doCheckSupport and doCalcDate are POSTs in the SDK but change nothing. They
// are deliberately left unguarded; see the package comment for the reasoning.
func doCheckSupport(ctx context.Context, connect func() (*dca.DCAContext, error)) error {
	syms := splitList(symbolsOr())
	if len(syms) == 0 {
		return fmt.Errorf("-symbol is required for -action check-support (comma-separated)")
	}
	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("DCA support")
	fmt.Println("  (POST /v1/dailycoins/batch-check-support — a query, not a mutation)")
	infos, err := dc.CheckSupport(c, syms)
	if err != nil {
		return fmt.Errorf("checking dca support: %w", err)
	}
	fmt.Printf("\n%-12s %s\n", "SYMBOL", "SUPPORTS_DCA")
	for _, i := range infos {
		fmt.Printf("%-12s %v\n", i.Symbol, i.SupportRegularSaving)
	}
	return nil
}

func doCalcDate(ctx context.Context, connect func() (*dca.DCAContext, error)) error {
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action calc-date")
	}
	freq, err := parseFrequency(frequency)
	if err != nil {
		return err
	}
	day, dayMonth, err := schedule()
	if err != nil {
		return err
	}
	opts := &dca.CalcDateOptions{DayOfWeek: day, DayOfMonth: dayMonth}
	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Projected DCA trade date")
	fmt.Println("  (POST /v1/dailycoins/calc-trd-date — a query, not a mutation)")
	res, err := dc.CalcDate(c, symbol, freq, opts)
	if err != nil {
		return fmt.Errorf("calculating dca trade date for %s: %w", symbol, err)
	}
	if res.TradeDate.IsZero() {
		fmt.Println("   (no date returned)")
		return nil
	}
	fmt.Printf("  %s %s -> %s\n", symbol, freq.String(),
		res.TradeDate.Format("2006-01-02"))
	return nil
}

// ---------------------------------------------------------------- write paths
//
// Every one of these follows the same shape, and the order matters: validate
// flags, PRINT the exact request under a [DRY-RUN] prefix, ask the gate, and
// only then connect. If you add another mutation, copy this shape verbatim.

func doCreate(ctx context.Context, cfg *appcfg.Config, connect func() (*dca.DCAContext, error)) error {
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action create")
	}
	if amount == "" {
		return fmt.Errorf("-amount is required for -action create")
	}
	freq, err := parseFrequency(frequency)
	if err != nil {
		return err
	}
	day, dayMonth, err := schedule()
	if err != nil {
		return err
	}
	opts := &dca.CreateOptions{DayOfWeek: day, DayOfMonth: dayMonth, AllowMargin: allowMargin}

	describe(map[string]string{
		"endpoint":          "POST /v1/dailycoins/create",
		"symbol":            symbol,
		"per_invest_amount": amount,
		"invest_frequency":  freq.String(),
		"day_of_week":       opts.DayOfWeek,
		"day_of_month":      optUint(opts.DayOfMonth),
		"allow_margin":      strconv.FormatBool(opts.AllowMargin),
	})
	if err := gate(cfg, fmt.Sprintf("create a DCA plan for %s", symbol)); err != nil {
		return err
	}

	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := dc.Create(c, symbol, amount, freq, opts)
	if err != nil {
		return fmt.Errorf("creating dca plan for %s: %w", symbol, err)
	}
	fmt.Printf("DCA plan created, plan_id=%s\n", res.PlanID)
	return nil
}

func doUpdate(ctx context.Context, cfg *appcfg.Config, connect func() (*dca.DCAContext, error)) error {
	if planID == "" {
		return fmt.Errorf("-plan-id is required for -action update")
	}
	opts := &dca.UpdateOptions{}
	if amount != "" {
		opts.Amount = &amount
	}
	if frequency != "" {
		f, err := parseFrequency(frequency)
		if err != nil {
			return err
		}
		opts.Frequency = &f
	}
	if dayOfWeek != "" {
		opts.DayOfWeek = &dayOfWeek
	}
	if dayOfMonth > 0 {
		d := uint32(dayOfMonth)
		opts.DayOfMonth = &d
	}
	if allowMargin {
		opts.AllowMargin = &allowMargin
	}

	describe(map[string]string{
		"endpoint":          "POST /v1/dailycoins/update",
		"plan_id":           planID,
		"per_invest_amount": derefStr(opts.Amount),
		"invest_frequency":  derefFreq(opts.Frequency),
		"day_of_week":       derefStr(opts.DayOfWeek),
		"day_of_month":      optUint(opts.DayOfMonth),
		"allow_margin":      derefBool(opts.AllowMargin),
	})
	if err := gate(cfg, fmt.Sprintf("update DCA plan %s", planID)); err != nil {
		return err
	}

	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := dc.Update(c, planID, opts)
	if err != nil {
		return fmt.Errorf("updating dca plan %s: %w", planID, err)
	}
	fmt.Printf("DCA plan %s updated, plan_id=%s\n", planID, res.PlanID)
	return nil
}

// doToggle serves pause, resume and stop. All three are the same endpoint with
// a different status, which is why the guard has to be applied here at the
// method level: there is no shared SDK call to guard.
func doToggle(ctx context.Context, cfg *appcfg.Config,
	connect func() (*dca.DCAContext, error), status dca.DCAStatus, verb string) error {
	if planID == "" {
		return fmt.Errorf("-plan-id is required for -action %s", verb)
	}
	if status == dca.DCAStatusFinished {
		describe(map[string]string{
			"endpoint": "POST /v1/dailycoins/toggle",
			"plan_id":  planID,
			"status":   status.String(),
			"WARNING":  "IRREVERSIBLE: a Finished plan cannot be resumed. To invest again you must create a NEW plan.",
		})
	} else {
		describe(map[string]string{
			"endpoint": "POST /v1/dailycoins/toggle",
			"plan_id":  planID,
			"status":   status.String(),
		})
	}
	if err := gate(cfg, fmt.Sprintf("%s DCA plan %s", verb, planID)); err != nil {
		return err
	}

	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch status {
	case dca.DCAStatusSuspended:
		err = dc.Pause(c, planID)
	case dca.DCAStatusActive:
		err = dc.Resume(c, planID)
	default:
		err = dc.Stop(c, planID)
	}
	if err != nil {
		return fmt.Errorf("%sing dca plan %s: %w", verb, planID, err)
	}
	fmt.Printf("DCA plan %s %sd (status=%s)\n", planID, verb, status.String())
	return nil
}

func doSetReminder(ctx context.Context, cfg *appcfg.Config, connect func() (*dca.DCAContext, error)) error {
	if reminderHours == "" {
		return fmt.Errorf("-reminder-hours is required for -action set-reminder (1, 6 or 12)")
	}
	describe(map[string]string{
		"endpoint":    "POST /v1/dailycoins/update-alter-hours",
		"alter_hours": reminderHours,
	})
	if err := gate(cfg, fmt.Sprintf("set the DCA execution reminder to %s hours", reminderHours)); err != nil {
		return err
	}

	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := dc.SetReminder(c, reminderHours); err != nil {
		return fmt.Errorf("setting dca reminder to %s hours: %w", reminderHours, err)
	}
	fmt.Printf("DCA execution reminder set to %s hours before execution\n", reminderHours)
	return nil
}

// ------------------------------------------------------------------- helpers

// gate is the DCA choke point. It returns nil only when the caller may
// proceed; on refusal it prints the full explanation under a [DRY-RUN] prefix
// and returns a compact *config.BlockedError so the process exits 3.
//
// The reason goes to stderr deliberately: a silent refusal looks like a crash,
// and "nothing happened" is exactly the message a safety gate must not leave
// unexplained. The error it returns is deliberately short — cli.Fail prints
// that too, and duplicating the whole block would make the transcript harder
// to read, not easier.
//
// The [DRY-RUN] prefix exists so a refused transcript can never be mistaken
// for a live one.
func gate(cfg *appcfg.Config, action string) error {
	if err := appcfg.DCAGuard.Check(cfg, confirmLiveDCA, action); err != nil {
		fmt.Fprintf(os.Stderr, "\n[DRY-RUN] BLOCKED: %v\n", err)
		return appcfg.Blockedf(
			"%s BLOCKED by the DCA safety gate. Nothing was sent to Longbridge.\n"+
				"See the [DRY-RUN] output above for the request and the full list\n"+
				"of unsatisfied conditions. To perform it:\n"+
				"  %s=0  +  %s  +  LONGPORT_MODE=live",
			action, appcfg.DCAGuard.DryRunEnv, appcfg.DCAGuard.ConfirmFlag)
	}
	return nil
}

// describe prints the request that WOULD be sent, in deterministic key order,
// so a dry run is informative rather than just a refusal. Every line carries
// the [DRY-RUN] prefix for the same reason the refusal does.
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

func parseFrequency(s string) (dca.DCAFrequency, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "daily":
		return dca.DCAFrequencyDaily, nil
	case "weekly":
		return dca.DCAFrequencyWeekly, nil
	case "fortnightly":
		return dca.DCAFrequencyFortnightly, nil
	case "monthly", "":
		return dca.DCAFrequencyMonthly, nil
	}
	return 0, fmt.Errorf(
		"unknown -frequency %q: want daily, weekly, fortnightly or monthly", s)
}

// schedule validates the day-of-week / day-of-month pairing and returns them
// separately, because the SDK models them on three different option structs
// (CreateOptions, CalcDateOptions and nothing at all for Update). The API
// requires the field matching the frequency, so catching it here turns a
// confusing server error into a local one.
func schedule() (string, *uint32, error) {
	if dayOfMonth > 31 {
		return "", nil, fmt.Errorf("-day-of-month must be 1-31, got %d", dayOfMonth)
	}
	var month *uint32
	if dayOfMonth > 0 {
		d := uint32(dayOfMonth)
		month = &d
	}
	switch strings.ToLower(strings.TrimSpace(frequency)) {
	case "weekly", "fortnightly":
		if dayOfWeek == "" {
			return "", nil, fmt.Errorf("-day-of-week is required for -frequency %s", frequency)
		}
	case "monthly", "":
		if month != nil && dayOfWeek != "" {
			return "", nil, fmt.Errorf(
				"-day-of-week and -day-of-month are mutually exclusive; " +
					"monthly uses -day-of-month and weekly uses -day-of-week")
		}
	}
	return dayOfWeek, month, nil
}

// symbolsOr lets check-support accept either -symbol or a comma-separated
// list, so the flag reads naturally in both cases.
func symbolsOr() string { return symbol }

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func optUint(p *uint32) string {
	if p == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*p), 10)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefFreq(p *dca.DCAFrequency) string {
	if p == nil {
		return ""
	}
	return p.String()
}

func derefBool(p *bool) string {
	if p == nil {
		return ""
	}
	return strconv.FormatBool(*p)
}
