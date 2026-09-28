// Command market prints reference data: market-wide trading status, trading
// calendar days, an intraday price timeline, and the rest of the read-only
// surface of the SDK's market package — A/H premium, market anomaly alerts,
// broker holdings, index constituents, top movers, ranked lists and trade
// statistics.
//
// It is entirely READ-ONLY. Every method it calls is a GET (or, for
// TopMovers, a POST that only queries) and none of them mutate server-side
// state, so there is no gate to trip here.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go"
	"github.com/longbridge/openapi-go/market"
	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	sections    string
	marketCode  string
	daysBack    int
	daysFwd     int
	timeline    string
	lastLines   int
	timeout     time.Duration
	ahSymbol    string
	ahPeriod    string
	ahCount     int
	anomalyMkt  string
	brokerSym   string
	brokerPart  string
	brokerPer   string
	indexSymbol string
	moversMkts  string
	moversSort  string
	moversDate  string
	moversLimit int
	rankKey     string
	rankArticle bool
	tradeSym    string
)

const sectionList = "status, calendar, timeline, " +
	"ahpremium, ahpremium-intraday, anomaly, " +
	"broker-holding, broker-holding-daily, broker-holding-detail, " +
	"constituent, top-movers, rank-categories, rank-list, trade-stats"

func main() {
	u := cli.NewUsage("market", "market status, calendar, intraday timeline and the read-only market package")
	u.FS.StringVar(&sections, "sections", "status,calendar,timeline",
		"comma-separated subset of: "+sectionList)
	u.FS.StringVar(&marketCode, "market", "HK", "market code for the calendar and anomaly alerts: HK, US, CN, SG, UK")
	u.FS.IntVar(&daysBack, "back", 7, "calendar days to look back")
	u.FS.IntVar(&daysFwd, "forward", 7, "calendar days to look forward")
	u.FS.StringVar(&timeline, "timeline-symbol", "700.HK", "symbol for the intraday timeline")
	u.FS.IntVar(&lastLines, "lines", 20, "number of intraday lines to print (most recent last)")

	u.FS.StringVar(&ahSymbol, "ah-symbol", "00700.HK", "dual-listed symbol for the A/H premium sections")
	u.FS.StringVar(&ahPeriod, "ah-period", "day", "A/H premium kline period: 1m 5m 15m 30m 60m day week month year")
	u.FS.IntVar(&ahCount, "ah-count", 30, "number of A/H premium klines to print")
	u.FS.StringVar(&anomalyMkt, "anomaly-market", "HK", "market for the anomaly alert section")
	u.FS.StringVar(&brokerSym, "broker-symbol", "700.HK", "symbol for the broker holding sections")
	u.FS.StringVar(&brokerPart, "broker-id", "", "broker participant number, required for the broker-holding-daily section")
	u.FS.StringVar(&brokerPer, "broker-period", "5", "broker holding window: 1 5 20 60")
	u.FS.StringVar(&indexSymbol, "index", "HSI.HK", "index symbol for the constituent section, e.g. HSI.HK")
	u.FS.StringVar(&moversMkts, "mover-markets", "HK", "comma-separated markets for top movers, e.g. HK,US")
	u.FS.StringVar(&moversSort, "mover-sort", "desc", "top movers sort order: asc or desc")
	u.FS.StringVar(&moversDate, "mover-date", "", "optional YYYY-MM-DD filter for top movers (empty = today)")
	u.FS.IntVar(&moversLimit, "mover-limit", 10, "maximum top movers to return")
	u.FS.StringVar(&rankKey, "rank-key", "", "rank category key from the rank-categories section, e.g. turnover")
	u.FS.BoolVar(&rankArticle, "rank-article", false, "ask the rank list for article content as well")
	u.FS.StringVar(&tradeSym, "trade-stats-symbol", "700.HK", "symbol for the trade statistics section")

	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)

	wanted := map[string]bool{}
	for _, s := range strings.Split(sections, ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			wanted[s] = true
		}
	}
	for s := range wanted {
		if !validSection(s) {
			cli.Fail(fmt.Errorf("unknown section %q in -sections: want one of %s", s, sectionList))
		}
	}
	if wanted["broker-holding-daily"] && strings.TrimSpace(brokerPart) == "" {
		cli.Fail(fmt.Errorf("-broker-id is required for the broker-holding-daily section " +
			"(a broker participant number, e.g. from the broker-holding section)"))
	}
	if wanted["rank-list"] && strings.TrimSpace(rankKey) == "" {
		cli.Fail(fmt.Errorf("-rank-key is required for the rank-list section; " +
			"run -sections rank-categories to see the available keys"))
	}

	cli.Run(func(ctx context.Context) error {
		// The market package has no Close(); it is a thin HTTP client.
		mc, err := market.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating market context: %w", err)
		}

		// Calendar and timeline live on the quote context; everything else is on
		// the market context. The quote context opens a websocket and must be
		// closed, so it is created only when a section actually needs it —
		// otherwise a market-only run would pay for (and fail on) a connection
		// it never uses.
		var (
			qc  *quote.QuoteContext
			qce error
		)
		quoteCtx := func() (*quote.QuoteContext, error) {
			if qc == nil && qce == nil {
				qc, qce = quote.NewFromCfg(cfg.SDK)
			}
			return qc, qce
		}
		defer func() {
			if qc != nil {
				if err := qc.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "warning: closing quote context: %v\n", err)
				}
			}
		}()

		run := []struct {
			section string
			fn      func(context.Context) error
		}{
			{"status", func(c context.Context) error { return printStatus(c, mc) }},
			{"calendar", func(c context.Context) error {
				q, err := quoteCtx()
				if err != nil {
					return fmt.Errorf("creating quote context: %w", err)
				}
				return printCalendar(c, q)
			}},
			{"timeline", func(c context.Context) error {
				q, err := quoteCtx()
				if err != nil {
					return fmt.Errorf("creating quote context: %w", err)
				}
				return printTimeline(c, q)
			}},
			{"ahpremium", func(c context.Context) error { return printAhPremium(c, mc) }},
			{"ahpremium-intraday", func(c context.Context) error { return printAhPremiumIntraday(c, mc) }},
			{"anomaly", func(c context.Context) error { return printAnomaly(c, mc) }},
			{"broker-holding", func(c context.Context) error { return printBrokerHolding(c, mc) }},
			{"broker-holding-detail", func(c context.Context) error { return printBrokerHoldingDetail(c, mc) }},
			{"broker-holding-daily", func(c context.Context) error { return printBrokerHoldingDaily(c, mc) }},
			{"constituent", func(c context.Context) error { return printConstituent(c, mc) }},
			{"top-movers", func(c context.Context) error { return printTopMovers(c, mc) }},
			{"rank-categories", func(c context.Context) error { return printRankCategories(c, mc) }},
			{"rank-list", func(c context.Context) error { return printRankList(c, mc) }},
			{"trade-stats", func(c context.Context) error { return printTradeStats(c, mc) }},
		}
		for _, r := range run {
			if wanted[r.section] {
				if err := r.fn(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func validSection(s string) bool {
	switch s {
	case "status", "calendar", "timeline", "ahpremium", "ahpremium-intraday",
		"anomaly", "broker-holding", "broker-holding-daily", "broker-holding-detail",
		"constituent", "top-movers", "rank-categories", "rank-list", "trade-stats":
		return true
	}
	return false
}

func printStatus(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Println("=== Market status ===")
	st, err := mc.MarketStatus(c)
	if err != nil {
		return fmt.Errorf("market status: %w", err)
	}
	if len(st.MarketTime) == 0 {
		fmt.Println("   (no market status returned)")
		return nil
	}
	fmt.Printf("%-8s %-8s %-20s %-7s %-20s %-7s %s\n",
		"MARKET", "CODE", "STATUS", "TRADING", "DELAY STATUS", "DELAY", "SUB")
	for _, m := range st.MarketTime {
		label := m.TradeStatus.Label()
		if label == "" {
			label = "-"
		}
		delayLabel := m.DelayTradeStatus.Label()
		if delayLabel == "" {
			delayLabel = "-"
		}
		fmt.Printf("%-8s %-8d %-20s %-7v %-20s %-7v %d/%d\n",
			m.Market, m.TradeStatus.Code(), label, m.TradeStatus.IsTrading(),
			delayLabel, m.DelayTradeStatus.IsTrading(), m.SubStatus, m.DelaySubStatus)
	}
	return nil
}

func printCalendar(ctx context.Context, qc *quote.QuoteContext) error {
	mkt, err := parseMarket(marketCode)
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	end := time.Now().AddDate(0, 0, daysFwd)
	start := time.Now().AddDate(0, 0, -daysBack)

	fmt.Printf("\n=== Trading calendar %s (%s .. %s) ===\n",
		marketCode, start.Format("2006-01-02"), end.Format("2006-01-02"))
	days, err := qc.TradingDays(c, mkt, &start, &end)
	if err != nil {
		return fmt.Errorf("trading days for %s: %w", marketCode, err)
	}
	if days == nil {
		fmt.Println("   (no calendar returned)")
		return nil
	}
	fmt.Printf("full trading days: %d\n", len(days.TradeDay))
	for _, d := range days.TradeDay {
		fmt.Printf("   %s\n", d.Format("2006-01-02"))
	}
	if len(days.HalfTradeDay) > 0 {
		fmt.Printf("half trading days: %d\n", len(days.HalfTradeDay))
		for _, d := range days.HalfTradeDay {
			fmt.Printf("   %s (half day)\n", d.Format("2006-01-02"))
		}
	}
	return nil
}

func printTimeline(ctx context.Context, qc *quote.QuoteContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Printf("\n=== Intraday timeline %s (last %d lines) ===\n", timeline, lastLines)
	lines, err := qc.Intraday(c, timeline)
	if err != nil {
		return fmt.Errorf("intraday %s: %w", timeline, err)
	}
	if len(lines) == 0 {
		fmt.Println("   (no intraday data; the market is likely closed)")
		return nil
	}
	shown := lines
	if len(shown) > lastLines {
		shown = shown[len(shown)-lastLines:]
	}
	fmt.Printf("%-20s %-12s %-14s %-14s %s\n", "TIME", "PRICE", "AVG", "TURNOVER", "VOLUME")
	for _, l := range shown {
		fmt.Printf("%-20s %-12s %-14s %-14s %d\n",
			cli.FmtTime(l.Timestamp), dec(l.Price), dec(l.AvgPrice),
			dec(l.Turnover), l.Volume)
	}
	return nil
}

// printAhPremium renders A/H premium klines. The SDK's AhPremiumKline holds
// plain decimal.Decimal values (not pointers), so there is no absent-vs-zero
// distinction to preserve here.
func printAhPremium(ctx context.Context, mc *market.MarketContext) error {
	period, err := parseAhPeriod(ahPeriod)
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("A/H premium klines %s (%s, last %d)", ahSymbol, ahPeriod, ahCount))
	kl, err := mc.AhPremium(c, ahSymbol, period, uint32(ahCount))
	if err != nil {
		return fmt.Errorf("a/h premium for %s: %w", ahSymbol, err)
	}
	printAhKlines(kl.Klines, ahCount)
	return nil
}

func printAhPremiumIntraday(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("A/H premium intraday %s", ahSymbol))
	in, err := mc.AhPremiumIntraday(c, ahSymbol)
	if err != nil {
		return fmt.Errorf("a/h premium intraday for %s: %w", ahSymbol, err)
	}
	if in == nil {
		fmt.Println("   (no intraday A/H premium data; the market is likely closed)")
		return nil
	}
	printAhKlines(in.Klines, ahCount)
	return nil
}

func printAhKlines(klines []market.AhPremiumKline, max int) {
	if len(klines) == 0 {
		fmt.Println("   (no klines returned)")
		return
	}
	shown := klines
	if max > 0 && len(shown) > max {
		shown = shown[len(shown)-max:]
	}
	fmt.Printf("%-20s %-12s %-12s %-12s %-12s %-10s %s\n",
		"TIME", "A PRICE", "A PRECLOSE", "H PRICE", "H PRECLOSE", "FX", "PREMIUM%")
	for _, k := range shown {
		fmt.Printf("%-20s %-12s %-12s %-12s %-12s %-10s %s\n",
			ts(k.Timestamp), k.Aprice.StringFixed(3), k.Apreclose.StringFixed(3),
			k.Hprice.StringFixed(3), k.Hpreclose.StringFixed(3),
			k.CurrencyRate.StringFixed(4), k.AhpremiumRate.StringFixed(3))
	}
}

func printAnomaly(ctx context.Context, mc *market.MarketContext) error {
	if _, err := parseMarket(anomalyMkt); err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Market anomaly alerts %s", strings.ToUpper(anomalyMkt)))
	res, err := mc.Anomaly(c, anomalyMkt)
	if err != nil {
		return fmt.Errorf("anomaly alerts for %s: %w", anomalyMkt, err)
	}
	if res == nil {
		fmt.Println("   (no anomaly response)")
		return nil
	}
	if res.AllOff {
		fmt.Println("   anomaly alerts are switched off account-wide (all_off)")
	}
	if len(res.Changes) == 0 {
		fmt.Println("   (no anomaly alerts right now)")
		return nil
	}
	fmt.Printf("%-12s %-26s %-24s %-20s %-8s %s\n",
		"SYMBOL", "NAME", "ALERT", "TIME", "EMOTION", "CHANGES")
	for _, it := range res.Changes {
		fmt.Printf("%-12s %-26s %-24s %-20s %-8d %s\n",
			it.Symbol, cli.Truncate(it.Name, 26), cli.Truncate(it.AlertName, 24),
			cli.FmtTime(it.AlertTime), it.Emotion, strings.Join(it.ChangeValues, " "))
	}
	fmt.Printf("\n(%d anomaly alerts)\n", len(res.Changes))
	return nil
}

func printBrokerHolding(ctx context.Context, mc *market.MarketContext) error {
	period, err := parseBrokerPeriod(brokerPer)
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Broker holding %s (rct_%d)", brokerSym, int(period)))
	res, err := mc.BrokerHolding(c, brokerSym, period)
	if err != nil {
		return fmt.Errorf("broker holding for %s: %w", brokerSym, err)
	}
	if res == nil {
		fmt.Println("   (no broker holding data)")
		return nil
	}
	fmt.Printf("updated_at: %s\n", cli.OrDash(res.UpdatedAt))
	printBrokerEntries("BUY", res.Buy)
	printBrokerEntries("SELL", res.Sell)
	fmt.Println("\nPass one of the BROKER values above to -broker-id for -sections broker-holding-daily.")
	return nil
}

func printBrokerEntries(side string, entries []market.BrokerHoldingEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Printf("\n%s side\n", side)
	fmt.Printf("%-28s %-24s %-12s %s\n", "BROKER", "PARTI", "CHANGE", "STRONG")
	for _, e := range entries {
		fmt.Printf("%-28s %-24s %-12s %v\n",
			cli.Truncate(e.Name, 28), cli.OrDash(e.PartiNumber), cli.Dec(e.Chg), e.Strong)
	}
}

func printBrokerHoldingDetail(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Broker holding detail %s", brokerSym))
	res, err := mc.BrokerHoldingDetail(c, brokerSym)
	if err != nil {
		return fmt.Errorf("broker holding detail for %s: %w", brokerSym, err)
	}
	if res == nil {
		fmt.Println("   (no broker holding detail)")
		return nil
	}
	fmt.Printf("updated_at: %s   brokers: %d\n", cli.OrDash(res.UpdatedAt), len(res.List))
	if len(res.List) == 0 {
		fmt.Println("   (empty)")
		return nil
	}
	fmt.Printf("%-24s %-18s %-30s %-30s %s\n", "PARTI", "BROKER", "RATIO (val/1/5/20/60)", "SHARES (val/1/5/20/60)", "STRONG")
	for _, it := range res.List {
		fmt.Printf("%-24s %-18s %-30s %-30s %v\n",
			cli.OrDash(it.PartiNumber), cli.Truncate(it.Name, 18),
			fmtChanges(it.Ratio), fmtChanges(it.Shares), it.Strong)
	}
	return nil
}

func fmtChanges(c market.BrokerHoldingChanges) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s",
		cli.Dec(c.Value), cli.Dec(c.Chg1), cli.Dec(c.Chg5),
		cli.Dec(c.Chg20), cli.Dec(c.Chg60))
}

