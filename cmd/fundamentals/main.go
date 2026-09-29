// Command fundamentals reads company fundamentals, analyst and consensus
// data, valuations, ownership, corporate actions, macroeconomic series,
// account statement links and the financial calendar.
//
// It is entirely READ-ONLY. Every one of the 32 FundamentalContext methods,
// both AssetContext methods and the CalendarContext method it calls issues a
// plain GET; none of them mutates account or market state. That was checked
// against the v0.25.2 source (no Post/Put/Patch/Delete anywhere in the three
// packages), not assumed. There is no code path here that can place, modify
// or cancel an order, and the startup banner asserts the order gate is still
// closed.
//
// The five contexts it does NOT cover — AlertContext, SharelistContext,
// ContentContext, ScreenerContext and PortfolioContext — are all covered
// elsewhere in this repo: cmd/alert, cmd/sharelist, cmd/content, cmd/screener
// and cmd/portfolio respectively. ScreenerContext was the last one, and its five
// methods are all reads, so its absence here was never a safety decision.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/asset"
	"github.com/longbridge/openapi-go/calendar"
	"github.com/longbridge/openapi-go/fundamental"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// rawLimit caps how much of a raw-JSON field is echoed. Several fundamental
// responses are deliberately untyped in the SDK (json.RawMessage) because the
// payload shape varies; printing megabytes of it is not useful, but printing
// *something* is, and a truncation marker keeps a clipped block from being
// mistaken for a complete one.
const rawLimit = 4000

var (
	action      string
	symbol      string
	kind        string
	period      string
	peers       string
	currency    string
	report      string
	fiscalYear  int
	fiscalPer   string
	cate        string
	indicators  string
	sortType    string
	limit       int
	objectID    int64
	fileKey     string
	page        int
	pageSize    int
	statementTy string
	calCategory string
	calStart    string
	calEnd      string
	calMarket   string
	macroCode   string
	macroStart  string
	macroEnd    string
	macroOffset int
	timeout     time.Duration
)

