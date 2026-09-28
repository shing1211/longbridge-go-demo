// Command sharelist reads the Longbridge share-list surface — your own
// sharelists, one sharelist's constituents, the platform's popular sharelists —
// and, behind its own safety gate, edits the sharelists you own.
//
// # SAFETY — READ THIS BEFORE THE WRITE ACTIONS
//
// SharelistContext has eight methods. Three are reads (List, Detail, Popular)
// and are unguarded by design. The other five — Create, Delete, AddSecurities,
// RemoveSecurities and SortSecurities — were verified against the SDK source in
// github.com/longbridge/openapi-go v0.25.2 and every one of them is a real
// server-side mutation:
//
//	Create            POST   /v1/sharelists
//	Delete            DELETE /v1/sharelists/{id}
//	AddSecurities     POST   /v1/sharelists/{id}/items
//	RemoveSecurities  DELETE /v1/sharelists/{id}/items
//	SortSecurities    POST   /v1/sharelists/{id}/items/sort
//
// These are user-library edits, not orders, so they deliberately do NOT reuse
// the watchlist switch or the order switch. They have their own gate, checked
// inside this package (see sharelistGate) so that no shared guard file has to
// know about them:
//
//  1. LONGPORT_SHARELIST_DRY_RUN=0 (or false), AND
//  2. --confirm-live-sharelist on the command line, AND
//  3. LONGPORT_MODE=live
//
// All three are required and each one alone still refuses. A refusal prints the
// exact request that WOULD have been sent, prefixed [DRY-RUN], makes no
// network call at all, and exits 3 — deliberately distinct from 0 ("nothing
// happened, on purpose") and from 1/2 (errors).
//
// Delete is irreversible: there is no undelete endpoint in the SDK, and
// deleting a sharelist takes its constituents with it. The dry-run output says
// so in as many words.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/sharelist"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// exitBlocked is the exit code for "the safety gate refused": a write that was
// deliberately NOT sent. It is distinct from 0 (success), 1 (generic error)
// and 2 (missing credentials), so a wrapper script can tell "the safety gate
// did its job" from "the command failed". 3 is the convention used by the
// sibling Tiger project and by internal/config.ExitBlocked.
//
// It is duplicated here as a local constant rather than referenced, because
// this gate lives entirely inside the command package: it must keep working
// regardless of what the shared config package happens to export.
const exitBlocked = 3

var (
	action     string
	count      int
	detailID   int64
	popular    bool
	stockLimit int
	timeout    time.Duration

	// write-path flags
	listName        string
	listDescription string
	symbols         string
	confirmLive     bool
	showState       bool
)