func printBrokerHoldingDaily(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Broker holding daily %s broker %s", brokerSym, brokerPart))
	res, err := mc.BrokerHoldingDaily(c, brokerSym, brokerPart)
	if err != nil {
		return fmt.Errorf("broker holding daily for %s/%s: %w", brokerSym, brokerPart, err)
	}
	if res == nil || len(res.List) == 0 {
		fmt.Println("   (no daily history for this broker)")
		return nil
	}
	fmt.Printf("%-14s %-16s %-16s %s\n", "DATE", "HOLDING", "RATIO", "CHANGE")
	for _, d := range res.List {
		fmt.Printf("%-14s %-16s %-16s %s\n",
			cli.OrDash(d.Date), cli.Dec(d.Holding), cli.Dec(d.Ratio), cli.Dec(d.Chg))
	}
	fmt.Printf("\n(%d days)\n", len(res.List))
	return nil
}

func printConstituent(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Index constituents %s", indexSymbol))
	res, err := mc.Constituent(c, indexSymbol)
	if err != nil {
		return fmt.Errorf("constituents of %s: %w", indexSymbol, err)
	}
	if res == nil {
		fmt.Println("   (no constituent data)")
		return nil
	}
	fmt.Printf("rise %d / flat %d / fall %d   constituents: %d\n",
		res.RiseNum, res.FlatNum, res.FallNum, len(res.Stocks))
	if len(res.Stocks) == 0 {
		return nil
	}
	fmt.Printf("%-12s %-26s %-12s %-12s %-16s %-16s %s\n",
		"SYMBOL", "NAME", "LAST", "CHG%", "AMOUNT", "INFLOW", "MKT/TAGS")
	for _, s := range res.Stocks {
		mkt := cli.OrDash(s.Market)
		if len(s.Tags) > 0 {
			mkt += " " + strings.Join(s.Tags, ",")
		}
		fmt.Printf("%-12s %-26s %-12s %-12s %-16s %-16s %s\n",
			s.Symbol, cli.Truncate(s.Name, 26), cli.Dec(s.LastDone), cli.Dec(s.Chg),
			cli.Dec(s.Amount), cli.Dec(s.Inflow), mkt)
	}
	return nil
}

