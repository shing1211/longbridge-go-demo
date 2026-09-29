package main

import (
	"testing"

	"github.com/longbridge/openapi-go/quote"
)

// quote.Period is a bare int32 whose constants are NOT consecutive: the
// intraday periods are their own minute counts and the calendar periods are
// 1000/2000/3000/4000 (quote.PeriodDay == 1000, not 1). A dropped case, a
// duplicated case or a transposed "15m"/"30m" would therefore still return a
// plausible constant and the only visible symptom would be the wrong candles.
// Every accepted spelling is pinned to its own constant here.

// The SDK's Period values, spelled out, so a swap between two neighbours is a
// test failure rather than a silently wrong candlestick request.
func TestParsePeriod_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want quote.Period
	}{
		{"1m", quote.PeriodOneMinute},
		{"1min", quote.PeriodOneMinute},
		{"5m", quote.PeriodFiveMinute},
		{"5min", quote.PeriodFiveMinute},
		{"15m", quote.PeriodFifteenMinute},
		{"15min", quote.PeriodFifteenMinute},
		{"30m", quote.PeriodThirtyMinute},
		{"30min", quote.PeriodThirtyMinute},
		{"60m", quote.PeriodSixtyMinute},
		{"60min", quote.PeriodSixtyMinute},
		{"1h", quote.PeriodSixtyMinute},
		{"day", quote.PeriodDay},
		{"d", quote.PeriodDay},
		{"1d", quote.PeriodDay},
		{"week", quote.PeriodWeek},
		{"w", quote.PeriodWeek},
		{"1w", quote.PeriodWeek},
		{"month", quote.PeriodMonth},
		{"m", quote.PeriodMonth},
		{"1mo", quote.PeriodMonth},
		{"year", quote.PeriodYear},
		{"y", quote.PeriodYear},
		// ToLower+TrimSpace, so case and padding never change the answer.
		{"DAY", quote.PeriodDay},
		{"Week", quote.PeriodWeek},
		{"15M", quote.PeriodFifteenMinute},
		{"  day  ", quote.PeriodDay},
		{"\t1H\n", quote.PeriodSixtyMinute},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parsePeriod(tt.in)
			if err != nil {
				t.Fatalf("parsePeriod(%q) = error %v, want %d", tt.in, err, int32(tt.want))
			}
			if got != tt.want {
				t.Errorf("parsePeriod(%q) = %d, want %d", tt.in, int32(got), int32(tt.want))
			}
		})
	}
}

// The nine constants and their numeric values. quote.PeriodDay is 1000, not 1,
// which is exactly the kind of assumption that goes wrong quietly.
func TestParsePeriod_PeriodConstantsAreNotConsecutive(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int32
	}{
		{"1m", 1},
		{"5m", 5},
		{"15m", 15},
		{"30m", 30},
		{"60m", 60},
		{"day", 1000},
		{"week", 2000},
		{"month", 3000},
		{"year", 4000},
	} {
		got, err := parsePeriod(tt.in)
		if err != nil {
			t.Fatalf("parsePeriod(%q) = error %v", tt.in, err)
		}
		if int32(got) != tt.want {
			t.Errorf("parsePeriod(%q) = %d, want %d", tt.in, int32(got), tt.want)
		}
	}
}

// "1m" is one minute and "1mo" is one month, but "1M" lowercases to "1m" and is
// therefore one minute. Pinned so the surprise is a documented behaviour: the
// month spellings are "month", "m" and "1mo" only.
func TestParsePeriod_UppercaseMSuffixIsOneMinuteNotOneMonth(t *testing.T) {
	got, err := parsePeriod("1M")
	if err != nil {
		t.Fatalf("parsePeriod(\"1M\") = error %v", err)
	}
	if got != quote.PeriodOneMinute {
		t.Errorf("parsePeriod(\"1M\") = %d, want %d (OneMinute, because the input is lowercased)", int32(got), int32(quote.PeriodOneMinute))
	}
	if m, err := parsePeriod("1mo"); err != nil || m != quote.PeriodMonth {
		t.Errorf("parsePeriod(\"1mo\") = %d, %v; want %d (Month)", int32(m), err, int32(quote.PeriodMonth))
	}
}

