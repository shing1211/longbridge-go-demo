package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/longbridge/openapi-go"
	"github.com/longbridge/openapi-go/market"
)

// The SDK's market enums are bare ints, so every parser in this file exists to
// turn a typo into an error instead of into a valid-looking enum constant that
// returns the wrong data. The tests therefore assert two things for every
// input: the exact constant on success, and a non-nil error on rejection.

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written, so a section heading can be asserted on. fn is allowed to panic:
// cli.Section prints before the client is touched, so that is how the print
// functions are driven far enough to emit a heading.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("creating the capture file: %v", err)
	}
	orig := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = orig }()
	fn()
	if err := f.Close(); err != nil {
		t.Fatalf("closing the capture file: %v", err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("reading the captured stdout: %v", err)
	}
	return string(b)
}

// ------------------------------------------------------------------ sections

// validSection and the dispatch table in main() must agree. This walks the
// advertised vocabulary so a section can be added to one and not the other.
// sectionList is a human-facing help string joined with ", ", so the tokens
// carry a leading space that only the -sections parser strips.
func TestValidSection_EveryAdvertisedSectionIsAccepted(t *testing.T) {
	names := strings.Split(sectionList, ",")
	if len(names) != 14 {
		t.Fatalf("sectionList advertises %d sections (%q), want 14", len(names), sectionList)
	}
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		if !validSection(n) {
			t.Errorf("validSection(%q) = false, but the section is advertised in sectionList", n)
		}
	}
}

// A section that only looks like a real one must be rejected, or the run
// silently prints less than the user asked for without saying why.
func TestValidSection_NearMissNamesAreRejected(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"broker", "prefix of three real sections"},
		{"broker-holding-week", "plausible but unsupported window"},
		{"ahpremium_intraday", "underscore instead of the hyphen"},
		{"ahpremium intraday", "space instead of the hyphen"},
		{"top_movers", "underscore instead of the hyphen"},
		{"rank-categories ", "trailing space; main() trims, validSection does not"},
		{" list", "leading space; main() trims, validSection does not"},
		{"", "empty string"},
		{"all", "the word users reach for, not a section name"},
		{"*", "wildcard is not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if validSection(tt.in) {
				t.Errorf("validSection(%q) = true, want false", tt.in)
			}
		})
	}
}

// main() lowercases and trims each entry before calling validSection, so
// validSection itself is case- and whitespace-sensitive. Pinned because the
// two layers can drift: if validSection ever gains a "Status" case, main's
// normalisation hides it and the dispatch table would run the wrong thing.
func TestValidSection_IsCaseAndWhitespaceSensitiveBecauseMainNormalises(t *testing.T) {
	for _, in := range []string{"Status", "STATUS", "Top-Movers", "Status "} {
		if validSection(in) {
			t.Errorf("validSection(%q) = true; main() would never pass this, so the case would be dead code", in)
		}
	}
}

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
		// ToUpper+TrimSpace, so the canonical constant comes back regardless
		// of how the user typed it.
		{"hk", openapi.MarketHK},
		{"us", openapi.MarketUS},
		{"cn", openapi.MarketCN},
		{"sg", openapi.MarketSG},
		{"uk", openapi.MarketUK},
		{"  HK  ", openapi.MarketHK},
		{"\tUS\n", openapi.MarketUS},
		{"hK", openapi.MarketHK},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseMarket(tt.in)
			if err != nil {
				t.Fatalf("parseMarket(%q) = error %v, want %v", tt.in, err, tt.want)
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
		{"   ", "whitespace only"},
		{"USA", "near miss on US"},
		{"HKG", "the other common spelling of Hong Kong"},
		{"hk-ex", "suffix the flag help does not promise"},
		{"H", "truncated"},
		{"HKUS", "two markets in one"},
		{"日", "non-ASCII"},
		{"day", "valid for parseAhPeriod in this same file"},
		{"desc", "valid for parseMoverSort in this same file"},
		{"1m", "valid for parseAhPeriod in this same file"},
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

// --------------------------------------------------------------- a/h period

func TestParseAhPeriod_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want market.AhPremiumPeriod
	}{
		{"1m", market.AhPremiumPeriodMin1},
		{"min1", market.AhPremiumPeriodMin1},
		{"5m", market.AhPremiumPeriodMin5},
		{"min5", market.AhPremiumPeriodMin5},
		{"15m", market.AhPremiumPeriodMin15},
		{"min15", market.AhPremiumPeriodMin15},
		{"30m", market.AhPremiumPeriodMin30},
		{"min30", market.AhPremiumPeriodMin30},
		{"60m", market.AhPremiumPeriodMin60},
		{"min60", market.AhPremiumPeriodMin60},
		{"day", market.AhPremiumPeriodDay},
		{"week", market.AhPremiumPeriodWeek},
		{"month", market.AhPremiumPeriodMonth},
		{"year", market.AhPremiumPeriodYear},
		// Case-insensitive and trimmed.
		{"DAY", market.AhPremiumPeriodDay},
		{"Month", market.AhPremiumPeriodMonth},
		{"  week  ", market.AhPremiumPeriodWeek},
		{"MIN60", market.AhPremiumPeriodMin60},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseAhPeriod(tt.in)
			if err != nil {
				t.Fatalf("parseAhPeriod(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseAhPeriod(%q) = %d, want %d (%v)", tt.in, got, int(tt.want), tt.want)
			}
		})
	}
}

// The nine constants are consecutive, so a dropped or duplicated case in the
// switch would return a plausible-looking neighbouring period. Each input is
// pinned to its own constant, not to "not zero".
func TestParseAhPeriod_NeighbouringPeriodsAreNotInterchangeable(t *testing.T) {
	order := []market.AhPremiumPeriod{
		market.AhPremiumPeriodMin1, market.AhPremiumPeriodMin5, market.AhPremiumPeriodMin15,
		market.AhPremiumPeriodMin30, market.AhPremiumPeriodMin60, market.AhPremiumPeriodDay,
		market.AhPremiumPeriodWeek, market.AhPremiumPeriodMonth, market.AhPremiumPeriodYear,
	}
	in := []string{"1m", "5m", "15m", "30m", "60m", "day", "week", "month", "year"}
	if len(order) != len(in) {
		t.Fatalf("test table and SDK constant list disagree in length")
	}
	for i, want := range order {
		got, err := parseAhPeriod(in[i])
		if err != nil {
			t.Fatalf("parseAhPeriod(%q) = error %v", in[i], err)
		}
		if got != want {
			t.Errorf("parseAhPeriod(%q) = %d, want %d", in[i], int(got), int(want))
		}
	}
	// All nine are distinct, so no two spellings can be swapped undetected.
	seen := map[market.AhPremiumPeriod]string{}
	for i, p := range order {
		if prev, dup := seen[p]; dup {
			t.Errorf("%q and %q both map to %d", prev, in[i], int(p))
		}
		seen[p] = in[i]
	}
}

func TestParseAhPeriod_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"2m", "a real proto period the CLI does not expose"},
		{"10m", "a real proto period the CLI does not expose"},
		{"1min", "the spelling cmd/quote's parsePeriod accepts"},
		{"1d", "the spelling cmd/quote's parsePeriod accepts"},
		{"d", "the spelling cmd/quote's parsePeriod accepts"},
		{"quarter", "a real proto period the CLI does not expose"},
		{"min", "bare alias without the number"},
		{"minute", "word form not in the switch"},
		{"daily", "word form not in the switch"},
		{"1m30s", "duration, not a period"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseAhPeriod(tt.in); err == nil {
				t.Fatalf("parseAhPeriod(%q) = %d with no error, want an error", tt.in, int(got))
			}
		})
	}
}

// "1M" reads as "one month" to a finance user, but ToLower makes it the
// 1-minute period. Pinned so the surprise is a documented behaviour rather
// than a bug report.
func TestParseAhPeriod_UppercaseMIsOneMinuteNotOneMonth(t *testing.T) {
	got, err := parseAhPeriod("1M")
	if err != nil {
		t.Fatalf("parseAhPeriod(%q) = error %v", "1M", err)
	}
	if got != market.AhPremiumPeriodMin1 {
		t.Errorf("parseAhPeriod(%q) = %d, want %d (Min1, because the input is lowercased)", "1M", int(got), int(market.AhPremiumPeriodMin1))
	}
}

// AhPremiumPeriodMin1 is 0, so the error return is indistinguishable from a
// valid answer. Every caller in main() returns on the error, but a caller that
// did not would silently query 1-minute klines. Asserted so the hazard is
// recorded next to the parser rather than rediscovered.
func TestParseAhPeriod_ErrorPathReturnsZeroWhichIsAlsoMin1(t *testing.T) {
	got, err := parseAhPeriod("nonsense")
	if err == nil {
		t.Fatal("parseAhPeriod(\"nonsense\") returned no error")
	}
	if int(got) != 0 {
		t.Fatalf("parseAhPeriod error path returned %d, want 0", int(got))
	}
	if got != market.AhPremiumPeriodMin1 {
		t.Fatalf("SDK no longer starts AhPremiumPeriod at 0; this hazard comment is stale and must be revisited")
	}
}

// --------------------------------------------------------- broker period