func main() {
	u := cli.NewUsage("fundamentals", "read-only company fundamentals, asset statements and financial calendar")
	u.FS.StringVar(&action, "action", "valuation",
		"see the -action list in -h output; one SDK method per action, plus `all-symbol` to run the main per-symbol set")
	u.FS.StringVar(&symbol, "symbol", "700.HK", "primary symbol (700.HK, AAPL.US, 000001.SH, 3067.HK)")
	u.FS.StringVar(&kind, "kind", "all", "for -action report: is, bs, cf or all")
	u.FS.StringVar(&period, "period", "", "for -action report: af, saf, q1, q2, q3, qf, 3q (empty = server default)")
	u.FS.StringVar(&peers, "peers", "MSFT.US,GOOGL.US", "peer symbols for -action valuation-compare")
	u.FS.StringVar(&currency, "currency", "USD", "reporting currency for -action valuation-compare")
	u.FS.StringVar(&report, "report", "", "report code passed through to -action segments-history and snapshot")
	u.FS.IntVar(&fiscalYear, "fiscal-year", 0, "fiscal year for -action snapshot (0 = omit)")
	u.FS.StringVar(&fiscalPer, "fiscal-period", "", "fiscal period for -action snapshot, e.g. Q4")
	u.FS.StringVar(&cate, "cate", "", "category filter for -action segments-history")
	u.FS.StringVar(&indicators, "indicator", "0", "for -action industry-rank: 0-7 (numeric only; the SDK defines no names)")
	u.FS.StringVar(&sortType, "sort-type", "1", "for -action industry-rank: 0 ascending, 1 descending")
	u.FS.IntVar(&limit, "limit", 20, "row cap for industry-rank, macro-indicators and macro")
	u.FS.Int64Var(&objectID, "object-id", 0, "shareholder id for -action shareholder-detail (from -action shareholder)")
	u.FS.StringVar(&fileKey, "file-key", "", "file key for -action statement-url (from -action statements)")
	u.FS.IntVar(&page, "page", 1, "page number for -action statements")
	u.FS.IntVar(&pageSize, "page-size", 20, "page size for -action statements")
	u.FS.StringVar(&statementTy, "statement-type", "daily", "for -action statements: daily or monthly")
	u.FS.StringVar(&calCategory, "calendar-category", "report",
		"for -action calendar: report, dividend, split, ipo, macrodata, closed, meeting, merge")
	u.FS.StringVar(&calStart, "calendar-start", "", "calendar window start, YYYY-MM-DD (default: today)")
	u.FS.StringVar(&calEnd, "calendar-end", "", "calendar window end, YYYY-MM-DD (default: start + 30d)")
	u.FS.StringVar(&calMarket, "calendar-market", "", "optional market filter for -action calendar, e.g. HK")
	u.FS.StringVar(&macroCode, "indicator-code", "", "indicator code for -action macro (from -action macro-indicators)")
	u.FS.StringVar(&macroStart, "macro-start", "", "start date for -action macro, YYYY-MM-DD")
	u.FS.StringVar(&macroEnd, "macro-end", "", "end date for -action macro, YYYY-MM-DD")
	u.FS.IntVar(&macroOffset, "macro-offset", 0, "offset for -action macro and -action macro-indicators")
	u.FS.DurationVar(&timeout, "timeout", 15*time.Second, "per-request timeout")
	u.Parse(os.Args[1:])

	cfg := u.Load()
	timeout = appcfg.Timeout()
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: this binary is read-only; if the order gate were open something
	// is wrong with the environment and we should not keep going.
	if err := cfg.GuardWrite("run the fundamentals reader"); err == nil {
		cli.Fail(fmt.Errorf("internal invariant violated: fundamentals is read-only but the order gate is open"))
	}

	if err := validateFlags(); err != nil {
		cli.Fail(err)
	}

	cli.Run(func(ctx context.Context) error {
		fc, err := fundamental.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating fundamental context: %w", err)
		}
		ac, err := asset.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating asset context: %w", err)
		}
		cc, err := calendar.NewFromCfg(cfg.SDK)
		if err != nil {
			return fmt.Errorf("creating calendar context: %w", err)
		}
		return dispatch(ctx, fc, ac, cc)
	})
}

// ---------------------------------------------------------------- validation

// validateFlags rejects the inputs that can be checked before any network call,
// so an obvious typo costs nothing.
func validateFlags() error {
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("-symbol is empty")
	}
	if limit <= 0 {
		return fmt.Errorf("-limit must be greater than 0, got %d", limit)
	}
	if page <= 0 {
		return fmt.Errorf("-page must be greater than 0, got %d", page)
	}
	if pageSize <= 0 {
		return fmt.Errorf("-page-size must be greater than 0, got %d", pageSize)
	}
	if macroOffset < 0 {
		return fmt.Errorf("-macro-offset must not be negative, got %d", macroOffset)
	}
	switch strings.ToLower(statementTy) {
	case "daily":
	case "monthly":
	default:
		return fmt.Errorf("unknown -statement-type %q: want daily or monthly", statementTy)
	}
	switch strings.ToLower(calCategory) {
	case "report", "dividend", "split", "ipo", "macrodata", "closed", "meeting", "merge":
	default:
		return fmt.Errorf("unknown -calendar-category %q: want report, dividend, split, ipo, "+
			"macrodata, closed, meeting or merge", calCategory)
	}
	switch strings.ToLower(sortType) {
	case "0", "asc", "ascending":
	case "1", "desc", "descending":
	default:
		return fmt.Errorf("unknown -sort-type %q: want 0 (ascending) or 1 (descending)", sortType)
	}
	return nil
}

// ------------------------------------------------------------------ dispatch