func printTopMovers(ctx context.Context, mc *market.MarketContext) error {
	sortOrder, err := parseMoverSort(moversSort)
	if err != nil {
		return err
	}
	var mkts []string
	for _, m := range strings.Split(moversMkts, ",") {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			mkts = append(mkts, m)
		}
	}
	if len(mkts) == 0 {
		return fmt.Errorf("-mover-markets is empty; want at least one market code")
	}
	if moversDate != "" {
		if _, err := time.Parse("2006-01-02", moversDate); err != nil {
			return fmt.Errorf("-mover-date %q is not YYYY-MM-DD: %w", moversDate, err)
		}
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Top movers %s (sort %s, date %s, limit %d)",
		strings.Join(mkts, "+"), moversSort, cli.OrDash(moversDate), moversLimit))
	res, err := mc.TopMovers(c, mkts, sortOrder, moversDate, uint32(moversLimit))
	if err != nil {
		return fmt.Errorf("top movers %s: %w", strings.Join(mkts, "+"), err)
	}
	if res == nil || len(res.Events) == 0 {
		fmt.Println("   (no top movers returned)")
		return nil
	}
	fmt.Printf("%-22s %-12s %-26s %-10s %-12s %-8s %s\n",
		"TIME", "SYMBOL", "NAME", "CHANGE", "LAST", "TYPE", "REASON")
	for _, e := range res.Events {
		fmt.Printf("%-22s %-12s %-26s %-10s %-12s %-8d %s\n",
			cli.Truncate(e.Timestamp, 22), e.Stock.Symbol, cli.Truncate(e.Stock.Name, 26),
			cli.OrDash(e.Stock.Change), cli.OrDash(e.Stock.LastDone),
			e.AlertType, cli.Truncate(e.AlertReason, 40))
	}
	fmt.Printf("\n(%d events)\n", len(res.Events))
	return nil
}

