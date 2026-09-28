// Command reference prints static and market-wide reference data: security
// master records, the security list for a market, calculated index values,
// intraday capital flow, daily capital distribution, trading sessions, exchange
// participants, the account's quote profile and entitlements, realtime broker
// queues, short-sale data and symbol/counter-id resolution.
//
// It is entirely READ-ONLY. No code path here mutates anything or places an
// order, and the startup banner asserts the order gate is still closed.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go"
	"github.com/longbridge/openapi-go/quote"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	sections        string
	symbols         string
	sym             string
	market          string
	category        string
	indexes         string
	shortCount      int
	securityLimit   int
	historyCount    int
	historyDaysBack int
	forward         bool
	timeout         time.Duration
)

func main() {
	u := cli.NewUsage("reference", "static, market-wide and entitlement reference data")
	u.FS.StringVar(&sections, "sections", "static,index,session,participants,profile",
		"comma-separated subset of: static, list, index, flow, distribution, session, "+
			"participants, profile, realtime, history, optionvol, filings, brokers, short, counter")
	u.FS.StringVar(&symbols, "symbols", "700.HK,AAPL.US", "symbols for -sections static,index,flow,brokers,short,counter")
	u.FS.StringVar(&sym, "symbol", "700.HK", "primary symbol for -sections flow,distribution,realtime,history,optionvol,filings,brokers,short")
	u.FS.StringVar(&market, "market", "HK", "market for -sections list: HK, US, CN, SG, UK")
	u.FS.StringVar(&category, "category", "Overnight",
		"security-list category; the SDK only defines the constant Overnight")
	u.FS.StringVar(&indexes, "indexes", "last_done,change_rate,volume,turnover,pe_ttm,pb",
		"comma-separated CalcIndex names for -sections index")
	u.FS.IntVar(&shortCount, "short-count", 10, "number of short-sale records for -sections short")
	u.FS.IntVar(&securityLimit, "list-limit", 20, "how many security-list rows to print")
	u.FS.IntVar(&historyCount, "history-count", 10, "candles to fetch for -sections history")
	u.FS.IntVar(&historyDaysBack, "history-days", 5, "day range for -sections history and optionvol")
	u.FS.BoolVar(&forward, "forward", false, "for -sections history: look FORWARD from the date instead of back")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: read-only binary. If the order gate were open, stop.
	if err := cfg.GuardWrite("run the reference reader"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: reference is read-only but the order gate is open"))
	}

	want, err := parseSections(sections)
	if err != nil {
		cli.Fail(err)
	}
	idx, err := parseCalcIndexes(indexes)
	if err != nil {
		cli.Fail(err)
	}
	mkt, err := parseMarket(market)
	if err != nil {
		cli.Fail(err)
	}
	if shortCount <= 0 {
		cli.Fail(fmt.Errorf("-short-count must be greater than 0, got %d", shortCount))
	}
	if securityLimit <= 0 {
		cli.Fail(fmt.Errorf("-list-limit must be greater than 0, got %d", securityLimit))
	}
	syms := splitList(symbols)
	if len(syms) == 0 {
		cli.Fail(fmt.Errorf("-symbols is empty"))
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

		if want["static"] {
			if err := printStatic(ctx, qc, syms); err != nil {
				return err
			}
		}
		if want["list"] {
			if err := printSecurityList(ctx, qc, mkt); err != nil {
				return err
			}
		}
		if want["index"] {
			if err := printCalcIndex(ctx, qc, syms, idx); err != nil {
				return err
			}
		}
		if want["flow"] {
			if err := printCapitalFlow(ctx, qc, sym); err != nil {
				return err
			}
		}
		if want["distribution"] {
			if err := printCapitalDistribution(ctx, qc, sym); err != nil {
				return err
			}
		}
		if want["session"] {
			if err := printTradingSession(ctx, qc); err != nil {
				return err
			}
		}
		if want["participants"] {
			if err := printParticipants(ctx, qc); err != nil {
				return err
			}
		}
		if want["profile"] {
			printProfile(qc)
		}
		if want["realtime"] {
			if err := printRealtime(ctx, qc, syms); err != nil {
				return err
			}
		}
		if want["history"] {
			if err := printHistoryCandles(ctx, qc, sym); err != nil {
				return err
			}
		}
		if want["optionvol"] {
			if err := printOptionVolume(ctx, qc, sym); err != nil {
				return err
			}
		}
		if want["filings"] {
			if err := printFilings(ctx, qc, sym); err != nil {
				return err
			}
		}
		if want["brokers"] {
			if err := printRealtimeBrokers(ctx, qc, sym); err != nil {
				return err
			}
		}
		if want["short"] {
			if err := printShort(ctx, qc, sym, uint32(shortCount)); err != nil {
				return err
			}
		}
		if want["counter"] {
			if err := printCounterIDs(ctx, qc, syms); err != nil {
				return err
			}
		}
		return nil
	})
}