func TestParseBrokerPeriod_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want market.BrokerHoldingPeriod
	}{
		{"1", market.BrokerHoldingPeriodRct1},
		{"5", market.BrokerHoldingPeriodRct5},
		{"20", market.BrokerHoldingPeriodRct20},
		{"60", market.BrokerHoldingPeriodRct60},
		// The SDK's own api spelling is accepted too, so the flag is hard to misuse.
		{"rct_1", market.BrokerHoldingPeriodRct1},
		{"rct_5", market.BrokerHoldingPeriodRct5},
		{"rct_20", market.BrokerHoldingPeriodRct20},
		{"rct_60", market.BrokerHoldingPeriodRct60},
		// The numeric fallback runs after TrimPrefix+TrimSpace, so the api
		// spelling survives surrounding whitespace.
		{"  rct_20  ", market.BrokerHoldingPeriodRct20},
		{"  5 ", market.BrokerHoldingPeriodRct5},
		// strconv.Atoi accepts these, and they land on the same constants.
		{"01", market.BrokerHoldingPeriodRct1},
		{"+5", market.BrokerHoldingPeriodRct5},
		{"rct_05", market.BrokerHoldingPeriodRct5},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseBrokerPeriod(tt.in)
			if err != nil {
				t.Fatalf("parseBrokerPeriod(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseBrokerPeriod(%q) = %d, want %d", tt.in, int(got), int(tt.want))
			}
		})
	}
}

func TestParseBrokerPeriod_UnknownValueIsAnErrorNotZero(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"0", "plausible but not a window; 0 is also Rct1's constant"},
		{"2", "plausible small number"},
		{"10", "plausible small number"},
		{"30", "plausible number"},
		{"-5", "negative window"},
		{"5.0", "float form"},
		{"5m", "valid for parseAhPeriod in this same file"},
		{"day", "valid for parseAhPeriod in this same file"},
		{"RCT_5", "the api spelling is case-sensitive; only lowercase works"},
		{"rct_0", "api spelling of an unsupported window"},
		{"rct_rct_5", "the prefix is stripped only once"},
		{"rct_", "prefix with no number"},
		{"_5", "leading underscore"},
		{"0x5", "hex is not parsed; Atoi is base 10"},
		{"1e1", "exponent notation is not parsed"},
		{"five", "word form"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseBrokerPeriod(tt.in)
			if err == nil {
				t.Fatalf("parseBrokerPeriod(%q) = %d with no error, want an error", tt.in, int(got))
			}
			if int(got) != 0 {
				t.Errorf("parseBrokerPeriod(%q) error path returned %d, want 0", tt.in, int(got))
			}
		})
	}
}

// The four windows are consecutive from 0, so the error path's zero is a real
// constant. Same hazard as parseAhPeriod, recorded here too.
func TestParseBrokerPeriod_ErrorPathZeroIsAlsoRct1(t *testing.T) {
	got, err := parseBrokerPeriod("7")
	if err == nil {
		t.Fatal("parseBrokerPeriod(\"7\") returned no error")
	}
	if got != market.BrokerHoldingPeriodRct1 {
		t.Fatalf("BrokerHoldingPeriodRct1 is no longer 0; this hazard comment is stale and must be revisited")
	}
}

// ------------------------------------------------------------- mover sort

// parseMoverSort returns a raw uint32 that TopMovers posts to the API, so a
// wrong value is a wrong sort order rather than an error. Each word must map to
// its own code.
func TestParseMoverSort_EveryAcceptedValueMapsToItsCode(t *testing.T) {
	tests := []struct {
		in   string
		want uint32
	}{
		{"asc", 0},
		{"desc", 1},
		{"0", 0},
		{"1", 1},
		{"ASC", 0},
		{"DESC", 1},
		{"  asc  ", 0},
		{" Desc ", 1},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseMoverSort(tt.in)
			if err != nil {
				t.Fatalf("parseMoverSort(%q) = error %v, want %d", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseMoverSort(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseMoverSort_UnknownValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"2", "a plausible sort code that the API does not define"},
		{"-1", "negative"},
		{"10", "large"},
		{"ascending", "the long form"},
		{"descending", "the long form"},
		{"up", "direction word"},
		{"down", "direction word"},
		{"true", "boolean-ish"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parseMoverSort(tt.in); err == nil {
				t.Fatalf("parseMoverSort(%q) = %d with no error, want an error", tt.in, got)
			}
		})
	}
}

// 0 is a valid sort code (ascending), so the error path of parseMoverSort is
// indistinguishable from a successful "asc". This is precisely the silent-wrong
// -value the switch exists to prevent, and it is why the caller must check err.
func TestParseMoverSort_ErrorPathZeroIsAlsoAscending(t *testing.T) {
	got, err := parseMoverSort("sideways")
	if err == nil {
		t.Fatal("parseMoverSort(\"sideways\") returned no error")
	}
	if got != 0 {
		t.Errorf("parseMoverSort error path returned %d, want 0", got)
	}
	if got, err := parseMoverSort("asc"); err != nil || got != 0 {
		t.Fatalf("parseMoverSort(\"asc\") = %d, %v; the error path and a real ascending sort are indistinguishable", got, err)
	}
}

// The broker-period and mover-sort parsers both accept bare digits, so a value
// valid for one must not leak into the other. Guard against a future merge of
// the two switches.
func TestParseMoverSort_BrokerPeriodNumbersAreNotSortCodes(t *testing.T) {
	for _, in := range []string{"5", "20", "60"} {
		if got, err := parseMoverSort(in); err == nil {
			t.Errorf("parseMoverSort(%q) = %d with no error, want an error", in, got)
		}
	}
	if got, err := parseBrokerPeriod("desc"); err == nil {
		t.Errorf("parseBrokerPeriod(\"desc\") = %d with no error, want an error", int(got))
	}
}

// The SDK's BrokerHoldingPeriod is a 0-based iota (0..3) while the API's type
// parameter is rct_1/rct_5/rct_20/rct_60 — see the SDK's unexported
// BrokerHoldingPeriod.toAPIString, which does the real mapping. Pinned so a
// change to the SDK constants is caught here.
func TestBrokerHoldingPeriodConstantsAreZeroBased(t *testing.T) {
	for _, tt := range []struct {
		p       market.BrokerHoldingPeriod
		wantIdx int
	}{
		{market.BrokerHoldingPeriodRct1, 0},
		{market.BrokerHoldingPeriodRct5, 1},
		{market.BrokerHoldingPeriodRct20, 2},
		{market.BrokerHoldingPeriodRct60, 3},
	} {
		if got := int(tt.p); got != tt.wantIdx {
			t.Errorf("BrokerHoldingPeriod value = %d, want %d", got, tt.wantIdx)
		}
	}
}

// The heading has to name the window the API is asked for, not the 0-based enum
// constant. The SDK does that mapping itself for the request, in an unexported
// method, so nothing in the request was ever wrong — but formatting int(period)
// as "rct_%d" printed rct_0 for -broker-period 1, rct_1 for 5, rct_2 for 20 and
// rct_3 for 60.
//
// The heading is emitted before the client is touched, so a nil context panics
// immediately afterwards; the panic is expected and swallowed here, and the
// captured output is what is under test.
func TestPrintBrokerHoldingHeading_NamesTheAPIParameterNotTheEnumConstant(t *testing.T) {
	savePer, saveSym := brokerPer, brokerSym
	t.Cleanup(func() { brokerPer, brokerSym = savePer, saveSym })
	brokerSym = "700.HK"

	for _, tt := range []struct {
		flag string
		want string
	}{
		{"1", "rct_1"},
		{"5", "rct_5"},
		{"20", "rct_20"},
		{"60", "rct_60"},
	} {
		t.Run(tt.flag, func(t *testing.T) {
			brokerPer = tt.flag
			out := captureStdout(t, func() {
				defer func() { _ = recover() }()
				_ = printBrokerHolding(t.Context(), nil)
			})
			heading := "=== Broker holding 700.HK (" + tt.want + ") ==="
			if !strings.Contains(out, heading) {
				t.Errorf("-broker-period %s printed:\n%s\nwant a heading %q", tt.flag, out, heading)
			}
			// The old spelling is the bare constant; nothing may print it, or
			// the next section header is wrong in the same way again.
			if p, err := parseBrokerPeriod(tt.flag); err == nil {
				if stale := "rct_" + strconv.Itoa(int(p)); stale != tt.want &&
					strings.Contains(out, "(rct_"+strconv.Itoa(int(p))+")") {
					t.Errorf("-broker-period %s still prints the enum constant %q", tt.flag, stale)
				}
			}
		})
	}
}

// The mapping itself, pinned for every constant, plus the fallback: an enum the
// switch does not know must be visibly unmapped rather than rendered as a
// plausible window, since the values are 0, 1, 2, 3 and "rct_0" is not a window
// anyone asked for.
func TestBrokerPeriodAPIParam_MapsEachConstantToItsAPISpelling(t *testing.T) {
	for _, tt := range []struct {
		p    market.BrokerHoldingPeriod
		want string
	}{
		{market.BrokerHoldingPeriodRct1, "rct_1"},
		{market.BrokerHoldingPeriodRct5, "rct_5"},
		{market.BrokerHoldingPeriodRct20, "rct_20"},
		{market.BrokerHoldingPeriodRct60, "rct_60"},
	} {
		if got := brokerPeriodAPIParam(tt.p); got != tt.want {
			t.Errorf("brokerPeriodAPIParam(%d) = %q, want %q", int(tt.p), got, tt.want)
		}
	}
	if got := brokerPeriodAPIParam(market.BrokerHoldingPeriod(99)); got != "unknown(99)" {
		t.Errorf("brokerPeriodAPIParam(99) = %q, want \"unknown(99)\"", got)
	}
}

// Every window parseBrokerPeriod accepts must reach the client with the API
// spelling, so the heading cannot be right for a value the parser rejects and
// the request is right for a value the heading mislabels.
func TestBrokerPeriodAPIParam_EveryAcceptedFlagValueRendersItsAPISpelling(t *testing.T) {
	for flag, want := range map[string]string{
		"1": "rct_1", "5": "rct_5", "20": "rct_20", "60": "rct_60",
		// The parser also accepts the api spelling and the Atoi forms.
		"rct_1": "rct_1", "rct_5": "rct_5", "rct_20": "rct_20", "rct_60": "rct_60",
		"01": "rct_1", "+5": "rct_5", "rct_05": "rct_5", "  20  ": "rct_20",
	} {
		p, err := parseBrokerPeriod(flag)
		if err != nil {
			t.Fatalf("parseBrokerPeriod(%q) = error %v", flag, err)
		}
		if got := brokerPeriodAPIParam(p); got != want {
			t.Errorf("parseBrokerPeriod(%q) -> brokerPeriodAPIParam = %q, want %q", flag, got, want)
		}
	}
}

// A 32-bit platform word must not let strconv.Atoi overflow past a valid
// window, so the longest plausible digit strings are still errors.
func TestParseBrokerPeriod_LargeAndNonNumericInputIsAnError(t *testing.T) {
	for _, in := range []string{
		strconv.Itoa(1 << 31), strconv.Itoa(1 << 62), "99999999999999999999",
		"0.5", "５", "٥",
	} {
		if got, err := parseBrokerPeriod(in); err == nil {
			t.Errorf("parseBrokerPeriod(%q) = %d with no error, want an error", in, int(got))
		}
	}
}

// ------------------------------------------------- pre-client validation
//
// Each of these print functions validates its flags BEFORE it touches the
// client, so passing a nil *market.MarketContext exercises the flag checks and
// returns at the first error. Only the error paths are reachable this way: a
// valid flag set would dereference the nil client and panic, which is why no
// case here lets a check pass. If a future refactor moves a check after the
// client call, these tests panic rather than pass — the loud failure is the
// point, and the fix is to give the function a seam.

// parseBrokerPeriod is the first thing printBrokerHolding does, so a bad
// -broker-period is reported before the client is ever used.
func TestPrintBrokerHolding_RejectsABadPeriodBeforeUsingTheClient(t *testing.T) {
	save := brokerPer
	t.Cleanup(func() { brokerPer = save })

	for _, in := range []string{"", "2", "rct_7", "five", "  "} {
		brokerPer = in
		if err := printBrokerHolding(t.Context(), nil); err == nil {
			t.Errorf("printBrokerHolding with -broker-period %q returned no error, want an error", in)
		}
	}
}

// NOT COVERABLE: the "-broker-id is required for the broker-holding-daily
// section" and "-rank-key is required for the rank-list section" checks live in
// main(), not in the print functions, so there is no seam to reach them without
// running main() — which parses os.Args and calls cli.Fail/os.Exit. Asserted
// here as documentation: the two print functions really do have no such check,
// so the safety depends entirely on main() running first.
func TestPrintBrokerHoldingDailyAndRankList_HaveNoFlagCheckOfTheirOwn(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"printBrokerHoldingDaily with no -broker-id",
			func() error { return printBrokerHoldingDaily(t.Context(), nil) }},
		{"printRankList with no -rank-key",
			func() error { return printRankList(t.Context(), nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A nil client panics rather than returning a flag error, which is
			// the proof that the check does not live in the function.
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s did not panic on a nil client, so it must have its own flag check; update this test", tc.name)
				}
			}()
			_ = tc.call()
		})
	}
}

