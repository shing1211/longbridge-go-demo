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
//
// # THE SCHEDULE IS CROSS-VALIDATED LOCALLY
//
// -frequency, -day-of-week and -day-of-month are checked against each other
// before any request, and the rule is that each frequency takes exactly one
// day field:
//
//	daily        neither day field
//	weekly       -day-of-week, and no -day-of-month
//	fortnightly  -day-of-week, and no -day-of-month
//	monthly      -day-of-month, and no -day-of-week
//
// The API ignores the day field that does not match the frequency, so a
// contradictory pair is not an error there — it is a plan silently scheduled
// differently from what the command line said, which for a recurring investment
// is the kind of mistake worth refusing locally.
//
// -frequency is an empty flag by default, and the empty value means monthly
// for the actions that must send a frequency (create, calc-date). That leaves
// the schedule empty only for update, whose UpdateOptions is sparse by design:
// an update that sets no schedule flag at all changes nothing about the
// schedule, while one that sets any of them is held to the same table above.
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
	registerFlags(u)
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

// registerFlags is separated from main so the flag defaults are testable: the
// -frequency default of "" is load-bearing rather than cosmetic, because a
// non-empty default would make every update look like a request to change the
// plan's frequency. See doUpdate.
func registerFlags(u *cli.Usage) {
	u.FS.StringVar(&action, "action", "list",
		"list | history | stats | check-support | calc-date (read-only) | "+
			"create | update | pause | resume | stop | set-reminder (gated)")
	u.FS.StringVar(&symbol, "symbol", "",
		"symbol, e.g. 700.HK (required for create, calc-date; optional filter for stats; "+
			"comma-separated list for check-support). Padded values are trimmed; one that is "+
			"only whitespace is an error, not an omission")
	u.FS.StringVar(&amount, "amount", "",
		"per-investment amount as a decimal string (required for create; optional for update). "+
			"Padded values are trimmed; one that is only whitespace is an error, not an omission")
	u.FS.StringVar(&frequency, "frequency", "",
		"daily | weekly | fortnightly | monthly; empty = monthly, the documented default, and on "+
			"update means leave the schedule alone. A value of only whitespace is an error.")
	u.FS.StringVar(&dayOfWeek, "day-of-week", "",
		"weekday name for weekly/fortnightly, e.g. Monday; rejected for daily and monthly. "+
			"Padded values are trimmed; one that is only whitespace is an error, not an omission")
	u.FS.UintVar(&dayOfMonth, "day-of-month", 0,
		"day of month 1-31 for monthly (0 = unset, the server default); rejected for weekly, fortnightly and daily")
	u.FS.BoolVar(&allowMargin, "allow-margin", false, "for create/update: enable margin financing (real leverage)")
	u.FS.StringVar(&planID, "plan-id", "",
		"plan id (required for update, pause, resume, stop, history). Padded values are trimmed; "+
			"one that is only whitespace is an error, not an omission")
	u.FS.StringVar(&reminderHours, "reminder-hours", "",
		"for set-reminder: 1, 6 or 12 hours before execution. Padded values are trimmed; one that "+
			"is only whitespace is an error, not an omission")
	u.FS.IntVar(&limit, "limit", 20, "for history: max records")
	u.FS.IntVar(&page, "page", 1, "for history: page index, 1-based")
	u.FS.BoolVar(&confirmLiveDCA, "confirm-live-dca", false,
		"REQUIRED for any write; must be combined with LONGPORT_DCA_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
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
	// Trimmed BEFORE the check, not after. A flag that is present but blank is a
	// flag the user got wrong, and it has to be reported as one: comparing
	// planID == "" instead let `-plan-id "  "` reach the gate, which refused it
	// with a message about the three switches and named none of the flag the
	// user actually got wrong. This is the most misleading place in the command
	// for that to happen — a reader who sees "BLOCKED ... exit 3" reasonably
	// concludes a guard protected them, rather than that they left the plan id
	// blank. cmd/trade's doSubmit/doReplace/doCancel trim for the same reason.
	planID = strings.TrimSpace(planID)
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
	// Validated before connect(), not after: the order every other action in
	// this file uses, so a bad flag is refused before any SDK context exists.
	// -symbol is optional here — it is a filter, and the zero value is "no
	// filter" — so it goes through optionalFlag rather than a bare == "".
	sym, err := optionalFlag("symbol", symbol)
	if err != nil {
		return err
	}
	var filter *string
	if sym != "" {
		filter = &sym
	}

	dc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("DCA statistics")
	s, err := dc.Stats(c, filter)
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
	symbol = strings.TrimSpace(symbol) // blank flag = bad flag; see doHistory
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
	symbol = strings.TrimSpace(symbol) // blank flag = bad flag; see doHistory
	if symbol == "" {
		return fmt.Errorf("-symbol is required for -action create")
	}
	amount = strings.TrimSpace(amount) // and the amount goes on the wire verbatim
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

// doUpdate turns the flags into a sparse *dca.UpdateOptions, in which every
// pointer is "change this field" and nil means "leave it alone" — the SDK's own
// contract, "Nil/zero fields are not sent" (dca/context.go). So the schedule is
// validated by the same schedule() the create path uses, with the empty
// -frequency standing in for the documented default, monthly.
//
// The consequence worth stating: because an omitted -frequency reads as
// monthly, an update that moves only the weekday must also pass
// -frequency weekly. That is a deliberate restriction rather than an oversight.
// The command cannot see the plan's real frequency without reading it first, and
// a read before the gate would break the no-network-call-on-refusal promise
// every guard here makes. Naming the frequency keeps the request self-consistent
// instead of asking the API to pick between two contradictory day fields.
func doUpdate(ctx context.Context, cfg *appcfg.Config, connect func() (*dca.DCAContext, error)) error {
	planID = strings.TrimSpace(planID) // blank flag = bad flag; see doHistory
	if planID == "" {
		return fmt.Errorf("-plan-id is required for -action update")
	}
	// -amount is sparse-optional here, so it goes through optionalFlag: leaving
	// the flag out is how you say "do not touch the amount", and trimming a
	// blank into that same reading would change nothing about the plan while
	// looking as though the user had asked for a new amount.
	amt, err := optionalFlag("amount", amount)
	if err != nil {
		return err
	}
	day, dayMonth, err := schedule()
	if err != nil {
		return err
	}
	opts := &dca.UpdateOptions{DayOfWeek: dayOrNil(day), DayOfMonth: dayMonth}
	if amt != "" {
		opts.Amount = &amt
	}
	// -frequency is the one string flag deliberately NOT trimmed before this
	// test. An omitted flag means "leave the schedule alone" and a whitespace-only
	// one is an error, so trimming would merge two readings that have to stay
	// distinct; schedule() above has already run freqFlag, which refuses the
	// whitespace-only case, so what reaches this test is either "" or a word
	// parseFrequency understands.
	if frequency != "" {
		f, err := parseFrequency(frequency)
		if err != nil {
			return err
		}
		opts.Frequency = &f
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
	planID = strings.TrimSpace(planID) // blank flag = bad flag; see doHistory
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
	// The trimmed value is what describe() prints and what is sent, so the
	// preview really is the request: hours goes out as the raw body field
	// alter_hours, and " 6 " is a value the API has never seen.
	reminderHours = strings.TrimSpace(reminderHours) // blank flag = bad flag; see doHistory
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

// optionalFlag normalises an OPTIONAL string flag — the two this file has are
// -amount on update and -symbol on stats. It has the shape of cmd/trade's
// priceOrZero because the problem is the same one, and it separates three inputs
// that a bare `s == ""` test runs together:
//
//	absent (the empty string)  the only way to say "not supplied", and on
//	                           update that is load-bearing: UpdateOptions is
//	                           sparse, so an absent amount means the plan's
//	                           per-investment amount is left alone
//	padded (" 6 ")             a typo around a real value, so it is trimmed
//	blank ("   ")              neither of those, and reading it as an omission
//	                           would silently do nothing for a command line
//	                           that did name the flag — or, on stats, send a
//	                           blank symbol as a filter
//
// The blank case is refused by name rather than normalised, so a user who typed
// the flag learns that they typed it wrong.
func optionalFlag(name, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("-%s %q is only whitespace: pass a value, or leave the flag out entirely", name, s)
	}
	return trimmed, nil
}

// dcaFrequencyInvalid is what parseFrequency returns alongside its error.
//
// It has to be a value the API cannot accept, because the whole point is the
// caller that forgets to check the error. The zero value was the wrong choice
// here: dca.DCAFrequency is a bare int enum whose first member is
// DCAFrequencyDaily, so `return 0, err` handed back a *valid* frequency and an
// ignored error silently created a daily plan — a recurring investment on a
// schedule nobody asked for. All four call sites do check the error today, so
// this is a hazard for the next caller rather than a live bug, and a sentinel
// costs one line to remove it.
//
// -1 is outside the enum: the SDK declares the four members with iota and no
// negative ones (dca/types.go, v0.25.2), so the whole valid range is 0..3. The
// test that asserts this also pins those four numbers, so an SDK that ever adds
// a negative member fails there rather than quietly making the sentinel valid.
//
// Nothing else in this file uses the constant: every call site returns on the
// error, so the sentinel never reaches a request.
const dcaFrequencyInvalid dca.DCAFrequency = -1

func parseFrequency(s string) (dca.DCAFrequency, error) {
	norm, err := freqFlag(s)
	if err != nil {
		return dcaFrequencyInvalid, err
	}
	switch norm {
	case "daily":
		return dca.DCAFrequencyDaily, nil
	case "weekly":
		return dca.DCAFrequencyWeekly, nil
	case "fortnightly":
		return dca.DCAFrequencyFortnightly, nil
	case "monthly", "":
		return dca.DCAFrequencyMonthly, nil
	}
	return dcaFrequencyInvalid, fmt.Errorf(
		"unknown -frequency %q: want daily, weekly, fortnightly or monthly", s)
}

// freqFlag normalises -frequency for the switch that both parseFrequency and
// schedule() run, so the two can never disagree about which frequency they are
// looking at.
//
// A value that is only whitespace is an error rather than a silent empty. It
// used to trim to "" and so became the documented default, which made
// `-frequency "   "` indistinguishable from not passing the flag at all — and
// on update, "not passed" means the plan's schedule is left untouched. Padding a
// real word (" Weekly ") is still accepted, because that is a typo, not a value.
func freqFlag(s string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(s))
	if norm == "" && s != "" {
		return "", fmt.Errorf(
			"unknown -frequency %q: want daily, weekly, fortnightly or monthly", s)
	}
	return norm, nil
}

// dayOfWeekFlag normalises -day-of-week the same way freqFlag normalises
// -frequency, because the two flags have the same problem and the same reason
// for not solving it with TrimSpace alone: padding a real weekday is a typo and
// is trimmed, while a value that is only whitespace is refused rather than read
// as "no weekday given".
//
// The blank case is load-bearing here for a second reason. Under -frequency
// monthly and daily the weekday is not sent at all, so a blank value trimmed to
// "" would be dropped without a word and the command would go on to build a
// plan on a schedule the user never named — the same "silently not what was
// asked for" that freqFlag exists to stop.
func dayOfWeekFlag(s string) (string, error) {
	norm := strings.TrimSpace(s)
	if norm == "" && s != "" {
		return "", fmt.Errorf(
			"-day-of-week %q is only whitespace: want a weekday name, e.g. Monday, or leave the flag out", s)
	}
	return norm, nil
}

// schedule validates the day-of-week / day-of-month pairing and returns them
// separately, because the SDK models them on three different option structs
// (CreateOptions, CalcDateOptions and the sparse UpdateOptions). The API
// requires the field matching the frequency, so catching it here turns a
// confusing server error into a local one.
//
// Each frequency takes exactly one day field, and only that one: weekly and
// fortnightly a weekday, monthly a day of the month, daily neither. The API
// ignores the field that does not match rather than rejecting it, so the
// alternative to this check is a plan quietly running on a schedule the command
// line did not describe.
func schedule() (string, *uint32, error) {
	if dayOfMonth > 31 {
		return "", nil, fmt.Errorf("-day-of-month must be 1-31, got %d", dayOfMonth)
	}
	freq, err := freqFlag(frequency)
	if err != nil {
		return "", nil, err
	}
	// Normalised here, once, so every emptiness test below reads the trimmed
	// value: the SDK sends invest_day_of_week verbatim, so a padded or blank
	// weekday has to be settled before it can reach a request body.
	day, err := dayOfWeekFlag(dayOfWeek)
	if err != nil {
		return "", nil, err
	}
	var month *uint32
	if dayOfMonth > 0 {
		d := uint32(dayOfMonth)
		month = &d
	}
	// The mutual-exclusion message is the same shape whichever frequency is in
	// play, because the mistake is the same: the command line describes two
	// schedules at once and leaves the command to guess which one was meant.
	both := func(keep, drop string) error {
		return fmt.Errorf(
			"-day-of-week and -day-of-month are mutually exclusive: "+
				"-frequency %s takes %s, so drop -%s", freqOrDefault(freq), keep, drop)
	}
	switch freq {
	case "weekly", "fortnightly":
		if day == "" {
			return "", nil, fmt.Errorf("-day-of-week is required for -frequency %s", frequency)
		}
		if month != nil {
			return "", nil, both("-day-of-week", "day-of-month")
		}
	case "monthly", "":
		// "" is the documented default, so an update that names a weekday and no
		// frequency is naming a weekday on a monthly plan.
		if day != "" && month != nil {
			return "", nil, both("-day-of-month", "day-of-week")
		}
		if day != "" {
			return "", nil, fmt.Errorf(
				"-day-of-week is not used with -frequency %s, which uses -day-of-month; "+
					"pass -frequency weekly or fortnightly as well if the plan is weekly",
				freqOrDefault(freq))
		}
	case "daily":
		if day != "" {
			return "", nil, fmt.Errorf(
				"-day-of-week is not used with -frequency daily, which runs every trading day; " +
					"drop -day-of-week")
		}
		if month != nil {
			return "", nil, fmt.Errorf(
				"-day-of-month is not used with -frequency daily, which runs every trading day; " +
					"drop -day-of-month")
		}
	}
	return day, month, nil
}

// freqOrDefault renders the frequency an error message should quote, naming the
// default rather than the empty flag the user actually typed.
func freqOrDefault(norm string) string {
	if norm == "" {
		return "monthly"
	}
	return norm
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

// dayOrNil turns an absent weekday into a nil pointer, because UpdateOptions
// uses nil for "leave this field alone" and an empty-string pointer would tell
// the API to clear it.
func dayOrNil(day string) *string {
	if day == "" {
		return nil
	}
	return &day
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