// printRankCategories pretty-prints the raw JSON. The SDK returns an opaque
// json.RawMessage here rather than a typed struct, so there is nothing to map
// field-by-field; re-marshalling it indented is the honest rendering.
func printRankCategories(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Rank categories")
	res, err := mc.RankCategories(c)
	if err != nil {
		return fmt.Errorf("rank categories: %w", err)
	}
	if res == nil || len(res.Data) == 0 {
		fmt.Println("   (no rank categories returned)")
		return nil
	}
	var pretty any
	if err := json.Unmarshal(res.Data, &pretty); err != nil {
		// Not an object; show it verbatim rather than failing the whole run.
		fmt.Println(string(res.Data))
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pretty); err != nil {
		return fmt.Errorf("rendering rank categories: %w", err)
	}
	fmt.Println("\nPass a first_tags key to -rank-key for -sections rank-list.")
	return nil
}

func printRankList(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Rank list %s (need_article=%v)", rankKey, rankArticle))
	res, err := mc.RankList(c, rankKey, rankArticle)
	if err != nil {
		return fmt.Errorf("rank list %s: %w", rankKey, err)
	}
	if res == nil || len(res.Lists) == 0 {
		fmt.Println("   (empty rank list; check the key from rank-categories)")
		return nil
	}
	fmt.Printf("bmp: %v\n", res.Bmp)
	fmt.Printf("%-12s %-26s %-12s %-10s %-14s %-12s %s\n",
		"SYMBOL", "NAME", "LAST", "CHG", "TURNOVER RATE", "AMPLITUDE", "INDUSTRY")
	for _, it := range res.Lists {
		fmt.Printf("%-12s %-26s %-12s %-10s %-14s %-12s %s\n",
			it.Symbol, cli.Truncate(it.Name, 26), cli.OrDash(it.LastDone),
			cli.OrDash(it.Chg), cli.OrDash(it.TurnoverRate),
			cli.OrDash(it.Amplitude), cli.Truncate(it.Industry, 20))
	}
	fmt.Printf("\n(%d entries)\n", len(res.Lists))
	return nil
}