// ----------------------------------------------------------------- sections

func printStatic(ctx context.Context, qc *quote.QuoteContext, syms []string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Static info")
	infos, err := qc.StaticInfo(c, syms)
	if err != nil {
		return fmt.Errorf("static info: %w", err)
	}
	if len(infos) == 0 {
		fmt.Println("   (no static info returned)")
		return nil
	}
	for _, i := range infos {
		fmt.Printf("\n-- %s  %s\n", i.Symbol, cli.Truncate(firstNonEmpty(i.NameHk, i.NameEn, i.NameCn), 30))
		fmt.Printf("   exchange=%s currency=%s lot_size=%d\n",
			cli.OrDash(i.Exchange), cli.OrDash(i.Currency), i.LotSize)
		fmt.Printf("   shares: total=%d circulating=%d hk=%d\n",
			i.TotalShares, i.CirculatingShares, i.HkShares)
		fmt.Printf("   eps=%s eps_ttm=%s bps=%s dividend_yield=%s\n",
			cli.Dec4(i.Eps), cli.Dec4(i.EpsTtm), cli.Dec4(i.Bps),
			cli.OrDash(i.DividendYield))
		if len(i.StockDerivatives) > 0 {
			fmt.Printf("   stock_derivatives=%v\n", i.StockDerivatives)
		}
	}
	return nil
}

func printSecurityList(ctx context.Context, qc *quote.QuoteContext, mkt openapi.Market) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Security list (%s, category=%s)", mkt, category))
	list, err := qc.SecurityList(c, mkt, quote.SecurityListCategory(category))
	if err != nil {
		return fmt.Errorf("security list for %s: %w", mkt, err)
	}
	if len(list) == 0 {
		fmt.Println("   (empty list)")
		return nil
	}
	fmt.Printf("(%d securities returned; showing first %d)\n", len(list), securityLimit)
	fmt.Printf("%-14s %-30s %-30s %s\n", "SYMBOL", "NAME_CN", "NAME_EN", "NAME_HK")
	for i, s := range list {
		if i >= securityLimit {
			fmt.Printf("... %d more (raise -list-limit)\n", len(list)-securityLimit)
			break
		}
		fmt.Printf("%-14s %-30s %-30s %s\n",
			s.Symbol, cli.Truncate(s.NameCN, 30), cli.Truncate(s.NameEN, 30),
			cli.Truncate(s.NameHK, 30))
	}
	return nil
}