// dispatch routes one -action to exactly one SDK method. Keeping the mapping in
// a single switch is what makes the coverage table in the README checkable by
// grep: every method name appears here exactly once as a selector.
func dispatch(ctx context.Context, fc *fundamental.FundamentalContext, ac *asset.AssetContext, cc *calendar.CalendarContext) error {
	switch strings.ToLower(strings.ReplaceAll(action, "_", "-")) {
	// ---- fundamental: statements, ratings and estimates
	case "report":
		return printFinancialReport(ctx, fc)
	case "rating":
		return printInstitutionRating(ctx, fc)
	case "rating-detail":
		return printInstitutionRatingDetail(ctx, fc)
	case "rating-views":
		return printInstitutionRatingViews(ctx, fc)
	case "forecast-eps":
		return printForecastEps(ctx, fc)
	case "consensus":
		return printConsensus(ctx, fc)
	case "snapshot":
		return printSnapshot(ctx, fc)
	case "operating":
		return printOperating(ctx, fc)

	// ---- fundamental: dividends and corporate actions
	case "dividend":
		return printDividend(ctx, fc, "dividend")
	case "dividend-detail":
		return printDividend(ctx, fc, "dividend-detail")
	case "corp-action":
		return printCorpAction(ctx, fc)
	case "buyback":
		return printBuyback(ctx, fc)

	// ---- fundamental: valuation
	case "valuation":
		return printValuation(ctx, fc)
	case "valuation-history":
		return printValuationHistory(ctx, fc)
	case "industry-valuation":
		return printIndustryValuation(ctx, fc)
	case "industry-valuation-dist":
		return printIndustryValuationDist(ctx, fc)
	case "valuation-compare":
		return printValuationComparison(ctx, fc)

	// ---- fundamental: company profile and people
	case "company":
		return printCompany(ctx, fc)
	case "executive":
		return printExecutive(ctx, fc)
	case "segments":
		return printSegments(ctx, fc)
	case "segments-history":
		return printSegmentsHistory(ctx, fc)
	case "ratings":
		return printRatings(ctx, fc)

	// ---- fundamental: ownership
	case "shareholder":
		return printShareholder(ctx, fc)
	case "shareholder-top":
		return printShareholderTop(ctx, fc)
	case "shareholder-detail":
		return printShareholderDetail(ctx, fc)
	case "fund-holder":
		return printFundHolder(ctx, fc)
	case "invest-relation":
		return printInvestRelation(ctx, fc)

	// ---- fundamental: ETF
	case "etf-allocation":
		return printEtfAllocation(ctx, fc)

	// ---- fundamental: industry
	case "industry-rank":
		return printIndustryRank(ctx, fc)
	case "industry-peers":
		return printIndustryPeers(ctx, fc)

	// ---- fundamental: macro
	case "macro-indicators":
		return printMacroIndicators(ctx, fc)
	case "macro":
		return printMacro(ctx, fc)

	// ---- asset
	case "statements":
		return printStatements(ctx, ac)
	case "statement-url":
		return printStatementURL(ctx, ac)

	// ---- calendar
	case "calendar":
		return printCalendar(ctx, cc)

	// ---- convenience
	case "all-symbol":
		return runAllSymbol(ctx, fc)

	default:
		return fmt.Errorf("unknown -action %q; run with -h for the list", action)
	}
}

// runAllSymbol runs the main per-symbol set in one go. It is a convenience
// wrapper over the same actions above, not a distinct SDK surface.
func runAllSymbol(ctx context.Context, fc *fundamental.FundamentalContext) error {
	steps := []struct {
		act string
		fn  func(context.Context, *fundamental.FundamentalContext) error
	}{
		{"company", printCompany},
		{"valuation", printValuation},
		{"rating", printInstitutionRating},
		{"consensus", printConsensus},
		{"dividend", func(c context.Context, f *fundamental.FundamentalContext) error {
			return printDividend(c, f, "dividend")
		}},
		{"corp-action", printCorpAction},
		{"operating", printOperating},
		{"ratings", printRatings},
	}
	for _, s := range steps {
		cli.Section(fmt.Sprintf("%s  (-action %s)", symbol, s.act))
		if err := s.fn(ctx, fc); err != nil {
			return fmt.Errorf("-action %s: %w", s.act, err)
		}
	}
	return nil
}

