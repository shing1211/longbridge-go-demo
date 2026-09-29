package main

// One function per SDK method. Each calls exactly one FundamentalContext,
// AssetContext or CalendarContext method, applies the per-request timeout, and
// renders the response. Nothing here computes a value the SDK did not return.
//
// The field-by-field rendering is written against the v0.25.2 type definitions
// in the module cache, NOT against an observed response: no working access
// token was available, so no successful payload has ever been seen. Column
// widths are a first guess and should be tuned once real data flows.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go"
	"github.com/longbridge/openapi-go/asset"
	"github.com/longbridge/openapi-go/calendar"
	"github.com/longbridge/openapi-go/fundamental"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
)

// ------------------------------------------- reports, ratings, estimates

// printFinancialReport -> FundamentalContext.FinancialReport
func printFinancialReport(ctx context.Context, fc *fundamental.FundamentalContext) error {
	k, err := parseKind(kind)
	if err != nil {
		return err
	}
	p, err := parsePeriod(period)
	if err != nil {
		return err
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Financial reports %s (kind=%s period=%s)", symbol, kind, orNone(period)))
	// FinancialReports.List is raw JSON: the SDK leaves the indicator/account
	// tree untyped because its shape differs per kind and per market.
	reps, err := fc.FinancialReport(c, symbol, k, p)
	if err != nil {
		return fmt.Errorf("financial report for %s: %w", symbol, err)
	}
	fmt.Println(raw(reps.List))
	return nil
}

// printInstitutionRating -> FundamentalContext.InstitutionRating
//
// This one SDK method issues TWO GETs internally (institution-rating-latest
// and institution-ratings) and returns the combination, failing if either does.
func printInstitutionRating(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Analyst rating %s", symbol))
	r, err := fc.InstitutionRating(c, symbol)
	if err != nil {
		return fmt.Errorf("institution rating for %s: %w", symbol, err)
	}

	fmt.Println("latest snapshot")
	l := r.Latest
	fmt.Printf("  distribution: strong_buy/over=%d buy=%d hold=%d under=%d sell=%d no_opinion=%d total=%d\n",
		l.Evaluate.Over, l.Evaluate.Buy, l.Evaluate.Hold, l.Evaluate.Under,
		l.Evaluate.Sell, l.Evaluate.NoOpinion, l.Evaluate.Total)
	fmt.Printf("  window: %s .. %s\n", cli.OrDash(l.Evaluate.StartDate), cli.OrDash(l.Evaluate.EndDate))
	fmt.Printf("  target: low=%s high=%s (prev_close=%s)\n",
		cli.Dec4(l.Target.LowestPrice), cli.Dec4(l.Target.HighestPrice), cli.Dec4(l.Target.PrevClose))
	fmt.Printf("  industry: %s  rank %d/%d  mean analysts %d median %d\n",
		cli.OrDash(l.IndustryName), l.IndustryRank, l.IndustryTotal, l.IndustryMean, l.IndustryMedian)

	s := r.Summary
	fmt.Println("consensus summary")
	fmt.Printf("  currency=%s recommend=%s target=%s change=%s updated=%s\n",
		cli.OrDash(s.CcySymbol), parseRecommend(s.Recommend), cli.Dec4(s.Target),
		cli.Dec4(s.Change), cli.OrDash(s.UpdatedAt))
	fmt.Printf("  strong_buy=%d buy=%d hold=%d under=%d sell=%d (as of %s)\n",
		s.Evaluate.StrongBuy, s.Evaluate.Buy, s.Evaluate.Hold,
		s.Evaluate.Under, s.Evaluate.Sell, cli.OrDash(s.Evaluate.Date))
	return nil
}

// printInstitutionRatingDetail -> FundamentalContext.InstitutionRatingDetail
func printInstitutionRatingDetail(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Analyst rating history %s", symbol))
	d, err := fc.InstitutionRatingDetail(c, symbol)
	if err != nil {
		return fmt.Errorf("institution rating detail for %s: %w", symbol, err)
	}

	fmt.Printf("currency=%s  prediction accuracy=%s  data share=%s  updated=%s\n",
		cli.OrDash(d.CcySymbol), cli.Dec4(d.Target.PredictionAccuracy),
		cli.Dec4(d.Target.DataPercent), cli.OrDash(d.Target.UpdatedAt))

	fmt.Println("\nrating distribution over time")
	fmt.Printf("%-12s %9s %9s %9s %9s %9s %9s\n", "DATE", "STRONG_B", "BUY", "HOLD", "UNDER", "SELL", "NO_OP")
	for _, it := range d.Evaluate.List {
		fmt.Printf("%-12s %9d %9d %9d %9d %9d %9d\n",
			cli.OrDash(it.Date), it.StrongBuy, it.Buy, it.Hold, it.Under, it.Sell, it.NoOpinion)
	}

	fmt.Println("\ntarget price over time")
	fmt.Printf("%-12s %11s %11s %11s %11s %s\n", "DATE", "AVG", "MIN", "MAX", "PRICE", "MET")
	for _, it := range d.Target.List {
		fmt.Printf("%-12s %11s %11s %11s %11s %s\n",
			cli.OrDash(it.Date), cli.Dec4(it.AvgTarget), cli.Dec4(it.MinTarget),
			cli.Dec4(it.MaxTarget), cli.Dec4(it.Price), yesNo(it.Meet))
	}
	return nil
}

// printInstitutionRatingViews -> FundamentalContext.InstitutionRatingViews
//
// The counts on this type are STRINGS, not ints, unlike
// InstitutionRatingDetail's. That asymmetry is in the SDK, not a typo here.
func printInstitutionRatingViews(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Analyst rating views %s", symbol))
	v, err := fc.InstitutionRatingViews(c, symbol)
	if err != nil {
		return fmt.Errorf("institution rating views for %s: %w", symbol, err)
	}
	if len(v.Elist) == 0 {
		fmt.Println("   (no snapshots returned)")
		return nil
	}
	fmt.Printf("%-12s %9s %9s %9s %9s %9s %9s\n", "DATE", "BUY", "OVER", "HOLD", "UNDER", "SELL", "TOTAL")
	for _, it := range v.Elist {
		fmt.Printf("%-12s %9s %9s %9s %9s %9s %9s\n",
			it.Date.Format("2006-01-02"), cli.OrDash(it.Buy), cli.OrDash(it.Over),
			cli.OrDash(it.Hold), cli.OrDash(it.Under), cli.OrDash(it.Sell), cli.OrDash(it.Total))
	}
	return nil
}

// printForecastEps -> FundamentalContext.ForecastEps
func printForecastEps(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("EPS forecast %s", symbol))
	f, err := fc.ForecastEps(c, symbol)
	if err != nil {
		return fmt.Errorf("forecast EPS for %s: %w", symbol, err)
	}
	if len(f.Items) == 0 {
		fmt.Println("   (no forecasts returned)")
		return nil
	}
	fmt.Printf("%-11s %-11s %10s %10s %10s %10s %8s %6s %6s\n",
		"FROM", "TO", "MEDIAN", "MEAN", "LOW", "HIGH", "INSTS", "UP", "DOWN")
	for _, it := range f.Items {
		fmt.Printf("%-11s %-11s %10s %10s %10s %10s %8d %6d %6d\n",
			it.ForecastStartDate.Format("2006-01-02"), it.ForecastEndDate.Format("2006-01-02"),
			cli.Dec4(it.ForecastEpsMedian), cli.Dec4(it.ForecastEpsMean),
			cli.Dec4(it.ForecastEpsLowest), cli.Dec4(it.ForecastEpsHighest),
			it.InstitutionTotal, it.InstitutionUp, it.InstitutionDown)
	}
	return nil
}

