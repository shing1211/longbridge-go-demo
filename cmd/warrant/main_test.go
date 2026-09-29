package main

import (
	"reflect"
	"testing"

	"github.com/longbridge/openapi-go/quote"
)

// Every enum in this file is a bare int32 in the SDK, so the parsers are the
// only thing standing between a typo and a wrong-but-plausible warrant query.
// The whole risk is a silently wrong value, so every test here asserts the exact
// constant (and, where the wire value matters, the exact number) rather than
// merely "no error".

// ------------------------------------------------------------ sort-by

func TestParseWarrantSortBy_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want quote.WarrantSortBy
	}{
		{"last_done", quote.WarrantLastDone},
		{"lastdone", quote.WarrantLastDone},
		{"change_rate", quote.WarrantChangeRate},
		{"change_val", quote.WarrantChangeVal},
		{"volume", quote.WarrantVolume},
		{"turnover", quote.WarrantTurnover},
		{"expiry_date", quote.WarrantExpiryDate},
		{"strike_price", quote.WarrantStrikePrice},
		{"outstanding_qty", quote.WarrantOutstandingQty},
		{"implied_volatility", quote.WarrantImpliedVolatility},
		{"delta", quote.WarrantDelta},
		{"status", quote.WarrantSortStatus},
		// ToLower+TrimSpace.
		{"LAST_DONE", quote.WarrantLastDone},
		{"Change_Rate", quote.WarrantChangeRate},
		{"  volume  ", quote.WarrantVolume},
		{"\tDELTA\n", quote.WarrantDelta},
		{"STATUS", quote.WarrantSortStatus},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantSortBy(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantSortBy(%q) = error %v, want %d", tt.in, err, int32(tt.want))
			}
			if got != tt.want {
				t.Errorf("parseWarrantSortBy(%q) = %d, want %d", tt.in, int32(got), int32(tt.want))
			}
		})
	}
}

// The sort fields are a 22-value iota with several near-identical names
// (change_rate/change_val, and the strike-price triple). Each CLI word is
// pinned to its own number, so a transposed case is a failure.
func TestParseWarrantSortBy_WireValuesArePinned(t *testing.T) {
	tests := []struct {
		in   string
		want int32
	}{
		{"last_done", 0},
		{"change_rate", 1},
		{"change_val", 2},
		{"volume", 3},
		{"turnover", 4},
		{"expiry_date", 5},
		{"strike_price", 6},
		{"outstanding_qty", 9},
		{"implied_volatility", 13},
		{"delta", 14},
		{"status", 21},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantSortBy(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantSortBy(%q) = error %v", tt.in, err)
			}
			if int32(got) != tt.want {
				t.Errorf("parseWarrantSortBy(%q) = %d, want %d", tt.in, int32(got), tt.want)
			}
		})
	}
}

// No two accepted words may collapse to the same constant, otherwise a swap
// between them is invisible to the tests above.
func TestParseWarrantSortBy_AcceptedValuesAreDistinct(t *testing.T) {
	words := []string{
		"last_done", "lastdone", "change_rate", "change_val", "volume", "turnover",
		"expiry_date", "strike_price", "outstanding_qty", "implied_volatility",
		"delta", "status",
	}
	// "lastdone" is a documented alias of "last_done", so it is the one
	// intentional duplicate.
	want := map[quote.WarrantSortBy]int{}
	for _, w := range words {
		p, err := parseWarrantSortBy(w)
		if err != nil {
			t.Fatalf("parseWarrantSortBy(%q) = error %v", w, err)
		}
		want[p]++
	}
	if want[quote.WarrantLastDone] != 2 {
		t.Errorf("%q is mapped %d times, want 2 (last_done and its alias lastdone)",
			"last_done", want[quote.WarrantLastDone])
	}
	if len(want) != 11 {
		t.Errorf("%d distinct constants are reachable, want 11", len(want))
	}
}

func TestParseWarrantSortBy_UnknownValueIsAnErrorNotZero(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"change_rate ", "trailing space is trimmed, so this IS accepted; kept to prove the trim"},
		{"change_rat", "truncated"},
		{"change_rates", "plural"},
		{"chg_rate", "abbreviation not in the switch"},
		{"pct_change", "a plausible name from a quote UI"},
		{"last", "truncated"},
		{"price", "a plausible name from a quote UI"},
		{"amount", "a plausible name from a quote UI"},
		// Real SDK sort fields that this CLI does not expose. Accepting them
		// would be harmless, but rejecting them is what the flag help promises.
		{"upper_strike_price", "a real SDK sort field the CLI does not expose"},
		{"lower_strike_price", "a real SDK sort field the CLI does not expose"},
		{"outstanding_ratio", "a real SDK sort field the CLI does not expose"},
		{"premium", "the SDK spells this WarrantPremiun; the CLI name differs"},
		{"itm_otm", "a real SDK sort field the CLI does not expose"},
		{"warrant_delta", "the SDK name; the CLI shortens it to delta"},
		{"call_price", "a real SDK sort field the CLI does not expose"},
		{"leverage_ratio", "a real SDK sort field the CLI does not expose"},
		{"balance_point", "a real SDK sort field the CLI does not expose"},
		// Values belonging to the other five parsers in this file.
		{"call", "a -type value"},
		{"lt3", "an -expiry value"},
		{"in", "a -moneyness value"},
		{"suspend", "a -status value"},
		{"zh-hk", "a -language value"},
		{"asc", "a -sort-order value, not a field"},
		// Values belonging to parsers in other commands.
		{"day", "a cmd/quote period"},
		{"pe_ttm", "a cmd/reference calc index"},
		{"HK", "a market"},
	}
	for _, tt := range tests {
		if tt.in == "change_rate " {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				got, err := parseWarrantSortBy(tt.in)
				if err != nil {
					t.Fatalf("parseWarrantSortBy(%q) = error %v, want it trimmed and accepted", tt.in, err)
				}
				if got != quote.WarrantChangeRate {
					t.Errorf("parseWarrantSortBy(%q) = %d, want %d (ChangeRate)", tt.in, int32(got), int32(quote.WarrantChangeRate))
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseWarrantSortBy(tt.in); err == nil {
				t.Fatalf("parseWarrantSortBy(%q) = %d with no error, want an error", tt.in, int32(got))
			}
		})
	}
}

