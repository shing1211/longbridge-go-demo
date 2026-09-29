// Command screener reads the Longbridge screener: the available indicator
// catalogue, the recommended strategies, the account's own saved strategies,
// one strategy by id, and a screener search.
//
// It is entirely READ-ONLY. The five ScreenerContext methods it drives are
// four GETs and one POST:
//
//	Indicators           GET  /v1/quote/ai/screener/indicators
//	RecommendStrategies  GET  /v1/quote/ai/screener/strategies/recommend
//	UserStrategies       GET  /v1/quote/ai/screener/strategies/mine
//	Strategy             GET  /v1/quote/ai/screener/strategy/{id}
//	Search               POST /v1/quote/ai/screener/search
//
// The POST is a query, not a mutation: it takes a market, a filter list and a
// page and returns matching securities. It is the same shape as the POSTs in
// cmd/market (TopMovers), and nothing server-side changes. So there is NO
// write gate here, no RequireLive, no --confirm flag and no dry-run gate. The
// startup banner asserts the order gate is still closed, which is the
// read-only invariant shared with cmd/reference, cmd/fundamentals and
// cmd/warrant.
//
// # THE RESPONSES ARE UNTYPED
//
// screener/types.go defines no structs for any payload. Every response is a
// wrapper whose only field is `Data json.RawMessage`, because the payload
// shape varies by indicator and strategy. There is therefore nothing to map
// field-by-field, and this command does not invent one: it pretty-prints the
// JSON with sorted keys and clips long output at 4 KB with a visible marker.
// Run -action indicators first to see the real shapes before assuming anything
// about a search result.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/screener"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// rawLimit caps how much of a raw-JSON payload is echoed. The screener
// responses are untyped by design, so a strategy listing or an indicator
// catalogue can run to hundreds of kilobytes. Printing all of it is not
// useful; printing nothing is worse, and a truncation marker keeps a clipped
// block from being mistaken for a complete one.
const rawLimit = 4000

// actionList is the -action vocabulary, kept in one place so the flag help,
// the dispatch switch and the error message cannot drift apart.
const actionList = "indicators, recommend, mine, strategy, search"

// helpNotes is appended to the standard usage envelope. NewUsage owns the
// flags/credentials/safety/exit-code block; wrapping it (rather than
// replacing it) keeps that block and adds the screener specifics that do not
// fit in a one-line flag description.
const helpNotes = `
Screener notes:

  -action search runs in one of two modes, chosen by -strategy-id:

    Mode B (no -strategy-id)   ONE request.  POST /v1/quote/ai/screener/search
                               with -market, -condition and -page/-size.

    Mode A (-strategy-id set)  TWO requests.  The SDK first GETs
                               /v1/quote/ai/screener/strategy/{id} to read the
                               strategy's own market and its filter.filters[],
                               then POSTs the search with those filters.
                               -condition is IGNORED in this mode, and -market
                               is IGNORED: the market comes from the strategy,
                               and the SDK substitutes "US" if the strategy's
                               market is blank or "-".

  -page is 0-INDEXED for the screener (page 0 is the first page). Every other
  command in this repo is 1-based. This is a property of the screener API, not
  a bug here.

  -strategy-id 0 is rejected, not treated as unset. The SDK signals "no
  strategy" with a nil *int64, so a 0 would otherwise become a non-nil pointer
  to strategy 0, silently switch search from Mode A to Mode B and return a
  different result set. Omit the flag to search with -condition.

  -condition bounds are strings in the SDK and are forwarded verbatim, so
  "-condition pettm::30" asks for PE(TTM) of at most 30 and
  "-condition pettm:0:1" asks for a value between them. An empty bound means
  "unbounded" in both directions; at least one bound or one tech value is
  required, otherwise the filter is rejected locally instead of being sent.

  The search always requests these return columns, in addition to the ones
  implied by the filters and by -show:
    filter_prevclose filter_prevchg filter_marketcap filter_salesgrowthyoy
    filter_pettm filter_pbmrq filter_industry

  All five responses are untyped JSON. They are printed as pretty-printed JSON
  with sorted keys, clipped at 4000 bytes per block. Use -action indicators to
  discover real field names rather than assuming any layout.
`