// printConsensus -> FundamentalContext.Consensus
func printConsensus(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Financial consensus %s", symbol))
	cs, err := fc.Consensus(c, symbol)
	if err != nil {
		return fmt.Errorf("consensus for %s: %w", symbol, err)
	}
	fmt.Printf("currency=%s current period=%s available=%s current index=%d\n",
		cli.OrDash(cs.Currency), cli.OrDash(cs.CurrentPeriod),
		cli.OrDash(strings.Join(cs.OptPeriods, ",")), cs.CurrentIndex)
	if len(cs.List) == 0 {
		fmt.Println("   (no consensus periods returned)")
		return nil
	}
	for _, r := range cs.List {
		fmt.Printf("\n%s (%s)\n", cli.OrDash(r.PeriodText), cli.OrDash(r.FiscalPeriod))
		fmt.Printf("  %-22s %14s %14s %12s %-10s %s\n",
			"METRIC", "ACTUAL", "ESTIMATE", "SURPRISE", "VERDICT", "RELEASED")
		for _, d := range r.Details {
			fmt.Printf("  %-22s %14s %14s %12s %-10s %s\n",
				cli.Truncate(cli.OrDash(d.Name), 22), cli.Dec(d.Actual), cli.Dec(d.Estimate),
				cli.Dec(d.CompValue), cli.Truncate(cli.OrDash(d.CompDesc), 10), yesNo(d.IsReleased))
		}
	}
	return nil
}

// printSnapshot -> FundamentalContext.FinancialReportSnapshot
func printSnapshot(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Earnings snapshot %s", symbol))
	s, err := fc.FinancialReportSnapshot(c, symbol, report, fiscalYear, fiscalPer)
	if err != nil {
		return fmt.Errorf("earnings snapshot for %s: %w", symbol, err)
	}
	fmt.Printf("%s  ticker=%s  currency=%s\n", cli.OrDash(s.Name), cli.OrDash(s.Ticker), cli.OrDash(s.Currency))
	fmt.Printf("period %s .. %s  (%s)\n", cli.OrDash(s.FpStart), cli.OrDash(s.FpEnd), cli.OrDash(s.ReportDesc))

	fmt.Println("\nforecast vs reported")
	fmt.Printf("  %-14s %16s %10s %16s %10s %s\n", "METRIC", "FORECAST", "YOY", "REPORTED", "YOY", "VS EST")
	for _, m := range []struct {
		name string
		f    *fundamental.SnapshotForecastMetric
		r    *fundamental.SnapshotReportedMetric
	}{
		{"revenue", s.FoRevenue, s.FrRevenue},
		{"EBIT", s.FoEbit, nil},
		{"EPS", s.FoEps, nil},
		{"net profit", nil, s.FrProfit},
		{"operating CF", nil, s.FrOperateCash},
		{"investing CF", nil, s.FrInvestCash},
		{"financing CF", nil, s.FrFinanceCash},
		{"total assets", nil, s.FrTotalAssets},
		{"total liabilities", nil, s.FrTotalLiability},
	} {
		fv, fy, cmp, rv, ry := "-", "-", "-", "-", "-"
		if m.f != nil {
			fv, fy = cli.OrDash(m.f.Value), cli.OrDash(m.f.Yoy)
			cmp = cli.OrDash(m.f.CmpDesc)
		}
		if m.r != nil {
			rv, ry = cli.OrDash(m.r.Value), cli.OrDash(m.r.Yoy)
		}
		fmt.Printf("  %-14s %16s %10s %16s %10s %s\n", m.name, fv, fy, rv, ry, cmp)
	}

	fmt.Println("\nratios")
	fmt.Printf("  ROE (TTM)          %s\n", cli.OrDash(s.FrRoeTtm))
	fmt.Printf("  profit margin      %s (TTM %s)\n", cli.OrDash(s.FrProfitMargin), cli.OrDash(s.FrProfitMarginTtm))
	fmt.Printf("  asset turnover     %s (TTM)\n", cli.OrDash(s.FrAssetTurnTtm))
	fmt.Printf("  leverage           %s (TTM)\n", cli.OrDash(s.FrLeverageTtm))
	fmt.Printf("  debt / assets      %s\n", cli.OrDash(s.FrDebtAssetsRatio))
	return nil
}

// printOperating -> FundamentalContext.Operating
func printOperating(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Operating reports %s", symbol))
	l, err := fc.Operating(c, symbol)
	if err != nil {
		return fmt.Errorf("operating reports for %s: %w", symbol, err)
	}
	if len(l.List) == 0 {
		fmt.Println("   (no operating reports returned)")
		return nil
	}
	for _, item := range l.List {
		tag := " "
		if item.Latest {
			tag = "*"
		}
		fmt.Printf("\n[%s] %s (report=%s)\n", tag, cli.OrDash(item.Title), cli.OrDash(item.Report))
		if item.Latest {
			fmt.Println("    * = the most recent report")
		}
		f := item.Financial
		if f.Symbol != "" || f.Currency != "" {
			fmt.Printf("    %s  currency=%s  region=%s  %s\n",
				cli.OrDash(f.Symbol), cli.OrDash(f.Currency),
				cli.OrDash(f.Region), cli.OrDash(f.ReportTxt))
		}
		fmt.Printf("    %-26s %-18s %s\n", "INDICATOR", "VALUE", "YOY")
		for _, ind := range f.Indicators {
			fmt.Printf("    %-26s %-18s %s\n",
				cli.Truncate(cli.OrDash(ind.IndicatorName), 26),
				cli.Truncate(cli.OrDash(ind.IndicatorValue), 18),
				cli.Dec4(ind.Yoy))
		}
		if cli.OrDash(item.Txt) != "-" {
			fmt.Printf("    narrative: %s\n", cli.Truncate(item.Txt, 300))
		}
	}
	return nil
}

// ------------------------------------------- dividends, corporate actions

// printDividend -> FundamentalContext.Dividend or DividendDetail
//
// The two methods return the SAME type, DividendList, from different paths
// (/v1/quote/dividends vs /v1/quote/dividends/details). The selector is kept
// explicit so each is reachable on its own.
func printDividend(ctx context.Context, fc *fundamental.FundamentalContext, which string) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Dividends %s (%s)", symbol, which))
	var (
		l   *fundamental.DividendList
		err error
	)
	if which == "dividend-detail" {
		l, err = fc.DividendDetail(c, symbol)
	} else {
		l, err = fc.Dividend(c, symbol)
	}
	if err != nil {
		return fmt.Errorf("dividend (%s) for %s: %w", which, symbol, err)
	}
	if len(l.List) == 0 {
		fmt.Println("   (no dividend records returned)")
		return nil
	}
	fmt.Printf("%-12s %-12s %-12s %-10s %s\n", "RECORD", "EX-DATE", "PAYMENT", "SYMBOL", "DESCRIPTION")
	for _, d := range l.List {
		fmt.Printf("%-12s %-12s %-12s %-10s %s\n",
			cli.OrDash(d.RecordDate), cli.OrDash(d.ExDate), cli.OrDash(d.PaymentDate),
			cli.Truncate(cli.OrDash(d.Symbol), 10), cli.Truncate(cli.OrDash(d.Desc), 48))
	}
	return nil
}