func printCalcIndex(ctx context.Context, qc *quote.QuoteContext, syms []string, idx []quote.CalcIndex) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Calculated index values")
	fmt.Printf("indexes: %v\n", idx)
	rows, err := qc.CalcIndex(c, syms, idx)
	if err != nil {
		return fmt.Errorf("calc index: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("   (no values returned)")
		return nil
	}
	for _, r := range rows {
		fmt.Printf("\n-- %s\n", r.Symbol)
		fmt.Printf("   last_done=%s change_val=%s change_rate=%s volume=%d turnover=%s\n",
			cli.Dec4(r.LastDone), cli.Dec4(r.ChangeVal), cli.Dec4(r.ChangeRate),
			r.Volume, cli.Dec(r.Turnover))
		// The rest are zero for a plain equity, so print only what is set.
		kv := [][2]string{
			{"ytd_change_rate", cli.Dec4(r.YtdChangeRate)},
			{"turnover_rate", cli.Dec4(r.TurnoverRate)},
			{"total_market_value", cli.Dec(r.TotalMarketValue)},
			{"capital_flow", cli.Dec(r.CapitalFlow)},
			{"amplitude", cli.Dec4(r.Amplitude)},
			{"volume_ratio", cli.Dec4(r.VolumeRatio)},
			{"pe_ttm", cli.Dec4(r.PeTtmRatio)},
			{"pb", cli.Dec4(r.PbRatio)},
			{"dividend_ttm", cli.Dec4(r.DividendRatioTtm)},
			{"5d_change_rate", cli.Dec4(r.FiveDayChangeRate)},
			{"10d_change_rate", cli.Dec4(r.TenDayChangeRate)},
			{"6m_change_rate", cli.Dec4(r.HalfYearChangeRate)},
			{"5m_change_rate", cli.Dec4(r.FiveMinutesChangeRate)},
		}
		for _, p := range kv {
			if p[1] != "-" {
				fmt.Printf("   %-20s %s\n", p[0], p[1])
			}
		}
		// Option/warrant greeks only apply to derivative symbols.
		if r.OpenInterest != 0 || r.Delta != nil {
			fmt.Printf("   open_interest=%d delta=%s gamma=%s theta=%s vega=%s rho=%s\n",
				r.OpenInterest, cli.Dec4(r.Delta), cli.Dec4(r.Gamma),
				cli.Dec4(r.Theta), cli.Dec4(r.Vega), cli.Dec4(r.Rho))
		}
	}
	return nil
}

func printCapitalFlow(ctx context.Context, qc *quote.QuoteContext, symbol string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Intraday capital flow: %s", symbol))
	lines, err := qc.CapitalFlow(c, symbol)
	if err != nil {
		return fmt.Errorf("capital flow for %s: %w", symbol, err)
	}
	if len(lines) == 0 {
		fmt.Println("   (no flow data returned)")
		return nil
	}
	fmt.Printf("(%d points; showing the last 20)\n", len(lines))
	start := len(lines) - 20
	if start < 0 {
		start = 0
	}
	for _, l := range lines[start:] {
		fmt.Printf("   %-20s inflow=%s\n", cli.FmtTime(l.Timestamp), cli.Dec(l.Inflow))
	}
	return nil
}

func printCapitalDistribution(ctx context.Context, qc *quote.QuoteContext, symbol string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Daily capital distribution: %s", symbol))
	d, err := qc.CapitalDistribution(c, symbol)
	if err != nil {
		return fmt.Errorf("capital distribution for %s: %w", symbol, err)
	}
	fmt.Printf("symbol=%s updated=%s\n", d.Symbol, cli.FmtTime(d.Timestamp))
	printCapital("in", d.CapitalIn)
	printCapital("out", d.CapitalOut)
	return nil
}

func printCapital(label string, cap quote.Capital) {
	fmt.Printf("   %-4s large=%-16s medium=%-16s small=%s\n",
		label, cli.Dec(cap.Large), cli.Dec(cap.Medium), cli.Dec(cap.Small))
}

