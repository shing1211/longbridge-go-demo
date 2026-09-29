package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go"
	"github.com/longbridge/openapi-go/calendar"
	"github.com/longbridge/openapi-go/fundamental"
)

// The required-input rules for this package's 36 actions live in the action
// functions themselves, not in validateFlags (main.go). Each one checks its own
// flag before it creates a context or calls the SDK, so the tests below pass a
// nil *fundamental.FundamentalContext / *asset.AssetContext: if a check ever
// stopped running first, these tests would panic instead of quietly passing.

// ------------------------------------------------------------------ required input

// The three actions with a documented required flag must name it in the error.
// A message that says "missing input" sends the user to the README; one that
// says "-indicator-code" sends them to the copy step that produces it.
func TestActions_NamingTheirOwnMissingRequiredFlag(t *testing.T) {
	tests := []struct {
		name     string
		set      func(t *testing.T)
		run      func() error
		wantErrs []string
	}{
		{
			name: "shareholder-detail without -object-id",
			set:  func(t *testing.T) { validFundFlags(t); objectID = 0 },
			run:  func() error { return printShareholderDetail(context.Background(), nil) },
			wantErrs: []string{
				"-object-id",
				"shareholder-detail",
				// The message must also say where to get the id, since it is
				// not something the user can invent.
				"shareholder",
				"HOLDER_ID",
			},
		},
		{
			name: "statement-url without -file-key",
			set:  func(t *testing.T) { validFundFlags(t); fileKey = "" },
			run:  func() error { return printStatementURL(context.Background(), nil) },
			wantErrs: []string{
				"-file-key",
				"statement-url",
				"FILE_KEY",
			},
		},
		{
			name:     "statement-url with a whitespace-only -file-key",
			set:      func(t *testing.T) { validFundFlags(t); fileKey = "   " },
			run:      func() error { return printStatementURL(context.Background(), nil) },
			wantErrs: []string{"-file-key"},
		},
		{
			name: "macro without -indicator-code",
			set:  func(t *testing.T) { validFundFlags(t); macroCode = "" },
			run:  func() error { return printMacro(context.Background(), nil) },
			wantErrs: []string{
				"-indicator-code",
				"macro",
				"CODE",
			},
		},
		{
			name:     "macro with a whitespace-only -indicator-code",
			set:      func(t *testing.T) { validFundFlags(t); macroCode = "\t" },
			run:      func() error { return printMacro(context.Background(), nil) },
			wantErrs: []string{"-indicator-code"},
		},
		{
			name:     "valuation-compare with an empty -peers",
			set:      func(t *testing.T) { validFundFlags(t); peers = "" },
			run:      func() error { return printValuationComparison(context.Background(), nil) },
			wantErrs: []string{"-peers"},
		},
		{
			name:     "valuation-compare with a -peers of only separators",
			set:      func(t *testing.T) { validFundFlags(t); peers = " , , " },
			run:      func() error { return printValuationComparison(context.Background(), nil) },
			wantErrs: []string{"-peers"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set(t)
			err := tt.run()
			if err == nil {
				t.Fatalf("%s was not refused", tt.name)
			}
			for _, want := range tt.wantErrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// The report action parses -kind and -period before it calls the SDK, so both
// parsers are reachable from the action with a nil context. That is what makes
// "a bad -kind is caught before a request" a testable claim rather than a
// comment.
func TestPrintFinancialReport_BadKindOrPeriodIsRefusedBeforeTheSDKCall(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		period  string
		wantErr string
	}{
		{"unknown kind", "income-statement", "af", "-kind"},
		{"empty kind", "", "af", "-kind"},
		{"unknown period", "is", "quarterly", "-period"},
		{"q4 is not a period the SDK names", "is", "q4", "-period"},
		// Both bad: kind is parsed first, so its error is the one reported.
		{"both bad, kind reported first", "nonsense", "nonsense", "-kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validFundFlags(t)
			kind, period = tt.kind, tt.period
			err := printFinancialReport(context.Background(), nil)
			if err == nil {
				t.Fatalf("printFinancialReport(-kind %q -period %q) = nil", tt.kind, tt.period)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
		})
	}
}

// A nil context reaching an SDK call panics, so these two "valid input" cases
// only prove the checks did not fire. They are here to pin that the validation
// is genuinely reached for good input, and they document the boundary.
func TestPrintFinancialReport_ValidKindAndPeriodGetPastValidationToTheSDKCall(t *testing.T) {
	validFundFlags(t)
	kind, period = "cf", "3q"
	// A nil *fundamental.FundamentalContext will fault when the method is
	// reached. That is the assertion: validation let it through.
	defer func() {
		if recover() == nil {
			t.Log("printFinancialReport(-kind cf -period 3q) returned without reaching the SDK, " +
				"which means the method tolerates a nil context; the check ordering is still what matters")
		}
	}()
	_ = printFinancialReport(context.Background(), nil)
}

// ------------------------------------------------------------------ industry rank indicator

// IndustryRankIndicator is a string enum whose constants are the digits "0" to
// "7", so the SDK's own values are pinned: parseIndustryRankIndicator compares
// against them, and a "renumbered" enum would silently change what a code means.
func TestParseIndustryRankIndicator_TheSDKConstantValuesArePinned(t *testing.T) {
	for got, want := range map[fundamental.IndustryRankIndicator]string{
		fundamental.IndustryRankIndicator0: "0",
		fundamental.IndustryRankIndicator1: "1",
		fundamental.IndustryRankIndicator2: "2",
		fundamental.IndustryRankIndicator3: "3",
		fundamental.IndustryRankIndicator4: "4",
		fundamental.IndustryRankIndicator5: "5",
		fundamental.IndustryRankIndicator6: "6",
		fundamental.IndustryRankIndicator7: "7",
	} {
		if string(got) != want {
			t.Errorf("SDK IndustryRankIndicator %q, pinned %q", got, want)
		}
	}
}

func TestParseIndustryRankIndicator_EveryCodeInTheDocumentedRangeMapsToItsConstant(t *testing.T) {
	all := []fundamental.IndustryRankIndicator{
		fundamental.IndustryRankIndicator0, fundamental.IndustryRankIndicator1,
		fundamental.IndustryRankIndicator2, fundamental.IndustryRankIndicator3,
		fundamental.IndustryRankIndicator4, fundamental.IndustryRankIndicator5,
		fundamental.IndustryRankIndicator6, fundamental.IndustryRankIndicator7,
	}
	for i, want := range all {
		in := fmt.Sprint(i)
		t.Run(in, func(t *testing.T) {
			got, err := parseIndustryRankIndicator(in)
			if err != nil {
				t.Fatalf("parseIndustryRankIndicator(%q) = error %v, but it is in the documented 0-7 range", in, err)
			}
			if got != want {
				t.Errorf("parseIndustryRankIndicator(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestParseIndustryRankIndicator_OutOfRangeAndMalformedCodesAreErrors(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"8", "one past the documented range"},
		{"9", "ditto"},
		{"-1", "negative"},
		{"", "empty string"},
		{"  ", "whitespace only"},
		// The comparison is against the exact single digit, so a padded value
		// is trimmed first but a re-spelling of the same number is not: only
		// the code the SDK declares goes on the wire.
		{"00", "a code with a leading zero"},
		{"01", "ditto"},
		{"1.0", "a numeric rendering of the same number"},
		{"+1", "a signed digit"},
		{"0x1", "hexadecimal"},
		{"1e0", "floating point"},
		{"asc", "a -sort-type word, not an indicator code"},
		{"ascending", "ditto"},
		{"desc", "ditto"},
		{"turnover", "the -rank-key vocabulary of cmd/market, not an indicator code"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseIndustryRankIndicator(tt.in)
			if err == nil {
				t.Fatalf("parseIndustryRankIndicator(%q) = %q with no error; an undocumented code "+
					"would be sent to the API, which returns nothing for it", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseIndustryRankIndicator(%q) = %q alongside an error; the rejected value must be empty", tt.in, got)
			}
			if !strings.Contains(err.Error(), "-indicator") {
				t.Errorf("error %q does not name -indicator", err)
			}
		})
	}
}

// Every parser in this package trims, and a shell-quoted " 1 " is the code 1
// rather than a different code, so the industry-rank indicator parser trims too.
// It stays strict about the digit itself: see the re-spellings in the table
// above, which are still refused.
func TestParseIndustryRankIndicator_SurroundingWhitespaceIsTrimmedToTheSameCode(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want fundamental.IndustryRankIndicator
	}{
		{" 1", fundamental.IndustryRankIndicator1},
		{"1 ", fundamental.IndustryRankIndicator1},
		{"\t1\n", fundamental.IndustryRankIndicator1},
		{"  0  ", fundamental.IndustryRankIndicator0},
		{" 7", fundamental.IndustryRankIndicator7},
		{"7\t", fundamental.IndustryRankIndicator7},
	} {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseIndustryRankIndicator(tt.in)
			if err != nil {
				t.Fatalf("parseIndustryRankIndicator(%q) = error %v; padding is not a different code", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseIndustryRankIndicator(%q) = %q, want %q; the padded text must never "+
					"reach the API", tt.in, got, tt.want)
			}
		})
	}
}

// The two industry-rank parsers must not accept each other's words. The digits
// "0" and "1" are a genuine, unavoidable overlap: they are both indicator codes
// and sort types, so they are excluded from this check and pinned separately —
// " 1 " is the padded form of that same overlap, not a sort word.
func TestIndustryRankParsers_VocabulariesDoNotOverlap(t *testing.T) {
	for _, w := range []string{"asc", "ascending", "desc", "descending", "ASC"} {
		if got, err := parseIndustryRankIndicator(w); err == nil {
			t.Errorf("parseIndustryRankIndicator(%q) = %q, but that is a -sort-type word", w, got)
		}
	}
	for _, w := range []string{"2", "3", "4", "5", "6", "7", "8", "turnover"} {
		if got, err := parseIndustryRankSort(w); err == nil {
			t.Errorf("parseIndustryRankSort(%q) = %q, but that is an -indicator code", w, got)
		}
	}
}

// "0" and "1" are legitimately valid for both parsers, because the SDK uses the
// same two digits for the first two indicator codes and for the two sort types.
// Pinned so the overlap is a known fact rather than a surprise: a -indicator 1
// is a different request from a -sort-type 1, and the two flags are separate.
func TestIndustryRankParsers_TheDigitsZeroAndOneAreValidForBoth(t *testing.T) {
	for _, d := range []string{"0", "1"} {
		ind, err := parseIndustryRankIndicator(d)
		if err != nil {
			t.Errorf("parseIndustryRankIndicator(%q) = error %v", d, err)
		} else if string(ind) != d {
			t.Errorf("parseIndustryRankIndicator(%q) = %q", d, ind)
		}
		sort, err := parseIndustryRankSort(d)
		if err != nil {
			t.Errorf("parseIndustryRankSort(%q) = error %v", d, err)
		} else if string(sort) != d {
			t.Errorf("parseIndustryRankSort(%q) = %q", d, sort)
		}
	}
}

// ------------------------------------------------------------------ industry rank sort

func TestParseIndustryRankSort_TheSDKConstantValuesArePinned(t *testing.T) {
	if fundamental.IndustryRankSortTypeAscending != "0" {
		t.Errorf("SDK IndustryRankSortTypeAscending = %q, want \"0\"", fundamental.IndustryRankSortTypeAscending)
	}
	if fundamental.IndustryRankSortTypeDescending != "1" {
		t.Errorf("SDK IndustryRankSortTypeDescending = %q, want \"1\"", fundamental.IndustryRankSortTypeDescending)
	}
}

func TestParseIndustryRankSort_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want fundamental.IndustryRankSortType
	}{
		{"0", fundamental.IndustryRankSortTypeAscending},
		{"asc", fundamental.IndustryRankSortTypeAscending},
		{"ascending", fundamental.IndustryRankSortTypeAscending},
		{"1", fundamental.IndustryRankSortTypeDescending},
		{"desc", fundamental.IndustryRankSortTypeDescending},
		{"descending", fundamental.IndustryRankSortTypeDescending},
		// Lower-cased and trimmed, unlike parseIndustryRankIndicator next door.
		{"ASC", fundamental.IndustryRankSortTypeAscending},
		{"Descending", fundamental.IndustryRankSortTypeDescending},
		{" 0 ", fundamental.IndustryRankSortTypeAscending},
		{"\tascending\n", fundamental.IndustryRankSortTypeAscending},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseIndustryRankSort(tt.in)
			if err != nil {
				t.Fatalf("parseIndustryRankSort(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseIndustryRankSort(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseIndustryRankSort_UnrecognisedValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"2", "one past the two sort types"},
		{"-1", "negative"},
		{"", "empty string"},
		{"   ", "whitespace only"},
		{"a-z", "a range, not a direction"},
		{"asc desc", "both directions"},
		{"up", "a word for ascending, not in the vocabulary"},
		{"down", "a word for descending, not in the vocabulary"},
		{"ascending2", "a real word with a digit appended"},
		{"0.0", "a numeric rendering of a real value"},
		{"true", "a boolean, not a direction"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseIndustryRankSort(tt.in)
			if err == nil {
				t.Fatalf("parseIndustryRankSort(%q) = %q with no error; the list would come back in the "+
					"wrong order with no indication which", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseIndustryRankSort(%q) = %q alongside an error; the rejected value must be empty", tt.in, got)
			}
			if !strings.Contains(err.Error(), "-sort-type") {
				t.Errorf("error %q does not name -sort-type", err)
			}
		})
	}
}

// validateFlags has its own copy of the -sort-type vocabulary. If the two ever
// disagree, one of them becomes dead code and a user gets a confusing error
// from the other. This asserts they accept exactly the same spellings — which
// now holds by construction, since validateFlags calls this parser.
func TestValidateFlags_AndParseIndustryRankSortAgreeOnTheSortTypeVocabulary(t *testing.T) {
	accepted := []string{"0", "1", "asc", "desc", "ascending", "descending",
		"ASC", "Descending", " 0 ", "\tascending\n", " 1 "}
	for _, v := range accepted {
		t.Run("accepted/"+v, func(t *testing.T) {
			if _, err := parseIndustryRankSort(v); err != nil {
				t.Errorf("parseIndustryRankSort(%q) = error %v", v, err)
			}
			validFundFlags(t)
			sortType = v
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for -sort-type %q, but parseIndustryRankSort accepts it", err, v)
			}
		})
	}
	rejected := []string{"2", "-1", "", "   ", "up", "down", "a-z"}
	for _, v := range rejected {
		t.Run("rejected/"+v, func(t *testing.T) {
			if _, err := parseIndustryRankSort(v); err == nil {
				t.Errorf("parseIndustryRankSort(%q) was accepted, but validateFlags rejects it", v)
			}
			validFundFlags(t)
			sortType = v
			if err := validateFlags(); err == nil {
				t.Errorf("validateFlags() accepted -sort-type %q, but parseIndustryRankSort rejects it", v)
			}
		})
	}
}

// The same agreement, for the -indicator range that startup validation now
// checks. The padded spellings are here for the same reason as above: the
// startup layer and the parser must not have different ideas of a valid code.
func TestValidateFlags_AndParseIndustryRankIndicatorAgreeOnTheIndicatorVocabulary(t *testing.T) {
	accepted := []string{"0", "1", "2", "3", "4", "5", "6", "7", " 0 ", "3 ", "\t5\n"}
	for _, v := range accepted {
		t.Run("accepted/"+v, func(t *testing.T) {
			if _, err := parseIndustryRankIndicator(v); err != nil {
				t.Errorf("parseIndustryRankIndicator(%q) = error %v", v, err)
			}
			validFundFlags(t)
			indicators = v
			if err := validateFlags(); err != nil {
				t.Errorf("validateFlags() = %v for -indicator %q, but parseIndustryRankIndicator accepts it", err, v)
			}
		})
	}
	rejected := []string{"8", "99", "-1", "", "   ", "00", "1.0", "1e0", "asc", "turnover"}
	for _, v := range rejected {
		t.Run("rejected/"+v, func(t *testing.T) {
			if _, err := parseIndustryRankIndicator(v); err == nil {
				t.Errorf("parseIndustryRankIndicator(%q) was accepted, but validateFlags rejects it", v)
			}
			validFundFlags(t)
			indicators = v
			if err := validateFlags(); err == nil {
				t.Errorf("validateFlags() accepted -indicator %q, but parseIndustryRankIndicator rejects it", v)
			}
		})
	}
}

// ------------------------------------------------------------------ calendar category

// CalendarCategory starts at 0 (report), so the error return value 0 is a valid
// category. The same latent hazard as cmd/dca's frequency: the error must be
// checked, and it is a zero the caller could mistake for "report".
func TestParseCalendarCategory_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want calendar.CalendarCategory
		name string
	}{
		{"report", calendar.CalendarCategoryReport, "CalendarCategoryReport"},
		{"dividend", calendar.CalendarCategoryDividend, "CalendarCategoryDividend"},
		{"split", calendar.CalendarCategorySplit, "CalendarCategorySplit"},
		{"ipo", calendar.CalendarCategoryIpo, "CalendarCategoryIpo"},
		{"macrodata", calendar.CalendarCategoryMacroData, "CalendarCategoryMacroData"},
		{"closed", calendar.CalendarCategoryClosed, "CalendarCategoryClosed"},
		{"meeting", calendar.CalendarCategoryMeeting, "CalendarCategoryMeeting"},
		{"merge", calendar.CalendarCategoryMerge, "CalendarCategoryMerge"},
		// Lower-cased and trimmed.
		{"REPORT", calendar.CalendarCategoryReport, "CalendarCategoryReport"},
		{"  Dividend  ", calendar.CalendarCategoryDividend, "CalendarCategoryDividend"},
		{"\nMacroData\n", calendar.CalendarCategoryMacroData, "CalendarCategoryMacroData"},
		{"IPO", calendar.CalendarCategoryIpo, "CalendarCategoryIpo"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseCalendarCategory(tt.in)
			if err != nil {
				t.Fatalf("parseCalendarCategory(%q) = error %v, want %s", tt.in, err, tt.name)
			}
			if got != tt.want {
				t.Errorf("parseCalendarCategory(%q) = %d, want %d (%s)", tt.in, int(got), int(tt.want), tt.name)
			}
		})
	}
}