// printCorpAction -> FundamentalContext.CorpAction
func printCorpAction(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Corporate actions %s", symbol))
	a, err := fc.CorpAction(c, symbol)
	if err != nil {
		return fmt.Errorf("corporate actions for %s: %w", symbol, err)
	}
	if len(a.Items) == 0 {
		fmt.Println("   (no corporate actions returned)")
		return nil
	}
	fmt.Printf("%-10s %-10s %-14s %-24s %-20s %s\n",
		"DATE", "TYPE", "CATEGORY", "ACTION", "DESCRIPTION", "FLAGS")
	for _, it := range a.Items {
		var flags []string
		if it.Recent {
			flags = append(flags, "recent")
		}
		if it.IsDelay {
			flags = append(flags, "delayed")
		}
		if it.Live != nil {
			flags = append(flags, "live")
		}
		fmt.Printf("%-10s %-10s %-14s %-24s %-20s %s\n",
			cli.OrDash(it.Date), cli.Truncate(cli.OrDash(it.DateType), 10),
			cli.Truncate(cli.OrDash(it.ActType), 14), cli.Truncate(cli.OrDash(it.Action), 24),
			cli.Truncate(cli.OrDash(it.ActDesc), 20), cli.OrDash(strings.Join(flags, ",")))
		if it.IsDelay && it.DelayContent != "" {
			fmt.Printf("           delayed: %s\n", cli.Truncate(it.DelayContent, 100))
		}
	}
	return nil
}

// printBuyback -> FundamentalContext.Buyback
func printBuyback(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Buybacks %s", symbol))
	b, err := fc.Buyback(c, symbol)
	if err != nil {
		return fmt.Errorf("buyback data for %s: %w", symbol, err)
	}
	if r := b.RecentBuybacks; r != nil {
		fmt.Printf("TTM: net buyback %s %s  yield %s\n",
			cli.OrDash(r.Currency), cli.Dec(r.NetBuybackTTM), cli.Dec4(r.NetBuybackYieldTTM))
	}
	if len(b.BuybackHistory) > 0 {
		fmt.Println("\nhistory")
		fmt.Printf("%-12s %-24s %14s %10s %10s %s\n",
			"FISCAL_YEAR", "RANGE", "NET_BUYBACK", "YIELD", "GROWTH", "CCY")
		for _, h := range b.BuybackHistory {
			fmt.Printf("%-12s %-24s %14s %10s %10s %s\n",
				cli.OrDash(h.FiscalYear), cli.OrDash(h.FiscalYearRange),
				cli.Dec(h.NetBuyback), cli.Dec4(h.NetBuybackYield),
				cli.Dec4(h.NetBuybackGrowthRate), cli.OrDash(h.Currency))
		}
	}
	if len(b.BuybackRatios) > 0 {
		fmt.Println("\nratios (payout, then share of free cash flow)")
		for i, r := range b.BuybackRatios {
			fmt.Printf("  [%d] payout=%s  of_cashflow=%s\n",
				i, cli.Dec4(r.NetBuybackPayoutRatio), cli.Dec4(r.NetBuybackToCashflowRatio))
		}
	}
	if b.RecentBuybacks == nil && len(b.BuybackHistory) == 0 && len(b.BuybackRatios) == 0 {
		fmt.Println("   (no buyback data returned)")
	}
	return nil
}

// ------------------------------------------- valuation

// printValuation -> FundamentalContext.Valuation
func printValuation(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Valuation %s", symbol))
	v, err := fc.Valuation(c, symbol)
	if err != nil {
		return fmt.Errorf("valuation for %s: %w", symbol, err)
	}
	for _, m := range []struct {
		name string
		d    *fundamental.ValuationMetricData
	}{
		{"PE", v.Metrics.PE},
		{"PB", v.Metrics.PB},
		{"PS", v.Metrics.PS},
		{"DIV_YIELD", v.Metrics.DvdYld},
	} {
		if m.d == nil {
			fmt.Printf("%-10s (not returned)\n", m.name)
			continue
		}
		fmt.Printf("%-10s low=%-12s median=%-12s high=%-12s points=%d\n",
			m.name, cli.Dec4(m.d.Low), cli.Dec4(m.d.Median), cli.Dec4(m.d.High), len(m.d.List))
		if m.d.Desc != "" {
			fmt.Printf("           %s\n", cli.Truncate(m.d.Desc, 120))
		}
	}
	fmt.Println("\nThe most recent points of the first non-empty series:")
	printed := false
	for _, m := range []*fundamental.ValuationMetricData{v.Metrics.PE, v.Metrics.PB, v.Metrics.PS, v.Metrics.DvdYld} {
		if m == nil || len(m.List) == 0 {
			continue
		}
		start := len(m.List) - 5
		if start < 0 {
			start = 0
		}
		fmt.Printf("  %-20s %-12s %s\n", m.Desc, "", "")
		for _, p := range m.List[start:] {
			fmt.Printf("      %s  %s\n", p.Timestamp.Format("2006-01-02"), cli.Dec4(p.Value))
		}
		printed = true
		break
	}
	if !printed {
		fmt.Println("   (no valuation series returned)")
	}
	return nil
}

// printValuationHistory -> FundamentalContext.ValuationHistory
func printValuationHistory(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Valuation history %s", symbol))
	h, err := fc.ValuationHistory(c, symbol)
	if err != nil {
		return fmt.Errorf("valuation history for %s: %w", symbol, err)
	}
	// Note: this response carries PE/PB/PS only — no dividend yield.
	for _, m := range []struct {
		name string
		d    *fundamental.ValuationHistoryMetric
	}{
		{"PE", h.History.Metrics.PE},
		{"PB", h.History.Metrics.PB},
		{"PS", h.History.Metrics.PS},
	} {
		if m.d == nil {
			fmt.Printf("%-4s (not returned)\n", m.name)
			continue
		}
		fmt.Printf("%-4s low=%-12s median=%-12s high=%-12s points=%d  %s\n",
			m.name, cli.Dec4(m.d.Low), cli.Dec4(m.d.Median), cli.Dec4(m.d.High),
			len(m.d.List), cli.Truncate(m.d.Desc, 80))
	}
	return nil
}

// printIndustryValuation -> FundamentalContext.IndustryValuation
func printIndustryValuation(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Industry valuation peers %s", symbol))
	l, err := fc.IndustryValuation(c, symbol)
	if err != nil {
		return fmt.Errorf("industry valuation for %s: %w", symbol, err)
	}
	if len(l.List) == 0 {
		fmt.Println("   (no peers returned)")
		return nil
	}
	fmt.Printf("%-12s %-26s %-5s %9s %9s %9s %9s %8s %8s\n",
		"SYMBOL", "NAME", "CCY", "PE", "PB(implied)", "EPS", "DPS", "YIELD", "PAYOUT")
	for _, it := range l.List {
		// PB is not on this type; BPS and assets are the balance-sheet inputs
		// the API does return, so print those rather than inventing a ratio.
		fmt.Printf("%-12s %-26s %-5s %9s %9s %9s %9s %8s %8s\n",
			cli.OrDash(it.Symbol), cli.Truncate(cli.OrDash(it.Name), 26), cli.OrDash(it.Currency),
			cli.Dec4(it.PE), cli.Dec(it.Assets), cli.Dec4(it.Eps), cli.Dec4(it.Dps),
			cli.Dec4(it.DivYld), cli.Dec4(it.DivPayoutRatio))
	}
	fmt.Println("\n(assets is a balance-sheet total, not a PB ratio; BPS is the book value per share)")
	for _, it := range l.List {
		if len(it.History) == 0 {
			continue
		}
		fmt.Printf("  %-12s bps=%s  5y_avg_dps=%s  history points=%d\n",
			cli.OrDash(it.Symbol), cli.Dec4(it.Bps), cli.Dec4(it.FiveYAvgDps), len(it.History))
	}
	return nil
}