func printTradingSession(ctx context.Context, qc *quote.QuoteContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Trading sessions")
	sessions, err := qc.TradingSession(c)
	if err != nil {
		return fmt.Errorf("trading session: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("   (no sessions returned)")
		return nil
	}
	// BegTime/EndTime are minutes from local midnight, not clock strings.
	for _, s := range sessions {
		fmt.Printf("\n-- %s\n", s.Market)
		for _, p := range s.TradeSession {
			fmt.Printf("   %-10s %s - %s\n",
				tradeSessionName(p.TradeSession),
				minutesToClock(p.BegTime), minutesToClock(p.EndTime))
		}
	}
	return nil
}

func printParticipants(ctx context.Context, qc *quote.QuoteContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Exchange participants (brokers)")
	infos, err := qc.Participants(c)
	if err != nil {
		return fmt.Errorf("participants: %w", err)
	}
	if len(infos) == 0 {
		fmt.Println("   (no participants returned)")
		return nil
	}
	for _, p := range infos {
		fmt.Printf("%-30s ids=%v\n",
			cli.Truncate(firstNonEmpty(p.ParticipantNameHk, p.ParticipantNameEn, p.ParticipantNameCn), 30),
			p.BrokerIds)
	}
	return nil
}

// printProfile reads the entitlement from the already-connected context.
//
// Profile() takes no context and returns no error: the entitlement is pushed
// down the websocket at connect time. That is why it cannot be wrapped in the
// same error-returning shape as the HTTP calls above.
func printProfile(qc *quote.QuoteContext) {
	cli.Section("Quote profile and entitlements")
	p := qc.Profile()
	if p == nil {
		fmt.Println("   (no profile available yet; the entitlement arrives on connect)")
		return
	}
	fmt.Printf("member_id=%d quote_level=%s subscribe_limit=%d history_candlestick_limit=%d\n",
		p.MemberId, cli.OrDash(p.QuoteLevel), p.SubscribeLimit, p.HistoryCandlestickLimit)
	if len(p.RateLimit) > 0 {
		fmt.Println("  rate limits (cmd / limit / burst):")
		for _, r := range p.RateLimit {
			fmt.Printf("    %-8d %-8d %d\n", r.Cmd, r.Limit, r.Burst)
		}
	}
	if d := p.QuoteLevelDetail; d != nil {
		for _, k := range sortedKeys(d.ByPackageKey) {
			pd := d.ByPackageKey[k]
			fmt.Printf("    package %-16s limit=%-8d burst=%d\n", k, pd.Limit, pd.Burst)
		}
		for _, k := range sortedMarketKeys(d.ByMarketCode) {
			md := d.ByMarketCode[k]
			fmt.Printf("    market  %-16s limit=%-8d burst=%d\n", k, md.Limit, md.Burst)
		}
	}
}

// printRealtime pulls the latest snapshot over HTTP for each of the three
// realtime data families. These are the HTTP twins of the websocket pushes
// that cmd/watch streams, which makes them the useful thing to compare against
// a live subscription.
func printRealtime(ctx context.Context, qc *quote.QuoteContext, syms []string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Realtime quotes")
	quotes, err := qc.RealtimeQuote(c, syms)
	if err != nil {
		return fmt.Errorf("realtime quote: %w", err)
	}
	for _, q := range quotes {
		fmt.Printf("   %-14s last=%-10s open=%-10s high=%-10s low=%-10s vol=%-12d turnover=%-14s trade_status=%-4d %s\n",
			q.Symbol, cli.Dec4(q.LastDone), cli.Dec4(q.Open), cli.Dec4(q.High),
			cli.Dec4(q.Low), q.Volume, cli.Dec(q.Turnover),
			int(q.TradeStatus), cli.FmtTime(q.Timestamp))
	}

	cli.Section("Realtime depth (best 3 per side)")
	for _, s := range syms {
		d, err := qc.RealtimeDepth(c, s)
		if err != nil {
			return fmt.Errorf("realtime depth for %s: %w", s, err)
		}
		side := func(label string, rows []*quote.Depth) {
			fmt.Printf("   %-14s %s: %s\n", s, label, summarizeDepth(rows, 3))
		}
		side("bid", d.Bid)
		side("ask", d.Ask)
	}

	cli.Section("Realtime ticks (last 5 per symbol)")
	for _, s := range syms {
		ticks, err := qc.RealtimeTrades(c, s)
		if err != nil {
			return fmt.Errorf("realtime trades for %s: %w", s, err)
		}
		fmt.Printf("   %-14s %d ticks\n", s, len(ticks))
		start := len(ticks) - 5
		if start < 0 {
			start = 0
		}
		for _, t := range ticks[start:] {
			fmt.Printf("     %-20s price=%-10s vol=%d\n",
				cli.FmtTime(t.Timestamp), t.Price, t.Volume)
		}
	}

	// The broker queue is already covered by -sections brokers via
	// RealtimeBrokers; Brokers() is the raw form, so print it once here.
	cli.Section("Broker queues (raw)")
	for _, s := range syms {
		b, err := qc.Brokers(c, s)
		if err != nil {
			return fmt.Errorf("brokers for %s: %w", s, err)
		}
		printBrokerSide("ask", b.AskBrokers)
		printBrokerSide("bid", b.BidBrokers)
	}
	return nil
}

func summarizeDepth(rows []*quote.Depth, n int) string {
	if len(rows) == 0 {
		return "(empty)"
	}
	if len(rows) > n {
		rows = rows[:n]
	}
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%s x%d", cli.Dec4(r.Price), r.Volume))
	}
	return strings.Join(parts, ", ")
}