// printTopMovers checks the sort word, then that -mover-markets is non-empty,
// then that -mover-date is a real date. All three precede the client call.
func TestPrintTopMovers_ValidatesFlagsBeforeUsingTheClient(t *testing.T) {
	saveSort, saveMkts, saveDate := moversSort, moversMkts, moversDate
	t.Cleanup(func() { moversSort, moversMkts, moversDate = saveSort, saveMkts, saveDate })
	moversMkts, moversDate = "HK", ""

	t.Run("bad sort word", func(t *testing.T) {
		moversSort = "sideways"
		if err := printTopMovers(t.Context(), nil); err == nil {
			t.Error("printTopMovers with a bad -mover-sort returned no error, want an error")
		}
	})
	t.Run("empty -mover-markets", func(t *testing.T) {
		moversSort = "desc"
		for _, in := range []string{"", "   ", ",", " , "} {
			moversMkts = in
			if err := printTopMovers(t.Context(), nil); err == nil {
				t.Errorf("printTopMovers with -mover-markets %q returned no error, want an error", in)
			}
		}
	})
	t.Run("bad -mover-date", func(t *testing.T) {
		moversMkts = "HK"
		for _, in := range []string{"2026-13-01", "01-01-2026", "2026/01/01", "yesterday", "2026-1-1"} {
			moversDate = in
			if err := printTopMovers(t.Context(), nil); err == nil {
				t.Errorf("printTopMovers with -mover-date %q returned no error, want an error", in)
			}
		}
	})
	t.Run("a valid date is not rejected by the date check", func(t *testing.T) {
		moversDate = "2026-05-05"
		// This one is well-formed, so the call proceeds to the client and
		// panics on nil; that is the documented limit of this technique.
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected a nil-client panic once every flag check passed; the checks are no longer before the client")
			}
		}()
		_ = printTopMovers(t.Context(), nil)
	})
}

// printCalendar and printAnomaly both call parseMarket first, so a bad
// -market is refused without a connection.
func TestPrintCalendarAndAnomaly_RejectABadMarketBeforeUsingTheClient(t *testing.T) {
	save := marketCode
	t.Cleanup(func() { marketCode = save })

	for _, in := range []string{"", "USA", "hk-ex", "  "} {
		marketCode = in
		if err := printCalendar(t.Context(), nil); err == nil {
			t.Errorf("printCalendar with -market %q returned no error, want an error", in)
		}
		anomalyMkt := in
		if err := printAnomaly(t.Context(), nil); err == nil {
			t.Errorf("printAnomaly with -anomaly-market %q returned no error, want an error", anomalyMkt)
		}
	}
}

// printAhPremium parses the period first, so a bad -ah-period is refused
// without a connection.
func TestPrintAhPremium_RejectsABadPeriodBeforeUsingTheClient(t *testing.T) {
	save := ahPeriod
	t.Cleanup(func() { ahPeriod = save })

	for _, in := range []string{"", "2m", "quarter", "1min", "  "} {
		ahPeriod = in
		if err := printAhPremium(t.Context(), nil); err == nil {
			t.Errorf("printAhPremium with -ah-period %q returned no error, want an error", in)
		}
	}
}

// ------------------------------------------------------- trade status flags
//
// Every predicate here is a pure function of a TradeStatus value with no
// receiver state, so the whole matrix is assertable from hand-built statuses —
// no client, no fixtures, no network. Two things are worth pinning, and they are
// different things:
//
//   - what each predicate answers, for every state the SDK can produce. The
//     section prints these, so a predicate that quietly changes meaning turns
//     the output into a lie, and the tests are the only thing that notices.
//   - which of them are *defined* in terms of each other. Those are asserted as
//     identities over the whole code range rather than per state, because that
//     is what the SDK source actually guarantees. Nothing here asserts a
//     relationship the source does not state; where two predicates merely happen
//     to agree on today's constants, the table records the pair and no identity
//     is claimed.

const (
	flagIsTrading    = "IsTrading"
	flagAllowTrading = "AllowTrading"
	flagIsUSMarket   = "IsUSMarket"
	flagIsUSPrePost  = "IsUSPrePost"
	flagIsUSPre      = "IsUSPreMarket"
	flagIsUSPost     = "IsUSPostMarket"
	flagIsUSNight    = "IsUSNight"
	flagIsClosing    = "IsClosing"
	flagIsUSClosing  = "IsUSClosing"
	flagIsDark       = "IsDark"
	flagIsSpecial    = "IsSpecial"
	flagNormalize    = "Normalize"
)

// tradeStatusCodeRange is the sweep used by the identity tests. Every constant
// in the SDK's code table is inside it, including the two below 0, and it is
// wide enough that a constant added near either end of the table would be
// swept rather than skipped.
const (
	tradeStatusSweepLo = -2000
	tradeStatusSweepHi = 3000
)