// printIndustryValuationDist -> FundamentalContext.IndustryValuationDist
func printIndustryValuationDist(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Industry valuation distribution %s", symbol))
	d, err := fc.IndustryValuationDist(c, symbol)
	if err != nil {
		return fmt.Errorf("industry valuation distribution for %s: %w", symbol, err)
	}
	for _, m := range []struct {
		name string
		d    *fundamental.ValuationDist
	}{
		{"PE", d.PE}, {"PB", d.PB}, {"PS", d.PS},
	} {
		if m.d == nil {
			fmt.Printf("%-4s (not returned)\n", m.name)
			continue
		}
		fmt.Printf("%-4s value=%-12s low=%-12s median=%-12s high=%-12s\n",
			m.name, cli.Dec4(m.d.Value), cli.Dec4(m.d.Low),
			cli.Dec4(m.d.Median), cli.Dec4(m.d.High))
		fmt.Printf("     percentile=%-12s rank %s of %s\n",
			cli.Dec4(m.d.Ranking), cli.OrDash(m.d.RankIndex), cli.OrDash(m.d.RankTotal))
	}
	return nil
}

// printValuationComparison -> FundamentalContext.ValuationComparison
//
// This one takes a peer LIST and a currency, unlike the other valuation calls.
// Its fields are all strings because the SDK does not type them.
func printValuationComparison(ctx context.Context, fc *fundamental.FundamentalContext) error {
	peerSyms := splitList(peers)
	if len(peerSyms) == 0 {
		return fmt.Errorf("-peers is empty")
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Valuation comparison %s vs %s (%s)",
		symbol, strings.Join(peerSyms, ","), currency))
	r, err := fc.ValuationComparison(c, symbol, currency, peerSyms)
	if err != nil {
		return fmt.Errorf("valuation comparison for %s: %w", symbol, err)
	}
	if len(r.List) == 0 {
		fmt.Println("   (no comparison rows returned)")
		return nil
	}
	fmt.Printf("%-12s %-24s %-4s %12s %10s %9s %9s %9s %8s\n",
		"SYMBOL", "NAME", "CCY", "MARKET_VALUE", "PRICE", "PE", "PB", "PS", "ROE")
	for _, it := range r.List {
		fmt.Printf("%-12s %-24s %-4s %12s %10s %9s %9s %9s %8s\n",
			cli.OrDash(it.Symbol), cli.Truncate(cli.OrDash(it.Name), 24), cli.OrDash(it.Currency),
			cli.OrDash(it.MarketValue), cli.OrDash(it.PriceClose),
			cli.OrDash(it.Pe), cli.OrDash(it.Pb), cli.OrDash(it.Ps), cli.OrDash(it.Roe))
	}
	fmt.Println("\nper-share detail")
	fmt.Printf("%-12s %10s %10s %10s %10s %10s %14s\n",
		"SYMBOL", "EPS", "BPS", "DPS", "YIELD", "ROE", "ASSETS")
	for _, it := range r.List {
		fmt.Printf("%-12s %10s %10s %10s %10s %10s %14s\n",
			cli.OrDash(it.Symbol), cli.OrDash(it.Eps), cli.OrDash(it.Bps),
			cli.OrDash(it.Dps), cli.OrDash(it.DivYld), cli.OrDash(it.Roe),
			cli.OrDash(it.Assets))
	}
	for _, it := range r.List {
		if len(it.History) == 0 {
			continue
		}
		start := len(it.History) - 3
		if start < 0 {
			start = 0
		}
		fmt.Printf("\n%s valuation history (last %d):\n", cli.OrDash(it.Symbol), len(it.History[start:]))
		for _, p := range it.History[start:] {
			fmt.Printf("  %-26s pe=%-9s pb=%-9s ps=%s\n",
				cli.OrDash(p.Date), cli.OrDash(p.Pe), cli.OrDash(p.Pb), cli.OrDash(p.Ps))
		}
	}
	return nil
}

// ------------------------------------------- company, people, segments

// printCompany -> FundamentalContext.Company
func printCompany(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Company overview %s", symbol))
	o, err := fc.Company(c, symbol)
	if err != nil {
		return fmt.Errorf("company overview for %s: %w", symbol, err)
	}
	for _, r := range [][2]string{
		{"name", cli.OrDash(o.Name)},
		{"legal name", cli.OrDash(o.CompanyName)},
		{"ticker", cli.OrDash(o.Ticker)},
		{"founded", cli.OrDash(o.Founded)},
		{"listed", cli.OrDash(o.ListingDate)},
		{"market", cli.OrDash(o.Market) + " (" + cli.OrDash(o.Region) + ")"},
		{"category", cli.OrDash(o.Category)},
		{"sector code", strconv.Itoa(int(o.Sector))},
		{"year end", cli.OrDash(o.YearEnd)},
		{"issue price", cli.Dec4(o.IssuePrice)},
		{"shares offered", cli.OrDash(o.SharesOffered)},
		{"employees", cli.OrDash(o.Employees)},
		{"chairman", cli.OrDash(o.Chairman)},
		{"CEO / manager", cli.OrDash(o.Manager)},
		{"secretary", cli.OrDash(o.Secretary)},
		{"legal rep", cli.OrDash(o.LegalRepr)},
		{"auditor", cli.OrDash(o.AuditInst)},
		{"accounting firm", cli.OrDash(o.AccountingFirm)},
		{"securities rep", cli.OrDash(o.SecuritiesRep)},
		{"legal counsel", cli.OrDash(o.LegalCounsel)},
		{"website", cli.OrDash(o.Website)},
		{"IR email", cli.OrDash(o.Email)},
		{"phone", cli.OrDash(o.Phone)},
		{"fax", cli.OrDash(o.Fax)},
		{"zip", cli.OrDash(o.ZipCode)},
		{"business licence", cli.OrDash(o.BusLicense)},
		{"registered address", cli.OrDash(o.Address)},
		{"office address", cli.OrDash(o.OfficeAddress)},
		{"ADS ratio", cli.OrDash(o.AdsRatio)},
	} {
		fmt.Printf("  %-20s %s\n", r[0], r[1])
	}
	if o.Profile != "" {
		fmt.Printf("\nprofile:\n%s\n", o.Profile)
	}
	return nil
}

// printExecutive -> FundamentalContext.Executive
func printExecutive(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Executives and board %s", symbol))
	l, err := fc.Executive(c, symbol)
	if err != nil {
		return fmt.Errorf("executives for %s: %w", symbol, err)
	}
	if len(l.ProfessionalList) == 0 {
		fmt.Println("   (no executives returned)")
		return nil
	}
	for _, g := range l.ProfessionalList {
		fmt.Printf("%s  (%d people)\n", cli.OrDash(g.Symbol), g.Total)
		for _, p := range g.Professionals {
			fmt.Printf("  %-16s %-40s %s\n",
				cli.Truncate(cli.OrDash(p.NameZhCN), 16),
				cli.Truncate(cli.OrDash(p.Title), 40),
				cli.Truncate(cli.OrDash(p.NameEn), 30))
			if p.Biography != "" {
				fmt.Printf("      %s\n", cli.Truncate(p.Biography, 200))
			}
		}
	}
	return nil
}

