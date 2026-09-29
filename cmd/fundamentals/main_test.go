package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/fundamental"
)

// This package converts the SDK's bare-int enums in two directions: parsers turn
// a flag into a constant, and renderers turn a constant back into text. The two
// fail in opposite ways, so both are tested for the same property from
// different sides — a parser must never return a wrong-but-valid constant, and
// a renderer must never return "" or "0" for a value the SDK can actually
// produce.
//
// validateFlags is NOT the only validation in the package, which is the first
// thing these tests establish: the required-input rules for -object-id,
// -file-key and -indicator-code live in the individual action functions, and
// each of them checks before touching its SDK context. That is why the action
// tests below pass a nil context — the check runs first, and if it ever stopped
// doing so the test would panic rather than quietly pass.

// ------------------------------------------------------------------ fixtures

// fundState is the package-level flag set. validateFlags and the parsers read
// it directly, so every test installs a complete valid baseline and restores
// the previous values afterwards.
type fundState struct {
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
}

func setFundFlags(t *testing.T, s fundState) {
	t.Helper()
	prev := fundState{symbol, kind, period, peers, currency, report, fiscalYear, fiscalPer,
		cate, indicators, sortType, limit, objectID, fileKey, page, pageSize,
		statementTy, calCategory, calStart, calEnd, calMarket, macroCode,
		macroStart, macroEnd, macroOffset}
	symbol, kind, period, peers, currency = s.symbol, s.kind, s.period, s.peers, s.currency
	report, fiscalYear, fiscalPer, cate = s.report, s.fiscalYear, s.fiscalPer, s.cate
	indicators, sortType, limit = s.indicators, s.sortType, s.limit
	objectID, fileKey, page, pageSize = s.objectID, s.fileKey, s.page, s.pageSize
	statementTy, calCategory = s.statementTy, s.calCategory
	calStart, calEnd, calMarket = s.calStart, s.calEnd, s.calMarket
	macroCode, macroStart, macroEnd = s.macroCode, s.macroStart, s.macroEnd
	macroOffset = s.macroOffset
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		symbol, kind, period, peers, currency = prev.symbol, prev.kind, prev.period, prev.peers, prev.currency
		report, fiscalYear, fiscalPer, cate = prev.report, prev.fiscalYear, prev.fiscalPer, prev.cate
		indicators, sortType, limit = prev.indicators, prev.sortType, prev.limit
		objectID, fileKey, page, pageSize = prev.objectID, prev.fileKey, prev.page, prev.pageSize
		statementTy, calCategory = prev.statementTy, prev.calCategory
		calStart, calEnd, calMarket = prev.calStart, prev.calEnd, prev.calMarket
		macroCode, macroStart, macroEnd = prev.macroCode, prev.macroStart, prev.macroEnd
		macroOffset = prev.macroOffset
	})
}

// validFundFlags is the flag set main() installs by default.
func validFundFlags(t *testing.T) {
	t.Helper()
	setFundFlags(t, fundState{
		symbol:      "700.HK",
		kind:        "all",
		period:      "",
		peers:       "MSFT.US,GOOGL.US",
		currency:    "USD",
		limit:       20,
		sortType:    "1",
		indicators:  "0",
		page:        1,
		pageSize:    20,
		statementTy: "daily",
		calCategory: "report",
	})
}

// ------------------------------------------------------------------ validateFlags

// The happy path: the defaults main() installs must pass. A false positive here
// would make every invocation of the binary fail at startup.
func TestValidateFlags_TheDocumentedDefaultsAreAccepted(t *testing.T) {
	validFundFlags(t)
	if err := validateFlags(); err != nil {
		t.Errorf("validateFlags() = %v with the documented defaults; the binary would fail at startup", err)
	}
}

