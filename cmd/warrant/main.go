// Command warrant explores Hong Kong derivative warrants: the warrant list
// issued over an underlying, live quotes for specific warrant symbols, and the
// list of issuers.
//
// It is entirely READ-ONLY: it touches only WarrantList, WarrantQuote and
// WarrantIssuers, none of which mutate anything. There is no code path here
// that can place, modify or cancel an order, and the startup banner asserts
// that the order gate is still closed.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/quote"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action      string
	underlying  string
	warrantSyms string
	sortBy      string
	sortOrder   string
	sortCount   int
	sortOffset  int
	warrantType string
	expiry      string
	inOut       string
	status      string
	language    string
	timeout     time.Duration
)

func main() {
	u := cli.NewUsage("warrant", "read-only HK warrant data: list, quote, issuers")
	u.FS.StringVar(&action, "action", "list",
		"list (warrants over -underlying) | quote (-warrant-symbols) | issuers")
	u.FS.StringVar(&underlying, "underlying", "700.HK", "underlying stock symbol, e.g. 700.HK")
	u.FS.StringVar(&warrantSyms, "warrant-symbols", "12181.HK,67408.HK",
		"comma-separated warrant symbols for -action quote")
	u.FS.StringVar(&sortBy, "sort-by", "last_done",
		"sort field: last_done change_rate change_val volume turnover expiry_date strike_price outstanding_qty implied_volatility delta status")
	u.FS.StringVar(&sortOrder, "sort-order", "desc", "asc or desc; surrounding whitespace is ignored")
	u.FS.IntVar(&sortCount, "count", 10, "how many warrants to return (sort_count)")
	u.FS.IntVar(&sortOffset, "offset", 0, "sort offset for paging")
	u.FS.StringVar(&warrantType, "type", "", "optional type filter: call put bull bear inline (comma-separated, empty = all)")
	u.FS.StringVar(&expiry, "expiry", "", "optional expiry filter: lt3 bt3_6 bt6_12 gt12 (comma-separated)")
	u.FS.StringVar(&inOut, "moneyness", "", "optional moneyness filter: in out (comma-separated)")
	u.FS.StringVar(&status, "status", "", "optional status filter: suspend listed normal (comma-separated)")
	u.FS.StringVar(&language, "language", "zh-hk", "zh-cn, en or zh-hk")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary is read-only; if the order gate were open something
	// is wrong with the environment and we should not keep going.
	if err := cfg.GuardWrite("run the warrant reader"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: warrant is read-only but the order gate is open"))
	}

	cli.Run(func(ctx context.Context) error {
		qc, err := quote.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating quote context: %w", err)
		}
		defer func() {
			if err := qc.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: closing quote context: %v\n", err)
			}
		}()

		switch strings.ToLower(action) {
		case "list":
			return printWarrantList(ctx, qc)
		case "quote":
			return printWarrantQuotes(ctx, qc)
		case "issuers":
			return printWarrantIssuers(ctx, qc)
		default:
			return fmt.Errorf("unknown -action %q: want list, quote or issuers", action)
		}
	})
}

// ------------------------------------------------------------------- actions