// printSegments -> FundamentalContext.BusinessSegments
func printSegments(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Business segments %s", symbol))
	s, err := fc.BusinessSegments(c, symbol)
	if err != nil {
		return fmt.Errorf("business segments for %s: %w", symbol, err)
	}
	fmt.Printf("as of %s  total %s %s\n",
		cli.OrDash(s.Date), cli.OrDash(s.Total), cli.OrDash(s.Currency))
	if len(s.Business) == 0 {
		fmt.Println("   (no segments returned)")
		return nil
	}
	fmt.Printf("  %-34s %s\n", "SEGMENT", "PERCENT")
	for _, b := range s.Business {
		fmt.Printf("  %-34s %s\n", cli.Truncate(cli.OrDash(b.Name), 34), cli.OrDash(b.Percent))
	}
	return nil
}

// printSegmentsHistory -> FundamentalContext.BusinessSegmentsHistory
func printSegmentsHistory(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Business segment history %s (report=%s cate=%s)", symbol, orNone(report), orNone(cate)))
	h, err := fc.BusinessSegmentsHistory(c, symbol, report, cate)
	if err != nil {
		return fmt.Errorf("business segment history for %s: %w", symbol, err)
	}
	if len(h.Historical) == 0 {
		fmt.Println("   (no history returned)")
		return nil
	}
	for _, item := range h.Historical {
		fmt.Printf("\n%s  total %s %s\n",
			cli.OrDash(item.Date), cli.OrDash(item.Total), cli.OrDash(item.Currency))
		// This snapshot carries BOTH a business and a regional breakdown.
		for _, g := range []struct {
			title string
			items []fundamental.BusinessSegmentHistoryItem
		}{
			{"by business", item.Business},
			{"by region", item.Regionals},
		} {
			if len(g.items) == 0 {
				continue
			}
			fmt.Printf("  %s\n", g.title)
			for _, b := range g.items {
				fmt.Printf("    %-30s %10s %18s\n",
					cli.Truncate(cli.OrDash(b.Name), 30), cli.OrDash(b.Percent), cli.OrDash(b.Value))
			}
		}
	}
	return nil
}

// printRatings -> FundamentalContext.Ratings
//
// The score fields are json.RawMessage in the SDK because the API may return
// an int, a float or null for each, so they are printed verbatim.
func printRatings(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Stock ratings %s", symbol))
	r, err := fc.Ratings(c, symbol)
	if err != nil {
		return fmt.Errorf("stock ratings for %s: %w", symbol, err)
	}
	fmt.Printf("style=%s scale=%s period=%s\n",
		cli.OrDash(r.StyleTxtName), cli.OrDash(r.ScaleTxtName), cli.OrDash(r.ReportPeriodTxt))
	fmt.Printf("composite: score=%s letter=%s change=%+d\n",
		raw(r.MultiScore), cli.OrDash(r.MultiLetter), r.MultiScoreChange)
	fmt.Printf("industry: %s  rank=%s of %s  mean=%s median=%s\n",
		cli.OrDash(r.IndustryName), raw(r.IndustryRank), raw(r.IndustryTotal),
		raw(r.IndustryMeanScore), raw(r.IndustryMedianScore))

	for _, cat := range r.Ratings {
		fmt.Printf("\ncategory kind=%d\n", cat.Kind)
		for _, g := range cat.SubIndicators {
			fmt.Printf("  %-34s score=%-8s letter=%s\n",
				cli.Truncate(cli.OrDash(g.Indicator.Name), 34),
				raw(g.Indicator.Score), cli.OrDash(g.Indicator.Letter))
			for _, l := range g.SubIndicators {
				fmt.Printf("      %-30s %-12s score=%-8s letter=%s\n",
					cli.Truncate(cli.OrDash(l.Name), 30), cli.Truncate(cli.OrDash(l.Value), 12),
					raw(l.Score), cli.OrDash(l.Letter))
			}
		}
	}
	return nil
}

// ------------------------------------------- ownership

// printShareholder -> FundamentalContext.Shareholder
func printShareholder(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Major shareholders %s", symbol))
	l, err := fc.Shareholder(c, symbol)
	if err != nil {
		return fmt.Errorf("shareholders for %s: %w", symbol, err)
	}
	fmt.Printf("total=%d\n", l.Total)
	if len(l.ShareholderList) == 0 {
		fmt.Println("   (no shareholders returned)")
		return nil
	}
	fmt.Printf("%-14s %-30s %10s %16s %-12s %s\n",
		"HOLDER_ID", "NAME", "PERCENT", "SHARES_CHANGED", "TYPE", "REPORTED")
	for _, s := range l.ShareholderList {
		fmt.Printf("%-14s %-30s %10s %16s %-12s %s\n",
			cli.Truncate(cli.OrDash(s.ShareholderID), 14),
			cli.Truncate(cli.OrDash(s.ShareholderName), 30),
			cli.Dec4(s.PercentOfShares), cli.Dec(s.SharesChanged),
			cli.Truncate(cli.OrDash(s.InstitutionType), 12), cli.OrDash(s.ReportDate))
		for _, st := range s.Stocks {
			fmt.Printf("      also holds %-12s %-8s %-4s %s\n",
				cli.OrDash(st.Symbol), cli.OrDash(st.Code), cli.OrDash(st.Market), cli.OrDash(st.Chg))
		}
	}
	fmt.Println("\n(pass -action shareholder-detail -object-id <HOLDER_ID> for one holder)")
	return nil
}

// printShareholderTop -> FundamentalContext.ShareholderTop
func printShareholderTop(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Top shareholders (raw) %s", symbol))
	r, err := fc.ShareholderTop(c, symbol)
	if err != nil {
		return fmt.Errorf("top shareholders for %s: %w", symbol, err)
	}
	// The SDK returns this as raw JSON; there is no documented struct for it.
	fmt.Println(raw(r.Data))
	return nil
}

