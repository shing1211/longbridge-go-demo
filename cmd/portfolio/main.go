// Command portfolio prints the read-only portfolio analytics surface:
// currency exchange rates, the account-level profit/loss summary, the
// per-security breakdown, a by-market P&L page, per-security P&L detail and
// the executed flow records behind it.
//
// It is entirely READ-ONLY. All five PortfolioContext methods are queries; none
// of them mutate server-side state, so there is no gate to trip here.
//
// Note the money fields are *decimal.Decimal, so they are nil when the API did
// not supply them. That is distinct from zero and is rendered as "-", not 0.00,
// because "the API did not say" and "the number is zero" are different facts
// for a P&L report.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/portfolio"
	"github.com/shopspring/decimal"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

var (
	action     string
	startDate  string
	endDate    string
	marketFlt  string
	currency   string
	symbol     string
	page       int
	size       int
	derivative bool
	itemLimit  int
	timeout    time.Duration
)

func main() {
	u := cli.NewUsage("portfolio", "read-only portfolio analytics: rates, P&L summary, by-market, detail, flows")
	u.FS.StringVar(&action, "action", "summary",
		"rates | summary | by-market | detail (-symbol) | flows (-symbol)")
	u.FS.StringVar(&startDate, "start", "", "start date YYYY-MM-DD (optional, default: server default)")
	u.FS.StringVar(&endDate, "end", "", "end date YYYY-MM-DD (optional, default: server default)")
	u.FS.StringVar(&marketFlt, "market", "", "market filter for -action by-market, e.g. HK")
	u.FS.StringVar(&currency, "currency", "", "currency filter for -action by-market, e.g. HKD")
	u.FS.StringVar(&symbol, "symbol", "700.HK", "security symbol, required for -action detail and flows")
	u.FS.IntVar(&page, "page", 1, "1-based page for -action by-market and flows")
	u.FS.IntVar(&size, "size", 50, "page size for -action by-market and flows")
	u.FS.BoolVar(&derivative, "derivative", false, "include derivative flows for -action flows")
	u.FS.IntVar(&itemLimit, "limit", 20, "how many detail rows to print (0 = all)")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary is read-only; an open order gate here would mean the
	// environment is wrong, not that we should start writing.
	cli.AssertReadOnly(cfg, "portfolio", "run the portfolio reader")

	cli.Run(func(ctx context.Context) error {
		// The portfolio package has no Close(); it is a thin HTTP client.
		pc, err := portfolio.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating portfolio context: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(action)) {
		case "rates", "exchange-rate":
			return printRates(ctx, pc)
		case "summary", "profit-analysis":
			return printSummary(ctx, pc)
		case "by-market", "by-market-profit":
			return printByMarket(ctx, pc)
		case "detail", "detail-profit":
			if strings.TrimSpace(symbol) == "" {
				return fmt.Errorf("-symbol is required for -action detail")
			}
			return printDetail(ctx, pc)
		case "flows", "profit-flows":
			if strings.TrimSpace(symbol) == "" {
				return fmt.Errorf("-symbol is required for -action flows")
			}
			return printFlows(ctx, pc)
		default:
			return fmt.Errorf(
				"unknown -action %q: want rates, summary, by-market, detail or flows", action)
		}
	})
}

func printRates(ctx context.Context, pc *portfolio.PortfolioContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section("Exchange rates")
	res, err := pc.ExchangeRate(c)
	if err != nil {
		return fmt.Errorf("exchange rates: %w", err)
	}
	if res == nil || len(res.Exchanges) == 0 {
		fmt.Println("   (no exchange rates returned)")
		return nil
	}
	fmt.Printf("%-8s %-8s %-18s %-18s %s\n", "BASE", "OTHER", "AVERAGE", "BID", "OFFER")
	for _, e := range res.Exchanges {
		fmt.Printf("%-8s %-8s %-18s %-18s %s\n",
			cli.OrDash(e.BaseCurrency), cli.OrDash(e.OtherCurrency),
			strconv.FormatFloat(e.AverageRate, 'f', -1, 64),
			strconv.FormatFloat(e.BidRate, 'f', -1, 64),
			strconv.FormatFloat(e.OfferRate, 'f', -1, 64))
	}
	fmt.Printf("\n(%d currency pairs)\n", len(res.Exchanges))
	return nil
}