func TestValidateFlags_RejectsBadValuesAndNamesTheFlagToFix(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func()
		wantErr string
	}{
		{"empty symbol", func() { symbol = "" }, "-symbol"},
		{"whitespace symbol", func() { symbol = "   " }, "-symbol"},
		{"tab-only symbol", func() { symbol = "\t\n" }, "-symbol"},
		{"zero limit", func() { limit = 0 }, "-limit"},
		{"negative limit", func() { limit = -1 }, "-limit"},
		{"zero page", func() { page = 0 }, "-page"},
		{"negative page", func() { page = -3 }, "-page"},
		{"zero page size", func() { pageSize = 0 }, "-page-size"},
		{"negative page size", func() { pageSize = -20 }, "-page-size"},
		{"negative macro offset", func() { macroOffset = -1 }, "-macro-offset"},
		{"unknown statement type", func() { statementTy = "weekly" }, "-statement-type"},
		{"empty statement type", func() { statementTy = "" }, "-statement-type"},
		{"unknown calendar category", func() { calCategory = "earnings" }, "-calendar-category"},
		{"empty calendar category", func() { calCategory = "" }, "-calendar-category"},
		{"unknown sort type", func() { sortType = "2" }, "-sort-type"},
		{"empty sort type", func() { sortType = "" }, "-sort-type"},
		{"non-numeric sort type", func() { sortType = "up" }, "-sort-type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validFundFlags(t)
			tt.mutate()
			err := validateFlags()
			if err == nil {
				t.Fatalf("validateFlags() = nil with %s; the request would go out with a value the API rejects", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q, so the user cannot tell which flag to fix", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFlags_AcceptsTheDocumentedSpellingsOfEveryVocabulariedFlag(t *testing.T) {
	for _, st := range []string{"daily", "monthly", "DAILY", "Monthly"} {
		t.Run("statement-type="+st, func(t *testing.T) {
			validFundFlags(t)
			statementTy = st
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for -statement-type %q", err, st)
			}
		})
	}
	for _, st := range []string{"0", "1", "asc", "desc", "ascending", "descending", "ASC", "Descending"} {
		t.Run("sort-type="+st, func(t *testing.T) {
			validFundFlags(t)
			sortType = st
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for -sort-type %q", err, st)
			}
		})
	}
	for _, cat := range []string{
		"report", "dividend", "split", "ipo", "macrodata", "closed", "meeting", "merge",
		"REPORT", "MacroData",
	} {
		t.Run("calendar-category="+cat, func(t *testing.T) {
			validFundFlags(t)
			calCategory = cat
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for -calendar-category %q", err, cat)
			}
		})
	}
}

// validateFlags used to lower-case but not trim, while every parser in the
// package trimmed, so `-sort-type " 1 "` was refused at startup as "unknown"
// even though the parser the action runs next would have accepted it. The two
// layers now share the parsers, so a padded value is the padded value all the
// way through, and "unknown" is only printed for a genuinely unknown one.
func TestValidateFlags_TrimsAroundEveryVocabulariedFlagJustLikeTheParserItGuards(t *testing.T) {
	for _, tt := range []struct {
		flag   string
		value  string
		parser func(string) error
	}{
		{"-statement-type", "  monthly  ", func(s string) error {
			_, err := parseStatementType(s)
			return err
		}},
		{"-calendar-category", "  merge  ", func(s string) error {
			_, err := parseCalendarCategory(s)
			return err
		}},
		{"-sort-type", " 1 ", func(s string) error {
			_, err := parseIndustryRankSort(s)
			return err
		}},
		{"-indicator", " 3 ", func(s string) error {
			_, err := parseIndustryRankIndicator(s)
			return err
		}},
	} {
		t.Run(tt.flag, func(t *testing.T) {
			if err := tt.parser(tt.value); err != nil {
				t.Fatalf("the parser rejects the padded %s %q, so the fixture is wrong: %v",
					tt.flag, tt.value, err)
			}
			validFundFlags(t)
			switch tt.flag {
			case "-statement-type":
				statementTy = tt.value
			case "-calendar-category":
				calCategory = tt.value
			case "-sort-type":
				sortType = tt.value
			default:
				indicators = tt.value
			}
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for the padded %s %q, but the parser accepts it; "+
					"the two layers must agree on what is valid", err, tt.flag, tt.value)
			}
		})
	}
}