func main() {
	u := cli.NewUsage("sharelist",
		"read share lists; gated writes for the lists you own")
	u.FS.StringVar(&action, "action", "list",
		"list (your sharelists) | detail (-id) | popular (trending sharelists) | "+
			"create | delete | add | remove | sort")
	u.FS.IntVar(&count, "count", 20, "how many sharelists to return (list and popular)")
	u.FS.Int64Var(&detailID, "id", 0, "sharelist id, required for detail, delete, add, remove and sort")
	u.FS.BoolVar(&popular, "popular", false, "alias for -action popular")
	u.FS.IntVar(&stockLimit, "stock-limit", 20, "how many constituent rows to print per sharelist")
	u.FS.StringVar(&listName, "name", "", "sharelist name, required for -action create")
	u.FS.StringVar(&listDescription, "description", "",
		"sharelist description for -action create (defaults to the name, as the SDK does)")
	u.FS.StringVar(&symbols, "symbols", "",
		"comma-separated CODE.MARKET symbols for -action add, remove and sort")
	u.FS.BoolVar(&confirmLive, "confirm-live-sharelist", false,
		"REQUIRED acknowledgement for any sharelist write; must be combined with "+
			"LONGPORT_SHARELIST_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.BoolVar(&showState, "show-state", false,
		"for a blocked add/remove/sort: also fetch and print the list's current contents "+
			"(this DOES make a read-only network call, so it is off by default)")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	// This switch is local to this package, so the shared config loader cannot
	// reject a bad value; do it here, before any request, and fail loudly.
	if err := validateSharelistDryRun(); err != nil {
		cli.Fail(err)
	}
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	fmt.Fprintf(os.Stderr,
		"[config] sharelist_dry_run=%v (separate gate: LONGPORT_SHARELIST_DRY_RUN "+
			"+ --confirm-live-sharelist + LONGPORT_MODE=live)\n", sharelistDryRun())

	cli.Run(func(ctx context.Context) error {
		// The sharelist package has no Close(); it is a thin HTTP client, so
		// there is nothing to defer. But the context is still built lazily:
		// a blocked write must not authenticate or reach the network.
		var sc *sharelist.SharelistContext
		connect := func() (*sharelist.SharelistContext, error) {
			if sc != nil {
				return sc, nil
			}
			c, err := sharelist.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating sharelist context: %w", err)
			}
			sc = c
			return sc, nil
		}

		act := strings.ToLower(strings.TrimSpace(action))
		if popular {
			act = "popular"
		}

		switch act {
		case "list":
			// Read-only: no gate is consulted, by design.
			c, err := connect()
			if err != nil {
				return err
			}
			return printLists(ctx, c, "Your sharelists", false)
		case "popular":
			c, err := connect()
			if err != nil {
				return err
			}
			return printLists(ctx, c, "Popular sharelists", true)
		case "detail":
			if detailID == 0 {
				return fmt.Errorf("-id is required for -action detail")
			}
			c, err := connect()
			if err != nil {
				return err
			}
			return printDetail(ctx, c)
		case "create":
			return doCreate(ctx, cfg, connect)
		case "delete":
			return doDelete(ctx, cfg, connect)
		case "add":
			return doSecurities(ctx, cfg, connect, "add")
		case "remove":
			return doSecurities(ctx, cfg, connect, "remove")
		case "sort":
			return doSecurities(ctx, cfg, connect, "sort")
		default:
			return fmt.Errorf(
				"unknown -action %q: want list, detail, popular, create, delete, add, remove or sort",
				action)
		}
	})
}

// ----------------------------------------------------------------- read path

func printLists(ctx context.Context, sc *sharelist.SharelistContext, title string, pop bool) error {
	if count < 1 {
		return fmt.Errorf("-count must be at least 1, got %d", count)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("%s (count %d)", title, count))
	var (
		res *sharelist.SharelistList
		err error
	)
	if pop {
		res, err = sc.Popular(c, uint32(count))
		if err != nil {
			return fmt.Errorf("popular sharelists: %w", err)
		}
	} else {
		res, err = sc.List(c, uint32(count))
		if err != nil {
			return fmt.Errorf("sharelist list: %w", err)
		}
	}
	if res == nil {
		fmt.Println("   (no sharelist response)")
		return nil
	}
	printSharelists(res.Sharelists)
	if len(res.SubscribedSharelists) > 0 {
		cli.Section("Subscribed sharelists")
		printSharelists(res.SubscribedSharelists)
	}
	if res.TailMark != "" {
		fmt.Printf("\ntail_mark: %s\n", res.TailMark)
	}
	return nil
}

func printSharelists(lists []sharelist.SharelistInfo) {
	if len(lists) == 0 {
		fmt.Println("   (none)")
		return
	}
	fmt.Printf("%-10s %-28s %-40s %-12s %-9s %-8s %-6s %s\n",
		"ID", "NAME", "DESCRIPTION", "SUBS", "YTD%", "CHG%", "SUB'D", "TYPE")
	for _, s := range lists {
		fmt.Printf("%-10d %-28s %-40s %-12d %-9s %-8s %-6v %s\n",
			s.ID, cli.Truncate(s.Name, 28), cli.Truncate(s.Description, 40),
			s.SubscribersCount, cli.Dec(s.ThisYearChg), cli.Dec(s.Chg),
			s.Subscribed, sharelistTypeName(s.SharelistType))
	}
	fmt.Printf("\n(%d sharelists; use -action detail -id <ID> for constituents)\n", len(lists))
}