// printShareholderDetail -> FundamentalContext.ShareholderDetail
func printShareholderDetail(ctx context.Context, fc *fundamental.FundamentalContext) error {
	if objectID == 0 {
		return fmt.Errorf("-action shareholder-detail needs -object-id; " +
			"run -action shareholder and copy a HOLDER_ID")
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Shareholder holdings %s (object_id=%d)", symbol, objectID))
	r, err := fc.ShareholderDetail(c, symbol, objectID)
	if err != nil {
		return fmt.Errorf("shareholder detail for %s: %w", symbol, err)
	}
	fmt.Println(raw(r.Data))
	return nil
}

// printFundHolder -> FundamentalContext.FundHolder
func printFundHolder(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Funds holding %s", symbol))
	h, err := fc.FundHolder(c, symbol)
	if err != nil {
		return fmt.Errorf("fund holders for %s: %w", symbol, err)
	}
	if len(h.Lists) == 0 {
		fmt.Println("   (no fund holders returned)")
		return nil
	}
	// PositionRatio is a non-pointer decimal.Decimal here, so "-" is not
	// possible: absent parses as zero upstream.
	fmt.Printf("%-10s %-12s %-5s %-34s %10s %-12s\n",
		"CODE", "SYMBOL", "CCY", "NAME", "POSITION", "REPORT_DATE")
	for _, f := range h.Lists {
		fmt.Printf("%-10s %-12s %-5s %-34s %10s %-12s\n",
			cli.OrDash(f.Code), cli.Truncate(cli.OrDash(f.Symbol), 12), cli.OrDash(f.Currency),
			cli.Truncate(cli.OrDash(f.Name), 34), f.PositionRatio.StringFixed(4),
			cli.OrDash(f.ReportDate))
	}
	return nil
}

// printInvestRelation -> FundamentalContext.InvestRelation
//
// Note the direction: this lists what the QUERIED company holds in OTHERS.
func printInvestRelation(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Investment holdings OF %s", symbol))
	r, err := fc.InvestRelation(c, symbol)
	if err != nil {
		return fmt.Errorf("invest relations for %s: %w", symbol, err)
	}
	if len(r.InvestSecurities) == 0 {
		fmt.Println("   (no investment holdings returned)")
		return nil
	}
	fmt.Printf("%-12s %-30s %-5s %10s %6s %16s\n",
		"SYMBOL", "COMPANY", "CCY", "PERCENT", "RANK", "VALUE")
	for _, s := range r.InvestSecurities {
		fmt.Printf("%-12s %-30s %-5s %10s %6s %16s\n",
			cli.OrDash(s.Symbol), cli.Truncate(cli.OrDash(s.CompanyName), 30),
			cli.OrDash(s.Currency), cli.Dec4(s.PercentOfShares),
			cli.OrDash(s.SharesRank), cli.Dec(s.SharesValue))
	}
	return nil
}

// ------------------------------------------- ETF

// printEtfAllocation -> FundamentalContext.EtfAssetAllocation
//
// This is the one fundamental method that resolves the symbol through the
// directory-aware counter package, so an ETF like 3067.HK maps correctly;
// a stock symbol will simply return nothing useful.
func printEtfAllocation(ctx context.Context, fc *fundamental.FundamentalContext) error {
	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("ETF asset allocation %s", symbol))
	r, err := fc.EtfAssetAllocation(c, symbol)
	if err != nil {
		return fmt.Errorf("ETF asset allocation for %s: %w", symbol, err)
	}
	if len(r.Info) == 0 {
		fmt.Println("   (no allocation groups returned; is this symbol an ETF?)")
		return nil
	}
	for _, g := range r.Info {
		// ElementType is a bare int32 with no String() method, so print the
		// mapped name and the raw code; %s on the code itself is a vet error.
		fmt.Printf("\n%s (code %d) as of %s\n",
			parseElementType(g.AssetType), int(g.AssetType), cli.OrDash(g.ReportDate))
		fmt.Printf("  %-12s %-30s %10s %s\n", "SYMBOL", "NAME", "RATIO", "CODE")
		for _, it := range g.Lists {
			fmt.Printf("  %-12s %-30s %10s %s\n",
				cli.OrDash(it.Symbol), cli.Truncate(cli.OrDash(it.Name), 30),
				cli.OrDash(it.PositionRatio), cli.OrDash(it.Code))
			if d := it.HoldingDetail; d != nil {
				fmt.Printf("      industry=%s index=%s type=%s (%s)\n",
					cli.OrDash(d.IndustryName), cli.OrDash(d.IndexName),
					cli.OrDash(d.HoldingType), cli.OrDash(d.HoldingTypeName))
			}
		}
	}
	return nil
}

// ------------------------------------------- industry

// printIndustryRank -> FundamentalContext.IndustryRank
//
// Indicator and sort type are bare strings in the SDK ("0".."7", "0"/"1")
// with no name mapping, so -indicator is numeric on purpose: inventing names
// for undocumented codes would be worse than showing the code. Both are parsed
// here as well as at startup, because the parsed constant is what the request
// carries — a code the SDK does not define would otherwise go out verbatim and
// come back as an empty ranking that reads as "no industries ranked".
func printIndustryRank(ctx context.Context, fc *fundamental.FundamentalContext) error {
	ind, err := parseIndustryRankIndicator(indicators)
	if err != nil {
		return err
	}
	st, err := parseIndustryRankSort(sortType)
	if err != nil {
		return err
	}
	// IndustryRank is market-wide rather than per symbol, so it takes a market
	// code instead of a security symbol.
	mkt, err := marketFromSymbol(symbol)
	if err != nil {
		return err
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Industry rank (market=%s indicator=%s sort=%s limit=%d)",
		mkt, ind, st, limit))
	r, err := fc.IndustryRank(c, mkt, ind, st, limit)
	if err != nil {
		return fmt.Errorf("industry rank for %s: %w", mkt, err)
	}
	printed := 0
	for _, g := range r.Items {
		for _, it := range g.Lists {
			fmt.Printf("%-30s %-18s %-9s %-26s %-10s %-12s %s\n",
				cli.Truncate(cli.OrDash(it.Name), 30), cli.OrDash(it.CounterID), cli.OrDash(it.Chg),
				cli.Truncate(cli.OrDash(it.LeadingName), 26), cli.OrDash(it.LeadingTicker),
				cli.OrDash(it.LeadingChg), cli.OrDash(it.ValueName+" "+it.ValueData))
			printed++
		}
	}
	if printed == 0 {
		fmt.Println("   (no ranked industries returned)")
	}
	return nil
}

func parseIndustryRankIndicator(s string) (fundamental.IndustryRankIndicator, error) {
	// The SDK exposes these as string constants, so map the digits explicitly
	// rather than casting: an out-of-range value would silently return nothing.
	// Whitespace is trimmed first so a shell-quoted " 1 " is the code 1, but the
	// comparison stays against the exact single digit, so "00", "1.0" and "1e0"
	// are still rejected: only the documented code goes on the wire.
	s = strings.TrimSpace(s)
	for i := 0; i <= 7; i++ {
		if s == strconv.Itoa(i) {
			return fundamental.IndustryRankIndicator(s), nil
		}
	}
	return "", fmt.Errorf("unknown -indicator %q: want 0 through 7 "+
		"(the SDK defines no names for these codes)", s)
}

// parseIndustryRankSort needs no sentinel, unlike the int-enum parsers in this
// package: fundamental.IndustryRankSortType is a *string* type whose only members
// are "0" and "1", so the zero value "" is already a value the API cannot be
// sent. The error path returns that, and a caller that ignored the error would
// send an empty sort_type rather than a valid-looking wrong one. Pinned by
// TestParseIndustryRankSort_TheErrorValueIsNotASortType.
func parseIndustryRankSort(s string) (fundamental.IndustryRankSortType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "asc", "ascending":
		return fundamental.IndustryRankSortTypeAscending, nil
	case "1", "desc", "descending":
		return fundamental.IndustryRankSortTypeDescending, nil
	}
	return "", fmt.Errorf("unknown -sort-type %q: want 0 (ascending) or 1 (descending)", s)
}

// marketFromSymbol derives the market code from a symbol's suffix, because
// IndustryRank and IndustryPeers take a market rather than a security.
//
// The accepted spellings are the SDK's own openapi.Market constants — the same
// five every other command in this repo accepts for -market — so a suffix that
// is not one of them is refused here rather than forwarded to a market-wide
// endpoint, which would answer "no data" and read as an empty market. The
// no-suffix case keeps its documented HK fallback.
func marketFromSymbol(sym string) (string, error) {
	if strings.LastIndex(sym, ".") < 0 {
		return string(openapi.MarketHK), nil
	}
	suffix := symbolSuffix(sym)
	switch openapi.Market(suffix) {
	case openapi.MarketHK, openapi.MarketUS, openapi.MarketCN, openapi.MarketSG, openapi.MarketUK:
		return suffix, nil
	}
	return "", fmt.Errorf("cannot read a market from -symbol %q: %q is not a market code, "+
		"want HK, US, CN, SG or UK (a symbol with no suffix means HK)", sym, suffix)
}