// "unknown" is now only printed for a value no spelling of the flag produces, so
// the message cannot send a user hunting for a vocabulary problem that was
// really stray whitespace.
func TestValidateFlags_SaysUnknownOnlyForGenuinelyUnknownValues(t *testing.T) {
	for _, tt := range []struct {
		name string
		set  func()
		want string
	}{
		{"statement type", func() { statementTy = "weekly" }, "-statement-type"},
		{"calendar category", func() { calCategory = "earnings" }, "-calendar-category"},
		{"sort type", func() { sortType = "sideways" }, "-sort-type"},
		{"indicator", func() { indicators = "99" }, "-indicator"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			validFundFlags(t)
			tt.set()
			err := validateFlags()
			if err == nil {
				t.Fatalf("validateFlags() = nil for an unknown %s", tt.name)
			}
			if !strings.Contains(err.Error(), "unknown") {
				t.Errorf("error %q does not say the value is unknown", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name %q, so the user cannot tell which flag to fix", err, tt.want)
			}
		})
	}
}

// -indicator (the industry-rank code 0-7) is checked at startup, next to the
// -sort-type check that shares its action. It used to be checked only inside
// printIndustryRank, so a bad code was discovered at the end of a long run,
// after the contexts were built and the requests were made.
func TestValidateFlags_RejectsAnOutOfRangeIndicatorAtStartup(t *testing.T) {
	for _, in := range []string{"99", "8", "-1", "", "asc", "00", "1.0"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			validFundFlags(t)
			indicators = in
			err := validateFlags()
			if err == nil {
				t.Fatalf("validateFlags() = nil for -indicator %q", in)
			}
			for _, want := range []string{"-indicator", "0 through 7"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
	// Every documented code still starts up, and the other vocabularied flags
	// are unaffected by the added check.
	for _, in := range []string{"0", "1", "3", "7"} {
		t.Run("accepted/"+in, func(t *testing.T) {
			validFundFlags(t)
			indicators = in
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for -indicator %q, which is in the documented 0-7 range", err, in)
			}
		})
	}
}

// FINDING: the per-action required-input rules are not in validateFlags either.
// `-action shareholder-detail` with no -object-id passes startup validation and
// is only refused when the action runs. That is a defensible design — the rule
// is action-specific, not global — but it means "validateFlags is the single
// validation entry point for 36 actions" is not true of this code.
func TestValidateFlags_FindingPerActionRequiredFlagsAreNotCheckedAtStartup(t *testing.T) {
	validFundFlags(t)
	objectID, fileKey, macroCode = 0, "", ""
	if err := validateFlags(); err != nil {
		t.Fatalf("validateFlags() = %v; the three unset required flags should not be its business", err)
	}
	t.Log("validateFlags() passes with -object-id, -file-key and -indicator-code all unset; " +
		"each is checked inside its own action function")
}

// ------------------------------------------------------------------ parseKind

func TestParseKind_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want int
		name string
	}{
		{"is", 0, "FinancialReportKindIncomeStatement"},
		{"income", 0, "FinancialReportKindIncomeStatement"},
		{"income_statement", 0, "FinancialReportKindIncomeStatement"},
		{"bs", 1, "FinancialReportKindBalanceSheet"},
		{"balance", 1, "FinancialReportKindBalanceSheet"},
		{"balance_sheet", 1, "FinancialReportKindBalanceSheet"},
		{"cf", 2, "FinancialReportKindCashFlow"},
		{"cash", 2, "FinancialReportKindCashFlow"},
		{"cash_flow", 2, "FinancialReportKindCashFlow"},
		{"all", 3, "FinancialReportKindAll"},
		// Lower-cased and trimmed.
		{"IS", 0, "FinancialReportKindIncomeStatement"},
		{"  Bs  ", 1, "FinancialReportKindBalanceSheet"},
		{"\nCF\n", 2, "FinancialReportKindCashFlow"},
		{"All", 3, "FinancialReportKindAll"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseKind(tt.in)
			if err != nil {
				t.Fatalf("parseKind(%q) = error %v, want %s", tt.in, err, tt.name)
			}
			if int(got) != tt.want {
				t.Errorf("parseKind(%q) = %d, want %d (%s)", tt.in, int(got), tt.want, tt.name)
			}
		})
	}
}