// ------------------------------------------------------------------ helpers

// withTimeout applies the demo-wide per-request timeout to one SDK call.
func withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

func parseKind(s string) (fundamental.FinancialReportKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "is", "income", "income_statement":
		return fundamental.FinancialReportKindIncomeStatement, nil
	case "bs", "balance", "balance_sheet":
		return fundamental.FinancialReportKindBalanceSheet, nil
	case "cf", "cash", "cash_flow":
		return fundamental.FinancialReportKindCashFlow, nil
	case "all":
		return fundamental.FinancialReportKindAll, nil
	}
	return 0, fmt.Errorf("unknown -kind %q: want is, bs, cf or all", s)
}

func parsePeriod(s string) (*fundamental.FinancialReportPeriod, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil // nil means "let the server decide"
	}
	var p fundamental.FinancialReportPeriod
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "af", "annual":
		p = fundamental.FinancialReportPeriodAnnual
	case "saf", "semi", "semi_annual":
		p = fundamental.FinancialReportPeriodSemiAnnual
	case "q1":
		p = fundamental.FinancialReportPeriodQ1
	case "q2":
		p = fundamental.FinancialReportPeriodQ2
	case "q3":
		p = fundamental.FinancialReportPeriodQ3
	case "qf", "full_quarter":
		p = fundamental.FinancialReportPeriodQuarterlyFull
	case "3q", "three_q":
		p = fundamental.FinancialReportPeriodThreeQ
	default:
		return nil, fmt.Errorf("unknown -period %q: want af, saf, q1, q2, q3, qf or 3q", s)
	}
	return &p, nil
}

func parseRecommend(r fundamental.InstitutionRecommend) string {
	switch r {
	case fundamental.InstitutionRecommendStrongBuy:
		return "strong_buy"
	case fundamental.InstitutionRecommendBuy:
		return "buy"
	case fundamental.InstitutionRecommendHold:
		return "hold"
	case fundamental.InstitutionRecommendSell:
		return "sell"
	case fundamental.InstitutionRecommendStrongSell:
		return "strong_sell"
	case fundamental.InstitutionRecommendUnderperform:
		return "underperform"
	case fundamental.InstitutionRecommendNoOpinion:
		return "no_opinion"
	}
	// InstitutionRecommend is a bare int with no String(); print the number
	// rather than a blank cell.
	return "unknown(" + strconv.Itoa(int(r)) + ")"
}

// parseElementType renders ElementType, the ETF allocation group kind.
func parseElementType(t fundamental.ElementType) string {
	switch t {
	case fundamental.ElementTypeHoldings:
		return "holdings"
	case fundamental.ElementTypeRegional:
		return "regional"
	case fundamental.ElementTypeAssetClass:
		return "asset_class"
	case fundamental.ElementTypeIndustry:
		return "industry"
	}
	return "unknown(" + strconv.Itoa(int(t)) + ")"
}

func parseImportance(i int32) string {
	switch i {
	case 1:
		return "low"
	case 2:
		return "medium"
	case 3:
		return "high"
	}
	return strconv.Itoa(int(i))
}

// raw pretty-prints an untyped SDK field, truncated with an explicit marker.
//
// Several fundamental responses are json.RawMessage in the SDK because the
// documented shape varies by symbol or market. Printing them verbatim is the
// honest option, but an unbounded dump is unreadable, so the clip is marked.
func raw(b json.RawMessage) string {
	if len(b) == 0 {
		return "-"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		// Not valid JSON (or empty): show the raw bytes, escaped.
		s := string(b)
		if len(s) > 400 {
			return s[:400] + "…(truncated, not indented)"
		}
		return s
	}
	s := buf.String()
	if len(s) > rawLimit {
		return s[:rawLimit] + fmt.Sprintf("…(truncated, %d bytes total)", len(s))
	}
	return s
}

func fmtTimePtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04:05")
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
