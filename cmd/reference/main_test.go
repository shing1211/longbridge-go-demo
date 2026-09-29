package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/longbridge/openapi-go"
	"github.com/longbridge/openapi-go/quote"
)

// quote.CalcIndex is a bare int32 with 41 constants, and this table maps 40
// human names onto it. A wrong constant here is a wrong column of index values,
// so every one of the 40 is pinned to its own constant and its own number.

// ---------------------------------------------------------------- sections

var allSections = []string{
	"static", "list", "index", "flow", "distribution", "session", "participants",
	"profile", "realtime", "history", "optionvol", "filings", "brokers", "short",
	"counter",
}

func TestParseSections_EveryValidKeyIsSelected(t *testing.T) {
	want, err := parseSections(strings.Join(allSections, ","))
	if err != nil {
		t.Fatalf("parseSections(all) = error %v", err)
	}
	if len(want) != len(allSections) {
		t.Fatalf("parseSections(all) selected %d sections, want %d", len(want), len(allSections))
	}
	for _, s := range allSections {
		if !want[s] {
			t.Errorf("parseSections(all) did not select %q", s)
		}
	}
}

func TestParseSections_SelectsASubsetAndIsCaseSensitive(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"one section", "static", []string{"static"}},
		{"a few", "static,index,session", []string{"static", "index", "session"}},
		{"the default from the flag help", "static,index,session,participants,profile",
			[]string{"static", "index", "session", "participants", "profile"}},
		{"whitespace around entries is trimmed", "  static , index  ", []string{"static", "index"}},
		{"empty entries are skipped", "static,,index,", []string{"static", "index"}},
		{"duplicates collapse into one key", "static,static,index", []string{"static", "index"}},
		{"order does not matter for a set", "index,static", []string{"static", "index"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSections(tt.in)
			if err != nil {
				t.Fatalf("parseSections(%q) = error %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseSections(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for _, s := range tt.want {
				if !got[s] {
					t.Errorf("parseSections(%q) = %v, missing %q", tt.in, got, s)
				}
			}
		})
	}
}

// Unlike every other parser in the repo, parseSections does NOT lower-case its
// input: a capitalised section is rejected rather than quietly accepted. Pinned
// because the inconsistency is easy to "fix" by accident.
func TestParseSections_CaseMismatchesAreRejected(t *testing.T) {
	for _, in := range []string{"Static", "STATIC", "Index", "Session", "Short"} {
		if got, err := parseSections(in); err == nil {
			t.Errorf("parseSections(%q) = %v with no error, want an error", in, got)
		}
	}
}