func printDetail(ctx context.Context, sc *sharelist.SharelistContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Sharelist %d", detailID))
	res, err := sc.Detail(c, detailID)
	if err != nil {
		return fmt.Errorf("sharelist detail %d: %w", detailID, err)
	}
	if res == nil {
		fmt.Println("   (no sharelist returned)")
		return nil
	}
	s := res.Sharelist
	fmt.Printf("name        %s\n", cli.OrDash(s.Name))
	fmt.Printf("type        %s\n", sharelistTypeName(s.SharelistType))
	if s.IndustryCode != "" {
		fmt.Printf("industry    %s\n", s.IndustryCode)
	}
	fmt.Printf("description %s\n", cli.OrDash(s.Description))
	fmt.Printf("cover       %s\n", cli.OrDash(s.Cover))
	fmt.Printf("subscribers %d   subscribed %v   owner %v\n",
		s.SubscribersCount, s.Subscribed, res.Scopes.IsSelf)
	fmt.Printf("created     %s\n", fmtTime(s.CreatedAt))
	fmt.Printf("edited      %s\n", fmtTime(s.EditedAt))
	fmt.Printf("chg%%        %s   ytd%% %s\n", cli.Dec(s.Chg), cli.Dec(s.ThisYearChg))

	if len(s.Stocks) == 0 {
		fmt.Println("\n   (no constituents)")
		return nil
	}
	shown := s.Stocks
	if stockLimit > 0 && len(shown) > stockLimit {
		shown = shown[:stockLimit]
	}
	cli.Section(fmt.Sprintf("Constituents (%d of %d)", len(shown), len(s.Stocks)))
	fmt.Printf("%-12s %-26s %-12s %-12s %-9s %-8s %s\n",
		"SYMBOL", "NAME", "LAST", "CHG%", "MKT", "STATUS", "DELAYED")
	for _, st := range shown {
		fmt.Printf("%-12s %-26s %-12s %-12s %-9s %-8s %s\n",
			st.Symbol, cli.Truncate(st.Name, 26), cli.Dec4(st.LastDone),
			cli.Dec(st.Change), cli.OrDash(st.Market), intStr(st.TradeStatus),
			boolStr(st.Latency))
	}
	if len(shown) < len(s.Stocks) {
		fmt.Printf("\n(%d of %d constituents; raise -stock-limit for more)\n", len(shown), len(s.Stocks))
	}
	return nil
}

// ---------------------------------------------------------------- write paths
//
// Same shape for all five, copied from cmd/watchlist: validate flags, PRINT
// the exact request, ask the gate, and only then connect. Print, gate,
// connect, call — in that order, always.

func doCreate(ctx context.Context, cfg *appcfg.Config,
	connect func() (*sharelist.SharelistContext, error)) error {

	if strings.TrimSpace(listName) == "" {
		return fmt.Errorf("-name is required for -action create")
	}
	describe("Create", "POST /v1/sharelists", [][2]string{
		{"name", listName},
		// The SDK substitutes the name when the description is empty; show
		// what it will actually send rather than a misleading "-".
		{"description", orNameAsDescription(listName, listDescription)},
		{"cover", "https://pub.pbkrs.com/files/202107/kaJSk6BsvPt6NJ3Q/sharelist_v1.png"},
	}, "")
	if !sharelistGate(cfg, "create a sharelist") {
		return nil
	}

	sc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := sc.Create(c, listName, listDescription); err != nil {
		return fmt.Errorf("creating sharelist %q: %w", listName, err)
	}
	fmt.Printf("sharelist created: %q\n", listName)
	return nil
}

func doDelete(ctx context.Context, cfg *appcfg.Config,
	connect func() (*sharelist.SharelistContext, error)) error {

	if detailID == 0 {
		return fmt.Errorf("-id is required for -action delete")
	}
	describe("Delete", "DELETE /v1/sharelists/{id}", [][2]string{
		{"id", strconv.FormatInt(detailID, 10)},
	}, "IRREVERSIBLE: there is no undelete endpoint. Deleting a sharelist "+
		"removes it from your library and takes its constituents with it; "+
		"recreating it will NOT restore the original contents or its ID.")
	showCurrentState(ctx, connect)
	if !sharelistGate(cfg, fmt.Sprintf("delete sharelist %d", detailID)) {
		return nil
	}

	sc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := sc.Delete(c, detailID); err != nil {
		return fmt.Errorf("deleting sharelist %d: %w", detailID, err)
	}
	fmt.Printf("sharelist %d deleted\n", detailID)
	return nil
}