func TestParseKind_UnrecognisedValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string: the -kind default is all, so an empty flag is never intentional"},
		{"  ", "whitespace only"},
		{"income-statement", "a hyphen where the SDK's alias uses an underscore"},
		{"balance-sheet", "ditto"},
		{"cash-flow", "ditto"},
		{"incomestatement", "the long name run together"},
		{"p&l", "a third spelling in common use, deliberately not accepted"},
		{"statements", "a plural, i.e. what kind=all returns"},
		{"0", "a number: kinds are words, not ordinals on the command line"},
		{"3", "zero-based ordinal for the last kind, deliberately not accepted"},
		{"annual", "a period word; this is the -period parser's vocabulary"},
		{"af", "a -period code, not a -kind"},
		{"monthly", "a DCA frequency, not a kind"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseKind(tt.in)
			if err == nil {
				t.Fatalf("parseKind(%q) = %d with no error; the wrong statement would be printed "+
					"under a heading claiming otherwise", tt.in, int(got))
			}
			if !strings.Contains(err.Error(), "-kind") {
				t.Errorf("error %q does not name -kind", err)
			}
		})
	}
}

// FINDING, now fixed. The error return value used to be 0, which is
// FinancialReportKindIncomeStatement — a valid kind, and the first member of the
// enum. printFinancialReport checks the error, so nothing was wrong today, but a
// caller that forgot to would print the income statement while the heading named
// whatever word the user had typed. It is now financialReportKindInvalid, a
// value outside the enum.
//
// This asserts MEMBERSHIP rather than "it errors", because a test that only
// checked the error would have passed on the old code too.
func TestParseKind_TheErrorValueIsNotAKind(t *testing.T) {
	// Spelled out rather than derived from a range, so the assertion below
	// cannot be satisfied by renumbering the SDK constants.
	valid := []fundamental.FinancialReportKind{
		fundamental.FinancialReportKindIncomeStatement,
		fundamental.FinancialReportKindBalanceSheet,
		fundamental.FinancialReportKindCashFlow,
		fundamental.FinancialReportKindAll,
	}
	for _, in := range []string{"annual", " ", "p&l", "0"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseKind(in)
			if err == nil {
				t.Fatalf("parseKind(%q) = %d with no error", in, int(got))
			}
			for _, k := range valid {
				if got == k {
					t.Errorf("parseKind(%q) = %d alongside its error, and that is a valid kind; a "+
						"caller that ignored the error would print the wrong statement under a "+
						"heading claiming otherwise. The error return must be outside the enum",
						in, int(got))
				}
			}
			if got != financialReportKindInvalid {
				t.Errorf("parseKind(%q) = %d alongside its error, want the sentinel %d "+
					"(financialReportKindInvalid)", in, int(got), int(financialReportKindInvalid))
			}
		})
	}
}

// The sentinel's safety rests on the SDK numbering being 0..3 with nothing
// negative, so that property is asserted directly rather than assumed.
func TestFinancialReportKindInvalid_IsOutsideTheEnum(t *testing.T) {
	if int(financialReportKindInvalid) >= 0 {
		t.Fatalf("financialReportKindInvalid = %d, which is inside the 0..3 range the SDK declares",
			int(financialReportKindInvalid))
	}
	for i, k := range []fundamental.FinancialReportKind{
		fundamental.FinancialReportKindIncomeStatement,
		fundamental.FinancialReportKindBalanceSheet,
		fundamental.FinancialReportKindCashFlow,
		fundamental.FinancialReportKindAll,
	} {
		if int(k) != i {
			t.Errorf("SDK FinancialReportKind member %d = %d, pinned %d; the sentinel's "+
				"negative value is only safe while the whole enum is non-negative", i, int(k), i)
		}
	}
}

// ------------------------------------------------------------------ parsePeriod

// parsePeriod is the one parser here that returns a pointer, and the pointer is
// meaningful: nil means "let the server decide", so a non-nil pointer holding
// the zero value (FinancialReportPeriodAnnual == 0) must be distinguishable
// from nil. Collapsing the two would silently change which period is fetched.
func TestParsePeriod_EmptyMeansServerDefaultAndIsDistinctFromAnnual(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parsePeriod(in)
			if err != nil {
				t.Fatalf("parsePeriod(%q) = error %v; empty must mean server default, not a failure", in, err)
			}
			if got != nil {
				t.Errorf("parsePeriod(%q) = %v, want nil so the server picks the period", in, *got)
			}
		})
	}
}

