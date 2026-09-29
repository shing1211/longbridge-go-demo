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
// the watchlist switch or the order switch. They have their own gate,
// config.SharelistGuard, built by the reusable config.WriteGuard helper:
//
//  1. LONGPORT_SHARELIST_DRY_RUN=0 (or false), AND
//  2. --confirm-live-sharelist on the command line, AND
//  3. LONGPORT_MODE=live
//
// All three are required and each one alone still refuses. A refusal prints the
// exact request that WOULD have been sent, prefixed [DRY-RUN], makes no
// network call at all, and exits 3 — deliberately distinct from 0 ("nothing
// happened, on purpose") and from 1/2 (errors). See config.ExitBlocked.
//
// The one documented exception to "a refused write makes no network call" is
// -show-state, which is opt-in and off by default: on a blocked add, remove,
// sort or delete it fetches the list's current constituents so the refusal is
// reviewable. It changes nothing about the gate's verdict.
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
	"unicode"

	"github.com/longbridge/openapi-go/sharelist"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

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
		"comma-separated CODE.MARKET symbols for -action add, remove and sort; "+
			"each must be a non-empty code, one dot, and a non-empty market, with no spaces (e.g. 700.HK)")
	u.FS.BoolVar(&confirmLive, "confirm-live-sharelist", false,
		"REQUIRED acknowledgement for any sharelist write; must be combined with "+
			"LONGPORT_SHARELIST_DRY_RUN=0 and LONGPORT_MODE=live")
	u.FS.BoolVar(&showState, "show-state", false,
		"for a blocked add/remove/sort: also fetch and print the list's current contents "+
			"(this DOES make a read-only network call, so it is off by default)")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	fmt.Fprintf(os.Stderr,
		"[config] sharelist_dry_run=%v (separate gate: %s + %s + LONGPORT_MODE=live)\n",
		appcfg.SharelistGuard.DryRun(), appcfg.SharelistGuard.DryRunEnv, appcfg.SharelistGuard.ConfirmFlag)

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
	if err := gate(cfg, "create a sharelist"); err != nil {
		return err
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
	if err := gate(cfg, fmt.Sprintf("delete sharelist %d", detailID)); err != nil {
		return err
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
		if err := checkSymbolShape(s); err != nil {
			return err
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
	if err := gate(cfg, fmt.Sprintf("%s securities on sharelist %d", mode, detailID)); err != nil {
		return err
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
//
// gate is the sharelist choke point for all five mutations: it runs before the
// SDK context is created, so a refusal never authenticates and never dials.
// See cmd/dca for why the error it returns is deliberately short.

func gate(cfg *appcfg.Config, desc string) error {
	if err := appcfg.SharelistGuard.Check(cfg, confirmLive, desc); err != nil {
		fmt.Fprintf(os.Stderr, "\n[DRY-RUN] BLOCKED: %v\n", err)
		return appcfg.Blockedf(
			"%s BLOCKED by the sharelist safety gate. Nothing was sent to Longbridge.\n"+
				"See the [DRY-RUN] output above for the request and the full list\n"+
				"of unsatisfied conditions. To perform it:\n"+
				"  %s=0  +  %s  +  LONGPORT_MODE=live",
			desc, appcfg.SharelistGuard.DryRunEnv, appcfg.SharelistGuard.ConfirmFlag)
	}
	return nil
}

// checkSymbolShape refuses a -symbols entry that is not exactly CODE.MARKET,
// with the code before the dot, the market after it, and no whitespace inside.
//
// The check exists because the SDK does no validation of its own: it splits the
// entry on the LAST dot and builds a counter_id from whatever is left, so a
// malformed entry is not caught here, it is sent, and the API then rejects the
// whole request with a message that names neither the flag nor the entry. Each
// error therefore says which part of the entry is wrong rather than reporting
// every rejection as "not in CODE.MARKET form" — ".HK" is not a mystery, it is
// a missing code.
//
// splitList has already trimmed the entry, so padding around it is not this
// function's business; only whitespace inside one is refused.
func checkSymbolShape(s string) error {
	const form = "CODE.MARKET, e.g. 700.HK or TSLA.US"

	if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return fmt.Errorf("symbol %q contains whitespace: the SDK keeps it inside the "+
			"counter_id it builds, so the request can never match an instrument; want %s "+
			"with no spaces", s, form)
	}
	if !strings.Contains(s, ".") {
		return fmt.Errorf(
			"symbol %q is not in CODE.MARKET form (e.g. 700.HK, TSLA.US); "+
				"the SDK converts it to a counter_id and a bare code would be rejected", s)
	}
	if n := strings.Count(s, "."); n > 1 {
		return fmt.Errorf("symbol %q has %d dots: the SDK splits on the last one to build the "+
			"counter_id, so the market and the code are not the two halves you typed; want %s",
			s, n, form)
	}
	code, market, _ := strings.Cut(s, ".")
	if code == "" {
		return fmt.Errorf("symbol %q has an empty code before the dot: the SDK would build a "+
			"counter_id with no instrument code, which nothing can match; want %s", s, form)
	}
	if market == "" {
		return fmt.Errorf("symbol %q has an empty market after the dot: the SDK would build a "+
			"counter_id with no market, which the API cannot resolve; want %s", s, form)
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
