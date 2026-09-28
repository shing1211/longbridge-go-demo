// Command quote prints a snapshot of market data: live quotes, candlesticks,
// order-book depth and the option chain for a Hong Kong name and a US name.
//
// It is read-only. No order can be placed by this binary.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// Flag values are registered in main so that -h can describe them without
// touching configuration.
var (
	symbols  string
	period   string
	count    int
	noAdjust bool
	depthN   int
	chainSym string
	usSym    string
	timeout  time.Duration
)

func main() {
	u := cli.NewUsage("quote", "read-only market data snapshot")
	u.FS.StringVar(&symbols, "symbols", "700.HK,AAPL.US", "comma-separated symbols to quote")
	u.FS.StringVar(&period, "period", "day", "candlestick period: 1m,5m,15m,30m,60m,day,week,month,year")
	u.FS.IntVar(&count, "count", 5, "number of candlesticks")
	u.FS.BoolVar(&noAdjust, "no-adjust", false, "disable forward price adjustment")
	u.FS.IntVar(&depthN, "depth", 5, "levels of order-book depth to print per side")
	u.FS.StringVar(&chainSym, "chain", "700.HK", "underlying symbol for the option chain")
	u.FS.StringVar(&usSym, "us", "AAPL.US", "US symbol printed alongside the HK one")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary issues no writes. Assert the gate would stop it.
	if err := cfg.GuardWrite("quote a symbol"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: quote is read-only but the write gate is open"))
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

		list := splitList(symbols)
		if err := printQuotes(ctx, qc, list); err != nil {
			return err
		}
		if err := printCandles(ctx, qc, list); err != nil {
			return err
		}
		if err := printDepth(ctx, qc, list); err != nil {
			return err
		}
		if err := printTicks(ctx, qc, list, 10); err != nil {
			return err
		}
		return printOptionChain(ctx, qc, chainSym, usSym)
	})
}

func printQuotes(ctx context.Context, qc *quote.QuoteContext, symbols []string) error {
	reqCtx, cancel := context.WithTimeout(ctx, cfgTimeout())
	defer cancel()

	fmt.Println("\n=== Quotes ===")
	quotes, err := qc.Quote(reqCtx, symbols)
	if err != nil {
		return fmt.Errorf("quote %s: %w", strings.Join(symbols, ","), err)
	}
	for _, q := range quotes {
		// quote.TradeStatus is a bare int32 with no String() method in
		// v0.25.2, so print the raw code rather than inventing a label.
		fmt.Printf("%-12s last=%-12s open=%-12s high=%-12s low=%-12s prev_close=%-12s vol=%-12d status_code=%d\n",
			q.Symbol, dec(q.LastDone), dec(q.Open), dec(q.High), dec(q.Low),
			dec(q.PrevClose), q.Volume, int32(q.TradeStatus))
		if q.PreMarketQuote != nil {
			fmt.Printf("%-12s  pre-market: last=%s vol=%d\n", "", dec(q.PreMarketQuote.LastDone), q.PreMarketQuote.Volume)
		}
		if q.PostMarketQuote != nil {
			fmt.Printf("%-12s  post-market: last=%s vol=%d\n", "", dec(q.PostMarketQuote.LastDone), q.PostMarketQuote.Volume)
		}
	}
	return nil
}

func printCandles(ctx context.Context, qc *quote.QuoteContext, symbols []string) error {
	p, err := parsePeriod(period)
	if err != nil {
		return err
	}
	adj := quote.AdjustTypeForward
	if noAdjust {
		adj = quote.AdjustTypeNo
	}

	fmt.Printf("\n=== Candlesticks (%s) ===\n", period)
	for _, sym := range symbols {
		reqCtx, cancel := context.WithTimeout(ctx, cfgTimeout())
		sticks, err := qc.Candlesticks(reqCtx, sym, p, int32(count), adj)
		cancel()
		if err != nil {
			// One bad symbol should not abort the whole report.
			fmt.Printf("%-12s error: %v\n", sym, err)
			continue
		}
		fmt.Printf("-- %s (%d bars)\n", sym, len(sticks))
		for _, s := range sticks {
			fmt.Printf("   %s  O=%-10s H=%-10s L=%-10s C=%-10s vol=%d turnover=%s\n",
				cli.FmtTime(s.Timestamp), dec(s.Open), dec(s.High), dec(s.Low),
				dec(s.Close), s.Volume, dec(s.Turnover))
		}
	}
	return nil
}

// printTicks pulls the recent trade tape over HTTP. It is the pull-based
// counterpart to the websocket tick stream in cmd/watch: same data, different
// transport, so the two can be compared against each other.
func printTicks(ctx context.Context, qc *quote.QuoteContext, symbols []string, count int32) error {
	fmt.Printf("\n=== Recent trades (last %d per symbol) ===\n", count)
	for _, sym := range symbols {
		reqCtx, cancel := context.WithTimeout(ctx, cfgTimeout())
		trades, err := qc.Trades(reqCtx, sym, count)
		cancel()
		if err != nil {
			fmt.Printf("%-12s error: %v\n", sym, err)
			continue
		}
		fmt.Printf("-- %s (%d ticks)\n", sym, len(trades))
		for _, t := range trades {
			fmt.Printf("   %s  price=%-10s vol=%-8d type=%s\n",
				cli.FmtTime(t.Timestamp), t.Price, t.Volume, t.TradeType)
		}
	}
	return nil
}