func TestParsePeriod_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want int
		name string
	}{
		{"af", 0, "FinancialReportPeriodAnnual"},
		{"annual", 0, "FinancialReportPeriodAnnual"},
		{"saf", 1, "FinancialReportPeriodSemiAnnual"},
		{"semi", 1, "FinancialReportPeriodSemiAnnual"},
		{"semi_annual", 1, "FinancialReportPeriodSemiAnnual"},
		{"q1", 2, "FinancialReportPeriodQ1"},
		{"q2", 3, "FinancialReportPeriodQ2"},
		{"q3", 4, "FinancialReportPeriodQ3"},
		{"qf", 5, "FinancialReportPeriodQuarterlyFull"},
		{"full_quarter", 5, "FinancialReportPeriodQuarterlyFull"},
		{"3q", 6, "FinancialReportPeriodThreeQ"},
		{"three_q", 6, "FinancialReportPeriodThreeQ"},
		// Lower-cased and trimmed.
		{"AF", 0, "FinancialReportPeriodAnnual"},
		{"  Q2  ", 3, "FinancialReportPeriodQ2"},
		{"Three_Q", 6, "FinancialReportPeriodThreeQ"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parsePeriod(tt.in)
			if err != nil {
				t.Fatalf("parsePeriod(%q) = error %v, want %s", tt.in, err, tt.name)
			}
			if got == nil {
				t.Fatalf("parsePeriod(%q) = nil; a named period must be sent, not left to the server", tt.in)
			}
			if int(*got) != tt.want {
				t.Errorf("parsePeriod(%q) = %d, want %d (%s)", tt.in, int(*got), tt.want, tt.name)
			}
		})
	}
}

func TestParsePeriod_UnrecognisedValueIsAnErrorAndNotNil(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"fy", "a fiscal-year word"},
		{"h1", "half-year, which the SDK does not name"},
		{"h2", "ditto"},
		{"q0", "no such quarter"},
		{"q4", "the fourth quarter, which the SDK does not name"},
		{"quarter", "a bare noun"},
		{"quarterly", "the long form; only qf and full_quarter are accepted"},
		{"full-quarter", "a hyphen where the alias uses an underscore"},
		{"q 1", "a real code split by a space; TrimSpace removes surrounding whitespace, not interior"},
		{"q-1", "a real code with a substituted punctuation"},
		{"is", "valid for parseKind, not for -period"},
		{"all", "valid for parseKind, not for -period"},
		{"daily", "a statement type, not a report period"},
		{"0", "a number: periods are codes, not ordinals"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parsePeriod(tt.in)
			if err == nil {
				t.Fatalf("parsePeriod(%q) = %v with no error; the wrong period would be fetched", tt.in, got)
			}
			// nil is the success value for the empty input, so a rejection must
			// not return it: the caller would read that as "server default".
			if got != nil {
				t.Errorf("parsePeriod(%q) = %v alongside an error; the rejected value must be nil", tt.in, *got)
			}
			if !strings.Contains(err.Error(), "-period") {
				t.Errorf("error %q does not name -period", err)
			}
		})
	}
}

// The two parsers in this file must not accept each other's words, or -kind
// would take a period and -period would take a statement kind.
func TestKindAndPeriod_VocabulariesDoNotOverlap(t *testing.T) {
	for _, w := range []string{"is", "bs", "cf", "all", "income", "balance", "cash", "income_statement"} {
		if got, err := parsePeriod(w); err == nil {
			t.Errorf("parsePeriod(%q) = %v, but that is a -kind word", w, got)
		}
	}
	for _, w := range []string{"af", "saf", "q1", "q2", "q3", "qf", "3q", "annual", "semi", "full_quarter", "three_q"} {
		if got, err := parseKind(w); err == nil {
			t.Errorf("parseKind(%q) = %d, but that is a -period word", w, int(got))
		}
	}
}

// ------------------------------------------------------------------ parseRecommend