// statusFlagSet is the rendered flag list for one status: each SDK method by
// name and the boolean it returned (Normalize is not a predicate, so it has no
// entry here and is covered by its own test).
func statusFlagSet(s market.TradeStatus) map[string]bool {
	return map[string]bool{
		flagIsTrading:    s.IsTrading(),
		flagAllowTrading: s.AllowTrading(),
		flagIsUSMarket:   s.IsUSMarket(),
		flagIsUSPrePost:  s.IsUSPrePost(),
		flagIsUSPre:      s.IsUSPreMarket(),
		flagIsUSPost:     s.IsUSPostMarket(),
		flagIsUSNight:    s.IsUSNight(),
		flagIsClosing:    s.IsClosing(),
		flagIsUSClosing:  s.IsUSClosing(),
		flagIsDark:       s.IsDark(),
		flagIsSpecial:    s.IsSpecial(),
	}
}

func TestTradeStatusPredicates_EachStateAnswersEveryPredicate(t *testing.T) {
	tests := []struct {
		name   string
		status market.TradeStatus
		want   map[string]bool
	}{
		{
			name:   "US pre-market",
			status: market.TradeStatusUSPrev,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				flagIsUSPrePost: true, flagIsUSPre: true, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			// 206 is the quote engine's spelling of 201, but the predicates read
			// the raw value, so it must look like a pre-market all the same.
			name:   "US clearing+pre-market alias",
			status: market.TradeStatusUSClean,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				flagIsUSPrePost: true, flagIsUSPre: true, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US regular session",
			status: market.TradeStatusUSTrading,
			want: map[string]bool{
				flagIsTrading: true, flagAllowTrading: true, flagIsUSMarket: true,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US post-market clearing alias",
			status: market.TradeStatusUSAfterMarketClean,
			want: map[string]bool{
				flagIsTrading: true, flagAllowTrading: true, flagIsUSMarket: true,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US post-market",
			status: market.TradeStatusUSAfter,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				flagIsUSPrePost: true, flagIsUSPre: false, flagIsUSPost: true,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US overnight",
			status: market.TradeStatusUSNight,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				// Overnight is neither pre nor post as the SDK defines them,
				// so IsUSPrePost is false here even though the market is open
				// for extended hours. This is the row that would look like a
				// bug if it were not printed and pinned.
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: true, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US closed",
			status: market.TradeStatusUSClosing,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: true, flagIsUSClosing: true,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US pre-market clearing alias",
			status: market.TradeStatusUSPrevMarketClean,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: true, flagIsUSClosing: true,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			// The one state that separates IsClosing from IsUSClosing in the
			// other direction: closed, but not a US close.
			name:   "half-day close",
			status: market.TradeStatusHalfClosing,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: true, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "US halted",
			status: market.TradeStatusUSStop,
			want: map[string]bool{
				// A US market and a special status at once: IsSpecial is a
				// code-range test with an explicit halt, not "not regular".
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: true,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: true,
			},
		},
		{
			name:   "regular session",
			status: market.TradeStatusTrading,
			want: map[string]bool{
				flagIsTrading: true, flagAllowTrading: true, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			// The pair that separates IsTrading from AllowTrading: an auction is
			// order-accepting but not regular trading.
			name:   "opening auction",
			status: market.TradeStatusOpenBid,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: true, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "mid-day break",
			status: market.TradeStatusNoonClosing,
			want: map[string]bool{
				// Orders are still accepted during the break, so IsTrading
				// false + AllowTrading true is a real state, not a contradiction.
				flagIsTrading: false, flagAllowTrading: true, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "closing auction",
			status: market.TradeStatusCloseBid,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: true, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "waiting to open under special conditions",
			status: market.TradeStatusNotOpened,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: true, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			// Name() says "Closed" and IsClosing() says false: 101 is the
			// alias that normalises to 108, and the predicates read the raw
			// code. Printed side by side, that disagreement is visible instead
			// of being something a reader has to guess at.
			name:   "clearing before the open",
			status: market.TradeStatusClean,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "closed",
			status: market.TradeStatusClosing,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: true, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "dark wait",
			status: market.TradeStatusDarkWait,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: true, flagIsSpecial: false,
			},
		},
		{
			name:   "dark trading",
			status: market.TradeStatusDarkTrading,
			want: map[string]bool{
				// Dark and trading, but not IsTrading — the venue is what the
				// predicate is about.
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: true, flagIsSpecial: false,
			},
		},
		{
			name:   "dark closed",
			status: market.TradeStatusDarkClosing,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: true, flagIsSpecial: false,
			},
		},
		{
			name:   "after-hours fixed price",
			status: market.TradeStatusAfterFix,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "temporary intraday break",
			status: market.TradeStatusRealtimeQuote,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: false,
			},
		},
		{
			name:   "delisted",
			status: market.TradeStatusDelist,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: true,
			},
		},
		{
			name:   "halted",
			status: market.TradeStatusStop,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: true,
			},
		},
		{
			name:   "quote not registered",
			status: market.TradeStatusNoRegisterQuote,
			want: map[string]bool{
				// No predicate is true except IsSpecial, which is a code-range
				// test and so says "special" about a status we do not have.
				// This is why an absent status is not printed as flags.
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: true,
			},
		},
		{
			name:   "unknown code",
			status: market.TradeStatusUnknown,
			want: map[string]bool{
				flagIsTrading: false, flagAllowTrading: false, flagIsUSMarket: false,
				flagIsUSPrePost: false, flagIsUSPre: false, flagIsUSPost: false,
				flagIsUSNight: false, flagIsClosing: false, flagIsUSClosing: false,
				flagIsDark: false, flagIsSpecial: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusFlagSet(tt.status)
			for _, name := range []string{
				flagIsTrading, flagAllowTrading, flagIsUSMarket, flagIsUSPrePost,
				flagIsUSPre, flagIsUSPost, flagIsUSNight, flagIsClosing,
				flagIsUSClosing, flagIsDark, flagIsSpecial,
			} {
				want, ok := tt.want[name]
				if !ok {
					t.Fatalf("test case %q has no expectation for %s", tt.name, name)
				}
				if got[name] != want {
					t.Errorf("%s(code %d).%s = %v, want %v", tt.name, tt.status.Code(), name, got[name], want)
				}
			}
		})
	}
}

// The two identities the SDK's source actually defines, swept over every code
// in and around the SDK's table rather than over the handful of states above:
// a new constant landing between two of them would otherwise be unchecked.
func TestTradeStatusPredicates_OverlappingPredicatesAgreeByDefinition(t *testing.T) {
	for code := tradeStatusSweepLo; code <= tradeStatusSweepHi; code++ {
		s := market.TradeStatusFromCode(int32(code))
		if s.IsUSPrePost() != (s.IsUSPreMarket() || s.IsUSPostMarket()) {
			t.Errorf("code %d: IsUSPrePost = %v, but IsUSPreMarket || IsUSPostMarket = %v",
				code, s.IsUSPrePost(), s.IsUSPreMarket() || s.IsUSPostMarket())
		}
		// IsClosing's body lists every code IsUSClosing lists, plus two, so
		// IsUSClosing can never be true while IsClosing is false.
		if s.IsUSClosing() && !s.IsClosing() {
			t.Errorf("code %d: IsUSClosing = true while IsClosing = false", code)
		}
	}
}

// The reverse of the previous identity is *not* an SDK guarantee and is
// deliberately not asserted as one: a state could be added to IsClosing without
// being a US close. What is asserted is that the two can disagree in that
// direction today, so nobody later "fixes" the output by printing IsClosing
// under the IsUSClosing heading.
func TestTradeStatusPredicates_IsClosingCanBeTrueWhileIsUSClosingIsFalse(t *testing.T) {
	for _, s := range []market.TradeStatus{market.TradeStatusClosing, market.TradeStatusHalfClosing} {
		if !s.IsClosing() {
			t.Errorf("code %d: IsClosing = false, want true", s.Code())
		}
		if s.IsUSClosing() {
			t.Errorf("code %d: IsUSClosing = true, want false; the two are no longer distinguishable", s.Code())
		}
	}
}

// A code that the SDK does not define must not answer any session question.
// This is the shape of the absent case with a positive code attached, and it is
// why a 4xx the SDK later adds is a "no status" rather than a wrong one.
func TestTradeStatusPredicates_UnrecognisedCodeIsSpecialButNotTrading(t *testing.T) {
	for _, code := range []int32{456, 999, 3, 100} {
		s := market.TradeStatusFromCode(code)
		if !s.IsSpecial() {
			t.Errorf("code %d: IsSpecial = false, want true", code)
		}
		if s.IsTrading() || s.AllowTrading() || s.IsClosing() || s.IsDark() {
			t.Errorf("code %d: answers a session question for a status the SDK does not define", code)
		}
	}
}