// symbolSuffix returns the upper-cased, trimmed part after the last dot, or ""
// when the symbol carries no suffix. It is the raw suffix, deliberately not
// validated: macro-indicators uses it to look up a country code, where an
// unrecognised one means "no filter" instead of a wrong request.
func symbolSuffix(sym string) string {
	i := strings.LastIndex(sym, ".")
	if i < 0 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(sym[i+1:]))
}

// printIndustryPeers -> FundamentalContext.IndustryPeers
func printIndustryPeers(ctx context.Context, fc *fundamental.FundamentalContext) error {
	mkt, err := marketFromSymbol(symbol)
	if err != nil {
		return err
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Industry peer chain %s", symbol))
	// industry_id is optional: passing "" lets the API infer it from counter_id.
	r, err := fc.IndustryPeers(c, symbol, mkt, "")
	if err != nil {
		return fmt.Errorf("industry peers for %s: %w", symbol, err)
	}
	fmt.Printf("industry=%s market=%s\n", cli.OrDash(r.Top.Name), cli.OrDash(r.Top.Market))
	if r.Chain == nil {
		fmt.Println("   (no peer chain returned)")
		return nil
	}
	printPeerNode(r.Chain, 1)
	return nil
}

func printPeerNode(n *fundamental.IndustryPeerNode, depth int) {
	if n == nil || depth > 8 {
		return // the tree is recursive; bound the depth so a cycle cannot hang
	}
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s%-30s %-16s %6d %-9s %s\n",
		indent, cli.Truncate(cli.OrDash(n.Name), 30), cli.OrDash(n.CounterID),
		n.StockNum, cli.OrDash(n.Chg), cli.OrDash(n.YtdChg))
	for i := range n.Next {
		printPeerNode(&n.Next[i], depth+1)
	}
}

// ------------------------------------------- macro

// printMacroIndicators -> FundamentalContext.MacroeconomicIndicators
//
// The SDK's country type is a bare string with no parser, so -symbol's
// suffix doubles as the country filter when it is one of the known codes.
func printMacroIndicators(ctx context.Context, fc *fundamental.FundamentalContext) error {
	country := macroCountry(symbol)
	var keyword *string
	if k := strings.TrimSpace(peers); k != "" {
		keyword = &k
	}
	off := int32(macroOffset)
	lim := int32(limit)

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Macroeconomic indicators (country=%s keyword=%s offset=%d limit=%d)",
		orNone(derefString(country)), orNone(derefString(keyword)), off, lim))
	r, err := fc.MacroeconomicIndicators(c, country, keyword, &off, &lim)
	if err != nil {
		return fmt.Errorf("macroeconomic indicators: %w", err)
	}
	fmt.Printf("count=%d\n", r.Count)
	if len(r.Data) == 0 {
		fmt.Println("   (no indicators matched)")
		fmt.Println("\n(pass -peers <text> as a fuzzy name filter, or -symbol 700.US)")
		return nil
	}
	fmt.Printf("%-10s %-4s %-30s %-14s %-9s %s\n",
		"CODE", "CTRY", "NAME", "PERIODICITY", "IMPORT.", "DESCRIPTION")
	for _, m := range r.Data {
		fmt.Printf("%-10s %-4s %-30s %-14s %-9s %s\n",
			cli.OrDash(m.IndicatorCode), cli.OrDash(m.Country), cli.Truncate(cli.OrDash(m.Name), 30),
			cli.Truncate(cli.OrDash(m.Periodicity), 14),
			parseImportance(fundamental.MacroeconomicImportance(m.Importance)),
			cli.Truncate(cli.OrDash(m.Describe), 50))
	}
	fmt.Println("\n(pass -action macro -indicator-code <CODE> for the series)")
	return nil
}

var macroCountries = map[string]fundamental.MacroeconomicCountry{
	"HK": fundamental.MacroeconomicCountryHK,
	"CN": fundamental.MacroeconomicCountryCN,
	"US": fundamental.MacroeconomicCountryUS,
	"EU": fundamental.MacroeconomicCountryEU,
	"JP": fundamental.MacroeconomicCountryJP,
	"SG": fundamental.MacroeconomicCountrySG,
}

// macroCountry resolves -symbol's suffix to a macro country filter, or nil for
// "no filter". It is deliberately tolerant where marketFromSymbol is strict: a
// country is a different vocabulary from a market code (it has EU and JP, no UK)
// and this one only narrows a list, so an unrecognised suffix such as the SH of
// 000001.SH or a typo yields no filter instead of refusing the action.
func macroCountry(sym string) *fundamental.MacroeconomicCountry {
	if mc, ok := macroCountries[symbolSuffix(sym)]; ok {
		return &mc
	}
	return nil
}

// printMacro -> FundamentalContext.Macroeconomic
func printMacro(ctx context.Context, fc *fundamental.FundamentalContext) error {
	if strings.TrimSpace(macroCode) == "" {
		return fmt.Errorf("-action macro needs -indicator-code; " +
			"run -action macro-indicators and copy a CODE")
	}
	var start, end *string
	if macroStart != "" {
		start = &macroStart
	}
	if macroEnd != "" {
		end = &macroEnd
	}
	off := int32(macroOffset)
	lim := int32(limit)

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Macroeconomic series %s (offset=%d limit=%d)", macroCode, off, lim))
	r, err := fc.Macroeconomic(c, macroCode, start, end, &off, &lim)
	if err != nil {
		return fmt.Errorf("macroeconomic %s: %w", macroCode, err)
	}
	i := r.Info
	fmt.Printf("%s  country=%s  periodicity=%s  importance=%s\n",
		cli.OrDash(i.Name), cli.OrDash(i.Country), cli.OrDash(i.Periodicity),
		parseImportance(fundamental.MacroeconomicImportance(i.Importance)))
	// The unit is stamped on every data point, not on the metadata header, so
	// only show it when a point exists — an empty series has no unit to read.
	unit := ""
	if len(r.Data) > 0 {
		unit = r.Data[0].Unit
	}
	fmt.Printf("unit=%s  total points=%d\n", orNone(unit), r.Count)

	if len(r.Data) == 0 {
		fmt.Println("   (no data points returned)")
		return nil
	}
	// Newest first: the SDK hard-codes sort=desc for this endpoint.
	fmt.Printf("%-12s %18s %18s %18s %-20s\n", "PERIOD", "ACTUAL", "PREVIOUS", "FORECAST", "RELEASED")
	for _, d := range r.Data {
		fmt.Printf("%-12s %18s %18s %18s %-20s\n",
			cli.OrDash(d.Period), cli.OrDash(d.ActualValue), cli.OrDash(d.PreviousValue),
			cli.OrDash(d.ForecastValue), fmtTimePtr(d.ReleaseAt))
	}
	return nil
}

// ------------------------------------------- asset