func printSummary(ctx context.Context, pc *portfolio.PortfolioContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("Portfolio P&L %s", dateRange(startDate, endDate)))
	res, err := pc.ProfitAnalysis(c, &portfolio.ProfitAnalysisOptions{
		Start: startDate, End: endDate,
	})
	if err != nil {
		return fmt.Errorf("profit analysis: %w", err)
	}
	if res == nil {
		fmt.Println("   (no profit analysis returned)")
		return nil
	}

	s := res.Summary
	fmt.Printf("currency            %s\n", cli.OrDash(s.Currency))
	fmt.Printf("window              %s .. %s\n",
		cli.OrDash(s.StartDate), cli.OrDash(s.EndDate))
	fmt.Printf("current total asset %s\n", cli.Dec(s.CurrentTotalAsset))
	fmt.Printf("initial asset value %s   invested %s   ending %s\n",
		cli.Dec(s.InitialAssetValue), cli.Dec(s.InvestAmount),
		cli.Dec(s.EndingAssetValue))
	fmt.Printf("total P&L           %s  (%s)\n", cli.Dec(s.SumProfit), cli.Dec(s.SumProfitRate))
	fmt.Printf("traded              %v   orders %s   securities %s\n",
		s.IsTraded, orDashStr(s.Profits.TradeOrderNum), orDashStr(s.Profits.TradeStockNum))
	fmt.Printf("cumulative traded   %s\n", cli.Dec(s.Profits.CumulativeTransactionAmount))

	p := s.Profits
	fmt.Printf("\n-- by asset type --\n")
	fmt.Printf("%-14s %s\n", "stock", cli.Dec(p.Stock))
	fmt.Printf("%-14s %s\n", "fund", cli.Dec(p.Fund))
	fmt.Printf("%-14s %s\n", "crypto", cli.Dec(p.Crypto))
	fmt.Printf("%-14s %s\n", "money market", cli.Dec(p.Mmf))
	fmt.Printf("%-14s %s\n", "other", cli.Dec(p.Other))
	fmt.Printf("%-14s %s (%d hits, %d subscriptions)\n",
		"ipo", cli.Dec(p.Ipo), p.IpoHit, p.IpoSubscription)

	if len(p.SummaryInfo) > 0 {
		fmt.Printf("\n-- per category best/worst --\n")
		fmt.Printf("%-16s %-12s %-26s %-12s %s\n",
			"TYPE", "PROFIT MAX", "NAME", "LOSS MAX", "NAME")
		for _, si := range p.SummaryInfo {
			fmt.Printf("%-16s %-12s %-26s %-12s %s\n",
				assetTypeName(si.AssetType), orDashStr(si.ProfitMax),
				cli.Truncate(orDashStr(si.ProfitMaxName), 26), orDashStr(si.LossMax),
				cli.Truncate(orDashStr(si.LossMaxName), 26))
		}
	}

	sl := res.Sublist
	if len(sl.Items) == 0 {
		fmt.Println("\n   (no per-security breakdown returned)")
		return nil
	}
	fmt.Printf("\n-- per-security P&L (%d total, updated %s) --\n",
		len(sl.Items), cli.OrDash(sl.UpdatedDate))
	fmt.Printf("%-12s %-26s %-8s %-16s %-12s %-10s %-10s %s\n",
		"SYMBOL", "NAME", "TYPE", "PROFIT", "RATE", "HOLDING", "TRADES", "CURRENCY")
	for _, it := range limitSlice(sl.Items, itemLimit) {
		fmt.Printf("%-12s %-26s %-8s %-16s %-12s %-10v %-10d %s\n",
			cli.OrDash(it.Symbol), cli.Truncate(it.Name, 26), assetTypeName(it.ItemType),
			cli.Dec(it.Profit), cli.Dec(it.ProfitRate), it.IsHolding,
			it.ClearanceTimes, cli.OrDash(it.Currency))
	}
	if shown(itemLimit, len(sl.Items)) < len(sl.Items) {
		fmt.Printf("\n(%d of %d securities; raise -limit or narrow with -start/-end)\n",
			shown(itemLimit, len(sl.Items)), len(sl.Items))
	}
	return nil
}