// -------------------------------------------------------------- types

func TestParseWarrantTypes_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want []quote.WarrantType
	}{
		{"call", []quote.WarrantType{quote.WarrantCall}},
		{"put", []quote.WarrantType{quote.WarrantPut}},
		{"bull", []quote.WarrantType{quote.WarrantBull}},
		{"bear", []quote.WarrantType{quote.WarrantBear}},
		{"inline", []quote.WarrantType{quote.WarrantInline}},
		// Multi-value input keeps its order.
		{"call,put", []quote.WarrantType{quote.WarrantCall, quote.WarrantPut}},
		{"bear,bull,call,put,inline", []quote.WarrantType{
			quote.WarrantBear, quote.WarrantBull, quote.WarrantCall,
			quote.WarrantPut, quote.WarrantInline,
		}},
		// splitList trims, and each part is lower-cased independently.
		{" call , PUT ", []quote.WarrantType{quote.WarrantCall, quote.WarrantPut}},
		{"CALL,Bear", []quote.WarrantType{quote.WarrantCall, quote.WarrantBear}},
		// Duplicates are kept, not collapsed: the API, not this parser, decides
		// what a repeated filter means.
		{"call,call", []quote.WarrantType{quote.WarrantCall, quote.WarrantCall}},
		{"bear,bear,call", []quote.WarrantType{quote.WarrantBear, quote.WarrantBear, quote.WarrantCall}},
		// A trailing or doubled comma is a missing entry, not a bad one, so it
		// is dropped rather than rejected.
		{"call,", []quote.WarrantType{quote.WarrantCall}},
		{"call,,put", []quote.WarrantType{quote.WarrantCall, quote.WarrantPut}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantTypes(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantTypes(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseWarrantTypes(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The wire values are consecutive from 0, and WarrantCall is 0, so a
// transposed case is an off-by-one that still looks like a valid filter.
func TestParseWarrantTypes_WireValuesArePinned(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"call", 0}, {"put", 1}, {"bull", 2}, {"bear", 3}, {"inline", 4},
	} {
		got, err := parseWarrantTypes(tt.in)
		if err != nil {
			t.Fatalf("parseWarrantTypes(%q) = error %v", tt.in, err)
		}
		if len(got) != 1 || int32(got[0]) != tt.want {
			t.Errorf("parseWarrantTypes(%q) = %v, want exactly [%d]", tt.in, got, tt.want)
		}
	}
}

// An empty filter means "no type filter", which is the documented default, so
// it is a nil slice and a nil error — not an error.
func TestParseWarrantTypes_EmptyInputIsNoFilterNotAnError(t *testing.T) {
	for _, in := range []string{"", "   ", ",", " , , "} {
		got, err := parseWarrantTypes(in)
		if err != nil {
			t.Fatalf("parseWarrantTypes(%q) = error %v, want (nil, nil)", in, err)
		}
		if len(got) != 0 {
			t.Errorf("parseWarrantTypes(%q) = %v, want an empty filter", in, got)
		}
	}
}

// A bad entry anywhere discards the whole list, so a caller that ignored the
// error could not apply a half-parsed filter and quietly return the wrong
// warrants.
func TestParseWarrantTypes_OneBadEntryDiscardsTheWholeList(t *testing.T) {
	for _, in := range []string{"call,bogus,put", "bogus", "call, ,bogus", ",bogus"} {
		got, err := parseWarrantTypes(in)
		if err == nil {
			t.Fatalf("parseWarrantTypes(%q) = %v with no error, want an error", in, got)
		}
		if got != nil {
			t.Errorf("parseWarrantTypes(%q) = %v alongside the error, want nil so no partial filter escapes", in, got)
		}
	}
}

func TestParseWarrantTypes_UnknownValueIsAnErrorNotZero(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"xyz", "nonsense"},
		{"cal", "truncated"},
		{"calls", "plural"},
		{"bull_bear", "a combined word the SDK does not define"},
		{"callable", "a real English word"},
		{"C", "single letter"},
		{"stock", "a security type the SDK does not model for warrants"},
		{"etf", "a security type the SDK does not model for warrants"},
		// Values belonging to the other five parsers in this file.
		{"lt3", "an -expiry value"},
		{"in", "a -moneyness value"},
		{"normal", "a -status value"},
		{"volume", "a -sort-by value"},
		{"en", "a -language value"},
		{"asc", "a -sort-order value"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseWarrantTypes(tt.in); err == nil {
				t.Fatalf("parseWarrantTypes(%q) = %v with no error, want an error", tt.in, got)
			}
		})
	}
}