func printTradeStats(ctx context.Context, mc *market.MarketContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Trade statistics %s", tradeSym))
	res, err := mc.TradeStats(c, tradeSym)
	if err != nil {
		return fmt.Errorf("trade stats for %s: %w", tradeSym, err)
	}
	if res == nil {
		fmt.Println("   (no trade statistics returned)")
		return nil
	}
	s := res.Statistics
	fmt.Printf("avg_price %s  preclose %s  trades_count %s\n",
		s.Avgprice.StringFixed(3), s.Preclose.StringFixed(3), cli.OrDash(s.TradesCount))
	fmt.Printf("total_amount %s  timestamp %s\n",
		s.TotalAmount.StringFixed(2), cli.OrDash(s.Timestamp))
	if len(s.TradeDate) > 0 {
		fmt.Printf("trade_dates: %s\n", strings.Join(s.TradeDate, ", "))
	}
	if len(res.Trades) == 0 {
		fmt.Println("   (no price levels)")
		return nil
	}
	fmt.Printf("\n%-14s %-16s %-16s %s\n", "PRICE", "BUY AMOUNT", "NEUTRAL", "SELL AMOUNT")
	for _, t := range res.Trades {
		fmt.Printf("%-14s %-16s %-16s %s\n",
			t.Price.StringFixed(3), t.BuyAmount.StringFixed(2),
			t.NeutralAmount.StringFixed(2), t.SellAmount.StringFixed(2))
	}
	fmt.Printf("\n(%d price levels)\n", len(res.Trades))
	return nil
}