func doSecurities(ctx context.Context, cfg *appcfg.Config,
	connect func() (*sharelist.SharelistContext, error), mode string) error {

	if detailID == 0 {
		return fmt.Errorf("-id is required for -action %s", mode)
	}
	syms := splitList(symbols)
	if len(syms) == 0 {
		return fmt.Errorf("-symbols is required for -action %s (CODE.MARKET, e.g. 700.HK)", mode)
	}
	for _, s := range syms {
		if !strings.Contains(s, ".") {
			return fmt.Errorf(
				"symbol %q is not in CODE.MARKET form (e.g. 700.HK, TSLA.US); "+
					"the SDK converts it to a counter_id and a bare code would be rejected", s)
		}
	}

	var (
		op   string
		verb string
		path string
		warn string
	)
	switch mode {
	case "add":
		op, verb, path = "AddSecurities", "POST", "/v1/sharelists/{id}/items"
		warn = ""
	case "remove":
		op, verb, path = "RemoveSecurities", "DELETE", "/v1/sharelists/{id}/items"
		warn = "Removals take effect on the list's contents immediately."
	case "sort":
		op, verb, path = "SortSecurities", "POST", "/v1/sharelists/{id}/items/sort"
		warn = "Reordering REPLACES the list's order: every symbol you omit drops " +
			"back out of its current position, so pass the full intended order."
	default:
		return fmt.Errorf("internal: bad securities mode %q", mode)
	}

	describe(op, verb+" "+path, [][2]string{
		{"id", strconv.FormatInt(detailID, 10)},
		// The SDK sends counter_ids ("ST/HK/700"), derived from these symbols.
		{"counter_ids", strings.Join(syms, ",") + "   (derived)"},
	}, warn)
	showCurrentState(ctx, connect)
	if !sharelistGate(cfg, fmt.Sprintf("%s securities on sharelist %d", mode, detailID)) {
		return nil
	}

	sc, err := connect()
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch mode {
	case "add":
		err = sc.AddSecurities(c, detailID, syms)
	case "remove":
		err = sc.RemoveSecurities(c, detailID, syms)
	case "sort":
		err = sc.SortSecurities(c, detailID, syms)
	}
	if err != nil {
		return fmt.Errorf("%s on sharelist %d: %w", strings.ToLower(op), detailID, err)
	}
	fmt.Printf("sharelist %d %s (%d symbols)\n", detailID, mode, len(syms))
	return nil
}

// ------------------------------------------------------------------- the gate

// sharelistGate is the choke point for all five mutations. It returns true only
// when the caller may proceed. On refusal it explains exactly which condition
// failed and guarantees no write request was made.
//
// The three conditions are checked independently, so setting the env var to 0
// without the flag still refuses, and passing the flag with the env var left
// alone still refuses. Mode matters here because the account's share library is
// real, user-visible state that other devices read — unlike a watchlist
// group, it is not a private scratch preference.
func sharelistGate(cfg *appcfg.Config, actionDesc string) bool {
	var reasons []string
	if !confirmLive {
		reasons = append(reasons,
			"missing --confirm-live-sharelist on the command line")
	}
	if sharelistDryRun() {
		reasons = append(reasons,
			"LONGPORT_SHARELIST_DRY_RUN is on (default 1; set it to 0 to allow sharelist writes)")
	}
	if cfg.Mode != appcfg.ModeLive {
		reasons = append(reasons, fmt.Sprintf(
			"LONGPORT_MODE=%s (sharelist writes require LONGPORT_MODE=live)", cfg.Mode))
	}
	if len(reasons) == 0 {
		return true
	}

	fmt.Fprintf(os.Stderr, "\n[DRY-RUN] BLOCKED: refusing to %s.\n", actionDesc)
	fmt.Fprintf(os.Stderr, "[DRY-RUN] Unsatisfied condition(s):\n")
	for _, r := range reasons {
		fmt.Fprintf(os.Stderr, "[DRY-RUN]   - %s\n", r)
	}
	fmt.Fprintf(os.Stderr,
		"[DRY-RUN] NOTHING was sent to Longbridge. All three are required:\n"+
			"[DRY-RUN]   LONGPORT_SHARELIST_DRY_RUN=0  +  --confirm-live-sharelist  +  LONGPORT_MODE=live\n")
	os.Exit(exitBlocked)
	return false // unreachable; keeps vet happy about the control flow
}