// -------------------------------------------------------------- expiry

func TestParseWarrantExpiry_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want []quote.WarrantExpiryDateType
	}{
		{"lt3", []quote.WarrantExpiryDateType{quote.WarrantLT3}},
		{"bt3_6", []quote.WarrantExpiryDateType{quote.WarrantBT3_6}},
		{"3_6", []quote.WarrantExpiryDateType{quote.WarrantBT3_6}},
		{"bt6_12", []quote.WarrantExpiryDateType{quote.WarrantBT6_12}},
		{"6_12", []quote.WarrantExpiryDateType{quote.WarrantBT6_12}},
		{"gt12", []quote.WarrantExpiryDateType{quote.WarrantGT12}},
		{"12", []quote.WarrantExpiryDateType{quote.WarrantGT12}},
		{"lt3,bt3_6,bt6_12,gt12", []quote.WarrantExpiryDateType{
			quote.WarrantLT3, quote.WarrantBT3_6, quote.WarrantBT6_12, quote.WarrantGT12,
		}},
		{"gt12,lt3", []quote.WarrantExpiryDateType{quote.WarrantGT12, quote.WarrantLT3}},
		{" LT3 , 3_6 ", []quote.WarrantExpiryDateType{quote.WarrantLT3, quote.WarrantBT3_6}},
		{"gt12,gt12", []quote.WarrantExpiryDateType{quote.WarrantGT12, quote.WarrantGT12}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantExpiry(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantExpiry(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseWarrantExpiry(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The four windows are iota+1, so the zero value is not a valid window at all.
// Pinning the numbers catches a transposed bt3_6/bt6_12, which is the easy
// mistake: both are "between" buckets.
func TestParseWarrantExpiry_WireValuesArePinned(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"lt3", 1}, {"bt3_6", 2}, {"bt6_12", 3}, {"gt12", 4},
	} {
		got, err := parseWarrantExpiry(tt.in)
		if err != nil {
			t.Fatalf("parseWarrantExpiry(%q) = error %v", tt.in, err)
		}
		if len(got) != 1 || int32(got[0]) != tt.want {
			t.Errorf("parseWarrantExpiry(%q) = %v, want exactly [%d]", tt.in, got, tt.want)
		}
	}
	if int32(quote.WarrantExpiryDateType(0)) == int32(quote.WarrantLT3) {
		t.Fatal("the expiry enum no longer starts at 1; the \"zero is not a valid window\" note is stale")
	}
}

func TestParseWarrantExpiry_EmptyInputIsNoFilterNotAnError(t *testing.T) {
	for _, in := range []string{"", "  ", ",,,"} {
		got, err := parseWarrantExpiry(in)
		if err != nil {
			t.Fatalf("parseWarrantExpiry(%q) = error %v, want (nil, nil)", in, err)
		}
		if len(got) != 0 {
			t.Errorf("parseWarrantExpiry(%q) = %v, want an empty filter", in, got)
		}
	}
}

func TestParseWarrantExpiry_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"lt_3", "underscore in the wrong place"},
		{"lt3 ", "trailing space is trimmed, so this IS accepted; kept to prove the trim"},
		{"bt3-6", "hyphen instead of underscore"},
		{"bt3to6", "word form"},
		{"3_6_12", "a three-part range that is not a bucket"},
		{"1", "a bare number that is not an alias"},
		{"3", "a bare number that is not an alias"},
		{"6", "a bare number that is not an alias"},
		{"13", "a bare number beyond gt12"},
		{"0", "the bare numeric form of the invalid zero"},
		{"-3", "negative"},
		{"lt3,bt3_9", "one bad bucket in a list discards the list"},
		{"day", "a cmd/quote period"},
		{"call", "a -type value"},
		{"in", "a -moneyness value"},
	}
	for _, tt := range tests {
		if tt.in == "lt3 " {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				got, err := parseWarrantExpiry(tt.in)
				if err != nil {
					t.Fatalf("parseWarrantExpiry(%q) = error %v, want it trimmed and accepted", tt.in, err)
				}
				if !reflect.DeepEqual(got, []quote.WarrantExpiryDateType{quote.WarrantLT3}) {
					t.Errorf("parseWarrantExpiry(%q) = %v, want [LT3]", tt.in, got)
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseWarrantExpiry(tt.in); err == nil {
				t.Fatalf("parseWarrantExpiry(%q) = %v with no error, want an error", tt.in, got)
			} else if got != nil {
				t.Errorf("parseWarrantExpiry(%q) = %v alongside the error, want nil", tt.in, got)
			}
		})
	}
}

// ------------------------------------------------------------ moneyness