var (
	action     string
	market     string
	strategyID int64
	conditions string
	show       string
	page       int
	size       int
	timeout    time.Duration

	// strategyIDSet records whether -strategy-id appeared on the command line
	// at all, which a plain int64 default of 0 cannot express.
	strategyIDSet bool
)

func main() {
	u := cli.NewUsage("screener", "read-only stock screener: indicators, strategies and search")
	u.FS.StringVar(&action, "action", "indicators", "one of: "+actionList)
	u.FS.StringVar(&market, "market", "HK", "market for recommend, mine and search: HK, US, CN, SG, UK (ignored by search when -strategy-id is set)")
	u.FS.Int64Var(&strategyID, "strategy-id", 0, "for -action strategy (required, must be > 0) and -action search (optional; omit for none — 0 is rejected, not treated as unset)")
	u.FS.StringVar(&conditions, "condition", "",
		"for -action search, comma-separated KEY:MIN:MAX[:k=v;k=v]; either bound may be empty, e.g. pettm::30 or pettm:0:1; the filter_ prefix is optional")
	u.FS.StringVar(&show, "show", "",
		"for -action search, comma-separated extra return columns, e.g. capmk,roe")
	u.FS.IntVar(&page, "page", 0, "for -action search: page number, 0-INDEXED (0 = first page) — unlike every other command here, which is 1-based")
	u.FS.IntVar(&size, "size", 20, "for -action search: page size")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	stdUsage := u.FS.Usage
	u.FS.Usage = func() {
		stdUsage()
		fmt.Fprint(u.FS.Output(), helpNotes)
	}
	u.Parse(os.Args[1:])
	// Visit reports only the flags actually present, which is how -strategy-id
	// 0 is told apart from an omitted -strategy-id.
	u.FS.Visit(func(f *flag.Flag) {
		if f.Name == "strategy-id" {
			strategyIDSet = true
		}
	})

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary is read-only. If the order gate were open, stop.
	if err := cfg.GuardWrite("run the screener reader"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: screener is read-only but the order gate is open"))
	}

	plan, err := buildPlan()
	if err != nil {
		cli.Fail(err)
	}

	cli.Run(func(ctx context.Context) error {
		// Created lazily inside the run closure so a flag error above costs no
		// authentication. ScreenerContext has no Close() in v0.25.2 — it owns
		// no websocket — so there is nothing to defer here.
		var sc *screener.ScreenerContext
		connect := func() (*screener.ScreenerContext, error) {
			if sc != nil {
				return sc, nil
			}
			c, err := screener.NewFromCfg(cfg.SDK)
			if err != nil {
				return nil, fmt.Errorf("creating screener context: %w", err)
			}
			sc = c
			return sc, nil
		}
		return plan.run(ctx, connect)
	})
}

// ---------------------------------------------------------------------- plan

// plan is the validated form of every flag, resolved before any network call
// so a typo costs nothing.
type plan struct {
	action     string
	market     string
	strategyID *int64
	conditions []screener.ScreenerCondition
	condText   []string
	show       []string
	page       uint32
	size       uint32
}