// sharelistDryRun reports whether sharelist mutations are blocked. It is a
// local copy rather than a call into internal/config on purpose: the shared
// guard file is owned by another change in this repo, and this switch is
// specific to these five methods. An unparseable value fails safe: block.
func sharelistDryRun() bool {
	v, ok := os.LookupEnv("LONGPORT_SHARELIST_DRY_RUN")
	if !ok {
		return true
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return true
	}
	return b
}

// validateSharelistDryRun rejects a bad LONGPORT_SHARELIST_DRY_RUN at startup
// rather than letting it read as "on" forever. The config loader cannot check
// it (it is not a credential and not the order switch), so each sharelist
// invocation checks it here, before any request.
func validateSharelistDryRun() error {
	v, ok := os.LookupEnv("LONGPORT_SHARELIST_DRY_RUN")
	if !ok {
		return nil
	}
	if _, err := strconv.ParseBool(strings.TrimSpace(v)); err != nil {
		return fmt.Errorf("invalid LONGPORT_SHARELIST_DRY_RUN=%q: want 1, true, 0 or false", v)
	}
	return nil
}

// ------------------------------------------------------------------- helpers

// describe prints the exact call that would be made, so a dry run is
// informative rather than just a refusal. The [DRY-RUN] prefix is on every
// line so a captured block is never mistaken for output from a real write.
func describe(op, http string, fields [][2]string, warning string) {
	fmt.Printf("\n[DRY-RUN] --- %s (%s) request that would be sent ---\n", op, http)
	for _, kv := range fields {
		fmt.Printf("[DRY-RUN]   %-12s %s\n", kv[0], cli.OrDash(kv[1]))
	}
	if warning != "" {
		fmt.Printf("[DRY-RUN]   WARNING        %s\n", warning)
	}
}

// showCurrentState optionally prints what the list contains right now, so a
// blocked add/remove/sort is reviewable. It is opt-in via -show-state because
// it is the ONE thing in a blocked run that touches the network: the guarantee
// "a refused write makes no request" is only true when it is left off, which
// is the default. A failure here is reported, never fatal — it must not change
// the gate's verdict.
func showCurrentState(ctx context.Context, connect func() (*sharelist.SharelistContext, error)) {
	if !showState || detailID == 0 {
		return
	}
	sc, err := connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] (current state unavailable: %v)\n", err)
		return
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := sc.Detail(c, detailID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[DRY-RUN] (current state unavailable: %v)\n", err)
		return
	}
	if res == nil || len(res.Sharelist.Stocks) == 0 {
		fmt.Printf("[DRY-RUN]   current        (sharelist %d has no constituents)\n", detailID)
		return
	}
	fmt.Printf("[DRY-RUN]   current        sharelist %d %q holds %d constituents: %s\n",
		detailID, res.Sharelist.Name, len(res.Sharelist.Stocks),
		strings.Join(symbolNames(res.Sharelist.Stocks, 20), ", "))
}

func symbolNames(stocks []sharelist.SharelistStock, max int) []string {
	out := make([]string, 0, len(stocks))
	for i, s := range stocks {
		if max > 0 && i >= max {
			out = append(out, fmt.Sprintf("...+%d more", len(stocks)-max))
			break
		}
		out = append(out, s.Symbol)
	}
	return out
}

func orNameAsDescription(name, desc string) string {
	if strings.TrimSpace(desc) == "" {
		return name // the SDK's own default; shown so the dry run is truthful
	}
	return desc
}

func sharelistTypeName(t sharelist.SharelistType) string {
	switch t {
	case sharelist.SharelistTypeRegular:
		return "regular"
	case sharelist.SharelistTypeOfficial:
		return "official"
	case sharelist.SharelistTypeIndustry:
		return "industry"
	}
	return fmt.Sprintf("unknown(%d)", int32(t))
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

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func intStr(v *int32) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *v)
}

func boolStr(v *bool) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%v", *v)
}

// keep errors imported for the ErrHelp check below.