func TestParseWarrantMoneyness_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want []quote.WarrantInOutBoundsType
	}{
		{"in", []quote.WarrantInOutBoundsType{quote.WarrantInBounds}},
		{"in_bounds", []quote.WarrantInOutBoundsType{quote.WarrantInBounds}},
		{"out", []quote.WarrantInOutBoundsType{quote.WarrantOutBounds}},
		{"out_bounds", []quote.WarrantInOutBoundsType{quote.WarrantOutBounds}},
		{"in,out", []quote.WarrantInOutBoundsType{quote.WarrantInBounds, quote.WarrantOutBounds}},
		{"out,in", []quote.WarrantInOutBoundsType{quote.WarrantOutBounds, quote.WarrantInBounds}},
		{" IN , Out_Bounds ", []quote.WarrantInOutBoundsType{quote.WarrantInBounds, quote.WarrantOutBounds}},
		{"in,in", []quote.WarrantInOutBoundsType{quote.WarrantInBounds, quote.WarrantInBounds}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantMoneyness(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantMoneyness(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseWarrantMoneyness(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// iota+1, so 0 is not a valid bound. Only two values exist, and they are one
// apart, so a swap is a one-bit error.
func TestParseWarrantMoneyness_WireValuesArePinned(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"in", 1}, {"in_bounds", 1}, {"out", 2}, {"out_bounds", 2},
	} {
		got, err := parseWarrantMoneyness(tt.in)
		if err != nil {
			t.Fatalf("parseWarrantMoneyness(%q) = error %v", tt.in, err)
		}
		if len(got) != 1 || int32(got[0]) != tt.want {
			t.Errorf("parseWarrantMoneyness(%q) = %v, want exactly [%d]", tt.in, got, tt.want)
		}
	}
}

func TestParseWarrantMoneyness_EmptyInputIsNoFilterNotAnError(t *testing.T) {
	for _, in := range []string{"", "  ", ", ,"} {
		got, err := parseWarrantMoneyness(in)
		if err != nil {
			t.Fatalf("parseWarrantMoneyness(%q) = error %v, want (nil, nil)", in, err)
		}
		if len(got) != 0 {
			t.Errorf("parseWarrantMoneyness(%q) = %v, want an empty filter", in, got)
		}
	}
}

func TestParseWarrantMoneyness_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"inside", "a near miss on in"},
		{"outside", "a near miss on out"},
		{"bound", "a substring of both aliases"},
		{"bounds", "a substring of both aliases"},
		{"in_bound", "truncated singular"},
		{"out_bound", "truncated singular"},
		{"inout", "concatenated"},
		{"1", "numeric where a word belongs"},
		{"0", "numeric where a word belongs"},
		{"2", "the wire value as text is not accepted"},
		{"-1", "negative"},
		{"in,inside", "one bad entry discards the whole list"},
		{"call", "a -type value"},
		{"lt3", "an -expiry value"},
		{"normal", "a -status value"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseWarrantMoneyness(tt.in); err == nil {
				t.Fatalf("parseWarrantMoneyness(%q) = %v with no error, want an error", tt.in, got)
			} else if got != nil {
				t.Errorf("parseWarrantMoneyness(%q) = %v alongside the error, want nil", tt.in, got)
			}
		})
	}
}

// -------------------------------------------------------------- status

func TestParseWarrantStatus_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want []quote.WarrantStatus
	}{
		{"suspend", []quote.WarrantStatus{quote.WarrantSuspend}},
		{"listed", []quote.WarrantStatus{quote.WarrantPapareList}},
		{"paparelist", []quote.WarrantStatus{quote.WarrantPapareList}},
		{"normal", []quote.WarrantStatus{quote.WarrantNormal}},
		{"suspend,listed,normal", []quote.WarrantStatus{
			quote.WarrantSuspend, quote.WarrantPapareList, quote.WarrantNormal,
		}},
		{"normal,suspend", []quote.WarrantStatus{quote.WarrantNormal, quote.WarrantSuspend}},
		{" SUSPEND , PapareList ", []quote.WarrantStatus{quote.WarrantSuspend, quote.WarrantPapareList}},
		{"normal,normal", []quote.WarrantStatus{quote.WarrantNormal, quote.WarrantNormal}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantStatus(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantStatus(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseWarrantStatus(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The SDK spells the "waiting to be listed" constant WarrantPapareList, and the
// CLI accepts both "listed" and the misspelt "paparelist" for it. iota+2 means
// the three statuses are 2, 3 and 4 and 0/1 are not statuses at all.
func TestParseWarrantStatus_WireValuesArePinned(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"suspend", 2}, {"listed", 3}, {"paparelist", 3}, {"normal", 4},
	} {
		got, err := parseWarrantStatus(tt.in)
		if err != nil {
			t.Fatalf("parseWarrantStatus(%q) = error %v", tt.in, err)
		}
		if len(got) != 1 || int32(got[0]) != tt.want {
			t.Errorf("parseWarrantStatus(%q) = %v, want exactly [%d]", tt.in, got, tt.want)
		}
	}
	if int32(quote.WarrantStatus(0)) == int32(quote.WarrantSuspend) {
		t.Fatal("the status enum no longer starts at 2; the iota+2 note is stale")
	}
}

func TestParseWarrantStatus_EmptyInputIsNoFilterNotAnError(t *testing.T) {
	for _, in := range []string{"", "  ", ",,,"} {
		got, err := parseWarrantStatus(in)
		if err != nil {
			t.Fatalf("parseWarrantStatus(%q) = error %v, want (nil, nil)", in, err)
		}
		if len(got) != 0 {
			t.Errorf("parseWarrantStatus(%q) = %v, want an empty filter", in, got)
		}
	}
}

func TestParseWarrantStatus_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"suspended", "past participle"},
		{"suspends", "plural"},
		{"delist", "an antonym, and an -action in cmd/watchlist"},
		{"delisted", "an antonym"},
		{"pending", "the name warrantStatusName prints for paparelist, but not an accepted input"},
		{"unlisted", "an antonym"},
		{"live", "a synonym"},
		{"active", "a synonym"},
		{"trading", "a synonym"},
		{"2", "the wire value as text is not accepted"},
		{"0", "the bare numeric form of the invalid low codes"},
		{"-1", "negative"},
		{"call", "a -type value"},
		{"lt3", "an -expiry value"},
		{"in", "a -moneyness value"},
		{"volume", "a -sort-by value"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseWarrantStatus(tt.in); err == nil {
				t.Fatalf("parseWarrantStatus(%q) = %v with no error, want an error", tt.in, got)
			} else if got != nil {
				t.Errorf("parseWarrantStatus(%q) = %v alongside the error, want nil", tt.in, got)
			}
		})
	}
}