// Normalize is not a predicate but is printed, so its output is pinned against
// the SDK's own alias table: the header shows the raw code and the line shows
// where it folds to, and a reader comparing the two needs them to be the codes
// the SDK would use.
func TestStatusFlags_NormalizePrintsTheFoldedCodeAndSaysWhetherItMoved(t *testing.T) {
	tests := []struct {
		status    market.TradeStatus
		wantValue string
		wantMoved bool
	}{
		{market.TradeStatusUSTrading, "202", false},
		{market.TradeStatusUSAfterMarketClean, "202", true},
		{market.TradeStatusUSClean, "201", true},
		{market.TradeStatusUSPrevMarketClean, "204", true},
		{market.TradeStatusClean, "108", true},
		{market.TradeStatusUSNight, "207", false},
	}
	for _, tt := range tests {
		flags := statusFlags(tt.status)
		var got *statusFlag
		for i := range flags {
			if flags[i].name == flagNormalize {
				got = &flags[i]
			}
		}
		if got == nil {
			t.Errorf("statusFlags(code %d) has no %s line", tt.status.Code(), flagNormalize)
			continue
		}
		if got.value != tt.wantValue {
			t.Errorf("code %d: %s = %q, want %q", tt.status.Code(), flagNormalize, got.value, tt.wantValue)
		}
		moved := got.note != "already a display status"
		if moved != tt.wantMoved {
			t.Errorf("code %d: %s note = %q (moved=%v), want moved=%v",
				tt.status.Code(), flagNormalize, got.note, moved, tt.wantMoved)
		}
	}
}

// Every predicate the section prints must come from the SDK method of the same
// name, and no name may be printed twice or invented. A typo would otherwise
// only be caught by a test that happens to know the typo.
func TestStatusFlags_PrintsEachPredicateOnceUnderItsOwnSDKName(t *testing.T) {
	want := map[string]bool{
		flagIsTrading: true, flagAllowTrading: true, flagIsUSMarket: true,
		flagIsUSPrePost: true, flagIsUSPre: true, flagIsUSPost: true,
		flagIsUSNight: true, flagIsClosing: true, flagIsUSClosing: true,
		flagIsDark: true, flagIsSpecial: true, flagNormalize: true,
		// Already in the table above the block: printing them here too would
		// be duplication, not coverage. Name() is called by the block header
		// rather than as a flag, and String() is never printed at all.
		"Code": false, "Label": false, "Name": false, "String": false,
	}
	seen := map[string]int{}
	for _, f := range statusFlags(market.TradeStatusUSTrading) {
		if f.name == "" || f.value == "" || f.note == "" || f.group == "" {
			t.Errorf("flag %+v has an empty field; an unlabelled row is unreadable", f)
		}
		if _, dup := seen[f.name]; dup {
			t.Errorf("%s is printed %d times", f.name, seen[f.name]+1)
		}
		seen[f.name]++
		if _, listed := want[f.name]; !listed {
			t.Errorf("%s is not one of the section's flags", f.name)
		}
	}
	for name, listed := range want {
		if listed && seen[name] == 0 {
			t.Errorf("%s is never printed", name)
		}
		if !listed && seen[name] != 0 {
			t.Errorf("%s is printed here but already appears in the status table", name)
		}
	}
}

// The flags are the SDK's answer for the status it was given. A block whose
// IsTrading disagrees with the SDK would make the whole section a liar, and
// the table above would contradict the block directly beneath it.
func TestStatusFlags_EachValueIsTheSDKMethodsAnswerForThatStatus(t *testing.T) {
	for _, s := range []market.TradeStatus{
		market.TradeStatusUSPrev, market.TradeStatusUSTrading,
		market.TradeStatusUSAfter, market.TradeStatusUSNight,
		market.TradeStatusUSClosing, market.TradeStatusHalfClosing,
		market.TradeStatusTrading, market.TradeStatusOpenBid,
		market.TradeStatusDarkTrading, market.TradeStatusUSStop,
	} {
		want := statusFlagSet(s)
		for _, f := range statusFlags(s) {
			expected, ok := want[f.name]
			if !ok {
				continue // Normalize, covered above.
			}
			if f.value != yn(expected) {
				t.Errorf("code %d: %s printed as %q, want %q", s.Code(), f.name, f.value, yn(expected))
			}
		}
	}
}

// ------------------------------------------------------------ status rendering

// lineWithName returns the first line that carries name as a whole field, so a
// name that is a substring of another (IsClosing of IsUSClosing) cannot match
// the wrong row.
func lineWithName(lines []string, name string) string {
	for _, l := range lines {
		for _, f := range strings.Fields(l) {
			if f == name {
				return l
			}
		}
	}
	return ""
}