// ------------------------------------------------------------------ parsing

func parseMarket(s string) (openapi.Market, error) {
	switch v := openapi.Market(strings.ToUpper(strings.TrimSpace(s))); v {
	case openapi.MarketHK, openapi.MarketUS, openapi.MarketCN,
		openapi.MarketSG, openapi.MarketUK:
		return v, nil
	}
	return "", fmt.Errorf("unknown market %q: want HK, US, CN, SG or UK", s)
}

func parseAhPeriod(s string) (market.AhPremiumPeriod, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1m", "min1":
		return market.AhPremiumPeriodMin1, nil
	case "5m", "min5":
		return market.AhPremiumPeriodMin5, nil
	case "15m", "min15":
		return market.AhPremiumPeriodMin15, nil
	case "30m", "min30":
		return market.AhPremiumPeriodMin30, nil
	case "60m", "min60":
		return market.AhPremiumPeriodMin60, nil
	case "day":
		return market.AhPremiumPeriodDay, nil
	case "week":
		return market.AhPremiumPeriodWeek, nil
	case "month":
		return market.AhPremiumPeriodMonth, nil
	case "year":
		return market.AhPremiumPeriodYear, nil
	}
	return 0, fmt.Errorf("unknown -ah-period %q: want 1m, 5m, 15m, 30m, 60m, day, week, month or year", s)
}