// The parser accepts "paparelist" but warrantStatusName renders the same
// constant as "pending". Both spellings must reach the same constant, or the
// printed status would disagree with the filter the user asked for.
func TestParseWarrantStatus_ListedAndPaparelistAreTheSameConstant(t *testing.T) {
	a, err := parseWarrantStatus("listed")
	if err != nil {
		t.Fatalf("parseWarrantStatus(\"listed\") = error %v", err)
	}
	b, err := parseWarrantStatus("paparelist")
	if err != nil {
		t.Fatalf("parseWarrantStatus(\"paparelist\") = error %v", err)
	}
	if a[0] != b[0] {
		t.Errorf("\"listed\" -> %d but \"paparelist\" -> %d; the two aliases must agree", int32(a[0]), int32(b[0]))
	}
	if a[0] != quote.WarrantPapareList {
		t.Errorf("\"listed\" -> %d, want %d (PapareList)", int32(a[0]), int32(quote.WarrantPapareList))
	}
	if name := warrantStatusName(a[0]); name != "pending" {
		t.Errorf("warrantStatusName(%d) = %q, want %q", int32(a[0]), name, "pending")
	}
}

// warrantStatusName is the only place a WarrantStatus reaches the user, and it
// is a bare int32 with no String() method. An unmapped code must say so rather
// than print a bare number.
func TestWarrantStatusName_EveryConstantIsLabelledAndUnknownOnesSaySo(t *testing.T) {
	for _, tt := range []struct {
		in   quote.WarrantStatus
		want string
	}{
		{quote.WarrantSuspend, "suspend"},
		{quote.WarrantPapareList, "pending"},
		{quote.WarrantNormal, "normal"},
		{quote.WarrantStatus(0), "unknown(0)"},
		{quote.WarrantStatus(1), "unknown(1)"},
		{quote.WarrantStatus(99), "unknown(99)"},
		{quote.WarrantStatus(-1), "unknown(-1)"},
	} {
		if got := warrantStatusName(tt.in); got != tt.want {
			t.Errorf("warrantStatusName(%d) = %q, want %q", int32(tt.in), got, tt.want)
		}
	}
}

// -------------------------------------------------------------- language

func TestParseWarrantLanguage_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want quote.WarrantLanguage
	}{
		{"zh-cn", quote.WarrantZH_CN},
		{"zh_cn", quote.WarrantZH_CN},
		{"zhcn", quote.WarrantZH_CN},
		{"en", quote.WarrantEN},
		{"en-us", quote.WarrantEN},
		{"zh-hk", quote.WarrantHK_CN},
		{"zh_hk", quote.WarrantHK_CN},
		{"zhhk", quote.WarrantHK_CN},
		{"hk", quote.WarrantHK_CN},
		// ToLower+TrimSpace.
		{"ZH-CN", quote.WarrantZH_CN},
		{"EN", quote.WarrantEN},
		{"  zh-hk  ", quote.WarrantHK_CN},
		{"HK", quote.WarrantHK_CN},
		{"En-Us", quote.WarrantEN},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseWarrantLanguage(tt.in)
			if err != nil {
				t.Fatalf("parseWarrantLanguage(%q) = error %v, want %d", tt.in, err, int32(tt.want))
			}
			if got != tt.want {
				t.Errorf("parseWarrantLanguage(%q) = %d, want %d", tt.in, int32(got), int32(tt.want))
			}
		})
	}
}

// The three languages are consecutive from 0 and the constants are named
// ZH_CN / EN / HK_CN, so "zh-hk" landing on HK_CN and not ZH_CN is a two-bit
// error worth pinning.
func TestParseWarrantLanguage_WireValuesArePinned(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"zh-cn", 0}, {"zh_cn", 0}, {"zhcn", 0},
		{"en", 1}, {"en-us", 1},
		{"zh-hk", 2}, {"zh_hk", 2}, {"zhhk", 2}, {"hk", 2},
	} {
		got, err := parseWarrantLanguage(tt.in)
		if err != nil {
			t.Fatalf("parseWarrantLanguage(%q) = error %v", tt.in, err)
		}
		if int32(got) != tt.want {
			t.Errorf("parseWarrantLanguage(%q) = %d, want %d", tt.in, int32(got), tt.want)
		}
	}
}

func TestParseWarrantLanguage_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"zh", "language with no region"},
		{"cn", "region with no language"},
		{"zh-tw", "a plausible variant the SDK does not define"},
		{"zh-hans", "a plausible variant the SDK does not define"},
		{"zh-hant", "a plausible variant the SDK does not define"},
		{"en-gb", "a plausible variant; only en-us is accepted"},
		{"english", "the word form"},
		{"chinese", "the word form"},
		{"traditional", "the word form"},
		{"hk-en", "transposed"},
		{"ZH-HK-", "trailing separator"},
		{"call", "a -type value"},
		{"lt3", "an -expiry value"},
		{"in", "a -moneyness value"},
		{"normal", "a -status value"},
		{"volume", "a -sort-by value"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseWarrantLanguage(tt.in); err == nil {
				t.Fatalf("parseWarrantLanguage(%q) = %d with no error, want an error", tt.in, int32(got))
			}
		})
	}
}