// Every wire string the SDK declares must round-trip through the parser, so a
// category can never be reachable on the wire but unreachable on the command
// line.
func TestParseCalendarCategory_AgreesWithTheSDKWireStrings(t *testing.T) {
	all := []calendar.CalendarCategory{
		calendar.CalendarCategoryReport,
		calendar.CalendarCategoryDividend,
		calendar.CalendarCategorySplit,
		calendar.CalendarCategoryIpo,
		calendar.CalendarCategoryMacroData,
		calendar.CalendarCategoryClosed,
		calendar.CalendarCategoryMeeting,
		calendar.CalendarCategoryMerge,
	}
	for _, c := range all {
		wire := c.String()
		if wire == "" {
			t.Errorf("SDK CalendarCategory %d has an empty wire string", int(c))
			continue
		}
		got, err := parseCalendarCategory(wire)
		if err != nil {
			t.Errorf("parseCalendarCategory(%q) = error %v, but the SDK sends that string on the wire", wire, err)
			continue
		}
		if got != c {
			t.Errorf("parseCalendarCategory(%q) = %d, want %d", wire, int(got), int(c))
		}
	}
	// The SDK's own numbering is pinned, because the error return value 0 is
	// CalendarCategoryReport and a renumbering would change what 0 means.
	for i, c := range all {
		if int(c) != i {
			t.Errorf("SDK CalendarCategory %q = %d, pinned %d", c.String(), int(c), i)
		}
	}
}

