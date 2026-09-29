package main

import (
	"testing"

	"github.com/longbridge/openapi-go/trade"
)

// trade.OrderType and trade.OrderSide are string enums and trade.BalanceType is
// a bare int32, so the failure modes differ: a wrong OrderType sends a body the
// exchange rejects (or worse, accepts as a different order), a wrong
// OrderSide buys when the user asked to sell, and a wrong BalanceType silently
// filters cash-flow rows. Every accepted value is pinned to its own constant.

// trade.OrderType is a string type, so `trade.OrderType(strings.ToUpper(s))`
// alone would accept ANY word. The switch is the only thing standing between a
// typo and a live order body, so each of the 13 SDK order types is pinned.
func TestParseOrderType_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want trade.OrderType
	}{
		{"LO", trade.OrderTypeLO},
		{"ELO", trade.OrderTypeELO},
		{"MO", trade.OrderTypeMO},
		{"AO", trade.OrderTypeAO},
		{"ALO", trade.OrderTypeALO},
		{"ODD", trade.OrderTypeODD},
		{"LIT", trade.OrderTypeLIT},
		{"MIT", trade.OrderTypeMIT},
		{"TSLPAMT", trade.OrderTypeTSLPAMT},
		{"TSLPPCT", trade.OrderTypeTSLPPCT},
		{"TSMAMT", trade.OrderTypeTSMAMT},
		{"TSMPCT", trade.OrderTypeTSMPCT},
		{"SLO", trade.OrderTypeSLO},
		// ToUpper+TrimSpace: the SDK constants are already uppercase, so
		// lower-case input still returns the canonical value.
		{"lo", trade.OrderTypeLO},
		{"elo", trade.OrderTypeELO},
		{"mo", trade.OrderTypeMO},
		{"tslpamt", trade.OrderTypeTSLPAMT},
		{"  ALO  ", trade.OrderTypeALO},
		{"\tslo\n", trade.OrderTypeSLO},
		{"ElO", trade.OrderTypeELO},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseOrderType(tt.in)
			if err != nil {
				t.Fatalf("parseOrderType(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseOrderType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// "ELO" contains "LO", "ALO" contains "AO" and "AO" contains "O": a prefix or
// substring match in the switch would map a short word to the wrong order type.
func TestParseOrderType_PrefixAndSubstringNearMissesAreRejected(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"   ", "whitespace only"},
		{"E", "one letter"},
		{"L", "one letter"},
		{"SLO ", "trailing space is trimmed, so this IS accepted; kept to prove the trim"},
		{"O", "not an order type"},
		{"OL", "reversed LO"},
		{"OE", "reversed EO"},
		{"LOO", "LO with a stray character"},
		{"LEO", "transposed ELO"},
		{"LIT ", "trailing space is trimmed, so this IS accepted; kept to prove the trim"},
		{"LTI", "transposed LIT"},
		{"MUT", "transposed MIT"},
		{"TSL", "truncated trailing family"},
		{"TSLP", "truncated trailing family"},
		{"TSLPAM", "truncated trailing family"},
		{"MARKET", "the word behind MO"},
		{"limit", "word form not in the switch"},
		{"0", "numeric where a word belongs"},
		{"-1", "numeric where a word belongs"},
		{"Day", "a trade.TimeType, not an OrderType"},
		{"GTC", "a trade.TimeType, not an OrderType"},
		{"Buy", "a trade.OrderSide, not an OrderType"},
		{"cash", "a trade.BalanceType, not an OrderType"},
	}
	for _, tt := range tests {
		if tt.note == "trailing space is trimmed, so this IS accepted; kept to prove the trim" {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				if _, err := parseOrderType(tt.in); err != nil {
					t.Errorf("parseOrderType(%q) = error %v, want it trimmed and accepted", tt.in, err)
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseOrderType(tt.in); err == nil {
				t.Fatalf("parseOrderType(%q) = %q with no error, want an error", tt.in, got)
			}
		})
	}
}

// A raw type conversion would accept anything, so this asserts the switch is
// load-bearing: three nonsense words that ToUpper cannot turn into a real type.
func TestParseOrderType_RawCastingIsNotGoodEnough(t *testing.T) {
	for _, in := range []string{"XXX", "MOP", "ALOX", "BUY", "STOP", "FOK", "ELOO"} {
		if got, err := parseOrderType(in); err == nil {
			t.Errorf("parseOrderType(%q) = %q with no error, want an error", in, got)
		} else if got != "" {
			t.Errorf("parseOrderType(%q) = %q alongside the error, want the zero value \"\"", in, got)
		}
	}
}

// The two side words are two characters and differ by one letter, so a
// transposition is the classic silent bug: buy when the user asked to sell.
func TestParseSide_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want trade.OrderSide
	}{
		{"buy", trade.OrderSideBuy},
		{"sell", trade.OrderSideSell},
		// The SDK constants are mixed case ("Buy"/"Sell"), so the returned
		// value must be the canonical one, not the lower-cased input.
		{"Buy", trade.OrderSideBuy},
		{"SELL", trade.OrderSideSell},
		{"  buy  ", trade.OrderSideBuy},
		{"\tSell\n", trade.OrderSideSell},
		{"bUy", trade.OrderSideBuy},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSide(tt.in)
			if err != nil {
				t.Fatalf("parseSide(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseSide(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Lower-cased input must come back as the SDK's mixed-case constant, because
// the value is serialised into the request body verbatim.
func TestParseSide_ReturnsTheSDKMixedCaseConstantNotTheInput(t *testing.T) {
	got, err := parseSide("buy")
	if err != nil {
		t.Fatalf("parseSide(\"buy\") = error %v", err)
	}
	if got != "Buy" {
		t.Errorf("parseSide(\"buy\") = %q, want the SDK constant %q", got, "Buy")
	}
	if got, err := parseSide("sell"); err != nil || got != "Sell" {
		t.Errorf("parseSide(\"sell\") = %q, %v; want %q", got, err, "Sell")
	}
	if trade.OrderSideBuy == trade.OrderSideSell {
		t.Fatal("the two SDK side constants are equal, so a swap would be undetectable")
	}
}

func TestParseSide_UnknownValueIsAnErrorNotAnEmptySide(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"   ", "whitespace only"},
		{"b", "truncated"},
		{"buys", "plural"},
		{"buying", "gerund"},
		{"s", "truncated"},
		{"sells", "plural"},
		{"bsel", "anagram of sell"},
		{"short", "a real order concept the SDK does not model here"},
		{"hold", "a real order concept the SDK does not model here"},
		{"cover", "a real order concept the SDK does not model here"},
		{"0", "numeric where a word belongs"},
		{"1", "numeric where a word belongs"},
		{"LO", "an OrderType, not a side"},
		{"MO", "an OrderType, not a side"},
		{"cash", "a BalanceType, not a side"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseSide(tt.in)
			if err == nil {
				t.Fatalf("parseSide(%q) = %q with no error, want an error", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseSide(%q) = %q alongside the error, want the zero value \"\"", tt.in, got)
			}
		})
	}
}