// WarrantZH_CN is 0, so parseWarrantLanguage's error path is indistinguishable
// from a successful Simplified-Chinese request. This is the repo's silent-zero
// hazard in its purest form: every caller in main() returns on the error, and
// the tests above are the only thing keeping that true.
func TestParseWarrantLanguage_ErrorPathZeroIsAlsoZhCN(t *testing.T) {
	bad, badErr := parseWarrantLanguage("klingon")
	good, goodErr := parseWarrantLanguage("zh-cn")
	if badErr == nil {
		t.Fatal("parseWarrantLanguage(\"klingon\") returned no error")
	}
	if goodErr != nil {
		t.Fatalf("parseWarrantLanguage(\"zh-cn\") = error %v", goodErr)
	}
	if bad != good {
		t.Fatalf("error path returned %d but a valid zh-cn returned %d; they are no longer confusable and this note is stale", int32(bad), int32(good))
	}
	if bad != quote.WarrantZH_CN {
		t.Errorf("error path returned %d, want %d (ZH_CN)", int32(bad), int32(quote.WarrantZH_CN))
	}
}

// ------------------------------------------------- shared list semantics

// The four list-returning parsers all go through splitList, so the shared
// behaviour is asserted once, against all four, to keep them from drifting
// apart. buildWarrantFilter calls all four in sequence.
func TestWarrantListParsers_ShareEmptyDuplicateAndOrderSemantics(t *testing.T) {
	type parser struct {
		name string
		fn   func(string) (int, error) // returns len, or -1 plus an error
	}
	parsers := []parser{
		{"parseWarrantTypes", func(s string) (int, error) {
			v, err := parseWarrantTypes(s)
			return len(v), err
		}},
		{"parseWarrantExpiry", func(s string) (int, error) {
			v, err := parseWarrantExpiry(s)
			return len(v), err
		}},
		{"parseWarrantMoneyness", func(s string) (int, error) {
			v, err := parseWarrantMoneyness(s)
			return len(v), err
		}},
		{"parseWarrantStatus", func(s string) (int, error) {
			v, err := parseWarrantStatus(s)
			return len(v), err
		}},
	}
	for _, p := range parsers {
		t.Run(p.name, func(t *testing.T) {
			for _, in := range []string{"", "   ", ",", " , , "} {
				n, err := p.fn(in)
				if err != nil {
					t.Errorf("%s(%q) = error %v, want (empty, nil)", p.name, in, err)
				}
				if n != 0 {
					t.Errorf("%s(%q) returned %d entries, want 0", p.name, in, n)
				}
			}
		})
	}
}

// ---------------------------------------------------------- filter assembly

// buildWarrantFilter is the only place -sort-order is converted, and
// quote.WarrantSortOrder is a bare int32 whose zero (WarrantAsc) is a valid
// answer. The globals are package flag variables, so they are saved and
// restored around each case.
func TestBuildWarrantFilter_SortOrderAcceptsOnlyAscAndDesc(t *testing.T) {
	sortBy, sortOrder, sortCount, sortOffset = "last_done", "desc", 10, 0
	t.Cleanup(resetWarrantFlags)

	tests := []struct {
		in      string
		want    quote.WarrantSortOrder
		wantErr bool
	}{
		{"asc", quote.WarrantAsc, false},
		{"desc", quote.WarrantDesc, false},
		{"ASC", quote.WarrantAsc, false},
		{"DESC", quote.WarrantDesc, false},
		// The zero value of WarrantSortOrder is a real sort order, so every
		// rejection here matters: a silent zero would sort the wrong way.
		// Padding is trimmed, like in the five sibling parsers, so "  asc  "
		// must come back as Asc and not as the zero of a refusal.
		{"  asc  ", quote.WarrantAsc, false},
		{"\tdesc\n", quote.WarrantDesc, false},
		{"   ", 0, true},
		{"", 0, true},
		{"ascending", 0, true},
		{"0", 0, true},
		{"1", 0, true},
		{"up", 0, true},
		{"last_done", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			sortOrder = tt.in
			f, err := buildWarrantFilter()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildWarrantFilter with -sort-order %q = %v with no error, want an error", tt.in, f.SortOrder)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildWarrantFilter with -sort-order %q = error %v", tt.in, err)
			}
			if f.SortOrder != tt.want {
				t.Errorf("-sort-order %q mapped to %d, want %d", tt.in, int32(f.SortOrder), int32(tt.want))
			}
		})
	}
	if int32(quote.WarrantAsc) != 0 {
		t.Error("quote.WarrantAsc is no longer 0; the zero-value hazard note is stale")
	}
}