// buildPlan validates all flags up front. Every check here is one the SDK
// would either reject with an opaque server error or, worse, quietly accept
// and return the wrong data for.
func buildPlan() (*plan, error) {
	act := strings.ToLower(strings.TrimSpace(action))
	switch act {
	case "indicators", "recommend", "mine", "strategy", "search":
	default:
		return nil, fmt.Errorf("unknown -action %q: want one of %s", action, actionList)
	}

	// Validated for every action, even the ones that do not use it, so a typo
	// is caught by the same code path regardless of -action. The screener API
	// takes the market as a bare string, so nothing in the SDK would stop a
	// misspelling from being sent and quietly matching nothing.
	mkt, err := parseMarket(market)
	if err != nil {
		return nil, err
	}

	if strategyID < 0 {
		return nil, fmt.Errorf("-strategy-id must be greater than 0, got %d", strategyID)
	}
	// A strategy id of 0 is not a "no strategy" signal in the SDK: a nil
	// *int64 is. Silently mapping an explicit 0 to nil would quietly switch the
	// user from Mode A to Mode B and return a completely different result set
	// from the one they asked for, so it is refused here instead.
	if strategyIDSet && strategyID == 0 {
		return nil, fmt.Errorf("-strategy-id 0 is not a strategy id; omit -strategy-id entirely to search " +
			"with -condition, or pass a real id from -action recommend")
	}
	if strategyID == 0 && act == "strategy" {
		return nil, fmt.Errorf("-action strategy requires -strategy-id (the strategy id; see -action recommend for candidates)")
	}

	p := &plan{action: act, market: mkt}
	if strategyID > 0 {
		// Copied rather than taking &strategyID, so the pointer handed to the
		// SDK cannot be aliased to the flag variable.
		id := strategyID
		p.strategyID = &id
	}
	usesStrategy := act == "strategy" || act == "search"
	if p.strategyID != nil && !usesStrategy {
		return nil, fmt.Errorf("-strategy-id applies only to -action strategy and -action search, not %q", act)
	}

	if act != "search" {
		if strings.TrimSpace(conditions) != "" {
			return nil, fmt.Errorf("-condition applies only to -action search, not %q", act)
		}
		if strings.TrimSpace(show) != "" {
			return nil, fmt.Errorf("-show applies only to -action search, not %q", act)
		}
	}
	if act == "search" {
		p.conditions, p.condText, err = parseConditions(conditions)
		if err != nil {
			return nil, err
		}
		p.show = splitList(show)
		if page < 0 {
			return nil, fmt.Errorf("-page must be 0 or greater (it is 0-indexed), got %d", page)
		}
		if size <= 0 {
			return nil, fmt.Errorf("-size must be greater than 0, got %d", size)
		}
		p.page, p.size = uint32(page), uint32(size)
	}
	return p, nil
}

func (p *plan) run(ctx context.Context, connect func() (*screener.ScreenerContext, error)) error {
	sc, err := connect()
	if err != nil {
		return err
	}
	switch p.action {
	case "indicators":
		return printIndicators(ctx, sc)
	case "recommend":
		return printStrategies(ctx, sc, p.market, false)
	case "mine":
		return printStrategies(ctx, sc, p.market, true)
	case "strategy":
		return printStrategy(ctx, sc, *p.strategyID)
	case "search":
		return printSearch(ctx, sc, p)
	}
	return fmt.Errorf("unknown -action %q: want one of %s", action, actionList)
}

// ------------------------------------------------------------------- actions

func printIndicators(ctx context.Context, sc *screener.ScreenerContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section("Screener indicators")
	res, err := sc.ScreenerIndicators(c)
	if err != nil {
		return fmt.Errorf("screener indicators: %w", err)
	}
	emit(res.Data)
	fmt.Println("\nKeys have the SDK's \"filter_\" prefix already stripped, and")
	fmt.Println("tech_values is built by the SDK from each indicator's tech_indicators.")
	fmt.Println("Use the keys printed here to build -condition values.")
	return nil
}