// The empty string is the documented "no filter" case and maps to
// BalanceTypeUnknown, so it is accepted rather than rejected. Asserted
// explicitly: an empty -balance-type must not be an error, and a whitespace-only
// one trims to the same thing.
func TestParseBalanceType_EmptyMeansUnknownNotAnError(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		got, err := parseBalanceType(in)
		if err != nil {
			t.Fatalf("parseBalanceType(%q) = error %v, want BalanceTypeUnknown", in, err)
		}
		if got != trade.BalanceTypeUnknown {
			t.Errorf("parseBalanceType(%q) = %d, want %d (Unknown)", in, int32(got), int32(trade.BalanceTypeUnknown))
		}
	}
}

func TestParseBalanceType_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want trade.BalanceType
	}{
		{"cash", trade.BalanceTypeCash},
		{"stock", trade.BalanceTypeStock},
		{"fund", trade.BalanceTypeFund},
		{"CASH", trade.BalanceTypeCash},
		{"Stock", trade.BalanceTypeStock},
		{"  FUND  ", trade.BalanceTypeFund},
		{"\tcash\n", trade.BalanceTypeCash},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseBalanceType(tt.in)
			if err != nil {
				t.Fatalf("parseBalanceType(%q) = error %v, want %d", tt.in, err, int32(tt.want))
			}
			if got != tt.want {
				t.Errorf("parseBalanceType(%q) = %d, want %d", tt.in, int32(got), int32(tt.want))
			}
		})
	}
}

// The four constants are consecutive from 0, so a transposed case is a
// plausible-looking off-by-one filter on the cash-flow rows.
func TestParseBalanceType_TheSDKConstantsAreConsecutiveFromZero(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"", 0},
		{"cash", 1},
		{"stock", 2},
		{"fund", 3},
	} {
		got, err := parseBalanceType(tt.in)
		if err != nil {
			t.Fatalf("parseBalanceType(%q) = error %v", tt.in, err)
		}
		if int32(got) != tt.want {
			t.Errorf("parseBalanceType(%q) = %d, want %d", tt.in, int32(got), tt.want)
		}
	}
}