func TestParseSections_UnknownKeyIsAnErrorThatListsEveryValidName(t *testing.T) {
	got, err := parseSections("static,bogus,index")
	if err == nil {
		t.Fatalf("parseSections with an unknown key = %v with no error, want an error", got)
	}
	if got != nil {
		t.Errorf("parseSections returned %v alongside the error, want nil so no partial selection escapes", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"bogus"`) {
		t.Errorf("error %q does not name the offending key", msg)
	}
	// The message must contain every valid name, or the user cannot recover
	// without reading the source.
	for _, s := range allSections {
		if !strings.Contains(msg, s) {
			t.Errorf("error %q does not list the valid section %q", msg, s)
		}
	}
	// And it must be sorted, so the message is stable across runs (the valid
	// set is a map, whose iteration order is randomised).
	listed := msg[strings.Index(msg, "want a subset of ")+len("want a subset of "):]
	gotNames := strings.Split(listed, ", ")
	sorted := append([]string(nil), gotNames...)
	sort.Strings(sorted)
	if len(gotNames) != len(allSections) {
		t.Errorf("error lists %d names, want %d (%q)", len(gotNames), len(allSections), listed)
	}
	for i := range gotNames {
		if gotNames[i] != sorted[i] {
			t.Errorf("the listed names are not sorted: %q", listed)
			break
		}
	}
	// The message is deterministic across calls despite the map iteration.
	for i := 0; i < 20; i++ {
		_, again := parseSections("bogus")
		if again.Error() != msg {
			t.Fatalf("the error message is not deterministic:\n%q\n%q", msg, again.Error())
		}
	}
}

func TestParseSections_UnknownKeysAreRejectedEvenAmongValidOnes(t *testing.T) {
	for _, in := range []string{
		"bogus", "static,bogus", "bogus,static", "  bogus  ", "statics",
		"indexes", "all", "none", "*", "-", "Static", "shorts",
	} {
		t.Run(in, func(t *testing.T) {
			got, err := parseSections(in)
			if err == nil {
				t.Fatalf("parseSections(%q) = %v with no error, want an error", in, got)
			}
			if got != nil {
				t.Errorf("parseSections(%q) = %v alongside the error, want nil", in, got)
			}
		})
	}
}

// An empty selection is an error, not an empty map: main() would otherwise
// authenticate and open a websocket to print nothing.
func TestParseSections_EmptySelectionIsAnErrorNotAnEmptyMap(t *testing.T) {
	for _, in := range []string{"", "   ", ",", " , , ", "\t\n"} {
		got, err := parseSections(in)
		if err == nil {
			t.Fatalf("parseSections(%q) = %v with no error, want an error", in, got)
		}
		if got != nil {
			t.Errorf("parseSections(%q) = %v alongside the error, want nil", in, got)
		}
	}
}

// ------------------------------------------------------------- calc indexes

// The 40 names of the -indexes table, each with the constant it must map to.
var calcIndexTable = map[string]quote.CalcIndex{
	"last_done":          quote.CalcIndexLastDone,
	"change_val":         quote.CalcIndexChangeVal,
	"change_rate":        quote.CalcIndexChangeRate,
	"volume":             quote.CalcIndexVolume,
	"turnover":           quote.CalcIndexTurnover,
	"ytd_change_rate":    quote.CalcIndexYtdChangeRate,
	"turnover_rate":      quote.CalcIndexTurnoverRate,
	"total_market_value": quote.CalcIndexTotalMarketValue,
	"capital_flow":       quote.CalcIndexCapitalFlow,
	"amplitude":          quote.CalcIndexAmplitude,
	"volume_ratio":       quote.CalcIndexVolumeRatio,
	"pe_ttm":             quote.CalcIndexPeTTMRatio,
	"pb":                 quote.CalcIndexPbRatio,
	"dividend_ttm":       quote.CalcIndexDividendRatioTTM,
	"5d_change_rate":     quote.CalcIndexFiveDayChangeRate,
	"10d_change_rate":    quote.CalcIndexTenDayChangeRate,
	"6m_change_rate":     quote.CalcIndexHalfYearChangeRate,
	"5m_change_rate":     quote.CalcIndexFiveMinutesChangeRate,
	"expiry_date":        quote.CalcIndexExpiryDate,
	"strike_price":       quote.CalcIndexStrikePrice,
	"upper_strike_price": quote.CalcIndexUpperStrikePrice,
	"lower_strike_price": quote.CalcIndexLowerStrikePrice,
	"outstanding_qty":    quote.CalcIndexOutstandingQTY,
	"outstanding_ratio":  quote.CalcIndexOutstandingRatio,
	"premium":            quote.CalcIndexPremium,
	"itm_otm":            quote.CalcIndexItmOtm,
	"implied_volatility": quote.CalcIndexImpliedVolatility,
	"warrant_delta":      quote.CalcIndexWarrantDelta,
	"call_price":         quote.CalcIndexCallPrice,
	"to_call_price":      quote.CalcIndexToCallPrice,
	"effective_leverage": quote.CalcIndexEffectiveLeverage,
	"leverage_ratio":     quote.CalcIndexLeverageRatio,
	"conversion_ratio":   quote.CalcIndexConversionRatio,
	"balance_point":      quote.CalcIndexBalancePoint,
	"open_interest":      quote.CalcIndexOpenInterest,
	"delta":              quote.CalcIndexDELTA,
	"gamma":              quote.CalcIndexGAMMA,
	"theta":              quote.CalcIndexTHETA,
	"vega":               quote.CalcIndexVEGA,
	"rho":                quote.CalcIndexRHO,
}

func TestParseCalcIndexes_EveryNameMapsToItsOwnConstant(t *testing.T) {
	if len(calcIndexTable) != 40 {
		t.Fatalf("the test table has %d names, want 40", len(calcIndexTable))
	}
	// Distinct names, distinct constants: a copy-paste that points two names at
	// one constant is caught here.
	byConst := map[quote.CalcIndex][]string{}
	for name, want := range calcIndexTable {
		got, err := parseCalcIndexes(name)
		if err != nil {
			t.Errorf("parseCalcIndexes(%q) = error %v, want %d", name, err, int32(want))
			continue
		}
		if len(got) != 1 {
			t.Errorf("parseCalcIndexes(%q) returned %d entries, want 1", name, len(got))
			continue
		}
		if got[0] != want {
			t.Errorf("parseCalcIndexes(%q) = %d, want %d", name, int32(got[0]), int32(want))
		}
		byConst[got[0]] = append(byConst[got[0]], name)
	}
	for c, names := range byConst {
		if len(names) > 1 {
			t.Errorf("names %v all map to %d; each name needs its own column", names, int32(c))
		}
	}
	if len(byConst) != 40 {
		t.Errorf("%d distinct CalcIndex constants are reachable, want 40", len(byConst))
	}
}

// CalcIndex is a bare int32 whose values come from the proto, so the wire
// numbers are pinned too. The two easily-confused pairs are delta vs
// warrant_delta and 5d/5m_change_rate.
func TestParseCalcIndexes_WireValuesArePinned(t *testing.T) {
	tests := []struct {
		name string
		want int32
	}{
		{"last_done", 1}, {"change_val", 2}, {"change_rate", 3}, {"volume", 4},
		{"turnover", 5}, {"ytd_change_rate", 6}, {"turnover_rate", 7},
		{"total_market_value", 8}, {"capital_flow", 9}, {"amplitude", 10},
		{"volume_ratio", 11}, {"pe_ttm", 12}, {"pb", 13}, {"dividend_ttm", 14},
		{"5d_change_rate", 15}, {"10d_change_rate", 16}, {"6m_change_rate", 17},
		{"5m_change_rate", 18}, {"expiry_date", 19}, {"strike_price", 20},
		{"upper_strike_price", 21}, {"lower_strike_price", 22},
		{"outstanding_qty", 23}, {"outstanding_ratio", 24}, {"premium", 25},
		{"itm_otm", 26}, {"implied_volatility", 27}, {"warrant_delta", 28},
		{"call_price", 29}, {"to_call_price", 30}, {"effective_leverage", 31},
		{"leverage_ratio", 32}, {"conversion_ratio", 33}, {"balance_point", 34},
		{"open_interest", 35}, {"delta", 36}, {"gamma", 37}, {"theta", 38},
		{"vega", 39}, {"rho", 40},
	}
	if len(tests) != len(calcIndexTable) {
		t.Fatalf("the wire-value table has %d rows, want %d", len(tests), len(calcIndexTable))
	}
	for _, tt := range tests {
		got, err := parseCalcIndexes(tt.name)
		if err != nil {
			t.Fatalf("parseCalcIndexes(%q) = error %v", tt.name, err)
		}
		if len(got) != 1 || int32(got[0]) != tt.want {
			t.Errorf("parseCalcIndexes(%q) = %v, want exactly [%d]", tt.name, got, tt.want)
		}
	}
	// CalcIndexUnknown is 0 and must not be reachable through a name.
	if _, err := parseCalcIndexes("unknown"); err == nil {
		t.Error("parseCalcIndexes(\"unknown\") returned no error; the unknown column must not be selectable by name")
	}
}

// The numeric-prefixed names are where a case or separator slip shows up: the
// SDK calls them FiveDay/TenDay/HalfYear/FiveMinutes, the CLI shortens some and
// spells others out.
func TestParseCalcIndexes_NumericPrefixedNamesAreExact(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want quote.CalcIndex
	}{
		{"5d_change_rate", quote.CalcIndexFiveDayChangeRate},
		{"10d_change_rate", quote.CalcIndexTenDayChangeRate},
		{"6m_change_rate", quote.CalcIndexHalfYearChangeRate},
		{"5m_change_rate", quote.CalcIndexFiveMinutesChangeRate},
	} {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseCalcIndexes(tt.in)
			if err != nil {
				t.Fatalf("parseCalcIndexes(%q) = error %v", tt.in, err)
			}
			if got[0] != tt.want {
				t.Errorf("parseCalcIndexes(%q) = %d, want %d", tt.in, int32(got[0]), int32(tt.want))
			}
		})
	}
	// Near-misses on the same prefixes.
	for _, in := range []string{"5D_change_rate", "10D_change_rate", "6M_change_rate", "5M_change_rate"} {
		if got, err := parseCalcIndexes(in); err == nil {
			t.Errorf("parseCalcIndexes(%q) = %v with no error, want an error: the table is case-sensitive", in, got)
		}
	}
}

// The list parser preserves order, keeps duplicates and drops empty entries —
// unlike parseSections, which is a set.
func TestParseCalcIndexes_ListSemanticsAreOrderedWithDuplicatesKept(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []quote.CalcIndex
	}{
		{"the flag default", "last_done,change_rate,volume,turnover,pe_ttm,pb",
			[]quote.CalcIndex{quote.CalcIndexLastDone, quote.CalcIndexChangeRate, quote.CalcIndexVolume,
				quote.CalcIndexTurnover, quote.CalcIndexPeTTMRatio, quote.CalcIndexPbRatio}},
		{"order is preserved verbatim", "rho,delta,gamma", []quote.CalcIndex{
			quote.CalcIndexRHO, quote.CalcIndexDELTA, quote.CalcIndexGAMMA}},
		{"the reverse order is a different slice", "gamma,delta,rho", []quote.CalcIndex{
			quote.CalcIndexGAMMA, quote.CalcIndexDELTA, quote.CalcIndexRHO}},
		{"duplicates are kept, not collapsed", "pb,pb", []quote.CalcIndex{quote.CalcIndexPbRatio, quote.CalcIndexPbRatio}},
		{"whitespace is trimmed around entries", "  pb , pe_ttm  ", []quote.CalcIndex{
			quote.CalcIndexPbRatio, quote.CalcIndexPeTTMRatio}},
		{"empty entries are skipped", "pb,,pe_ttm,", []quote.CalcIndex{
			quote.CalcIndexPbRatio, quote.CalcIndexPeTTMRatio}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCalcIndexes(tt.in)
			if err != nil {
				t.Fatalf("parseCalcIndexes(%q) = error %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseCalcIndexes(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseCalcIndexes(%q)[%d] = %d, want %d", tt.in, i, int32(got[i]), int32(tt.want[i]))
				}
			}
		})
	}
}

func TestParseCalcIndexes_UnknownNameIsAnErrorNotAZero(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"   ", "whitespace only"},
		{"unknown", "the SDK's own unknown-column name"},
		{"calcindex_unknown", "the proto constant name"},
		{"last done", "space instead of underscore"},
		{"last-done", "hyphen instead of underscore"},
		{"LastDone", "the Go constant's camel case"},
		{"LAST_DONE", "upper case"},
		{"last_done ", "trailing space is trimmed, so this IS accepted; kept to prove the trim"},
		{"pb_mrq", "a name the SDK uses for a field but not for a column"},
		{"free_float", "a real quote field with no column"},
		{"market_cap", "a real quote field with no column"},
		{"roe", "a cmd/screener -show column, not a CalcIndex"},
		{"capmk", "a cmd/screener -show column, not a CalcIndex"},
		{"filter_pettm", "a screener key with the SDK's filter_ prefix still attached"},
		{"static", "a -sections value"},
		{"HK", "a market"},
		{"5d_changerate", "transposed underscore"},
		{"5x_change_rate", "letter for digit"},
		{"52_change_rate", "digit for letter"},
		{"outstanding_qty_", "trailing underscore"},
		{"_pb", "leading underscore"},
	}
	for _, tt := range tests {
		if tt.in == "last_done " {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				got, err := parseCalcIndexes(tt.in)
				if err != nil {
					t.Fatalf("parseCalcIndexes(%q) = error %v, want it trimmed and accepted", tt.in, err)
				}
				if len(got) != 1 || got[0] != quote.CalcIndexLastDone {
					t.Errorf("parseCalcIndexes(%q) = %v, want [LastDone]", tt.in, got)
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseCalcIndexes(tt.in)
			if err == nil {
				t.Fatalf("parseCalcIndexes(%q) = %v with no error, want an error", tt.in, got)
			}
			if got != nil {
				t.Errorf("parseCalcIndexes(%q) = %v alongside the error, want nil so no partial column list escapes", tt.in, got)
			}
		})
	}
}

// An empty selection is an error, not an empty list: an empty -indexes would
// ask the API for no columns and return a bare quote.
func TestParseCalcIndexes_EmptySelectionIsAnErrorNotAnEmptyList(t *testing.T) {
	for _, in := range []string{"", "   ", ",", " , , ", "\t\n"} {
		got, err := parseCalcIndexes(in)
		if err == nil {
			t.Fatalf("parseCalcIndexes(%q) = %v with no error, want an error", in, got)
		}
		if got != nil {
			t.Errorf("parseCalcIndexes(%q) = %v alongside the error, want nil", in, got)
		}
	}
}

// One bad name discards the whole list, so a caller that ignored the error
// could not ask for a half-parsed set of columns.
func TestParseCalcIndexes_OneBadNameDiscardsTheWholeList(t *testing.T) {
	for _, in := range []string{"pb,bogus,pe_ttm", "bogus", "pb, ,bogus", ",bogus", "pb,bogus"} {
		got, err := parseCalcIndexes(in)
		if err == nil {
			t.Fatalf("parseCalcIndexes(%q) = %v with no error, want an error", in, got)
		}
		if got != nil {
			t.Errorf("parseCalcIndexes(%q) = %v alongside the error, want nil", in, got)
		}
	}
}

// ------------------------------------------------------------------ market

func TestParseMarket_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want openapi.Market
	}{
		{"HK", openapi.MarketHK},
		{"US", openapi.MarketUS},
		{"CN", openapi.MarketCN},
		{"SG", openapi.MarketSG},
		{"UK", openapi.MarketUK},
		// ToUpper+TrimSpace.
		{"hk", openapi.MarketHK},
		{"us", openapi.MarketUS},
		{"cn", openapi.MarketCN},
		{"sg", openapi.MarketSG},
		{"uk", openapi.MarketUK},
		{"  HK  ", openapi.MarketHK},
		{"\tus\n", openapi.MarketUS},
		{"Uk", openapi.MarketUK},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseMarket(tt.in)
			if err != nil {
				t.Fatalf("parseMarket(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseMarket(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseMarket_UnknownValueIsAnErrorNotAnEmptyMarket(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"HKG", "the other common spelling of Hong Kong"},
		{"USA", "near miss on US"},
		{"GB", "another name for the UK"},
		{"jp", "a market the SDK does not define"},
		{"au", "a market the SDK does not define"},
		{"hk-ex", "suffix the flag help does not promise"},
		{"700.HK", "a symbol, not a market"},
		// Values belonging to the other parsers in this file.
		{"static", "a -sections value"},
		{"pb", "a -indexes value"},
		{"day", "a period"},
		{"Overnight", "a -category value, and the one constant the SDK defines"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseMarket(tt.in)
			if err == nil {
				t.Fatalf("parseMarket(%q) = %q with no error, want an error", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseMarket(%q) = %q alongside the error, want the zero value \"\"", tt.in, got)
			}
		})
	}
}

// sortStrings is the tiny insertion sort that keeps the section-error message
// deterministic. It is load-bearing for the error above, so it is tested here.
func TestSortStrings_SortsInPlaceAndHandlesTheDegenerateCases(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"already sorted", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"reversed", []string{"c", "b", "a"}, []string{"a", "b", "c"}},
		{"map iteration order", []string{"session", "brokers", "flow", "index", "static"},
			[]string{"brokers", "flow", "index", "session", "static"}},
		{"empty", []string{}, []string{}},
		{"single", []string{"only"}, []string{"only"}},
		{"duplicates are kept", []string{"b", "a", "b"}, []string{"a", "b", "b"}},
		{"prefix ordering is bytewise", []string{"ab", "a", "abc"}, []string{"a", "ab", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := append([]string(nil), tt.in...)
			sortStrings(got)
			if len(got) != len(tt.want) {
				t.Fatalf("sortStrings(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("sortStrings(%v) = %v, want %v", tt.in, got, tt.want)
					break
				}
			}
			if !sort.StringsAreSorted(got) {
				t.Errorf("sortStrings(%v) left %v unsorted", tt.in, got)
			}
		})
	}
}