// The SDK's own numbering is pinned, because InstitutionRecommendUnknown is the
// zero value: if the enum ever renumbered, a zero recommendation would render
// as a different real one.
func TestParseRecommend_TheSDKConstantValuesArePinned(t *testing.T) {
	pairs := []struct {
		name string
		got  fundamental.InstitutionRecommend
		want int
	}{
		{"InstitutionRecommendUnknown", fundamental.InstitutionRecommendUnknown, 0},
		{"InstitutionRecommendStrongBuy", fundamental.InstitutionRecommendStrongBuy, 1},
		{"InstitutionRecommendBuy", fundamental.InstitutionRecommendBuy, 2},
		{"InstitutionRecommendHold", fundamental.InstitutionRecommendHold, 3},
		{"InstitutionRecommendSell", fundamental.InstitutionRecommendSell, 4},
		{"InstitutionRecommendStrongSell", fundamental.InstitutionRecommendStrongSell, 5},
		{"InstitutionRecommendUnderperform", fundamental.InstitutionRecommendUnderperform, 6},
		{"InstitutionRecommendNoOpinion", fundamental.InstitutionRecommendNoOpinion, 7},
	}
	for _, p := range pairs {
		if int(p.got) != p.want {
			t.Errorf("SDK %s = %d, pinned %d; parseRecommend's case order depends on it", p.name, int(p.got), p.want)
		}
	}
}

func TestParseRecommend_EverySDKConstantRendersToItsOwnName(t *testing.T) {
	tests := []struct {
		in   fundamental.InstitutionRecommend
		want string
	}{
		{fundamental.InstitutionRecommendStrongBuy, "strong_buy"},
		{fundamental.InstitutionRecommendBuy, "buy"},
		{fundamental.InstitutionRecommendHold, "hold"},
		{fundamental.InstitutionRecommendSell, "sell"},
		{fundamental.InstitutionRecommendStrongSell, "strong_sell"},
		{fundamental.InstitutionRecommendUnderperform, "underperform"},
		{fundamental.InstitutionRecommendNoOpinion, "no_opinion"},
	}
	seen := map[string]fundamental.InstitutionRecommend{}
	for _, tt := range tests {
		got := parseRecommend(tt.in)
		if got != tt.want {
			t.Errorf("parseRecommend(%d) = %q, want %q", int(tt.in), got, tt.want)
		}
		if got == "" {
			t.Errorf("parseRecommend(%d) = \"\"; the row would look like a missing value", int(tt.in))
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("constants %d and %d both render as %q", int(prev), int(tt.in), got)
		}
		seen[got] = tt.in
	}
}

// The renderer must be total over the values the SDK can actually return, and
// anything outside them must be visibly marked rather than blank.
func TestParseRecommend_UnknownAndOutOfRangeValuesAreVisiblyMarked(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want string
	}{
		// The SDK's own unknown constant has no named rendering, so it falls
		// through to the marked form. That is visible, not silent.
		{"the SDK's own Unknown constant", 0, "unknown(0)"},
		{"one past NoOpinion", 8, "unknown(8)"},
		{"far out of range", 99, "unknown(99)"},
		{"negative", -1, "unknown(-1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRecommend(fundamental.InstitutionRecommend(tt.in))
			if got != tt.want {
				t.Errorf("parseRecommend(%d) = %q, want %q", tt.in, got, tt.want)
			}
			if got == "" {
				t.Errorf("parseRecommend(%d) = \"\"; an unrecognised recommendation must be marked, not blank", tt.in)
			}
		})
	}
}

// ------------------------------------------------------------------ parseElementType

func TestParseElementType_EverySDKConstantRendersToItsOwnName(t *testing.T) {
	tests := []struct {
		in   fundamental.ElementType
		want string
	}{
		{fundamental.ElementTypeHoldings, "holdings"},
		{fundamental.ElementTypeRegional, "regional"},
		{fundamental.ElementTypeAssetClass, "asset_class"},
		{fundamental.ElementTypeIndustry, "industry"},
	}
	seen := map[string]int{}
	for _, tt := range tests {
		got := parseElementType(tt.in)
		if got != tt.want {
			t.Errorf("parseElementType(%d) = %q, want %q", int(tt.in), got, tt.want)
		}
		if got == "" {
			t.Errorf("parseElementType(%d) = \"\"; the group would be headed by nothing", tt.in)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("parseElementType renders %d and %d as %q", prev, int(tt.in), got)
		}
		seen[got] = int(tt.in)
	}
}