// printStrategies covers both list endpoints: they differ only in the path,
// and sharing one renderer keeps the two outputs comparable.
func printStrategies(ctx context.Context, sc *screener.ScreenerContext, mkt string, mine bool) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	var res json.RawMessage
	if mine {
		cli.Section(fmt.Sprintf("My screener strategies (%s)", mkt))
		r, err := sc.ScreenerUserStrategies(c, mkt)
		if err != nil {
			return fmt.Errorf("user strategies for %s: %w", mkt, err)
		}
		res = r.Data
	} else {
		cli.Section(fmt.Sprintf("Recommended screener strategies (%s)", mkt))
		r, err := sc.ScreenerRecommendStrategies(c, mkt)
		if err != nil {
			return fmt.Errorf("recommend strategies for %s: %w", mkt, err)
		}
		res = r.Data
	}
	emit(res)
	fmt.Println("\nFeed a strategy id to -action strategy or -action search -strategy-id.")
	return nil
}

func printStrategy(ctx context.Context, sc *screener.ScreenerContext, id int64) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Screener strategy %d", id))
	res, err := sc.ScreenerStrategy(c, id)
	if err != nil {
		return fmt.Errorf("screener strategy %d: %w", id, err)
	}
	emit(res.Data)
	fmt.Println("\nThe SDK strips the \"filter_\" prefix from every filter.filters[].key.")
	return nil
}

func printSearch(ctx context.Context, sc *screener.ScreenerContext, p *plan) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section("Screener search")
	if p.strategyID != nil {
		fmt.Printf("mode A (-strategy-id %d): the SDK issues TWO requests — it first GETs\n"+
			"  /v1/quote/ai/screener/strategy/%d to read the strategy's market and its\n"+
			"  filter.filters[], then POSTs the search with those filters.\n"+
			"  -market (%s) and -condition are ignored in this mode; the SDK uses the\n"+
			"  strategy's own market, falling back to \"US\" when it is blank or \"-\".\n",
			*p.strategyID, *p.strategyID, p.market)
	} else {
		fmt.Printf("mode B (no -strategy-id): ONE POST request, market=%s, conditions=%d, page=%d size=%d\n",
			p.market, len(p.conditions), p.page, p.size)
		for _, t := range p.condText {
			fmt.Printf("   condition %s\n", t)
		}
	}
	if len(p.show) > 0 {
		fmt.Printf("extra return columns: %s\n", strings.Join(p.show, ", "))
	}
	fmt.Printf("-page is 0-indexed: page %d means the %s page (0 = first).\n",
		p.page, ordinal(p.page+1))

	res, err := sc.ScreenerSearch(c, p.market, p.strategyID, p.conditions, p.show, p.page, p.size)
	if err != nil {
		return fmt.Errorf("screener search: %w", err)
	}
	emit(res.Data)
	fmt.Println("\nitems[].indicators[].key has the \"filter_\" prefix stripped by the SDK.")
	return nil
}

// ------------------------------------------------------------------ rendering

// emit prints one untyped screener payload.
//
// The screener package ships no response structs: screener/types.go defines
// five response types whose single field is `Data json.RawMessage`, precisely
// because the payload varies. Inventing a typed layout for fields that have
// never been seen would be a guess dressed as a fact, so this is a
// pretty-printer. Determinism comes from re-marshalling — encoding/json emits
// object keys in sorted order — so the same payload always prints identically
// and two runs can be diffed.
func emit(raw json.RawMessage) {
	if len(raw) == 0 {
		fmt.Println("   (empty response body)")
		return
	}
	fmt.Println(prettyJSON(raw))
	fmt.Printf("(%d bytes of JSON, pretty-printed with sorted keys)\n", len(raw))
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Not decodable as JSON: show it verbatim rather than failing the run.
		return clip(string(raw))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Indicator labels and company names can legitimately contain <, > and &.
	// Escaping them to < / > / & makes the output harder to read and buys
	// nothing for terminal output.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return clip(string(raw))
	}
	return clip(strings.TrimRight(buf.String(), "\n"))
}

func clip(s string) string {
	if len(s) <= rawLimit {
		return s
	}
	// The cut can land mid-rune; drop the partial tail so the marker is never
	// preceded by a broken character.
	head := strings.ToValidUTF8(s[:rawLimit], "")
	return head + fmt.Sprintf("…(truncated, %d bytes total)", len(s))
}