// flagValue is the second field of a flag line: the predicate name, then the
// SDK's answer, then the note.
func flagValue(line, name string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

// The two "closed" rows are where the rendering could quietly collapse one
// predicate into the other. IsClosing true with IsUSClosing false is the case
// that proves they are two rows and not one row printed twice.
func TestStatusStatusLines_ClosingRowsAreNotCollapsedIntoEachOther(t *testing.T) {
	for _, tt := range []struct {
		status          market.TradeStatus
		wantIsClosing   string
		wantIsUSClosing string
	}{
		{market.TradeStatusUSClosing, "true", "true"},
		{market.TradeStatusUSPrevMarketClean, "true", "true"},
		{market.TradeStatusClosing, "true", "false"},
		{market.TradeStatusHalfClosing, "true", "false"},
		{market.TradeStatusUSTrading, "false", "false"},
	} {
		lines := statusStatusLines("US", "status", tt.status)
		for _, tc := range []struct{ name, want string }{
			{flagIsClosing, tt.wantIsClosing},
			{flagIsUSClosing, tt.wantIsUSClosing},
		} {
			got := flagValue(lineWithName(lines, tc.name), tc.name)
			if got != tc.want {
				t.Errorf("code %d: %s printed as %q, want %q\n%s",
					tt.status.Code(), tc.name, got, tc.want, strings.Join(lines, "\n"))
			}
		}
	}
}

// A status the API supplied gets a header with its code and its full name, and
// one line per predicate. Name() is in the header rather than as a flag
// because it is a name, not an answer, and because it fills the gap Label()
// leaves for the codes it declines to label.
func TestStatusStatusLines_PresentStatusPrintsHeaderAndEveryFlag(t *testing.T) {
	lines := statusStatusLines("HK", "status", market.TradeStatusTrading)
	joined := strings.Join(lines, "\n")

	if len(lines) == 0 {
		t.Fatal("a supplied status printed no lines")
	}
	if want := "code 105 (Trading)"; !strings.Contains(joined, want) {
		t.Errorf("block does not name the status:\n%s\nwant a header containing %q", joined, want)
	}
	// Grouped, not alphabetical: the grouping carries the meaning.
	wantOrder := []string{"session", "us hours", "closed", "other"}
	at := -1
	for _, g := range wantOrder {
		i := strings.Index(joined, "\n    "+g+"\n")
		if i < 0 {
			t.Fatalf("no %q group header in:\n%s", g, joined)
		}
		if i <= at {
			t.Errorf("group %q is out of order in:\n%s", g, joined)
		}
		at = i
	}
	for _, name := range []string{
		flagIsTrading, flagAllowTrading, flagIsUSMarket, flagIsUSPrePost,
		flagIsUSPre, flagIsUSPost, flagIsUSNight, flagIsClosing,
		flagIsUSClosing, flagIsDark, flagIsSpecial, flagNormalize,
	} {
		if lineWithName(lines, name) == "" {
			t.Errorf("no line for %s in:\n%s", name, joined)
		}
	}
	// A note that states what the predicate tests, so the row cannot be read as
	// more than it is.
	if !strings.Contains(joined, "orders accepted") {
		t.Errorf("no note for AllowTrading in:\n%s", joined)
	}
}

// A status the API did not supply prints a dash and a sentence, and no
// predicate at all. Printing a row of falses would claim the market is closed;
// printing the flags would be worse still, because IsSpecial is true for every
// code below 100 and would call a market we know nothing about "special".
func TestStatusStatusLines_AbsentStatusRendersADashAndNoPredicates(t *testing.T) {
	for _, s := range []market.TradeStatus{market.TradeStatusNoRegisterQuote, market.TradeStatusUnknown} {
		lines := statusStatusLines("HK", "status", s)
		joined := strings.Join(lines, "\n")

		if len(lines) != 1 {
			t.Errorf("code %d: printed %d lines, want exactly the absent note:\n%s", s.Code(), len(lines), joined)
		}
		if !strings.Contains(joined, "status: - (no status supplied") {
			t.Errorf("code %d: absent status is not rendered as a dash:\n%s", s.Code(), joined)
		}
		if !strings.Contains(joined, "no status supplied") {
			t.Errorf("code %d: absent status does not say why:\n%s", s.Code(), joined)
		}
		for _, name := range []string{
			flagIsTrading, flagAllowTrading, flagIsUSMarket, flagIsUSPrePost,
			flagIsUSPre, flagIsUSPost, flagIsUSNight, flagIsClosing,
			flagIsUSClosing, flagIsDark, flagIsSpecial,
		} {
			if strings.Contains(joined, name) {
				t.Errorf("code %d: an absent status printed the %s predicate", s.Code(), name)
			}
		}
		if strings.Contains(joined, "false") || strings.Contains(joined, "true") {
			t.Errorf("code %d: an absent status printed a boolean:\n%s", s.Code(), joined)
		}
	}
}

// Absent is exactly the two "no status" codes, and nothing else. A status that
// merely declines to be labelled (dark, the auctions) is still a status, and
// calling it absent would hide the very states the section exists to show.
func TestStatusAbsent_OnlyTheTwoNoStatusCodesCountAsAbsent(t *testing.T) {
	for _, s := range []market.TradeStatus{
		market.TradeStatusNoRegisterQuote, market.TradeStatusUnknown,
	} {
		if !statusAbsent(s) {
			t.Errorf("statusAbsent(code %d) = false, want true", s.Code())
		}
	}
	for _, s := range []market.TradeStatus{
		market.TradeStatusUSTrading, market.TradeStatusUSPrev, market.TradeStatusUSAfter,
		market.TradeStatusUSNight, market.TradeStatusUSClosing, market.TradeStatusTrading,
		market.TradeStatusDarkWait, market.TradeStatusDarkTrading, market.TradeStatusDarkClosing,
		market.TradeStatusOpenBid, market.TradeStatusCloseBid, market.TradeStatusNoonClosing,
		market.TradeStatusAfterFix, market.TradeStatusRealtimeQuote, market.TradeStatusNotOpened,
		market.TradeStatusClean, market.TradeStatusClosing, market.TradeStatusHalfClosing,
		market.TradeStatusUSStop, market.TradeStatusDelist, market.TradeStatusFuse,
	} {
		if statusAbsent(s) {
			t.Errorf("statusAbsent(code %d) = true, want false", s.Code())
		}
	}
}

// The live and the delayed status are two different answers and must stay two
// blocks. Rendering the delay status in the live block — the easy refactor,
// since the template is shared — would be a completely wrong answer rather
// than a slightly wrong one.
func TestStatusDetailLines_LiveAndDelayStatusAreSeparateBlocks(t *testing.T) {
	lines := statusDetailLines(market.MarketTimeItem{
		Market:           "US",
		TradeStatus:      market.TradeStatusUSPrev,
		DelayTradeStatus: market.TradeStatusUSTrading,
	})

	live, delay := splitStatusBlocks(t, lines)

	if !strings.Contains(live, "code 201 (Pre-Market)") {
		t.Errorf("live block does not carry the live status:\n%s", live)
	}
	if !strings.Contains(delay, "code 202 (Trading)") {
		t.Errorf("delay block does not carry the delay status:\n%s", delay)
	}
	if !strings.Contains(live, "status: code 201") {
		t.Errorf("live block is not labelled as the status:\n%s", live)
	}
	if !strings.Contains(delay, "delay status: code 202") {
		t.Errorf("delay block is not labelled as the delay status:\n%s", delay)
	}
	// The values have to belong to their own block, not merely be present
	// somewhere in the output.
	for _, tc := range []struct {
		block, name, want string
	}{
		{live, flagIsTrading, "false"},
		{live, flagIsUSPre, "true"},
		{live, flagIsUSPrePost, "true"},
		{delay, flagIsTrading, "true"},
		{delay, flagIsUSPre, "false"},
		{delay, flagIsUSPrePost, "false"},
	} {
		got := flagValue(lineWithName(strings.Split(tc.block, "\n"), tc.name), tc.name)
		if got != tc.want {
			t.Errorf("block containing %q: %s = %q, want %q\n%s",
				strings.Fields(tc.block)[1], tc.name, got, tc.want, tc.block)
		}
	}
}

// Both statuses absent, one absent and one present, and the reverse: each case
// must fall back independently, because the API does not supply them together.
func TestStatusDetailLines_EachBlockFallsBackOnItsOwnStatus(t *testing.T) {
	tests := []struct {
		name             string
		live             market.TradeStatus
		delay            market.TradeStatus
		wantLive         string
		wantDelay        string
		wantLiveHasFlags bool
	}{
		{
			name: "neither supplied", live: market.TradeStatusNoRegisterQuote,
			delay:     market.TradeStatusUnknown,
			wantLive:  "status: - (no status supplied, code 0)",
			wantDelay: "delay status: - (no status supplied, code -1)",
		},
		{
			name: "only the live status", live: market.TradeStatusUSTrading,
			delay:            market.TradeStatusNoRegisterQuote,
			wantLive:         "status: code 202 (Trading)",
			wantDelay:        "delay status: - (no status supplied, code 0)",
			wantLiveHasFlags: true,
		},
		{
			name: "only the delay status", live: market.TradeStatusNoRegisterQuote,
			delay:     market.TradeStatusUSTrading,
			wantLive:  "status: - (no status supplied, code 0)",
			wantDelay: "delay status: code 202 (Trading)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := statusDetailLines(market.MarketTimeItem{
				Market: "US", TradeStatus: tt.live, DelayTradeStatus: tt.delay,
			})
			live, delay := splitStatusBlocks(t, lines)
			if !strings.Contains(live, tt.wantLive) {
				t.Errorf("live block:\n%s\nwant it to contain %q", live, tt.wantLive)
			}
			if !strings.Contains(delay, tt.wantDelay) {
				t.Errorf("delay block:\n%s\nwant it to contain %q", delay, tt.wantDelay)
			}
			// A present block carries its predicates; an absent one carries no
			// boolean at all.
			for _, tc := range []struct {
				block    string
				wantFlag bool
			}{
				{live, tt.wantLiveHasFlags},
				{delay, !tt.wantLiveHasFlags && strings.Contains(tt.wantDelay, "status: code ")},
			} {
				hasFlag := lineWithName(strings.Split(tc.block, "\n"), flagIsTrading) != ""
				if hasFlag != tc.wantFlag {
					t.Errorf("block %q printed the %s line: %v, want %v\n%s",
						strings.Fields(tc.block)[0], flagIsTrading, hasFlag, tc.wantFlag, tc.block)
				}
			}
		})
	}
}

// splitStatusBlocks returns the live block and the delay block of a rendered
// detail, failing the test if either is missing or if a line names both. A
// block runs from its "status:"/"delay status:" header to the next header, so
// the predicate lines below a header are attributed to it.
func splitStatusBlocks(t *testing.T, lines []string) (live, delay string) {
	t.Helper()
	var liveLines, delayLines []string
	var cur *[]string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "delay status:"):
			if strings.Count(l, "status:") != 1 {
				t.Errorf("line %q names more than one status", l)
			}
			delayLines = append(delayLines, l)
			cur = &delayLines
		case strings.Contains(l, "status:"):
			liveLines = append(liveLines, l)
			cur = &liveLines
		default:
			if cur == nil {
				t.Fatalf("line %q belongs to no block:\n%s", l, strings.Join(lines, "\n"))
			}
			*cur = append(*cur, l)
		}
	}
	if len(liveLines) == 0 || len(delayLines) == 0 {
		t.Fatalf("want a live block and a delay block, got:\n%s", strings.Join(lines, "\n"))
	}
	return strings.Join(liveLines, "\n"), strings.Join(delayLines, "\n")
}

// ------------------------------------------- market.TradeStatus.UnmarshalJSON
//
// The one thing the SDK's coverage check cannot see, and the reason this
// section exists. encoding/json calls UnmarshalJSON implicitly for any field of
// type market.TradeStatus, so no call site naming the method exists anywhere in
// cmd/ for the Makefile's grep to find, and the method stays allow-listed
// `not-used` however much real decoding this file does. Only a decode proves
// anything about it.
//
// The wording above is deliberate. The scan is a plain grep over the file text,
// comments included, so a comment containing the method name followed by an
// opening parenthesis is indistinguishable from a call site and would move the
// method off the allow-list on the strength of a sentence. The allow-list entry
// stays; this comment is written so that it does not accidentally remove it.
//
// # THE WIRE FORMAT IS A NUMBER, AND NOTHING ELSE
//
// The method decodes a JSON number into an int32 and runs it through
// TradeStatusFromCode. It does not accept the name form. Measured, not assumed:
// `json.Unmarshal([]byte("\"Trading\""), &s)` fails with "cannot unmarshal
// string into Go value of type int32", and so does a float, a bool, an object
// and any code outside int32. Note that the error names int32 rather than
// market.TradeStatus, because the failure happens inside the method's own
// json.Unmarshal call and encoding/json passes the inner error straight
// through — which is a confusing message to hand a user, and is asserted below
// so that a change to it is a deliberate one.
//
// # NOT REACHED BY THE SDK ITSELF (VERIFIED, v0.25.2)
//
// Nothing in the SDK decodes a market.TradeStatus from JSON. The HTTP path
// decodes market/jsontypes.MarketStatusResponse, whose fields are plain int32,
// and then calls TradeStatusFromCode by hand (market/context.go, MarketStatus).
// The websocket path does not involve JSON at all: quote/core.go unmarshals a
// protobuf and copier fills a quote.PushQuote, whose TradeStatus is a different
// type in the quote package. So this method is reachable only by a caller who
// declares a struct with a market.TradeStatus field of their own — which is
// what the table-driven payload below does.