func printWarrantList(ctx context.Context, qc *quote.QuoteContext) error {
	filter, err := buildWarrantFilter()
	if err != nil {
		return err
	}
	lang, err := parseWarrantLanguage(language)
	if err != nil {
		return err
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Warrants over %s", underlying))
	fmt.Printf("filter: sort_by=%d sort_order=%d count=%d offset=%d lang=%d types=%v expiry=%v moneyness=%v status=%v\n",
		filter.SortBy, filter.SortOrder, filter.SortCount, filter.SortOffset, lang,
		filter.Type, filter.ExpiryDate, filter.PriceType, filter.Status)

	infos, err := qc.WarrantList(c, underlying, filter, lang)
	if err != nil {
		return fmt.Errorf("warrant list for %s: %w", underlying, err)
	}
	if len(infos) == 0 {
		fmt.Println("   (no warrants matched)")
		return nil
	}
	fmt.Printf("%-12s %-26s %-9s %-9s %-9s %-11s %-8s %s\n",
		"SYMBOL", "NAME", "LAST", "CHG%", "VOLUME", "EXPIRY", "STATUS", "STRIKE")
	for _, w := range infos {
		fmt.Printf("%-12s %-26s %-9s %-9s %-9d %-11s %-8s %s\n",
			w.Symbol, cli.Truncate(w.Name, 26), cli.Dec4(w.LastDone),
			cli.Dec4(w.ChangeRate), w.Volume, cli.OrDash(w.ExpiryDate),
			warrantStatusName(w.Status), cli.Dec4(w.StrikePrice))
	}
	fmt.Printf("\n(%d warrants; narrow with -count/-offset, or filter with -type/-expiry)\n", len(infos))
	return nil
}

func printWarrantQuotes(ctx context.Context, qc *quote.QuoteContext) error {
	syms := splitList(warrantSyms)
	if len(syms) == 0 {
		return fmt.Errorf("-warrant-symbols is empty")
	}

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Warrant quotes")
	quotes, err := qc.WarrantQuote(c, syms)
	if err != nil {
		return fmt.Errorf("warrant quote: %w", err)
	}
	if len(quotes) == 0 {
		fmt.Println("   (no quotes returned)")
		return nil
	}
	fmt.Printf("%-12s %-9s %-9s %-9s %-9s %-9s %-11s %s\n",
		"SYMBOL", "LAST", "OPEN", "HIGH", "LOW", "PREV", "TURNOVER", "TIME")
	for _, q := range quotes {
		fmt.Printf("%-12s %-9s %-9s %-9s %-9s %-9s %-11s %s\n",
			q.Symbol, cli.Dec4(q.LastDone), cli.Dec4(q.Open), cli.Dec4(q.High),
			cli.Dec4(q.Low), cli.Dec4(q.PrevClose), cli.Dec(q.Turnover),
			cli.FmtTime(q.Timestamp))
		// The extended block is where the interesting warrant specifics live.
		if e := q.WarrantExtend; e != nil {
			fmt.Printf("    type=%s underlying=%s strike=%s conv_ratio=%s expiry=%s last_trade=%s iv=%s oi=%d\n",
				cli.OrDash(e.Category), cli.OrDash(e.UnderlyingSymbol),
				cli.Dec4(e.StrikePrice), cli.OrDash(e.ConversionRatio),
				cli.OrDash(e.ExpiryDate), cli.OrDash(e.LastTradeDate),
				cli.OrDash(e.ImpliedVolatility), e.OutstandingQty)
		}
	}
	return nil
}

func printWarrantIssuers(ctx context.Context, qc *quote.QuoteContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Warrant issuers")
	infos, err := qc.WarrantIssuers(c)
	if err != nil {
		return fmt.Errorf("warrant issuers: %w", err)
	}
	if len(infos) == 0 {
		fmt.Println("   (no issuers returned)")
		return nil
	}
	fmt.Printf("%-8s %-30s %-30s %s\n", "ID", "NAME_CN", "NAME_EN", "NAME_HK")
	for _, i := range infos {
		fmt.Printf("%-8d %-30s %-30s %s\n",
			i.Id, cli.Truncate(i.NameCn, 30), cli.Truncate(i.NameEn, 30),
			cli.Truncate(i.NameHk, 30))
	}
	fmt.Printf("\n(%d issuers; use -issuer-filter with -action list to scope the list)\n", len(infos))
	return nil
}

// ------------------------------------------------------------------- helpers

// buildWarrantFilter turns the string flags into the SDK's WarrantFilter.
//
// The SDK models each enum as a bare int32, so the mapping is by explicit
// switch rather than by casting. An unknown value is an error, never a silent
// zero, because a wrong cast here would quietly return the wrong warrants.
// All six parsers fold case and trim, so a value pasted with padding is
// accepted the same way by every one of them.
func buildWarrantFilter() (quote.WarrantFilter, error) {
	var f quote.WarrantFilter

	sb, err := parseWarrantSortBy(sortBy)
	if err != nil {
		return f, err
	}
	f.SortBy = sb

	switch strings.ToLower(strings.TrimSpace(sortOrder)) {
	case "asc":
		f.SortOrder = quote.WarrantAsc
	case "desc":
		f.SortOrder = quote.WarrantDesc
	default:
		return f, fmt.Errorf("unknown -sort-order %q: want asc or desc", sortOrder)
	}

	if sortCount <= 0 {
		return f, fmt.Errorf("-count must be greater than 0, got %d", sortCount)
	}
	if sortOffset < 0 {
		return f, fmt.Errorf("-offset must not be negative, got %d", sortOffset)
	}
	f.SortCount = int32(sortCount)
	f.SortOffset = int32(sortOffset)

	if f.Type, err = parseWarrantTypes(warrantType); err != nil {
		return f, err
	}
	if f.ExpiryDate, err = parseWarrantExpiry(expiry); err != nil {
		return f, err
	}
	if f.PriceType, err = parseWarrantMoneyness(inOut); err != nil {
		return f, err
	}
	if f.Status, err = parseWarrantStatus(status); err != nil {
		return f, err
	}
	return f, nil
}

func parseWarrantSortBy(s string) (quote.WarrantSortBy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "last_done", "lastdone":
		return quote.WarrantLastDone, nil
	case "change_rate":
		return quote.WarrantChangeRate, nil
	case "change_val":
		return quote.WarrantChangeVal, nil
	case "volume":
		return quote.WarrantVolume, nil
	case "turnover":
		return quote.WarrantTurnover, nil
	case "expiry_date":
		return quote.WarrantExpiryDate, nil
	case "strike_price":
		return quote.WarrantStrikePrice, nil
	case "outstanding_qty":
		return quote.WarrantOutstandingQty, nil
	case "implied_volatility":
		return quote.WarrantImpliedVolatility, nil
	case "delta":
		return quote.WarrantDelta, nil
	case "status":
		return quote.WarrantSortStatus, nil
	}
	return 0, fmt.Errorf("unknown -sort-by %q", s)
}