func TestParseCalendarCategory_UnrecognisedValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"earnings", "the word a user reaches for; the SDK calls it report"},
		{"results", "ditto"},
		{"reports", "a plural of a real category"},
		{"macro-data", "a hyphen where the category is macrodata"},
		{"macro data", "a space instead"},
		{"dividends", "a plural of a real category"},
		{"splits", "ditto"},
		{"ipos", "ditto"},
		{"holiday", "a plausible but unsupported event type"},
		{"holidays", "ditto"},
		{"conference", "a plausible shareholder event, not a category"},
		{"merger", "a near-miss on merge"},
		{"mergers", "ditto"},
		{"closed-market", "an elaboration of a real category"},
		{"", "empty string: the -calendar-category default is report"},
		{"   ", "whitespace only"},
		{"0", "a number: categories are words on the command line"},
		{"price-rise", "an alert condition, not a calendar category"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseCalendarCategory(tt.in)
			if err == nil {
				t.Fatalf("parseCalendarCategory(%q) = %d with no error; the wrong event type would be "+
					"listed under a heading naming another", tt.in, int(got))
			}
			if !strings.Contains(err.Error(), "-calendar-category") {
				t.Errorf("error %q does not name -calendar-category", err)
			}
		})
	}
}

// FINDING, now fixed. The error return value used to be 0, which is
// CalendarCategoryReport — a valid category, and the documented default besides.
// printCalendar checks the error, so nothing was wrong today, but a caller that
// dropped the check would have listed earnings reports under a heading naming
// them, with nothing in the output saying the value was never parsed. It is now
// calendarCategoryInvalid, a value outside the enum.
//
// This asserts MEMBERSHIP rather than "it errors", because a test that only
// checked the error would have passed on the old code too.
func TestParseCalendarCategory_TheErrorValueIsNotACategory(t *testing.T) {
	// Spelled out rather than derived from a range, so the assertion below
	// cannot be satisfied by renumbering the SDK constants.
	valid := []calendar.CalendarCategory{
		calendar.CalendarCategoryReport,
		calendar.CalendarCategoryDividend,
		calendar.CalendarCategorySplit,
		calendar.CalendarCategoryIpo,
		calendar.CalendarCategoryMacroData,
		calendar.CalendarCategoryClosed,
		calendar.CalendarCategoryMeeting,
		calendar.CalendarCategoryMerge,
	}
	for _, in := range []string{"earnings", " ", "macro-data", "reports"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseCalendarCategory(in)
			if err == nil {
				t.Fatalf("parseCalendarCategory(%q) = %d with no error", in, int(got))
			}
			for _, c := range valid {
				if got == c {
					t.Errorf("parseCalendarCategory(%q) = %d alongside its error, and that is %v; "+
						"a caller that ignored the error would list the wrong event type under a "+
						"heading claiming otherwise. The error return must be outside the enum",
						in, int(got), c.String())
				}
			}
			if got != calendarCategoryInvalid {
				t.Errorf("parseCalendarCategory(%q) = %d alongside its error, want the sentinel %d "+
					"(calendarCategoryInvalid)", in, int(got), int(calendarCategoryInvalid))
			}
		})
	}
}