func TestParseBalanceType_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"csh", "transposed"},
		{"stockk", "doubled letter"},
		{"funds", "plural"},
		{"cash ", "trailing space is trimmed, so this IS accepted; kept to prove the trim"},
		{"stock balance", "words"},
		{"money", "a synonym"},
		{"shares", "a synonym"},
		{"units", "a synonym"},
		{"0", "the numeric form of Unknown is not in the switch"},
		{"1", "the numeric form of Cash is not in the switch"},
		{"2", "the numeric form of Stock is not in the switch"},
		{"-1", "negative"},
		{"all", "a word meaning \"no filter\", but not in the switch"},
		{"*", "wildcard is not supported"},
		{"buy", "a side, not a balance type"},
		{"sell", "a side, not a balance type"},
		{"LO", "an OrderType, not a balance type"},
	}
	for _, tt := range tests {
		if tt.in == "cash " {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				got, err := parseBalanceType(tt.in)
				if err != nil {
					t.Fatalf("parseBalanceType(%q) = error %v, want it trimmed and accepted", tt.in, err)
				}
				if got != trade.BalanceTypeCash {
					t.Errorf("parseBalanceType(%q) = %d, want %d (Cash)", tt.in, int32(got), int32(trade.BalanceTypeCash))
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseBalanceType(tt.in); err == nil {
				t.Fatalf("parseBalanceType(%q) = %d with no error, want an error", tt.in, int32(got))
			}
		})
	}
}

// BalanceTypeUnknown is 0, so parseBalanceType's error path returns the same
// value as the accepted empty input. This is the silent-zero case the repo's
// convention is about: the caller must check err, because a caller that only
// looks at the value sees "no filter" either way.
func TestParseBalanceType_ErrorPathIsIndistinguishableFromUnknown(t *testing.T) {
	bad, badErr := parseBalanceType("cahs")
	good, goodErr := parseBalanceType("")
	if badErr == nil {
		t.Fatal("parseBalanceType(\"cahs\") returned no error")
	}
	if goodErr != nil {
		t.Fatalf("parseBalanceType(\"\") = error %v", goodErr)
	}
	if bad != good {
		t.Errorf("error path returned %d but empty input returned %d; they are no longer confusable and this comment is stale", int32(bad), int32(good))
	}
	if bad != trade.BalanceTypeUnknown {
		t.Errorf("error path returned %d, want %d (Unknown)", int32(bad), int32(trade.BalanceTypeUnknown))
	}
}

// The four parsers in this file share a vocabulary, so a value valid for one
// must not be accepted by another. This is the "valid for a different parser
// in the same file" case, checked in bulk.
func TestExecutionsParsers_DoNotAcceptEachOthersValues(t *testing.T) {
	// -type must not take sides, balance types or time-in-force words.
	for _, in := range []string{"buy", "sell", "cash", "stock", "fund", "Day", "GTC", "GTD"} {
		if got, err := parseOrderType(in); err == nil {
			t.Errorf("parseOrderType(%q) = %q with no error, want an error", in, got)
		}
	}
	// -side must not take order types or balance types.
	for _, in := range []string{"LO", "ELO", "MO", "SLO", "cash", "stock", "fund", ""} {
		if got, err := parseSide(in); err == nil {
			t.Errorf("parseSide(%q) = %q with no error, want an error", in, got)
		}
	}
	// -balance-type must not take sides or order types.
	for _, in := range []string{"buy", "sell", "LO", "SLO", "Day", "GTC", "GTD"} {
		if got, err := parseBalanceType(in); err == nil {
			t.Errorf("parseBalanceType(%q) = %d with no error, want an error", in, int32(got))
		}
	}
}

// ------------------------------------------------- pre-client validation
//
// Each of these print functions validates its flags BEFORE it touches the
// client, so a nil *trade.TradeContext exercises the flag checks and returns at
// the first error. Only the error paths are reachable this way: a valid flag
// set would dereference the nil client and panic, which is why no case here
// lets a check pass. A future refactor that moves a check after the client call
// makes these tests panic — a loud failure that says the function now needs a
// seam, which is the correct signal.