// printHistoryCandles exercises BOTH of the SDK's paginated history calls.
//
// The offset form walks N candles from a point in time; the date form returns
// everything in a closed range. They are genuinely different endpoints with
// different paging, so both are called rather than one standing in for both.
func printHistoryCandles(ctx context.Context, qc *quote.QuoteContext, symbol string) error {
	if historyCount <= 0 {
		return fmt.Errorf("-history-count must be greater than 0, got %d", historyCount)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	anchor := time.Now()

	cli.Section(fmt.Sprintf("History by offset: %s (%d candles, %s)", symbol, historyCount,
		map[bool]string{true: "forward", false: "backward"}[forward]))
	byOffset, err := qc.HistoryCandlesticksByOffset(c, symbol, quote.PeriodDay,
		quote.AdjustTypeForward, forward, &anchor, int32(historyCount),
		quote.CandlestickRequestTradeSession(quote.CandlestickTradeSessionNormal))
	if err != nil {
		return fmt.Errorf("history candlesticks by offset for %s: %w", symbol, err)
	}
	printCandles(byOffset)

	if historyDaysBack <= 0 {
		return fmt.Errorf("-history-days must be greater than 0, got %d", historyDaysBack)
	}
	start := anchor.AddDate(0, 0, -historyDaysBack)
	cli.Section(fmt.Sprintf("History by date: %s (%s to %s)", symbol,
		start.Format("2006-01-02"), anchor.Format("2006-01-02")))
	byDate, err := qc.HistoryCandlesticksByDate(c, symbol, quote.PeriodDay,
		quote.AdjustTypeForward, &start, &anchor,
		quote.CandlestickRequestTradeSession(quote.CandlestickTradeSessionNormal))
	if err != nil {
		return fmt.Errorf("history candlesticks by date for %s: %w", symbol, err)
	}
	printCandles(byDate)
	return nil
}

func printCandles(sticks []*quote.Candlestick) {
	if len(sticks) == 0 {
		fmt.Println("   (no candles returned)")
		return
	}
	fmt.Printf("%-22s %-10s %-10s %-10s %-10s %-10s %-12s\n",
		"TIME", "OPEN", "HIGH", "LOW", "CLOSE", "VOLUME", "TURNOVER")
	for _, s := range sticks {
		fmt.Printf("%-22s %-10s %-10s %-10s %-10s %-10s %-12s\n",
			cli.FmtTime(s.Timestamp),
			cli.Dec4(s.Open), cli.Dec4(s.High), cli.Dec4(s.Low), cli.Dec4(s.Close),
			fmt.Sprint(s.Volume), cli.Dec(s.Turnover))
	}
	fmt.Printf("(%d candles)\n", len(sticks))
}

// printOptionVolume reports aggregate and per-day put/call volume. The
// per-day call needs an underlying symbol, not an option symbol.
func printOptionVolume(ctx context.Context, qc *quote.QuoteContext, symbol string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Option volume (aggregate): %s", symbol))
	stats, err := qc.OptionVolume(c, symbol)
	if err != nil {
		return fmt.Errorf("option volume for %s: %w", symbol, err)
	}
	if stats == nil {
		fmt.Println("   (no data)")
	} else {
		fmt.Printf("   call_volume=%s put_volume=%s\n",
			cli.OrDash(stats.CallVolume), cli.OrDash(stats.PutVolume))
	}

	if historyDaysBack <= 0 {
		return fmt.Errorf("-history-days must be greater than 0, got %d", historyDaysBack)
	}
	end := time.Now()
	start := end.AddDate(0, 0, -historyDaysBack)
	cli.Section(fmt.Sprintf("Option volume (daily): %s", symbol))
	rows, err := qc.OptionVolumeDaily(c, symbol, start, end)
	if err != nil {
		return fmt.Errorf("option volume daily for %s: %w", symbol, err)
	}
	if len(rows) == 0 {
		fmt.Println("   (no daily rows returned)")
		return nil
	}
	for _, r := range rows {
		ts, err := strconv.ParseInt(r.Timestamp, 10, 64)
		when := cli.OrDash(r.Timestamp)
		if err == nil && ts > 0 {
			when = cli.FmtTime(ts)
		}
		fmt.Printf("   %-22s total=%-12s call=%-12s put=%-12s put_call_ratio=%s\n",
			when, r.TotalVolume, r.TotalCallVolume, r.TotalPutVolume,
			cli.OrDash(r.PutCallVolumeRatio))
	}
	return nil
}

func printFilings(ctx context.Context, qc *quote.QuoteContext, symbol string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Filings: %s", symbol))
	items, err := qc.Filings(c, symbol)
	if err != nil {
		return fmt.Errorf("filings for %s: %w", symbol, err)
	}
	if len(items) == 0 {
		fmt.Println("   (no filings returned)")
		return nil
	}
	for _, it := range items {
		fmt.Printf("\n-- %s\n   title: %s\n   file:  %s\n",
			cli.OrDash(it.Id), cli.Truncate(it.Title, 70), cli.OrDash(it.FileName))
		if it.Description != "" {
			fmt.Printf("   desc:  %s\n", cli.Truncate(it.Description, 70))
		}
		for _, u := range it.FileUrls {
			fmt.Printf("   url:   %s\n", u)
		}
	}
	return nil
}

func printRealtimeBrokers(ctx context.Context, qc *quote.QuoteContext, symbol string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Broker queues: %s", symbol))
	b, err := qc.RealtimeBrokers(c, symbol)
	if err != nil {
		return fmt.Errorf("realtime brokers for %s: %w", symbol, err)
	}
	printBrokerSide("ask", b.AskBrokers)
	printBrokerSide("bid", b.BidBrokers)
	return nil
}

func printBrokerSide(label string, brokers []*quote.Brokers) {
	if len(brokers) == 0 {
		fmt.Printf("   %s (empty)\n", label)
		return
	}
	fmt.Printf("   %s:\n", label)
	for _, b := range brokers {
		fmt.Printf("     pos=%-4d broker_ids=%v\n", b.Position, b.BrokerIds)
	}
}

func printShort(ctx context.Context, qc *quote.QuoteContext, symbol string, count uint32) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Short positions: %s", symbol))
	pos, err := qc.ShortPositions(c, symbol, count)
	if err != nil {
		return fmt.Errorf("short positions for %s: %w", symbol, err)
	}
	if pos == nil || len(pos.Data) == 0 {
		fmt.Println("   (no short-position data; HKEX publishes on its own cadence)")
	} else {
		for _, p := range pos.Data {
			fmt.Printf("   %-22s rate=%-8s close=%-10s amount=%-14s balance=%-14s shares_short=%-14s days_to_cover=%s\n",
				p.Timestamp, p.Rate, p.Close, p.Amount, p.Balance,
				p.CurrentSharesShort, p.DaysToCover)
		}
	}

	cli.Section(fmt.Sprintf("Short trades: %s", symbol))
	tr, err := qc.ShortTrades(c, symbol, count)
	if err != nil {
		return fmt.Errorf("short trades for %s: %w", symbol, err)
	}
	if tr == nil || len(tr.Data) == 0 {
		fmt.Println("   (no short-trade data)")
		return nil
	}
	for _, t := range tr.Data {
		fmt.Printf("   %-22s rate=%-8s close=%-10s amount=%-14s balance=%-14s nasdaq=%-14s nyse=%s\n",
			t.Timestamp, t.Rate, t.Close, t.Amount, t.Balance, t.NusAmount, t.NyAmount)
	}
	return nil
}