// printStatements -> AssetContext.Statements
//
// These are account statements, so they are personal to the account whose
// token is configured — not market data.
func printStatements(ctx context.Context, ac *asset.AssetContext) error {
	ty, err := parseStatementType(statementTy)
	if err != nil {
		return err
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Account statements (%s page=%d size=%d)", statementTy, page, pageSize))
	items, err := ac.Statements(c, &asset.GetStatementList{
		StatementType: ty,
		Page:          int32(page),
		PageSize:      int32(pageSize),
	})
	if err != nil {
		return fmt.Errorf("account statements: %w", err)
	}
	if len(items) == 0 {
		fmt.Println("   (no statements returned)")
		return nil
	}
	fmt.Printf("%-12s %s\n", "DATE", "FILE_KEY")
	for _, it := range items {
		// Date is an int32 like 20250301, not a formatted string.
		fmt.Printf("%-12d %s\n", it.Date, cli.OrDash(it.FileKey))
	}
	fmt.Println("\n(pass -action statement-url -file-key <FILE_KEY> for the download link)")
	return nil
}

// parseStatementType maps -statement-type onto the SDK's statement type.
//
// This is the only place the daily/monthly vocabulary is written down: startup
// validation calls it and printStatements calls it, so a padded "-statement-type
// '  monthly  '" cannot be accepted by one and read as daily by the other.
func parseStatementType(s string) (asset.StatementType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "daily":
		return asset.StatementTypeDaily, nil
	case "monthly":
		return asset.StatementTypeMonthly, nil
	}
	return 0, fmt.Errorf("unknown -statement-type %q: want daily or monthly", s)
}

// printStatementURL -> AssetContext.StatementDownloadURL
//
// This returns a presigned URL. It is printed but never fetched: downloading
// is outside what this read-only demo does, and the URL embeds a signature.
func printStatementURL(ctx context.Context, ac *asset.AssetContext) error {
	if strings.TrimSpace(fileKey) == "" {
		return fmt.Errorf("-action statement-url needs -file-key; " +
			"run -action statements and copy a FILE_KEY")
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Statement download URL for %s", fileKey))
	url, err := ac.StatementDownloadURL(c, &asset.GetStatementDownloadURL{FileKey: fileKey})
	if err != nil {
		return fmt.Errorf("statement download URL: %w", err)
	}
	fmt.Println(url)
	fmt.Println("\n(this is a presigned URL; this demo prints it and does not download it)")
	return nil
}

// ------------------------------------------- calendar

// printCalendar -> CalendarContext.FinanceCalendar
func printCalendar(ctx context.Context, cc *calendar.CalendarContext) error {
	start, end, err := calendarWindow()
	if err != nil {
		return err
	}
	cat, err := parseCalendarCategory(calCategory)
	if err != nil {
		return err
	}
	var mkt *string
	if m := strings.TrimSpace(calMarket); m != "" {
		mkt = &m
	}

	c, cancel := withTimeout(ctx)
	defer cancel()

	cli.Section(fmt.Sprintf("Financial calendar %s (%s %s..%s market=%s)",
		cat, orNone(derefString(mkt)), start, end, calMarket))
	r, err := cc.FinanceCalendar(c, cat, start, end, mkt)
	if err != nil {
		return fmt.Errorf("finance calendar: %w", err)
	}
	if r.NextDate != "" {
		fmt.Printf("window %s .. %s; more pages start at %s\n", r.Date, end, r.NextDate)
	} else {
		fmt.Printf("window %s .. %s\n", r.Date, end)
	}

	events := 0
	for _, g := range r.List {
		if len(g.Infos) == 0 {
			continue
		}
		fmt.Printf("\n%s (%d)\n", g.Date, g.Count)
		fmt.Printf("  %-12s %-22s %-8s %-34s %s\n", "SYMBOL", "NAME", "SESSION", "CONTENT", "STARS")
		for _, e := range g.Infos {
			fmt.Printf("  %-12s %-22s %-8s %-34s %d\n",
				cli.Truncate(cli.OrDash(e.Symbol), 12), cli.Truncate(cli.OrDash(e.CounterName), 22),
				cli.Truncate(cli.OrDash(e.DateType), 8), cli.Truncate(cli.OrDash(e.Content), 34), e.Star)
			for _, kv := range e.DataKV {
				fmt.Printf("        %-18s %-16s raw=%s\n",
					cli.Truncate(cli.OrDash(kv.Key), 18), cli.Truncate(cli.OrDash(kv.Value), 16),
					cli.Dec4(kv.ValueRaw))
			}
		}
		events += len(g.Infos)
	}
	if events == 0 {
		fmt.Println("   (no events in this window)")
		fmt.Println("\n(widen it with -calendar-start / -calendar-end, or try another -calendar-category)")
	}
	return nil
}

// calendarWindow resolves the requested window, defaulting to the next 30 days
// from today. An end before the start is rejected rather than silently swapped.
func calendarWindow() (string, string, error) {
	start := strings.TrimSpace(calStart)
	end := strings.TrimSpace(calEnd)
	if start == "" {
		start = time.Now().Format("2006-01-02")
	}
	if end == "" {
		t, err := time.Parse("2006-01-02", start)
		if err != nil {
			return "", "", fmt.Errorf("cannot parse -calendar-start %q: want YYYY-MM-DD", start)
		}
		end = t.AddDate(0, 0, 30).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", start); err != nil {
		return "", "", fmt.Errorf("cannot parse -calendar-start %q: want YYYY-MM-DD", start)
	}
	if _, err := time.Parse("2006-01-02", end); err != nil {
		return "", "", fmt.Errorf("cannot parse -calendar-end %q: want YYYY-MM-DD", end)
	}
	if end < start {
		return "", "", fmt.Errorf("-calendar-end %q is before -calendar-start %q", end, start)
	}
	return start, end, nil
}

// calendarCategoryInvalid is what parseCalendarCategory returns alongside its
// error.
//
// The zero value was the wrong choice here: calendar.CalendarCategory is a bare
// int enum whose first member is CalendarCategoryReport, so `return 0, err`
// handed back a *valid* category — and the documented default besides. A caller
// that forgot to check the error would quietly list earnings reports instead of
// erroring, and the heading it printed would name the category rather than the
// word the user typed. printCalendar checks the error today; this is about the
// next caller.
//
// -1 is outside the enum: the SDK declares eight members with iota and none
// negative (calendar/types.go, v0.25.2), so the valid range is 0..7.
// TestParseCalendarCategory_AgreesWithTheSDKWireStrings already pins that
// numbering, and TestParseCalendarCategory_TheErrorValueIsNotACategory asserts
// the membership property itself, so an SDK that ever added a negative member
// would fail there rather than quietly making this sentinel valid.
const calendarCategoryInvalid calendar.CalendarCategory = -1

func parseCalendarCategory(s string) (calendar.CalendarCategory, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "report":
		return calendar.CalendarCategoryReport, nil
	case "dividend":
		return calendar.CalendarCategoryDividend, nil
	case "split":
		return calendar.CalendarCategorySplit, nil
	case "ipo":
		return calendar.CalendarCategoryIpo, nil
	case "macrodata":
		return calendar.CalendarCategoryMacroData, nil
	case "closed":
		return calendar.CalendarCategoryClosed, nil
	case "meeting":
		return calendar.CalendarCategoryMeeting, nil
	case "merge":
		return calendar.CalendarCategoryMerge, nil
	}
	return calendarCategoryInvalid, fmt.Errorf("unknown -calendar-category %q: want report, dividend, split, ipo, "+
		"macrodata, closed, meeting or merge", s)
}

// ------------------------------------------------------------------ output

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(server default)"
	}
	return s
}

// derefString renders an optional string-ish value for display. The SDK models
// several "no filter" arguments as a typed pointer, so this keeps the printing
// side from having to branch at every call site.
func derefString[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
