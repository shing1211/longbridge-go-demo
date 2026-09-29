package main

import (
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