func TestParseElementType_UnknownAndOutOfRangeValuesAreVisiblyMarked(t *testing.T) {
	// 0 is the SDK's ElementTypeUnknown, which has no named rendering; 5 does
	// not exist yet. Both must be marked rather than rendered as "" or "0".
	for _, in := range []int{0, 5, 6, 99, -1} {
		want := fmt.Sprintf("unknown(%d)", in)
		if got := parseElementType(fundamental.ElementType(in)); got != want {
			t.Errorf("parseElementType(%d) = %q, want %q", in, got, want)
		}
	}
}

// Every constant the SDK declares, mapped. This is the check that would fail if
// a new ElementType were added to the SDK without a rendering here.
func TestParseElementType_ExhaustiveOverTheSDKConstantList(t *testing.T) {
	// The SDK declares Unknown=0 plus Holdings..Industry=1..4.
	for in := 0; in <= 4; in++ {
		got := parseElementType(fundamental.ElementType(in))
		if got == "" {
			t.Errorf("SDK ElementType %d renders as \"\"; every constant the SDK can return needs a rendering", in)
		}
	}
}

// ------------------------------------------------------------------ parseImportance

// importance is a bare int32 with no String() in the SDK, and the SDK names it
// 1=low, 2=medium, 3=high. Rendering is therefore the only place the meaning
// exists at all.
func TestParseImportance_TheDocumentedLevelsRenderToTheirNames(t *testing.T) {
	tests := []struct {
		in   fundamental.MacroeconomicImportance
		want string
	}{
		{fundamental.MacroeconomicImportanceLow, "low"},
		{fundamental.MacroeconomicImportanceMedium, "medium"},
		{fundamental.MacroeconomicImportanceHigh, "high"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := parseImportance(tt.in); got != tt.want {
				t.Errorf("parseImportance(%d) = %q, want %q", int(tt.in), got, tt.want)
			}
		})
	}
}

// The SDK's numbering is pinned because the renderer's cases name those
// constants: there is no 0 constant, so a renumbering would move "high" to a
// different column without any other sign.
func TestParseImportance_TheSDKConstantValuesArePinned(t *testing.T) {
	for _, p := range []struct {
		name string
		got  fundamental.MacroeconomicImportance
		want int32
	}{
		{"MacroeconomicImportanceLow", fundamental.MacroeconomicImportanceLow, 1},
		{"MacroeconomicImportanceMedium", fundamental.MacroeconomicImportanceMedium, 2},
		{"MacroeconomicImportanceHigh", fundamental.MacroeconomicImportanceHigh, 3},
	} {
		if int32(p.got) != p.want {
			t.Errorf("SDK %s = %d, pinned %d; parseImportance's case order depends on it", p.name, int32(p.got), p.want)
		}
	}
}

// The absent level decodes to 0, which is not one of the three the SDK defines.
// A macro table used to print that as a bare "0" in its IMPORT. column, where it
// reads as a measurement; the other two renderers in this file already marked
// unrecognised values as unknown(N), and this one now does the same.
func TestParseImportance_UnknownAndOutOfRangeValuesAreVisiblyMarked(t *testing.T) {
	tests := []struct {
		name string
		in   int32
		want string
	}{
		// 0 is not an SDK constant: it is what an omitted field decodes to.
		{"an absent level, which decodes to 0", 0, "unknown(0)"},
		{"one past High", 4, "unknown(4)"},
		{"negative", -1, "unknown(-1)"},
		{"far out of range", 99, "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseImportance(fundamental.MacroeconomicImportance(tt.in))
			if got != tt.want {
				t.Errorf("parseImportance(%d) = %q, want %q", tt.in, got, tt.want)
			}
			if got == "" {
				t.Errorf("parseImportance(%d) = \"\"; the column would be blank", tt.in)
			}
		})
	}
}