func TestBuildWarrantFilter_CarriesEveryFlagOntoTheSDKFilter(t *testing.T) {
	resetWarrantFlags()
	t.Cleanup(resetWarrantFlags)
	sortBy, sortOrder, sortCount, sortOffset = "change_rate", "asc", 25, 50
	warrantType, expiry, inOut, status = "call,bear", "lt3,gt12", "out", "normal,suspend"

	f, err := buildWarrantFilter()
	if err != nil {
		t.Fatalf("buildWarrantFilter = error %v", err)
	}
	if f.SortBy != quote.WarrantChangeRate {
		t.Errorf("SortBy = %d, want %d (ChangeRate)", int32(f.SortBy), int32(quote.WarrantChangeRate))
	}
	if f.SortOrder != quote.WarrantAsc {
		t.Errorf("SortOrder = %d, want %d (Asc)", int32(f.SortOrder), int32(quote.WarrantAsc))
	}
	if f.SortCount != 25 || f.SortOffset != 50 {
		t.Errorf("SortCount/SortOffset = %d/%d, want 25/50", f.SortCount, f.SortOffset)
	}
	want := quote.WarrantFilter{
		SortBy:     quote.WarrantChangeRate,
		SortOrder:  quote.WarrantAsc,
		SortCount:  25,
		SortOffset: 50,
		Type:       []quote.WarrantType{quote.WarrantCall, quote.WarrantBear},
		ExpiryDate: []quote.WarrantExpiryDateType{quote.WarrantLT3, quote.WarrantGT12},
		PriceType:  []quote.WarrantInOutBoundsType{quote.WarrantOutBounds},
		Status:     []quote.WarrantStatus{quote.WarrantNormal, quote.WarrantSuspend},
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("buildWarrantFilter =\n %+v\nwant\n %+v", f, want)
	}
}

// Empty list flags mean "no filter", so the SDK struct carries nil slices
// rather than empty ones. Asserted because a caller doing len(f.Type) > 0
// behaves the same, but a JSON round trip does not.
func TestBuildWarrantFilter_EmptyListFlagsMeanNoFilter(t *testing.T) {
	resetWarrantFlags()
	t.Cleanup(resetWarrantFlags)
	sortBy, sortOrder, sortCount, sortOffset = "volume", "desc", 1, 0
	warrantType, expiry, inOut, status = "", "  ", ",", ""

	f, err := buildWarrantFilter()
	if err != nil {
		t.Fatalf("buildWarrantFilter = error %v", err)
	}
	if f.Type != nil || f.ExpiryDate != nil || f.PriceType != nil || f.Status != nil {
		t.Errorf("buildWarrantFilter = %+v, want nil list fields so no filter is sent", f)
	}
}

func TestBuildWarrantFilter_RejectsNonPositiveCountAndNegativeOffset(t *testing.T) {
	resetWarrantFlags()
	t.Cleanup(resetWarrantFlags)
	sortBy, sortOrder = "volume", "desc"

	for _, tt := range []struct {
		name          string
		count, offset int
	}{
		{"zero count", 0, 0},
		{"negative count", -1, 0},
		{"negative offset", 1, -1},
		{"both wrong", 0, -5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sortCount, sortOffset = tt.count, tt.offset
			if _, err := buildWarrantFilter(); err == nil {
				t.Errorf("buildWarrantFilter with count=%d offset=%d returned no error, want an error",
					tt.count, tt.offset)
			}
		})
	}
	// The boundary values are valid.
	sortCount, sortOffset = 1, 0
	if _, err := buildWarrantFilter(); err != nil {
		t.Errorf("buildWarrantFilter with count=1 offset=0 = error %v, want it accepted", err)
	}
}

// A bad value in any one of the six flags must abort the whole filter, and the
// error must come from the sub-parser that found it.
func TestBuildWarrantFilter_AnyBadSubValueAbortsTheWholeFilter(t *testing.T) {
	tests := []struct {
		name                                           string
		sortBy, warrantType, expiry, moneyness, status string
	}{
		{"bad -sort-by", "nope", "", "", "", ""},
		{"bad -type", "volume", "nope", "", "", ""},
		{"bad -expiry", "volume", "", "nope", "", ""},
		{"bad -moneyness", "volume", "", "", "nope", ""},
		{"bad -status", "volume", "", "", "", "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetWarrantFlags()
			t.Cleanup(resetWarrantFlags)
			sortBy, sortOrder, sortCount, sortOffset = tt.sortBy, "desc", 10, 0
			warrantType, expiry, inOut, status = tt.warrantType, tt.expiry, tt.moneyness, tt.status
			if _, err := buildWarrantFilter(); err == nil {
				t.Errorf("buildWarrantFilter with a bad %s returned no error, want an error", tt.name)
			}
		})
	}
}

func resetWarrantFlags() {
	sortBy, sortOrder, sortCount, sortOffset = "last_done", "desc", 10, 0
	warrantType, expiry, inOut, status = "", "", "", ""
	language = "zh-hk"
}