func printCounterIDs(ctx context.Context, qc *quote.QuoteContext, syms []string) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Symbol <-> counter id resolution")
	// Both calls are exercised: the first maps a symbol to its internal id,
	// the second resolves an id (or symbol) the other way.
	toCounter, err := qc.SymbolToCounterIds(c, syms)
	if err != nil {
		return fmt.Errorf("symbol to counter ids: %w", err)
	}
	fmt.Println("  SymbolToCounterIds (symbol -> counter id):")
	for _, s := range syms {
		fmt.Printf("    %-14s -> %s\n", s, cli.OrDash(toCounter[s]))
	}

	// Feed the resolved ids straight back, which is the realistic usage.
	ids := make([]string, 0, len(toCounter))
	for _, v := range toCounter {
		if v != "" {
			ids = append(ids, v)
		}
	}
	if len(ids) == 0 {
		fmt.Println("  (no counter ids to resolve back)")
		return nil
	}
	resolved, err := qc.ResolveCounterIds(c, ids)
	if err != nil {
		return fmt.Errorf("resolve counter ids: %w", err)
	}
	fmt.Println("  ResolveCounterIds (counter id -> symbol):")
	for _, id := range ids {
		fmt.Printf("    %-14s -> %s\n", id, cli.OrDash(resolved[id]))
	}
	return nil
}