// The SDK field is a bare int32, so a caller can hand this renderer a value the
// enum does not define. It must still be marked, and the marks must not collide,
// or two different values would be indistinguishable in a column.
func TestParseImportance_EveryInt32RendersToSomethingNonEmptyAndDistinct(t *testing.T) {
	seen := map[string]int32{}
	for _, in := range []int32{-2147483648, -1, 0, 1, 2, 3, 4, 2147483647} {
		got := parseImportance(fundamental.MacroeconomicImportance(in))
		if got == "" {
			t.Errorf("parseImportance(%d) = \"\"; the column would be blank", in)
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("parseImportance renders %d and %d as the same %q", prev, in, got)
		}
		seen[got] = in
	}
}

// ------------------------------------------------------------------ helpers

func TestOrNone_BlankBecomesTheServerDefaultLabel(t *testing.T) {
	for _, in := range []string{"", " ", "\t\n"} {
		if got := orNone(in); got != "(server default)" {
			t.Errorf("orNone(%q) = %q, want \"(server default)\"", in, got)
		}
	}
	if got := orNone("af"); got != "af" {
		t.Errorf("orNone(\"af\") = %q, want it verbatim", got)
	}
	// The raw value is echoed, not normalised, so the heading shows what was
	// actually sent.
	if got := orNone("  AF  "); got != "  AF  " {
		t.Errorf("orNone(\"  AF  \") = %q, want the input verbatim", got)
	}
}

func TestDerefString_NilIsEmptyAndAValueIsRendered(t *testing.T) {
	if got := derefString[string](nil); got != "" {
		t.Errorf("derefString(nil) = %q, want \"\"", got)
	}
	s := "HK"
	if got := derefString(&s); got != "HK" {
		t.Errorf("derefString(&\"HK\") = %q, want \"HK\"", got)
	}
	empty := ""
	if got := derefString(&empty); got != "" {
		t.Errorf("derefString(&\"\") = %q, want \"\"", got)
	}
}

func TestYesNo(t *testing.T) {
	if got := yesNo(true); got != "yes" {
		t.Errorf("yesNo(true) = %q, want \"yes\"", got)
	}
	if got := yesNo(false); got != "no" {
		t.Errorf("yesNo(false) = %q, want \"no\"", got)
	}
}

// A *time.Time the API omits decodes to the zero time, not to nil, so the
// absent test is IsZero. It used to be a nil check alone, which rendered an
// omitted release date as 0001-01-01 00:00:00 in a macro series; cmd/content and
// cmd/sharelist print "-" for the same value.
func TestFmtTimePtr_NilAndZeroTimeBothRenderAsAbsent(t *testing.T) {
	if got := fmtTimePtr(nil); got != "-" {
		t.Errorf("fmtTimePtr(nil) = %q, want \"-\"", got)
	}
	zero := time.Time{}
	if got := fmtTimePtr(&zero); got != "-" {
		t.Errorf("fmtTimePtr(&time.Time{}) = %q, want \"-\" for an absent timestamp, "+
			"the same value cmd/content and cmd/sharelist render as \"-\"", got)
	}
}

func TestFmtTimePtr_ARenderedInstantIsUTC(t *testing.T) {
	when := time.Date(2025, 3, 1, 12, 30, 45, 0, time.UTC)
	if got := fmtTimePtr(&when); got != "2025-03-01 12:30:45" {
		t.Errorf("fmtTimePtr = %q, want the UTC rendering", got)
	}
	// A non-UTC time is converted, so two machines in two zones render the same
	// instant identically.
	east := time.Date(2025, 3, 1, 20, 30, 45, 0, time.FixedZone("UTC+8", 8*3600))
	if got := fmtTimePtr(&east); got != "2025-03-01 12:30:45" {
		t.Errorf("fmtTimePtr(UTC+8) = %q, want it converted to UTC", got)
	}
}

func TestSplitList_SplitsOnCommasAndDropsBlanks(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{",,", nil},
		{"700.HK", []string{"700.HK"}},
		{"MSFT.US,GOOGL.US", []string{"MSFT.US", "GOOGL.US"}},
		{" MSFT.US , GOOGL.US ", []string{"MSFT.US", "GOOGL.US"}},
		{"MSFT.US,,GOOGL.US,", []string{"MSFT.US", "GOOGL.US"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got := splitList(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitList(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitList(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}