// everyTradeStatusCode is the SDK's code table, transcribed from the constants
// in market/trade_status.go, together with the name each renders. It is used
// three ways below: as the round-trip corpus, as the expected answer for the
// decode tests, and — with the sweep in TestTradeStatusUnmarshalJSON_KnownCodesAreExactlyTheTable —
// as the thing a new SDK constant would fail to join.
var everyTradeStatusCode = []struct {
	code int32
	name string
	want market.TradeStatus
}{
	{-1, "TradeStatusUnknown", market.TradeStatusUnknown},
	{0, "TradeStatusNoRegisterQuote", market.TradeStatusNoRegisterQuote},
	{101, "TradeStatusClean", market.TradeStatusClean},
	{102, "TradeStatusOpenBid", market.TradeStatusOpenBid},
	{103, "TradeStatusMorningClosing", market.TradeStatusMorningClosing},
	{105, "TradeStatusTrading", market.TradeStatusTrading},
	{106, "TradeStatusNoonClosing", market.TradeStatusNoonClosing},
	{107, "TradeStatusCloseBid", market.TradeStatusCloseBid},
	{108, "TradeStatusClosing", market.TradeStatusClosing},
	{110, "TradeStatusDarkWait", market.TradeStatusDarkWait},
	{111, "TradeStatusDarkTrading", market.TradeStatusDarkTrading},
	{112, "TradeStatusDarkClosing", market.TradeStatusDarkClosing},
	{120, "TradeStatusAfterFix", market.TradeStatusAfterFix},
	{121, "TradeStatusHalfClosing", market.TradeStatusHalfClosing},
	{122, "TradeStatusNotOpened", market.TradeStatusNotOpened},
	{123, "TradeStatusRealtimeQuote", market.TradeStatusRealtimeQuote},
	{201, "TradeStatusUSPrev", market.TradeStatusUSPrev},
	{202, "TradeStatusUSTrading", market.TradeStatusUSTrading},
	{203, "TradeStatusUSAfter", market.TradeStatusUSAfter},
	{204, "TradeStatusUSClosing", market.TradeStatusUSClosing},
	{205, "TradeStatusUSStop", market.TradeStatusUSStop},
	{206, "TradeStatusUSClean", market.TradeStatusUSClean},
	{207, "TradeStatusUSNight", market.TradeStatusUSNight},
	{209, "TradeStatusUSPrevMarketClean", market.TradeStatusUSPrevMarketClean},
	{210, "TradeStatusUSAfterMarketClean", market.TradeStatusUSAfterMarketClean},
	{1000, "TradeStatusRefresh", market.TradeStatusRefresh},
	{1001, "TradeStatusDelist", market.TradeStatusDelist},
	{1002, "TradeStatusPrepare", market.TradeStatusPrepare},
	{1003, "TradeStatusCodeChange", market.TradeStatusCodeChange},
	{1004, "TradeStatusStop", market.TradeStatusStop},
	{1005, "TradeStatusWillOpen", market.TradeStatusWillOpen},
	{1006, "TradeStatusCommonSuspend", market.TradeStatusCommonSuspend},
	{1007, "TradeStatusExpire", market.TradeStatusExpire},
	{1008, "TradeStatusNoQuote", market.TradeStatusNoQuote},
	{1009, "TradeStatusUnited", market.TradeStatusUnited},
	{1010, "TradeStatusTradingHalt", market.TradeStatusTradingHalt},
	{1011, "TradeStatusWaitListing", market.TradeStatusWaitListing},
	{2001, "TradeStatusFuse", market.TradeStatusFuse},
}

// The implicit path, not a direct call: json.Unmarshal on a bare value, on a
// struct field and inside a slice, a map and a pointer. A struct field is the
// case that matters, because that is the only way this method is ever reached
// in practice, and the tag on the field is the only thing a caller controls.
func TestTradeStatusUnmarshalJSON_ImplicitPathDecodesNumericCodes(t *testing.T) {
	t.Run("a bare value", func(t *testing.T) {
		var s market.TradeStatus
		if err := json.Unmarshal([]byte("202"), &s); err != nil {
			t.Fatalf("unmarshal 202: %v", err)
		}
		if s != market.TradeStatusUSTrading {
			t.Errorf("202 decoded to %d, want TradeStatusUSTrading", s.Code())
		}
	})

	t.Run("a struct field, the shape the wire actually has", func(t *testing.T) {
		// The payload is the shape of a /v1/quote/market-status market_time
		// entry. Its numbers come from the SDK's interface contract, not from
		// a response this project has seen: no successful authenticated
		// response has ever been observed here.
		const payload = `[{"market":"US","trade_status":202,"delay_trade_status":204}]`
		var got []struct {
			Market           string             `json:"market"`
			TradeStatus      market.TradeStatus `json:"trade_status"`
			DelayTradeStatus market.TradeStatus `json:"delay_trade_status"`
		}
		if err := json.Unmarshal([]byte(payload), &got); err != nil {
			t.Fatalf("unmarshal the payload: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d entries, want 1", len(got))
		}
		if got[0].TradeStatus != market.TradeStatusUSTrading {
			t.Errorf("trade_status = %d, want TradeStatusUSTrading", got[0].TradeStatus.Code())
		}
		if got[0].DelayTradeStatus != market.TradeStatusUSClosing {
			t.Errorf("delay_trade_status = %d, want TradeStatusUSClosing", got[0].DelayTradeStatus.Code())
		}
	})

	t.Run("a slice, a map and a pointer", func(t *testing.T) {
		var list []market.TradeStatus
		if err := json.Unmarshal([]byte(`[105,4242,2001]`), &list); err != nil {
			t.Fatalf("slice: %v", err)
		}
		if len(list) != 3 || list[0] != market.TradeStatusTrading ||
			list[1] != market.TradeStatusUnknown || list[2] != market.TradeStatusFuse {
			t.Errorf("slice decoded to %v, want [Trading Unknown Fuse]", list)
		}
		byMarket := map[string]market.TradeStatus{}
		if err := json.Unmarshal([]byte(`{"HK":108,"US":9999}`), &byMarket); err != nil {
			t.Fatalf("map: %v", err)
		}
		if byMarket["HK"] != market.TradeStatusClosing {
			t.Errorf("HK = %d, want TradeStatusClosing", byMarket["HK"].Code())
		}
		if byMarket["US"] != market.TradeStatusUnknown {
			t.Errorf("US = %d, want TradeStatusUnknown for a code the SDK does not define",
				byMarket["US"].Code())
		}
		one := new(market.TradeStatus)
		if err := json.Unmarshal([]byte("107"), one); err != nil {
			t.Fatalf("pointer: %v", err)
		}
		if *one != market.TradeStatusCloseBid {
			t.Errorf("107 decoded to %d, want TradeStatusCloseBid", one.Code())
		}
	})
}

// Every code in the table, and the boundaries around it. The SDK collapses an
// unrecognised code to TradeStatusUnknown rather than failing, which is the
// behaviour that makes a status section safe to print, and it is asserted here
// rather than assumed.
func TestTradeStatusUnmarshalJSON_CodesAndBoundaries(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int32
	}{
		{"below the first code in the table", "-2", market.TradeStatusUnknown.Code()},
		{"the first code in the table", "-1", market.TradeStatusUnknown.Code()},
		{"the zero value, which is a defined code", "0", market.TradeStatusNoRegisterQuote.Code()},
		{"a gap in the middle of the table", "104", market.TradeStatusUnknown.Code()},
		{"regular trading", "105", market.TradeStatusTrading.Code()},
		{"the last code in the table", "2001", market.TradeStatusFuse.Code()},
		{"one above the last", "2002", market.TradeStatusUnknown.Code()},
		{"far above the last", "99999", market.TradeStatusUnknown.Code()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s market.TradeStatus
			if err := json.Unmarshal([]byte(tt.json), &s); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.json, err)
			}
			if s.Code() != tt.want {
				t.Errorf("%s decoded to %d, want %d", tt.json, s.Code(), tt.want)
			}
			// An unrecognised code must land on the documented "unknown"
			// representation, not on some other defined status.
			if tt.want == market.TradeStatusUnknown.Code() && s != market.TradeStatusUnknown {
				t.Errorf("%s decoded to %d, which is a defined status, want TradeStatusUnknown",
					tt.json, s.Code())
			}
			if tt.want == market.TradeStatusUnknown.Code() && s.Name() != "Unknown" {
				t.Errorf("%s decoded to a status named %q, want \"Unknown\"", tt.json, s.Name())
			}
		})
	}
}