// ------------------------------------------------------------------- helpers

func parseSections(s string) (map[string]bool, error) {
	valid := map[string]bool{
		"static": true, "list": true, "index": true, "flow": true,
		"distribution": true, "session": true, "participants": true,
		"profile": true, "realtime": true, "history": true,
		"optionvol": true, "filings": true, "brokers": true,
		"short": true, "counter": true,
	}
	want := map[string]bool{}
	for _, p := range splitList(s) {
		if !valid[p] {
			names := make([]string, 0, len(valid))
			for k := range valid {
				names = append(names, k)
			}
			sortStrings(names)
			return nil, fmt.Errorf("unknown section %q: want a subset of %s", p, strings.Join(names, ", "))
		}
		want[p] = true
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("-sections selected nothing")
	}
	return want, nil
}

func parseCalcIndexes(s string) ([]quote.CalcIndex, error) {
	all := map[string]quote.CalcIndex{
		"last_done": quote.CalcIndexLastDone, "change_val": quote.CalcIndexChangeVal,
		"change_rate": quote.CalcIndexChangeRate, "volume": quote.CalcIndexVolume,
		"turnover": quote.CalcIndexTurnover, "ytd_change_rate": quote.CalcIndexYtdChangeRate,
		"turnover_rate":      quote.CalcIndexTurnoverRate,
		"total_market_value": quote.CalcIndexTotalMarketValue,
		"capital_flow":       quote.CalcIndexCapitalFlow, "amplitude": quote.CalcIndexAmplitude,
		"volume_ratio": quote.CalcIndexVolumeRatio, "pe_ttm": quote.CalcIndexPeTTMRatio,
		"pb": quote.CalcIndexPbRatio, "dividend_ttm": quote.CalcIndexDividendRatioTTM,
		"5d_change_rate":  quote.CalcIndexFiveDayChangeRate,
		"10d_change_rate": quote.CalcIndexTenDayChangeRate,
		"6m_change_rate":  quote.CalcIndexHalfYearChangeRate,
		"5m_change_rate":  quote.CalcIndexFiveMinutesChangeRate,
		"expiry_date":     quote.CalcIndexExpiryDate, "strike_price": quote.CalcIndexStrikePrice,
		"upper_strike_price": quote.CalcIndexUpperStrikePrice,
		"lower_strike_price": quote.CalcIndexLowerStrikePrice,
		"outstanding_qty":    quote.CalcIndexOutstandingQTY,
		"outstanding_ratio":  quote.CalcIndexOutstandingRatio,
		"premium":            quote.CalcIndexPremium, "itm_otm": quote.CalcIndexItmOtm,
		"implied_volatility": quote.CalcIndexImpliedVolatility,
		"warrant_delta":      quote.CalcIndexWarrantDelta, "call_price": quote.CalcIndexCallPrice,
		"to_call_price":      quote.CalcIndexToCallPrice,
		"effective_leverage": quote.CalcIndexEffectiveLeverage,
		"leverage_ratio":     quote.CalcIndexLeverageRatio,
		"conversion_ratio":   quote.CalcIndexConversionRatio,
		"balance_point":      quote.CalcIndexBalancePoint,
		"open_interest":      quote.CalcIndexOpenInterest,
		"delta":              quote.CalcIndexDELTA, "gamma": quote.CalcIndexGAMMA,
		"theta": quote.CalcIndexTHETA, "vega": quote.CalcIndexVEGA,
		"rho": quote.CalcIndexRHO,
	}
	var out []quote.CalcIndex
	for _, p := range splitList(s) {
		v, ok := all[p]
		if !ok {
			return nil, fmt.Errorf("unknown -indexes entry %q (see cmd/reference -h for the list)", p)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("-indexes selected nothing")
	}
	return out, nil
}

func parseMarket(s string) (openapi.Market, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "HK":
		return openapi.MarketHK, nil
	case "US":
		return openapi.MarketUS, nil
	case "CN":
		return openapi.MarketCN, nil
	case "SG":
		return openapi.MarketSG, nil
	case "UK":
		return openapi.MarketUK, nil
	}
	return "", fmt.Errorf("unknown -market %q: want HK, US, CN, SG or UK", s)
}

// minutesToClock renders the SDK's minute-from-midnight integer as HH:MM.
// The API does not send a timezone, so the value is the market's local time.
func minutesToClock(m int32) string {
	if m < 0 {
		return "-"
	}
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// tradeSessionName maps TradeSession, a bare int32 with no String() method.
func tradeSessionName(s quote.TradeSession) string {
	switch s {
	case quote.TradeSessionNormal:
		return "normal"
	case quote.TradeSessionPreTrade:
		return "pre-trade"
	case quote.TradeSessionPostTrade:
		return "post-trade"
	case quote.TradeSessionOvernight:
		return "overnight"
	}
	return "unknown(" + strconv.Itoa(int(s)) + ")"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]*quote.PackageDetail) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortedMarketKeys(m map[string]*quote.MarketPackageDetail) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// sortStrings is a tiny insertion sort, used to keep output deterministic
// without pulling "sort" into every call site.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
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