func printByMarket(ctx context.Context, pc *portfolio.PortfolioContext) error {
	if page == 0 {
		return fmt.Errorf("-page must be at least 1, got 0")
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("P&L by market %s (page %d, size %d)",
		cli.OrDash(marketFlt), page, size))
	res, err := pc.ProfitAnalysisByMarket(c, &portfolio.ProfitAnalysisByMarketOptions{
		Market:   marketFlt,
		Start:    startDate,
		End:      endDate,
		Currency: currency,
		Page:     uint32(page),
		Size:     uint32(size),
	})
	if err != nil {
		return fmt.Errorf("profit analysis by market: %w", err)
	}
	if res == nil {
		fmt.Println("   (no response)")
		return nil
	}
	fmt.Printf("total P&L: %s   has_more: %v\n", cli.Dec(res.Profit), res.HasMore)
	if len(res.StockItems) == 0 {
		fmt.Println("   (no entries on this page)")
		return nil
	}
	fmt.Printf("%-12s %-26s %-8s %s\n", "CODE", "NAME", "MARKET", "PROFIT")
	for _, it := range limitSlice(res.StockItems, itemLimit) {
		fmt.Printf("%-12s %-26s %-8s %s\n",
			cli.OrDash(it.Code), cli.Truncate(it.Name, 26),
			cli.OrDash(it.Market), cli.Dec(it.Profit))
	}
	if res.HasMore {
		fmt.Printf("\n(%d entries; more pages available, continue with -page %d)\n",
			shown(itemLimit, len(res.StockItems)), page+1)
	}
	return nil
}

func printDetail(ctx context.Context, pc *portfolio.PortfolioContext) error {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("P&L detail %s %s", symbol, dateRange(startDate, endDate)))
	res, err := pc.ProfitAnalysisDetail(c, &portfolio.ProfitAnalysisDetailOptions{
		Symbol: symbol, Start: startDate, End: endDate,
	})
	if err != nil {
		return fmt.Errorf("profit analysis detail for %s: %w", symbol, err)
	}
	if res == nil {
		fmt.Println("   (no detail returned)")
		return nil
	}
	fmt.Printf("name       %s\n", cli.OrDash(res.Name))
	fmt.Printf("currency   %s\n", cli.OrDash(res.Currency))
	fmt.Printf("window     %s .. %s   updated %s\n",
		cli.OrDash(res.StartDate), cli.OrDash(res.EndDate), cli.OrDash(res.UpdatedDate))
	fmt.Printf("total P&L  %s   default tab %d (0 underlying, 1 derivative)\n",
		cli.Dec(res.Profit), res.DefaultTag)
	printProfitDetails("underlying", res.UnderlyingDetails)
	printProfitDetails("derivative", res.DerivativePnlDetails)
	return nil
}

func printProfitDetails(label string, d portfolio.ProfitDetails) {
	fmt.Printf("\n-- %s --\n", label)
	fmt.Printf("holding value        %s (long %s / short %s)\n",
		cli.Dec(d.HoldingValue), cli.Dec(d.LongHoldingValue), cli.Dec(d.ShortHoldingValue))
	fmt.Printf("holding at start/end %s / %s\n",
		cli.Dec(d.HoldingValueAtBeginning), cli.Dec(d.HoldingValueAtEnding))
	fmt.Printf("profit               %s\n", cli.Dec(d.Profit))
	printEntries("credited", d.CumulativeCreditedAmount, d.CreditedDetails)
	printEntries("debited", d.CumulativeDebitedAmount, d.DebitedDetails)
	printEntries("fees", d.CumulativeFeeAmount, d.FeeDetails)
}