// "Absent" has to be a distinguishable state and it is three different things,
// which is the whole reason this test exists. The zero value is a DEFINED code
// (quote not registered), it is not the same as the unknown sentinel, and
// neither of them is the same as a real status — yet the two of them render
// identically through Name(), so only the code can tell them apart.
func TestTradeStatusUnmarshalJSON_AbsentIsDistinguishableAndNullIsNotAbsence(t *testing.T) {
	t.Run("the zero value is a defined code, not the unknown sentinel", func(t *testing.T) {
		var s market.TradeStatus
		if err := json.Unmarshal([]byte("0"), &s); err != nil {
			t.Fatalf("unmarshal 0: %v", err)
		}
		if s != market.TradeStatusNoRegisterQuote {
			t.Errorf("0 decoded to %d, want TradeStatusNoRegisterQuote", s.Code())
		}
		if s == market.TradeStatusUnknown {
			t.Error("0 and -1 are the same value, so an unregistered quote and an " +
				"unrecognised code would be indistinguishable")
		}
		if s == market.TradeStatusTrading {
			t.Error("0 decoded to a real status, so absence would be unreadable")
		}
		if !statusAbsent(s) {
			t.Errorf("statusAbsent(0) = false; the section prints \"-\" for it and " +
				"must not print a status instead")
		}
	})

	t.Run("the two absent codes share a name, so the code is the only discriminator", func(t *testing.T) {
		// Worth pinning because it is the trap in using Name() to decide
		// whether a status is real: "Unknown" covers both, and a caller
		// comparing names would treat a quote-not-registered as a parse
		// failure.
		if market.TradeStatusNoRegisterQuote.Name() != market.TradeStatusUnknown.Name() {
			t.Skip("the two absent codes now render differently; the name can " +
				"distinguish them and the comment above is out of date")
		}
		if market.TradeStatusNoRegisterQuote.String() != market.TradeStatusUnknown.String() {
			t.Error("String() and Name() disagree for the two absent codes")
		}
	})

	t.Run("an absent field leaves the field alone", func(t *testing.T) {
		type row struct {
			Market      string             `json:"market"`
			TradeStatus market.TradeStatus `json:"trade_status"`
		}
		got := row{Market: "US", TradeStatus: market.TradeStatusUSTrading}
		if err := json.Unmarshal([]byte(`{"market":"US"}`), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.TradeStatus != market.TradeStatusUSTrading {
			t.Errorf("a field the payload omits came back as %d, want the "+
				"previous value %d: encoding/json does not visit absent fields",
				got.TradeStatus.Code(), market.TradeStatusUSTrading.Code())
		}
	})

	t.Run("null is NOT absence; it decodes to the zero code", func(t *testing.T) {
		// The convention most UnmarshalJSON implementations follow is that a
		// JSON null leaves the destination untouched, and this one does not:
		// it decodes null into the int32 as 0, which is a DEFINED status
		// (quote not registered). A payload with "trade_status": null would
		// therefore print as a real status rather than as "-". Measured, and
		// asserted rather than left to be discovered by a user.
		var s market.TradeStatus = market.TradeStatusUSTrading
		if err := json.Unmarshal([]byte("null"), &s); err != nil {
			t.Fatalf("unmarshal null: %v", err)
		}
		if s != market.TradeStatusNoRegisterQuote {
			t.Errorf("null left the value at %d, want it decoded to 0 "+
				"(TradeStatusNoRegisterQuote); if this ever changes it is a "+
				"behaviour change worth a comment, not a silent fix", s.Code())
		}
	})
}

// A rejected payload must leave the destination as it was. It does — the
// method returns before assigning — and that is worth pinning, because a
// half-decoded status printed as a real one is worse than an error.
func TestTradeStatusUnmarshalJSON_BadJSONIsAnErrorAndChangesNothing(t *testing.T) {
	tests := []struct {
		name string
		json string
		// reachesMethod is false for a payload the outer scanner rejects
		// before UnmarshalJSON runs, which therefore produces a different
		// error entirely.
		reachesMethod bool
	}{
		{name: "the name form, which is not a wire form", json: `"Trading"`, reachesMethod: true},
		{name: "an integral float", json: "105.0", reachesMethod: true},
		{name: "an exponent", json: "1e3", reachesMethod: true},
		{name: "a bool", json: "true", reachesMethod: true},
		{name: "an object", json: "{}", reachesMethod: true},
		{name: "an array", json: "[]", reachesMethod: true},
		{name: "a code that overflows int32", json: "2147483648", reachesMethod: true},
		{name: "trailing rubbish", json: "105x"},
		{name: "an empty document", json: ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := market.TradeStatusUSTrading
			err := json.Unmarshal([]byte(tt.json), &s)
			if err == nil {
				t.Fatalf("unmarshal %s = nil error, want a rejection", tt.json)
			}
			if s != market.TradeStatusUSTrading {
				t.Errorf("a rejected payload left %d behind, want the previous "+
					"value %d unchanged", s.Code(), market.TradeStatusUSTrading.Code())
			}
			if !tt.reachesMethod {
				return
			}
			// The message names int32, not market.TradeStatus, because the
			// failure is the inner json.Unmarshal(data, &code) passed straight
			// through. Confusing to a user, so it is pinned.
			if !strings.Contains(err.Error(), "int32") {
				t.Errorf("error = %q; it is expected to name int32, the type the "+
					"method decodes into", err)
			}
		})
	}
}

// MEASURED, and the surprising part: marshal and decode are symmetric for every
// code the SDK defines, because TradeStatus has no MarshalJSON at all and is
// marshalled as the int32 it is. What is NOT symmetric is everything else, and
// both asymmetries are asserted below rather than left as folklore.
func TestTradeStatusUnmarshalJSON_MarshalAndRoundTrip(t *testing.T) {
	t.Run("every defined code survives a round trip unchanged", func(t *testing.T) {
		for _, tt := range everyTradeStatusCode {
			encoded, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("%s: marshal: %v", tt.name, err)
			}
			if want := strconv.Itoa(int(tt.code)); string(encoded) != want {
				t.Errorf("%s marshalled to %s, want the bare number %s: there is "+
					"no MarshalJSON on this type, so encoding/json writes the "+
					"underlying int32", tt.name, encoded, want)
			}
			var back market.TradeStatus
			if err := json.Unmarshal(encoded, &back); err != nil {
				t.Fatalf("%s: re-parse %s: %v", tt.name, encoded, err)
			}
			if back != tt.want {
				t.Errorf("%s round-tripped to %d, want %d", tt.name, back.Code(), tt.code)
			}
		}
	})

	t.Run("ASYMMETRY: the name form does not decode back", func(t *testing.T) {
		// Name() and String() are for printing. Encoded as JSON they are a
		// JSON string, which the decoder rejects, so a status rendered for a
		// human cannot be read back by the same type. Anything that persists
		// a status has to write the number.
		for _, tt := range everyTradeStatusCode {
			encoded, err := json.Marshal(tt.want.Name())
			if err != nil {
				t.Fatalf("marshal name: %v", err)
			}
			if !strings.HasPrefix(string(encoded), `"`) {
				t.Fatalf("Name() marshalled to %s, expected a JSON string", encoded)
			}
			var back market.TradeStatus
			if err := json.Unmarshal(encoded, &back); err == nil {
				t.Errorf("the name form %s decoded to %d with no error; the "+
					"decoder is meant to accept numbers only", encoded, back.Code())
			}
		}
	})

	t.Run("ASYMMETRY: an unrecognised code is lost on the way through", func(t *testing.T) {
		// Decode, then re-encode: the original number is gone, because the
		// decoder replaced it with the -1 sentinel. So a status that arrived
		// with a code this SDK version does not know cannot be forwarded
		// intact, and -1 is what the other end would receive.
		const raw = "4242"
		var s market.TradeStatus
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if s.Code() != market.TradeStatusUnknown.Code() {
			t.Fatalf("%s decoded to %d, want the unknown sentinel", raw, s.Code())
		}
		encoded, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(encoded) != "-1" {
			t.Errorf("re-encoding the decoded status produced %s, want -1: the "+
				"original code is not recoverable", encoded)
		}
		// The other direction, which is the one that can surprise a writer:
		// a value the SDK never produced by decoding still marshals as itself,
		// because there is no MarshalJSON to normalise it.
		out, err := json.Marshal(market.TradeStatus(4242))
		if err != nil {
			t.Fatalf("marshal an undefined value: %v", err)
		}
		if string(out) != raw {
			t.Errorf("marshalling an undefined value produced %s, want %s", out, raw)
		}
	})
}

// The table above is a transcription, so it can drift from the SDK. This is
// the check that it has not: sweeping the code space, the set of codes that
// decode to themselves must be exactly the table, which also pins that nothing
// outside the table is a known status.
func TestTradeStatusUnmarshalJSON_KnownCodesAreExactlyTheTable(t *testing.T) {
	known := map[int32]string{}
	for _, tt := range everyTradeStatusCode {
		known[tt.code] = tt.name
	}
	if len(known) != len(everyTradeStatusCode) {
		t.Fatalf("the table has a duplicate code, so it cannot be compared with "+
			"the SDK's own: %d rows, %d distinct codes", len(everyTradeStatusCode), len(known))
	}

	for code := tradeStatusSweepLo; code <= tradeStatusSweepHi; code++ {
		var s market.TradeStatus
		if err := json.Unmarshal([]byte(strconv.Itoa(code)), &s); err != nil {
			t.Fatalf("code %d: %v", code, err)
		}
		_, inTable := known[int32(code)]
		switch {
		case inTable && s.Code() != int32(code):
			t.Errorf("code %d (%s) decoded to %d; the table lists it as known, "+
				"so either the SDK dropped it or this transcription is wrong",
				code, known[int32(code)], s.Code())
		case !inTable && s.Code() != market.TradeStatusUnknown.Code():
			t.Errorf("code %d is not in the table but decoded to %d, which is a "+
				"defined status; add it to everyTradeStatusCode", code, s.Code())
		}
	}

	// And the boundaries the sweep is too wide to be a natural place for.
	for _, code := range []int32{tradeStatusSweepLo - 1, tradeStatusSweepHi + 1} {
		var s market.TradeStatus
		if err := json.Unmarshal([]byte(strconv.Itoa(int(code))), &s); err != nil {
			t.Fatalf("code %d: %v", code, err)
		}
		if s != market.TradeStatusUnknown {
			t.Errorf("code %d decoded to %d, want the unknown sentinel", code, s.Code())
		}
	}
}