// "D" and "1D" both mean day here, but the SDK's own calendar words are
// day/week/month/year; a "2d" or "7d" is not in the switch and must not be
// accepted as a silent reinterpretation of a day period.
func TestParsePeriod_NearMissesAreRejected(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"2m", "a real proto period the CLI does not expose"},
		{"10m", "a real proto period the CLI does not expose"},
		{"45m", "a real proto period the CLI does not expose"},
		{"2h", "a real proto period the CLI does not expose"},
		{"min", "bare unit without a number"},
		{"mins", "bare unit without a number"},
		{"quarter", "a real proto period the CLI does not expose"},
		{"1mo ", "trailing space is trimmed, so this one IS accepted; kept here to prove the trim"},
		{"daily", "word form not in the switch"},
		{"1day", "concatenation not in the switch"},
		{"y1", "reversed suffix"},
		{"wk", "abbreviation not in the switch"},
		{"1y", "the year spellings are \"year\" and \"y\" only"},
	}
	for _, tt := range tests {
		if tt.in == "1mo " {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				got, err := parsePeriod(tt.in)
				if err != nil {
					t.Fatalf("parsePeriod(%q) = error %v, want it trimmed and accepted", tt.in, err)
				}
				if got != quote.PeriodMonth {
					t.Errorf("parsePeriod(%q) = %d, want %d (Month)", tt.in, int32(got), int32(quote.PeriodMonth))
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			if got, err := parsePeriod(tt.in); err == nil {
				t.Fatalf("parsePeriod(%q) = %d with no error, want an error", tt.in, int32(got))
			}
		})
	}
}

// Period 0 is the proto's UNKNOWN_PERIOD, so the error path here is the one
// place in the repo where a silent zero is genuinely unambiguous. Asserted so
// that a future SDK bump to a 0-based Period (where 0 would mean 1 minute)
// fails here rather than in a wrong-candles report.
func TestParsePeriod_ErrorPathIsTheProtoUnknownPeriod(t *testing.T) {
	got, err := parsePeriod("fortnight")
	if err == nil {
		t.Fatal("parsePeriod(\"fortnight\") returned no error")
	}
	if int32(got) != 0 {
		t.Errorf("parsePeriod error path returned %d, want 0", int32(got))
	}
	if got == quote.PeriodOneMinute {
		t.Fatal("quote.PeriodOneMinute is now 0, so the error path is indistinguishable from a real period; " +
			"the caller must check err, and this test needs revisiting")
	}
}

// A value valid for a different parser must not be accepted here. The watchlist
// and executions commands use the same "add"/"remove"/"asc" vocabulary.
func TestParsePeriod_UnrelatedWordsAreRejected(t *testing.T) {
	for _, in := range []string{"add", "remove", "asc", "desc", "HK", "US", "call", "put"} {
		if got, err := parsePeriod(in); err == nil {
			t.Errorf("parsePeriod(%q) = %d with no error, want an error", in, int32(got))
		}
	}
}

// splitList is the -symbols splitter. It trims, drops empties and falls back to
// a single default, so the "empty flag means one sensible symbol" behaviour is
// pinned rather than assumed.
func TestSplitList_TrimsDropsEmptiesAndFallsBackToADefault(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "700.HK", []string{"700.HK"}},
		{"several", "700.HK,AAPL.US", []string{"700.HK", "AAPL.US"}},
		{"whitespace around entries is dropped", " 700.HK , AAPL.US ", []string{"700.HK", "AAPL.US"}},
		{"empty entries are skipped", "700.HK,,AAPL.US,", []string{"700.HK", "AAPL.US"}},
		{"whitespace-only entries are skipped", "700.HK, ,AAPL.US", []string{"700.HK", "AAPL.US"}},
		{"empty string falls back to the default", "", []string{"700.HK"}},
		{"whitespace only falls back to the default", "   ", []string{"700.HK"}},
		{"commas only fall back to the default", ",,,", []string{"700.HK"}},
		{"order is preserved", "B.US,A.US,C.US", []string{"B.US", "A.US", "C.US"}},
		{"duplicates are kept", "700.HK,700.HK", []string{"700.HK", "700.HK"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

// printCandles parses -period before it uses the client, so a nil
// *quote.QuoteContext exercises the flag check and returns at the first error.
// Only the error path is reachable this way; a valid period would dereference
// the nil client and panic, which is why no case here lets the check pass.
func TestPrintCandles_RejectsABadPeriodBeforeUsingTheClient(t *testing.T) {
	save := period
	t.Cleanup(func() { period = save })

	for _, in := range []string{"", "2m", "quarter", "1y", "min", "  "} {
		period = in
		if err := printCandles(t.Context(), nil, []string{"700.HK"}); err == nil {
			t.Errorf("printCandles with -period %q returned no error, want an error", in)
		}
	}
}