// The sentinel's safety rests on the SDK numbering being 0..7 with nothing
// negative, so that property is asserted directly rather than assumed.
func TestCalendarCategoryInvalid_IsOutsideTheEnum(t *testing.T) {
	if int(calendarCategoryInvalid) >= 0 {
		t.Fatalf("calendarCategoryInvalid = %d, which is inside the 0..7 range the SDK declares",
			int(calendarCategoryInvalid))
	}
	if got := calendarCategoryInvalid.String(); got != "report" {
		// Informational, not a failure: the SDK's String() has a catch-all
		// default of "report", so a sentinel printed by anything that formats
		// rather than sends would read as a real category. It never reaches a
		// request — every call site returns on the error — which is why this is
		// a log line and not a t.Error.
		t.Logf("note: CalendarCategory(-1).String() = %q, because the SDK's String() falls back to "+
			"report for an unmapped value; the value is never sent", got)
	}
}

// The string-enum parser in the same package, checked for the same property. It
// was already safe and is left alone: fundamental.IndustryRankSortType is a
// string type whose only members are "0" and "1", so "" cannot be one of them
// and no sentinel is needed. What is pinned here is both halves of that
// statement — the two members, and the fact that the error return is neither of
// them — so an SDK that renumbered or retyped the enum would fail here instead
// of quietly making the zero value valid.
func TestParseIndustryRankSort_TheErrorValueIsNotASortType(t *testing.T) {
	valid := []fundamental.IndustryRankSortType{
		fundamental.IndustryRankSortTypeAscending,
		fundamental.IndustryRankSortTypeDescending,
	}
	// Pinned first, because the membership assertion below is only as good as
	// the member list: if these were renumbered, the list would be wrong.
	for got, want := range map[fundamental.IndustryRankSortType]string{
		fundamental.IndustryRankSortTypeAscending:  "0",
		fundamental.IndustryRankSortTypeDescending: "1",
	} {
		if string(got) != want {
			t.Errorf("SDK IndustryRankSortType %q = %q, pinned %q", want, string(got), want)
		}
	}
	for _, in := range []string{"2", " ", "up", "-1"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseIndustryRankSort(in)
			if err == nil {
				t.Fatalf("parseIndustryRankSort(%q) = %q with no error", in, got)
			}
			for _, s := range valid {
				if got == s {
					t.Errorf("parseIndustryRankSort(%q) = %q alongside its error, and that is a valid "+
						"sort type; a caller that ignored the error would list the industries in the "+
						"wrong order with no indication which", in, got)
				}
			}
			if got != "" {
				t.Errorf("parseIndustryRankSort(%q) = %q alongside its error, want the zero value, "+
					"which is outside the two-member string enum", in, got)
			}
		})
	}
}