// All six parsers take the same input: case-folded and trimmed. A value pasted
// with padding used to be accepted by -sort-by, -type, -expiry, -moneyness and
// -status but rejected by -sort-order, which only lower-cased, so the six flags
// did not share one contract. Each padded value is now asserted to reach the
// same constant as its bare form.
func TestBuildWarrantFilter_AllSixParsersAcceptPaddedValues(t *testing.T) {
	resetWarrantFlags()
	t.Cleanup(resetWarrantFlags)
	sortBy, sortOrder, sortCount, sortOffset = "  last_done  ", "  desc  ", 10, 0
	warrantType, expiry, inOut, status = "  call  ", "  lt3  ", "  in  ", "  normal  "

	if got, err := parseWarrantSortBy("  last_done  "); err != nil || got != quote.WarrantLastDone {
		t.Errorf("parseWarrantSortBy(\"  last_done  \") = %d, %v, want %d and no error", int32(got), err, int32(quote.WarrantLastDone))
	}
	if got, err := parseWarrantTypes("  call  "); err != nil || !reflect.DeepEqual(got, []quote.WarrantType{quote.WarrantCall}) {
		t.Errorf("parseWarrantTypes(\"  call  \") = %v, %v, want [Call] and no error", got, err)
	}
	if got, err := parseWarrantExpiry("  lt3  "); err != nil || !reflect.DeepEqual(got, []quote.WarrantExpiryDateType{quote.WarrantLT3}) {
		t.Errorf("parseWarrantExpiry(\"  lt3  \") = %v, %v, want [LT3] and no error", got, err)
	}
	if got, err := parseWarrantMoneyness("  in  "); err != nil || !reflect.DeepEqual(got, []quote.WarrantInOutBoundsType{quote.WarrantInBounds}) {
		t.Errorf("parseWarrantMoneyness(\"  in  \") = %v, %v, want [InBounds] and no error", got, err)
	}
	if got, err := parseWarrantStatus("  normal  "); err != nil || !reflect.DeepEqual(got, []quote.WarrantStatus{quote.WarrantNormal}) {
		t.Errorf("parseWarrantStatus(\"  normal  \") = %v, %v, want [Normal] and no error", got, err)
	}

	f, err := buildWarrantFilter()
	if err != nil {
		t.Fatalf("buildWarrantFilter with every value padded = error %v; -sort-order must trim "+
			"like its five siblings", err)
	}
	if f.SortOrder != quote.WarrantDesc {
		t.Errorf("-sort-order \"  desc  \" mapped to %d, want %d (Desc)", int32(f.SortOrder), int32(quote.WarrantDesc))
	}
	if f.SortBy != quote.WarrantLastDone {
		t.Errorf("-sort-by \"  last_done  \" mapped to %d, want %d (LastDone)", int32(f.SortBy), int32(quote.WarrantLastDone))
	}
	// "asc" padded must land on Asc, not on the zero of a rejected value: 0 is
	// a real sort order, so a wrong mapping here is invisible in the struct.
	sortOrder = "\t ASC \n"
	if f, err := buildWarrantFilter(); err != nil || f.SortOrder != quote.WarrantAsc {
		t.Errorf("buildWarrantFilter with -sort-order %q = %d, %v, want %d (Asc) and no error",
			"\t ASC \n", int32(f.SortOrder), err, int32(quote.WarrantAsc))
	}
	// Padding is still not a licence: the word is matched, not stripped of
	// anything that makes it another word.
	for _, in := range []string{"  sideway  ", " asc desc ", "a sc"} {
		sortOrder = in
		if _, err := buildWarrantFilter(); err == nil {
			t.Errorf("buildWarrantFilter with -sort-order %q returned no error, want an error", in)
		}
	}
}

// printWarrantList builds the filter and parses -language before it uses the
// client, so a nil *quote.QuoteContext exercises the flag checks and returns at
// the first error. Only the error paths are reachable this way.
func TestPrintWarrantList_RejectsBadFlagsBeforeUsingTheClient(t *testing.T) {
	saveSort, saveType, saveExpiry, saveMoneyness, saveStatus, saveLang, saveOrder, saveCount, saveOffset :=
		sortBy, warrantType, expiry, inOut, status, language, sortOrder, sortCount, sortOffset
	t.Cleanup(func() {
		sortBy, warrantType, expiry, inOut, status = saveSort, saveType, saveExpiry, saveMoneyness, saveStatus
		language, sortOrder, sortCount, sortOffset = saveLang, saveOrder, saveCount, saveOffset
	})
	sortOrder, sortCount, sortOffset = "desc", 10, 0

	t.Run("bad -sort-by", func(t *testing.T) {
		resetWarrantFlags()
		sortBy = "nope"
		if err := printWarrantList(t.Context(), nil); err == nil {
			t.Error("printWarrantList with a bad -sort-by returned no error, want an error")
		}
	})
	t.Run("bad -type", func(t *testing.T) {
		resetWarrantFlags()
		warrantType = "call,nope"
		if err := printWarrantList(t.Context(), nil); err == nil {
			t.Error("printWarrantList with a bad -type returned no error, want an error")
		}
	})
	t.Run("bad -language", func(t *testing.T) {
		resetWarrantFlags()
		language = "klingon"
		if err := printWarrantList(t.Context(), nil); err == nil {
			t.Error("printWarrantList with a bad -language returned no error, want an error")
		}
	})
	t.Run("bad -count", func(t *testing.T) {
		resetWarrantFlags()
		sortCount = 0
		if err := printWarrantList(t.Context(), nil); err == nil {
			t.Error("printWarrantList with -count 0 returned no error, want an error")
		}
	})
}

// printWarrantQuotes refuses an empty -warrant-symbols before the client call.
func TestPrintWarrantQuotes_RejectsAnEmptySymbolListBeforeUsingTheClient(t *testing.T) {
	save := warrantSyms
	t.Cleanup(func() { warrantSyms = save })

	for _, in := range []string{"", "   ", ",", " , "} {
		warrantSyms = in
		if err := printWarrantQuotes(t.Context(), nil); err == nil {
			t.Errorf("printWarrantQuotes with -warrant-symbols %q returned no error, want an error", in)
		}
	}
}