// -------------------------------------------------------------------- helpers

func withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// parseMarket maps -market to the screener's market string through an explicit
// switch. The screener API takes a bare string, so the SDK offers nothing to
// catch a typo: "USA", "hk-ex" or "" would be sent verbatim and come back empty,
// which reads as "no strategies exist" rather than as a mistake. A bare
// untested value is never passed through.
func parseMarket(s string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "HK", "US", "CN", "SG", "UK":
		return strings.ToUpper(strings.TrimSpace(s)), nil
	}
	return "", fmt.Errorf("unknown -market %q: want HK, US, CN, SG or UK", s)
}

// parseConditions parses the comma-separated -condition list into
// ScreenerCondition values, plus the same values in their original text form
// for echoing back to the user.
//
// Each entry is KEY:MIN:MAX with an optional fourth KEY:MIN:MAX:k=v;k=v
// segment for the TechValues map. Both bounds may be empty, meaning
// unbounded, which is why the fields are separated by ':' rather than by a
// positional shorthand that could not express "no upper bound". The empty
// middle in pettm::30 is deliberate, not a typo.
func parseConditions(s string) ([]screener.ScreenerCondition, []string, error) {
	entries := splitList(s)
	if len(entries) == 0 {
		return nil, nil, nil
	}
	out := make([]screener.ScreenerCondition, 0, len(entries))
	text := make([]string, 0, len(entries))
	for _, e := range entries {
		parts := strings.Split(e, ":")
		if len(parts) > 4 {
			return nil, nil, fmt.Errorf("malformed -condition %q: want KEY:MIN:MAX[:k=v;k=v] "+
				"(at most three colons)", e)
		}
		for len(parts) < 4 {
			parts = append(parts, "")
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, nil, fmt.Errorf("malformed -condition %q: the indicator key is empty", e)
		}
		if strings.ContainsAny(key, " \t") {
			return nil, nil, fmt.Errorf("malformed -condition %q: the indicator key %q contains whitespace", e, key)
		}
		c := screener.ScreenerCondition{
			Key: key,
			Min: strings.TrimSpace(parts[1]),
			Max: strings.TrimSpace(parts[2]),
		}
		if c.Min == "" && c.Max == "" {
			if strings.TrimSpace(parts[3]) == "" {
				return nil, nil, fmt.Errorf("malformed -condition %q: give a min, a max or a tech value "+
					"(e.g. pettm::30, pettm:0:1 or macd_day:::<category>=goldenfork)", e)
			}
		}
		tv, err := parseTechValues(e, parts[3])
		if err != nil {
			return nil, nil, err
		}
		c.TechValues = tv
		out = append(out, c)
		text = append(text, e)
	}
	return out, text, nil
}

// parseTechValues reads the optional fourth segment of a condition: semicolon
// separated key=value pairs, e.g. "category=goldenfork;period=day".
func parseTechValues(entry, spec string) (map[string]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	out := map[string]string{}
	for _, kv := range strings.Split(spec, ";") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("malformed -condition %q: tech value %q is not key=value", entry, kv)
		}
		out[k] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("malformed -condition %q: the tech-value segment %q held no key=value pair", entry, spec)
	}
	return out, nil
}

// ordinal renders a 1-based page number for display only. The value sent to
// the API stays 0-indexed; this only keeps the echoed message unambiguous, so
// that a user who typed "-page 0" can see it means the first page.
func ordinal(n uint32) string {
	s := strconv.FormatUint(uint64(n), 10)
	// 11, 12 and 13 are exceptions to the last-digit rule.
	if len(s) >= 2 {
		switch s[len(s)-2:] {
		case "11", "12", "13":
			return s + "th"
		}
	}
	switch s[len(s)-1] {
	case '1':
		return s + "st"
	case '2':
		return s + "nd"
	case '3':
		return s + "rd"
	}
	return s + "th"
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