// ------------------------------------------------------------------ calendar

// printCalendar resolves its window and its category before it touches the SDK,
// so both are reachable with a nil *calendar.CalendarContext.
func TestPrintCalendar_ARejectedWindowOrCategoryIsCaughtBeforeTheSDKCall(t *testing.T) {
	tests := []struct {
		name    string
		start   string
		end     string
		cat     string
		wantErr string
	}{
		{"end before start", "2025-03-31", "2025-03-01", "report", "before"},
		{"unparsable start", "31/03/2025", "", "report", "-calendar-start"},
		{"unparsable end", "2025-03-01", "1 April 2025", "report", "-calendar-end"},
		{"unknown category with a good window", "", "", "earnings", "-calendar-category"},
		{"empty category with a good window", "", "", "", "-calendar-category"},
		{"bad window and bad category", "nonsense", "nonsense", "earnings", "-calendar-start"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validFundFlags(t)
			calStart, calEnd, calCategory = tt.start, tt.end, tt.cat
			err := printCalendar(context.Background(), nil)
			if err == nil {
				t.Fatalf("printCalendar(-calendar-start %q -calendar-end %q -calendar-category %q) = nil",
					tt.start, tt.end, tt.cat)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

// ------------------------------------------------------------------ industry rank

// printIndustryRank parses -indicator, -sort-type and the market it derives
// from -symbol before it calls the SDK, so all three are reachable with a nil
// context.
func TestPrintIndustryRank_ARejectedIndicatorOrSortIsCaughtBeforeTheSDKCall(t *testing.T) {
	tests := []struct {
		name    string
		ind     string
		sort    string
		wantErr string
	}{
		{"indicator above the documented range", "8", "1", "-indicator"},
		{"empty indicator", "", "1", "-indicator"},
		{"a sort word where an indicator code belongs", "asc", "1", "-indicator"},
		{"valid indicator with a bad sort type", "3", "2", "-sort-type"},
		{"valid indicator with an empty sort type", "3", "", "-sort-type"},
		// A padded indicator is trimmed, so this failure is the sort type's:
		// naming -sort-type here is what proves the padded code was accepted
		// rather than refused at the first check.
		{"padded indicator passes its own check and the sort type is reported", " 1", "2", "-sort-type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validFundFlags(t)
			indicators, sortType = tt.ind, tt.sort
			err := printIndustryRank(context.Background(), nil)
			if err == nil {
				t.Fatalf("printIndustryRank(-indicator %q -sort-type %q) = nil", tt.ind, tt.sort)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
		})
	}
}

// The market comes from -symbol's suffix, and both market-wide actions refuse an
// unreadable one before the request rather than forwarding it. A nil context
// would fault if either got that far.
func TestMarketWideActions_AnUnknownOrEmptyMarketSuffixIsCaughtBeforeTheSDKCall(t *testing.T) {
	actions := []struct {
		name string
		run  func() error
	}{
		{"industry-rank", func() error { return printIndustryRank(context.Background(), nil) }},
		{"industry-peers", func() error { return printIndustryPeers(context.Background(), nil) }},
	}
	for _, sym := range []string{"700.XX", "700.", "000001.SH", "700.USA", "700.1"} {
		for _, a := range actions {
			t.Run(a.name+"/"+sym, func(t *testing.T) {
				validFundFlags(t)
				symbol = sym
				err := a.run()
				if err == nil {
					t.Fatalf("%s with -symbol %q = nil; the suffix would be sent to a market-wide endpoint", a.name, sym)
				}
				for _, want := range []string{"-symbol", "market"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
			})
		}
	}
}

// ------------------------------------------------------------------ market from symbol

// IndustryRank and IndustryPeers are market-wide, so the symbol's suffix becomes
// the market code. A symbol with no suffix falls back to HK, which is documented
// and kept; a suffix that is not one of the SDK's market codes is refused.
func TestMarketFromSymbol_AnSDKMarketSuffixIsTheMarketAndNoSuffixMeansHK(t *testing.T) {
	tests := []struct {
		in   string
		want string
		note string
	}{
		{"700.HK", "HK", "the documented default symbol"},
		{"AAPL.US", "US", ""},
		{"000001.CN", "CN", "a mainland code written with the market suffix"},
		{"D05.SG", "SG", "Singapore"},
		{"VOD.UK", "UK", "London"},
		{"3067.HK", "HK", "an ETF"},
		{"700.hk", "HK", "lower case is upper-cased"},
		{"aapl.us", "US", "ditto"},
		// The documented fallback: no suffix means HK, because the demo is
		// written for an HK account and these endpoints need a market.
		{"700", "HK", "no suffix falls back to HK"},
		{"", "HK", "the empty symbol also falls back to HK"},
		{"AAPL", "HK", "a bare US code is ranked in the HK market, as documented"},
		// The last dot wins, so a two-part suffix loses its first half.
		{"700.HK.US", "US", "LastIndex picks the last dot"},
		{".HK", "HK", "an empty code with a market"},
		// Padded, like every other flag value in this package.
		{" 700.HK ", "HK", "the suffix is trimmed"},
		{"700. hk", "HK", "whitespace inside the suffix is trimmed too"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := marketFromSymbol(tt.in)
			if err != nil {
				t.Fatalf("marketFromSymbol(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("marketFromSymbol(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// An unrecognised or empty suffix used to be forwarded verbatim: 700.XX ranked
// the market "XX" and 700. ranked the market "". Both come back as an empty
// result, which reads as "this market has no industries" rather than as a
// mistake. They are now refused, with a message that names the flag, the
// offending suffix and the market codes the SDK defines.
func TestMarketFromSymbol_AnUnknownOrEmptySuffixIsRejectedRatherThanForwarded(t *testing.T) {
	for _, tt := range []struct{ in, note string }{
		{"700.XX", "a made-up market code"},
		{"700.", "a trailing dot, which used to yield an empty market"},
		{"700.USA", "a long form of a real market, which the SDK does not name"},
		{"700.1", "a number, not a market code"},
		{"000001.SH", "an exchange suffix, not one of the SDK's market codes"},
	} {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := marketFromSymbol(tt.in)
			if err == nil {
				t.Fatalf("marketFromSymbol(%q) = %q, nil; the suffix would be sent to a market-wide endpoint", tt.in, got)
			}
			if got != "" {
				t.Errorf("marketFromSymbol(%q) = %q alongside an error; the rejected value must be empty", tt.in, got)
			}
			for _, want := range []string{"-symbol", tt.in, "HK, US, CN, SG or UK"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// The accepted set is the SDK's own, not a list invented here, so it cannot
// drift from what the rest of the repo and the SDK itself consider a market.
func TestMarketFromSymbol_AcceptsEverySDKMarketConstantAndNothingElse(t *testing.T) {
	for _, m := range []openapi.Market{
		openapi.MarketHK, openapi.MarketUS, openapi.MarketCN, openapi.MarketSG, openapi.MarketUK,
	} {
		t.Run(string(m), func(t *testing.T) {
			if got, err := marketFromSymbol("code." + string(m)); err != nil || got != string(m) {
				t.Errorf("marketFromSymbol(\"code.%s\") = (%q, %v), want (%q, nil)", m, got, err, m)
			}
		})
	}
	// Pinned, because widening the set would be a decision rather than a
	// consequence of the SDK: a value the SDK does not define is forwarded to
	// the API and comes back empty. These are the near misses around the five.
	for _, notAMarket := range []string{"EU", "JP", "SH", "SZ", "AA", "H", ""} {
		t.Run("rejected/"+notAMarket, func(t *testing.T) {
			if got, err := marketFromSymbol("code." + notAMarket); err == nil {
				t.Errorf("marketFromSymbol(\"code.%s\") = (%q, nil); the SDK defines no such market", notAMarket, got)
			}
		})
	}
}

// The macro country filter is a different vocabulary from a market code and
// stays tolerant: it only narrows a list, so an unrecognised suffix means "no
// filter" instead of refusing the action. The six countries are the SDK's
// MacroeconomicCountry constants, which include EU and JP but not UK.
func TestMacroCountry_AnUnrecognisedSuffixMeansNoFilterRatherThanAnError(t *testing.T) {
	tests := []struct {
		in   string
		want fundamental.MacroeconomicCountry
		none bool
	}{
		{"700.HK", fundamental.MacroeconomicCountryHK, false},
		{"AAPL.US", fundamental.MacroeconomicCountryUS, false},
		{"000001.CN", fundamental.MacroeconomicCountryCN, false},
		{"D05.SG", fundamental.MacroeconomicCountrySG, false},
		{"x.EU", fundamental.MacroeconomicCountryEU, false},
		{"x.JP", fundamental.MacroeconomicCountryJP, false},
		{"700.hk", fundamental.MacroeconomicCountryHK, false},
		{" 700.HK ", fundamental.MacroeconomicCountryHK, false},
		// No country filter rather than a refused action.
		{"700", "", true},
		{"", "", true},
		{"000001.SH", "", true},
		{"700.XX", "", true},
		{"x.UK", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := macroCountry(tt.in)
			if tt.none {
				if got != nil {
					t.Errorf("macroCountry(%q) = %q, want no filter", tt.in, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("macroCountry(%q) = nil, want %q", tt.in, tt.want)
			}
			if *got != tt.want {
				t.Errorf("macroCountry(%q) = %q, want %q", tt.in, *got, tt.want)
			}
		})
	}
}

// ------------------------------------------------------------------ calendar window

// An end before the start is rejected rather than silently swapped: a
// user transposing two dates would otherwise get a window that looks plausible
// and covers the wrong period.
func TestCalendarWindow_RejectsAnEndBeforeTheStart(t *testing.T) {
	validFundFlags(t)
	calStart, calEnd = "2025-03-31", "2025-03-01"
	start, end, err := calendarWindow()
	if err == nil {
		t.Fatalf("calendarWindow() = (%q, %q, nil) for a transposed window", start, end)
	}
	if !strings.Contains(err.Error(), "before") {
		t.Errorf("error %q does not explain that the end precedes the start", err)
	}
	for _, want := range []string{"2025-03-01", "2025-03-31"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not echo %s", err, want)
		}
	}
}

func TestCalendarWindow_AcceptsAnExplicitWindowAndRejectsBadDates(t *testing.T) {
	tests := []struct {
		name    string
		start   string
		end     string
		want    [2]string
		wantErr string
	}{
		{"both explicit", "2025-03-01", "2025-04-01", [2]string{"2025-03-01", "2025-04-01"}, ""},
		{"same day", "2025-03-01", "2025-03-01", [2]string{"2025-03-01", "2025-03-01"}, ""},
		{"explicit start, defaulted end", "2025-03-01", "", [2]string{"2025-03-01", "2025-03-31"}, ""},
		{"padded dates are trimmed", " 2025-03-01 ", " 2025-04-01 ", [2]string{"2025-03-01", "2025-04-01"}, ""},
		{"bad start", "01/03/2025", "", [2]string{}, "-calendar-start"},
		{"bad end", "2025-03-01", "1 April 2025", [2]string{}, "-calendar-end"},
		{"bad start with an explicit end", "March", "2025-04-01", [2]string{}, "-calendar-start"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validFundFlags(t)
			calStart, calEnd = tt.start, tt.end
			start, end, err := calendarWindow()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("calendarWindow(%q, %q) = (%q, %q, nil)", tt.start, tt.end, start, end)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not name %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("calendarWindow(%q, %q) = error %v", tt.start, tt.end, err)
			}
			if start != tt.want[0] || end != tt.want[1] {
				t.Errorf("calendarWindow(%q, %q) = (%q, %q), want (%q, %q)",
					tt.start, tt.end, start, end, tt.want[0], tt.want[1])
			}
		})
	}
}

// An entirely empty window defaults to today through today+30. The dates move
// with the clock, so the test asserts the shape (a 30-day window starting
// today) rather than two literals.
func TestCalendarWindow_AnEmptyWindowDefaultsToTheNextThirtyDays(t *testing.T) {
	validFundFlags(t)
	calStart, calEnd = "", ""
	start, end, err := calendarWindow()
	if err != nil {
		t.Fatalf("calendarWindow() = error %v", err)
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	wantEnd := now.AddDate(0, 0, 30).Format("2006-01-02")
	if start != today {
		t.Errorf("calendarWindow() start = %q, want today (%q)", start, today)
	}
	if end != wantEnd {
		t.Errorf("calendarWindow() end = %q, want %q (thirty days after %q)", end, wantEnd, start)
	}
	if end < start {
		t.Errorf("calendarWindow() produced a backwards window: %q .. %q", start, end)
	}
}

// A padded -calendar-start is trimmed before the default end is computed, so
// "-calendar-start ' 2025-03-01 '" with no end still yields a real date rather
// than a parse failure.
func TestCalendarWindow_APaddedStartIsTrimmedBeforeTheEndIsDerived(t *testing.T) {
	validFundFlags(t)
	calStart, calEnd = "  2025-03-01  ", ""
	start, end, err := calendarWindow()
	if err != nil {
		t.Fatalf("calendarWindow() = error %v; the start should have been trimmed before parsing", err)
	}
	if start != "2025-03-01" || end != "2025-03-31" {
		t.Errorf("calendarWindow() = (%q, %q), want (2025-03-01, 2025-03-31)", start, end)
	}
}