func parseWarrantTypes(s string) ([]quote.WarrantType, error) {
	var out []quote.WarrantType
	for _, p := range splitList(s) {
		switch strings.ToLower(p) {
		case "call":
			out = append(out, quote.WarrantCall)
		case "put":
			out = append(out, quote.WarrantPut)
		case "bull":
			out = append(out, quote.WarrantBull)
		case "bear":
			out = append(out, quote.WarrantBear)
		case "inline":
			out = append(out, quote.WarrantInline)
		default:
			return nil, fmt.Errorf("unknown -type %q: want call, put, bull, bear or inline", p)
		}
	}
	return out, nil
}

func parseWarrantExpiry(s string) ([]quote.WarrantExpiryDateType, error) {
	var out []quote.WarrantExpiryDateType
	for _, p := range splitList(s) {
		switch strings.ToLower(p) {
		case "lt3":
			out = append(out, quote.WarrantLT3)
		case "bt3_6", "3_6":
			out = append(out, quote.WarrantBT3_6)
		case "bt6_12", "6_12":
			out = append(out, quote.WarrantBT6_12)
		case "gt12", "12":
			out = append(out, quote.WarrantGT12)
		default:
			return nil, fmt.Errorf("unknown -expiry %q: want lt3, bt3_6, bt6_12 or gt12", p)
		}
	}
	return out, nil
}

func parseWarrantMoneyness(s string) ([]quote.WarrantInOutBoundsType, error) {
	var out []quote.WarrantInOutBoundsType
	for _, p := range splitList(s) {
		switch strings.ToLower(p) {
		case "in", "in_bounds":
			out = append(out, quote.WarrantInBounds)
		case "out", "out_bounds":
			out = append(out, quote.WarrantOutBounds)
		default:
			return nil, fmt.Errorf("unknown -moneyness %q: want in or out", p)
		}
	}
	return out, nil
}

func parseWarrantStatus(s string) ([]quote.WarrantStatus, error) {
	var out []quote.WarrantStatus
	for _, p := range splitList(s) {
		switch strings.ToLower(p) {
		case "suspend":
			out = append(out, quote.WarrantSuspend)
		case "listed", "paparelist":
			out = append(out, quote.WarrantPapareList)
		case "normal":
			out = append(out, quote.WarrantNormal)
		default:
			return nil, fmt.Errorf("unknown -status %q: want suspend, listed or normal", p)
		}
	}
	return out, nil
}

func parseWarrantLanguage(s string) (quote.WarrantLanguage, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh-cn", "zh_cn", "zhcn":
		return quote.WarrantZH_CN, nil
	case "en", "en-us":
		return quote.WarrantEN, nil
	case "zh-hk", "zh_hk", "zhhk", "hk":
		return quote.WarrantHK_CN, nil
	}
	return 0, fmt.Errorf("unknown -language %q: want zh-cn, en or zh-hk", s)
}

// warrantStatusName renders WarrantStatus, which is a bare int32 with no
// String() method. Printing the raw int would pass vet but tell the user
// nothing, so map it explicitly and fall back to the number.
func warrantStatusName(s quote.WarrantStatus) string {
	switch s {
	case quote.WarrantSuspend:
		return "suspend"
	case quote.WarrantPapareList:
		return "pending"
	case quote.WarrantNormal:
		return "normal"
	}
	return "unknown(" + strconv.Itoa(int(s)) + ")"
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