func TestPrintMaxPurchase_ValidatesEveryFlagBeforeUsingTheClient(t *testing.T) {
	saveSym, savePrice, saveSide, saveType := symbol, price, side, orderType
	t.Cleanup(func() { symbol, price, side, orderType = saveSym, savePrice, saveSide, saveType })

	t.Run("missing -symbol", func(t *testing.T) {
		symbol, price, side, orderType = "", "", "Buy", "LO"
		if err := printMaxPurchase(t.Context(), nil); err == nil {
			t.Error("printMaxPurchase with no -symbol returned no error, want an error")
		}
	})
	t.Run("unparseable -price", func(t *testing.T) {
		symbol, side, orderType = "700.HK", "Buy", "LO"
		for _, in := range []string{"abc", "1.2.3", "1,5", "$10", "0x10"} {
			price = in
			if err := printMaxPurchase(t.Context(), nil); err == nil {
				t.Errorf("printMaxPurchase with -price %q returned no error, want an error", in)
			}
		}
	})
	t.Run("bad -type", func(t *testing.T) {
		symbol, price, orderType = "700.HK", "", "nope"
		if err := printMaxPurchase(t.Context(), nil); err == nil {
			t.Error("printMaxPurchase with a bad -type returned no error, want an error")
		}
	})
	t.Run("bad -side", func(t *testing.T) {
		symbol, price, side, orderType = "700.HK", "", "both", "LO"
		if err := printMaxPurchase(t.Context(), nil); err == nil {
			t.Error("printMaxPurchase with a bad -side returned no error, want an error")
		}
	})
}

func TestPrintOrderDetailAndMarginRatio_RequireASymbolOrOrderID(t *testing.T) {
	saveSym, saveID := symbol, orderID
	t.Cleanup(func() { symbol, orderID = saveSym, saveID })

	symbol, orderID = "700.HK", ""
	if err := printOrderDetail(t.Context(), nil); err == nil {
		t.Error("printOrderDetail with no -order-id returned no error, want an error")
	}
	symbol, orderID = "", "12345"
	if err := printMarginRatio(t.Context(), nil); err == nil {
		t.Error("printMarginRatio with no -symbol returned no error, want an error")
	}
}

func TestPrintCashFlow_ValidatesEveryFlagBeforeUsingTheClient(t *testing.T) {
	saveDays, savePage, saveSize, saveBT := days, page, pageSize, balanceType
	t.Cleanup(func() { days, page, pageSize, balanceType = saveDays, savePage, saveSize, saveBT })

	t.Run("non-positive -days", func(t *testing.T) {
		days, page, pageSize, balanceType = 0, 0, 50, ""
		if err := printCashFlow(t.Context(), nil); err == nil {
			t.Error("printCashFlow with -days 0 returned no error, want an error")
		}
		days = -1
		if err := printCashFlow(t.Context(), nil); err == nil {
			t.Error("printCashFlow with -days -1 returned no error, want an error")
		}
	})
	t.Run("negative -page", func(t *testing.T) {
		days, page, pageSize, balanceType = 7, -1, 50, ""
		if err := printCashFlow(t.Context(), nil); err == nil {
			t.Error("printCashFlow with -page -1 returned no error, want an error")
		}
	})
	t.Run("non-positive -size", func(t *testing.T) {
		days, page, pageSize, balanceType = 7, 0, 0, ""
		if err := printCashFlow(t.Context(), nil); err == nil {
			t.Error("printCashFlow with -size 0 returned no error, want an error")
		}
		pageSize = -1
		if err := printCashFlow(t.Context(), nil); err == nil {
			t.Error("printCashFlow with -size -1 returned no error, want an error")
		}
	})
	t.Run("bad -balance-type", func(t *testing.T) {
		days, page, pageSize = 7, 0, 50
		for _, in := range []string{"cahs", "all", "0", "money"} {
			balanceType = in
			if err := printCashFlow(t.Context(), nil); err == nil {
				t.Errorf("printCashFlow with -balance-type %q returned no error, want an error", in)
			}
		}
	})
}

// orZeroPrice is what makes an unset -price mean "at market", so an empty and a
// whitespace-only value both become a parseable 0 rather than a parse error.
func TestOrZeroPrice_BlankBecomesZeroAndAnythingElseIsPassedThrough(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"", "0"},
		{"   ", "0"},
		{"\t\n", "0"},
		{"0", "0"},
		{"0.0", "0.0"},
		{"123.45", "123.45"},
		{"  1.5  ", "  1.5  "},
		{"abc", "abc"},
	} {
		if got := orZeroPrice(tt.in); got != tt.want {
			t.Errorf("orZeroPrice(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