func printDepth(ctx context.Context, qc *quote.QuoteContext, symbols []string) error {
	fmt.Printf("\n=== Order book (top %d) ===\n", depthN)
	for _, sym := range symbols {
		reqCtx, cancel := context.WithTimeout(ctx, cfgTimeout())
		d, err := qc.Depth(reqCtx, sym)
		cancel()
		if err != nil {
			fmt.Printf("%-12s error: %v\n", sym, err)
			continue
		}
		fmt.Printf("-- %s\n", sym)
		fmt.Println("   BID                                | ASK")
		asks := d.Ask
		if len(asks) > depthN {
			asks = asks[:depthN]
		}
		bids := d.Bid
		if len(bids) > depthN {
			bids = bids[:depthN]
		}
		for i := 0; i < depthN; i++ {
			var b, a string
			if i < len(bids) {
				b = fmt.Sprintf("%-10s x%-10d", dec(bids[i].Price), bids[i].Volume)
			} else {
				b = strings.Repeat(" ", 22)
			}
			if i < len(asks) {
				a = fmt.Sprintf("%-10s x%-10d", dec(asks[i].Price), asks[i].Volume)
			}
			fmt.Printf("   %-32s | %s\n", b, a)
		}
	}
	return nil
}

func printOptionChain(ctx context.Context, qc *quote.QuoteContext, hkSym, usSym string) error {
	fmt.Println("\n=== Option chain ===")
	for _, sym := range []string{hkSym, usSym} {
		reqCtx, cancel := context.WithTimeout(ctx, cfgTimeout())
		dates, err := qc.OptionChainExpiryDateList(reqCtx, sym)
		cancel()
		if err != nil {
			fmt.Printf("%-12s expiry list error: %v\n", sym, err)
			continue
		}
		if len(dates) == 0 {
			fmt.Printf("%-12s no option expiries returned\n", sym)
			continue
		}
		// Use the nearest expiry in the past if one exists, else the first.
		chosen := dates[0]
		for _, d := range dates {
			if !d.After(time.Now()) {
				chosen = d
				break
			}
		}
		fmt.Printf("-- %s  nearest expiry: %s (%d expiries total)\n",
			sym, chosen.Format("2006-01-02"), len(dates))

		reqCtx2, cancel2 := context.WithTimeout(ctx, cfgTimeout())
		strikes, err := qc.OptionChainInfoByDate(reqCtx2, sym, &chosen)
		cancel2()
		if err != nil {
			fmt.Printf("%-12s chain error: %v\n", sym, err)
			continue
		}
		shown := strikes
		if len(shown) > 10 {
			shown = shown[:10]
		}
		fmt.Printf("   %-12s %-18s %-18s %s\n", "STRIKE", "CALL", "PUT", "STANDARD")
		for _, s := range shown {
			fmt.Printf("   %-12s %-18s %-18s %v\n", dec(s.Price), s.CallSymbol, s.PutSymbol, s.Standard)
		}
		if len(strikes) > len(shown) {
			fmt.Printf("   ... %d more strikes\n", len(strikes)-len(shown))
		}

		// Quote the actual option contracts from the chain, not just the
		// underlying: OptionQuote is the per-contract endpoint and carries
		// the greeks and open interest that StrikePriceInfo does not.
		contracts := make([]string, 0, len(shown)*2)
		for _, s := range shown {
			if s.CallSymbol != "" {
				contracts = append(contracts, s.CallSymbol)
			}
			if s.PutSymbol != "" {
				contracts = append(contracts, s.PutSymbol)
			}
		}
		if len(contracts) == 0 {
			continue
		}
		fmt.Println("   -- option contracts --")
		optCtx, optCancel := context.WithTimeout(ctx, cfgTimeout())
		opts, err := qc.OptionQuote(optCtx, contracts)
		optCancel()
		if err != nil {
			fmt.Printf("   %-12s option quote error: %v\n", sym, err)
			continue
		}
		fmt.Printf("   %-14s %-10s %-10s %-12s %-12s %-9s %s\n",
			"CONTRACT", "LAST", "OPEN", "IV", "OI", "DIRECTION", "EXPIRY")
		for _, o := range opts {
			iv, dir, expiry, oi := "-", "-", "-", "0"
			if e := o.OptionExtend; e != nil {
				iv, dir, expiry = cli.OrDash(e.ImpliedVolatility),
					cli.OrDash(e.Direction), cli.OrDash(e.ExpiryDate)
				oi = fmt.Sprint(e.OpenInterest)
			}
			fmt.Printf("   %-14s %-10s %-10s %-12s %-12s %-9s %s\n",
				o.Symbol, dec(o.LastDone), dec(o.Open), iv, oi, dir, expiry)
		}
	}
	return nil
}

func parsePeriod(s string) (quote.Period, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1m", "1min":
		return quote.PeriodOneMinute, nil
	case "5m", "5min":
		return quote.PeriodFiveMinute, nil
	case "15m", "15min":
		return quote.PeriodFifteenMinute, nil
	case "30m", "30min":
		return quote.PeriodThirtyMinute, nil
	case "60m", "60min", "1h":
		return quote.PeriodSixtyMinute, nil
	case "day", "d", "1d":
		return quote.PeriodDay, nil
	case "week", "w", "1w":
		return quote.PeriodWeek, nil
	case "month", "m", "1mo":
		return quote.PeriodMonth, nil
	case "year", "y":
		return quote.PeriodYear, nil
	}
	return 0, fmt.Errorf("unknown -period %q: want one of 1m,5m,15m,30m,60m,day,week,month,year", s)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"700.HK"}
	}
	return out
}

func dec(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(2)
}

func cfgTimeout() time.Duration { return timeout }