func printEntries(label string, total *decimal.Decimal, entries []portfolio.ProfitDetailEntry) {
	if total == nil && len(entries) == 0 {
		fmt.Printf("%-20s (none)\n", label+":")
		return
	}
	fmt.Printf("%s: %s\n", label, cli.Dec(total))
	shownN := shown(itemLimit, len(entries))
	for _, e := range entries[:shownN] {
		fmt.Printf("   %-40s %s\n", cli.Truncate(orDashStr(e.Describe), 40), cli.Dec(e.Amount))
	}
	if shownN < len(entries) {
		fmt.Printf("   (%d of %d entries; raise -limit for more)\n", shownN, len(entries))
	}
}

func printFlows(ctx context.Context, pc *portfolio.PortfolioContext) error {
	if page == 0 {
		return fmt.Errorf("-page must be at least 1 (pages are 1-based), got %d", page)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli.Section(fmt.Sprintf("P&L flows %s (page %d, size %d, derivative=%v)",
		symbol, page, size, derivative))
	res, err := pc.ProfitAnalysisFlows(c, &portfolio.ProfitAnalysisFlowsOptions{
		Symbol:     symbol,
		Page:       uint32(page),
		Size:       uint32(size),
		Derivative: derivative,
		Start:      startDate,
		End:        endDate,
	})
	if err != nil {
		return fmt.Errorf("profit analysis flows for %s: %w", symbol, err)
	}
	if res == nil || len(res.FlowsList) == 0 {
		fmt.Println("   (no flow records on this page)")
		return nil
	}
	fmt.Printf("%-14s %-12s %-10s %-14s %-14s %s\n",
		"DATE", "CODE", "DIRECTION", "QUANTITY", "PRICE", "COST")
	for _, f := range limitSlice(res.FlowsList, itemLimit) {
		fmt.Printf("%-14s %-12s %-10s %-14s %-14s %s\n",
			cli.OrDash(f.ExecutedDate), cli.OrDash(f.Code), flowDirectionName(f.Direction),
			cli.Dec(f.ExecutedQuantity), cli.Dec(f.ExecutedPrice), cli.Dec(f.ExecutedCost))
	}
	if res.HasMore {
		fmt.Printf("\n(%d flows; more pages available, continue with -page %d)\n",
			shown(itemLimit, len(res.FlowsList)), page+1)
	}
	return nil
}

// ------------------------------------------------------------------ helpers

func assetTypeName(t portfolio.AssetType) string {
	switch t {
	case portfolio.AssetTypeStock:
		return "stock"
	case portfolio.AssetTypeFund:
		return "fund"
	case portfolio.AssetTypeCrypto:
		return "crypto"
	}
	return "unknown"
}

func flowDirectionName(d portfolio.FlowDirection) string {
	switch d {
	case portfolio.FlowDirectionBuy:
		return "buy"
	case portfolio.FlowDirectionSell:
		return "sell"
	}
	return "unknown"
}

func dateRange(start, end string) string {
	if start == "" && end == "" {
		return "(server default window)"
	}
	return fmt.Sprintf("(%s .. %s)", orDashStr(start), orDashStr(end))
}

func orDashStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// limitSlice caps a printout at n elements, where n <= 0 means "all".
func limitSlice[T any](items []T, n int) []T {
	if n > 0 && len(items) > n {
		return items[:n]
	}
	return items
}

func shown(limitN, total int) int {
	if limitN > 0 && limitN < total {
		return limitN
	}
	return total
}
