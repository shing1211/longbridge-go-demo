// Command market prints reference data: market-wide trading status, trading
// calendar days and an intraday price timeline.
//
// It is read-only and places no orders.
package main

import (
	"context"
	"fmt"
	"os"
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
	sections   string
	marketCode string
	daysBack   int
	daysFwd    int
	timeline   string
	lastLines  int
	timeout    time.Duration
)

func main() {
	u := cli.NewUsage("market", "market status, trading calendar and intraday timeline")
	u.FS.StringVar(&sections, "sections", "status,calendar,timeline",
		"comma-separated subset of: status, calendar, timeline")
	u.FS.StringVar(&marketCode, "market", "HK", "market code for the calendar: HK, US, CN, SG, UK")
	u.FS.IntVar(&daysBack, "back", 7, "calendar days to look back")
	u.FS.IntVar(&daysFwd, "forward", 7, "calendar days to look forward")
	u.FS.StringVar(&timeline, "timeline-symbol", "700.HK", "symbol for the intraday timeline")
	u.FS.IntVar(&lastLines, "lines", 20, "number of intraday lines to print (most recent last)")
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
		switch s {
		case "status", "calendar", "timeline":
		default:
			cli.Fail(fmt.Errorf(
				"unknown section %q in -sections: want status, calendar or timeline", s))
		}
	}

	cli.Run(func(ctx context.Context) error {
		// The market package has no Close(); it is a thin HTTP client.
		mc, err := market.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating market context: %w", err)
		}
		qc, err := quote.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating quote context: %w", err)
		}
		defer func() {
			if err := qc.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: closing quote context: %v\n", err)
			}
		}()

		if wanted["status"] {
			if err := printStatus(ctx, mc); err != nil {
				return err
			}
		}
		if wanted["calendar"] {
			if err := printCalendar(ctx, qc); err != nil {
				return err
			}
		}
		if wanted["timeline"] {
			if err := printTimeline(ctx, qc); err != nil {
				return err
			}
		}
		return nil
	})
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
	fmt.Printf("%-8s %-8s %-20s %s\n", "MARKET", "CODE", "STATUS", "TRADING")
	for _, m := range st.MarketTime {
		label := m.TradeStatus.Label()
		if label == "" {
			label = "-"
		}
		fmt.Printf("%-8s %-8d %-20s %v\n",
			m.Market, m.TradeStatus.Code(), m.TradeStatus.String(), m.TradeStatus.IsTrading())
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

func parseMarket(s string) (openapi.Market, error) {
	switch v := openapi.Market(strings.ToUpper(strings.TrimSpace(s))); v {
	case openapi.MarketHK, openapi.MarketUS, openapi.MarketCN,
		openapi.MarketSG, openapi.MarketUK:
		return v, nil
	}
	return "", fmt.Errorf("unknown -market %q: want HK, US, CN, SG or UK", s)
}

func dec(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(3)
}