func parseBrokerPeriod(s string) (market.BrokerHoldingPeriod, error) {
	switch strings.TrimSpace(s) {
	case "1":
		return market.BrokerHoldingPeriodRct1, nil
	case "5":
		return market.BrokerHoldingPeriodRct5, nil
	case "20":
		return market.BrokerHoldingPeriodRct20, nil
	case "60":
		return market.BrokerHoldingPeriodRct60, nil
	}
	// Accept the SDK's own api spelling too, so the flag is hard to misuse.
	if v, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "rct_")); err == nil {
		switch v {
		case 1:
			return market.BrokerHoldingPeriodRct1, nil
		case 5:
			return market.BrokerHoldingPeriodRct5, nil
		case 20:
			return market.BrokerHoldingPeriodRct20, nil
		case 60:
			return market.BrokerHoldingPeriodRct60, nil
		}
	}
	return 0, fmt.Errorf("unknown -broker-period %q: want 1, 5, 20 or 60", s)
}

// parseMoverSort maps the CLI word to the SDK's raw uint32 sort code. TopMovers
// takes a bare uint32, so an unrecognised value would be silently sent as
// something else; a switch turns that into an error.
func parseMoverSort(s string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "asc", "0":
		return 0, nil
	case "desc", "1":
		return 1, nil
	}
	return 0, fmt.Errorf("unknown -mover-sort %q: want asc or desc", s)
}

func ts(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func dec(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(3)
}
