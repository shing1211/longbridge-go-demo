package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/alert"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// alert.AlertCondition and alert.AlertFrequency are bare int enums that go on
// the wire as integer codes, so a wrong mapping is not a wrong number in a
// table but the wrong trigger on a live notification. Every test below asserts
// the exact SDK constant, never just "no error".
//
// The SDK also starts AlertCondition at 1 and AlertFrequency at 1, so the zero
// value is not a valid member of either enum. That is what makes the error
// returns safe here in a way they are not in cmd/dca.

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q): %v", path, err)
	}
	orig := os.Stderr
	os.Stderr = f
	fn()
	os.Stderr = orig
	if err := f.Close(); err != nil {
		t.Fatalf("closing captured stderr: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return string(b)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q): %v", path, err)
	}
	orig := os.Stdout
	os.Stdout = f
	fn()
	os.Stdout = orig
	if err := f.Close(); err != nil {
		t.Fatalf("closing captured stdout: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(b)
}

type alertState struct {
	symbol       string
	condition    string
	triggerValue string
	frequency    string
	alertID      string
	enabled      bool
}

func setAlertFlags(t *testing.T, s alertState) {
	t.Helper()
	prev := alertState{symbol, condition, triggerValue, frequency, alertID, enabled}
	symbol, condition, triggerValue = s.symbol, s.condition, s.triggerValue
	frequency, alertID, enabled = s.frequency, s.alertID, s.enabled
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		symbol, condition, triggerValue = prev.symbol, prev.condition, prev.triggerValue
		frequency, alertID, enabled = prev.frequency, prev.alertID, prev.enabled
	})
}

func blockedCfg() *appcfg.Config {
	return &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
}

func isBlocked(err error) bool { return errors.Is(err, appcfg.ErrBlocked) }

type failConnect struct{ calls int }

func (c *failConnect) connect() (*alert.AlertContext, error) {
	c.calls++
	return nil, context.Canceled
}

// ------------------------------------------------------------------ condition

// The SDK's own values are pinned first. The IndicatorID that goes on the wire
// is the decimal form of the condition, so a mis-numbered constant means a
// notification on the wrong trigger.
func TestParseCondition_TheSDKConstantValuesArePinned(t *testing.T) {
	for got, want := range map[alert.AlertCondition]int{
		alert.AlertConditionPriceRise:   1,
		alert.AlertConditionPriceFall:   2,
		alert.AlertConditionPercentRise: 3,
		alert.AlertConditionPercentFall: 4,
	} {
		if int(got) != want {
			t.Errorf("SDK AlertCondition %v = %d, pinned %d; doAdd prints this as indicator_id", got, int(got), want)
		}
	}
}

func TestParseCondition_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want alert.AlertCondition
	}{
		{"price-rise", alert.AlertConditionPriceRise},
		{"price-fall", alert.AlertConditionPriceFall},
		{"percent-rise", alert.AlertConditionPercentRise},
		{"percent-fall", alert.AlertConditionPercentFall},
		// The underscore spellings and the bare rise/fall are accepted
		// aliases, so each is pinned to the constant it aliases rather than
		// assumed from its name.
		{"price_rise", alert.AlertConditionPriceRise},
		{"price_fall", alert.AlertConditionPriceFall},
		{"percent_rise", alert.AlertConditionPercentRise},
		{"percent_fall", alert.AlertConditionPercentFall},
		{"rise", alert.AlertConditionPriceRise},
		{"fall", alert.AlertConditionPriceFall},
		// Lower-cased and trimmed.
		{"PRICE-RISE", alert.AlertConditionPriceRise},
		{"  Price_Fall  ", alert.AlertConditionPriceFall},
		{"\nPERCENT-RISE\n", alert.AlertConditionPercentRise},
		{"Percent-Fall", alert.AlertConditionPercentFall},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseCondition(tt.in)
			if err != nil {
				t.Fatalf("parseCondition(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseCondition(%q) = %d, want %d (%q)", tt.in, int(got), int(tt.want), tt.want)
			}
		})
	}
}

func TestParseCondition_UnrecognisedValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string: the -condition default is price-rise, so an empty flag is never intentional"},
		{"price", "half a condition, on the wrong axis"},
		{"percent", "the other axis, unspecified direction"},
		{"price-rise-fall", "both directions at once"},
		{"up", "a word for rising, not in the vocabulary"},
		{"down", "a word for falling, not in the vocabulary"},
		{"1", "a number: conditions are named, not ordinals on the command line"},
		{"0", "zero; the SDK enum starts at 1, so this cannot be a condition"},
		{"daily", "valid for parseFrequency in this same file, not for -condition"},
		{"once", "valid for parseFrequency in this same file, not for -condition"},
		{"every-time", "valid for parseFrequency in this same file, not for -condition"},
		{"Day", "a trade time in force, not a condition"},
		{"Buy", "a trade side, not a condition"},
		{"price-ris", "a real condition missing its last letter"},
		{"percent-fal", "ditto"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseCondition(tt.in)
			if err == nil {
				t.Fatalf("parseCondition(%q) = %d with no error; the alert would fire on the wrong trigger", tt.in, int(got))
			}
			// The enum starts at 1, so 0 is unambiguously "rejected" rather
			// than a real condition.
			if got != 0 {
				t.Errorf("parseCondition(%q) = %d alongside an error; the rejected value must be 0", tt.in, int(got))
			}
			if !strings.Contains(err.Error(), "-condition") {
				t.Errorf("error %q does not name -condition", err)
			}
		})
	}
}

// The two parsers in this file must not accept each other's words. A -condition
// of "daily" that quietly became a frequency, or the reverse, would be a silent
// reordering of what the user asked for.
func TestAlertParsers_VocabulariesDoNotOverlap(t *testing.T) {
	for _, w := range []string{"daily", "every-time", "everytime", "always", "once"} {
		if got, err := parseCondition(w); err == nil {
			t.Errorf("parseCondition(%q) = %d, but that is a -frequency word", w, int(got))
		}
	}
	for _, w := range []string{"price-rise", "price-fall", "percent-rise", "percent-fall", "rise", "fall"} {
		if got, err := parseFrequency(w); err == nil {
			t.Errorf("parseFrequency(%q) = %d, but that is a -condition word", w, int(got))
		}
	}
}

// ------------------------------------------------------------------ frequency

func TestParseFrequency_TheSDKConstantValuesArePinned(t *testing.T) {
	for got, want := range map[alert.AlertFrequency]int{
		alert.AlertFrequencyDaily:     1,
		alert.AlertFrequencyEveryTime: 2,
		alert.AlertFrequencyOnce:      3,
	} {
		if int(got) != want {
			t.Errorf("SDK AlertFrequency %v = %d, pinned %d; doAdd prints this as the frequency field", got, int(got), want)
		}
	}
}

func TestParseFrequency_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want alert.AlertFrequency
	}{
		{"daily", alert.AlertFrequencyDaily},
		{"every-time", alert.AlertFrequencyEveryTime},
		{"once", alert.AlertFrequencyOnce},
		// Aliases for every-time.
		{"everytime", alert.AlertFrequencyEveryTime},
		{"always", alert.AlertFrequencyEveryTime},
		// The empty string resolves to the documented flag default.
		{"", alert.AlertFrequencyOnce},
		// Lower-cased and trimmed.
		{"DAILY", alert.AlertFrequencyDaily},
		{"Every-Time", alert.AlertFrequencyEveryTime},
		{"  ONCE  ", alert.AlertFrequencyOnce},
		{"\tAlways\n", alert.AlertFrequencyEveryTime},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseFrequency(tt.in)
			if err != nil {
				t.Fatalf("parseFrequency(%q) = error %v, want %d", tt.in, err, int(tt.want))
			}
			if got != tt.want {
				t.Errorf("parseFrequency(%q) = %d, want %d (%q)", tt.in, int(got), int(tt.want), tt.want)
			}
		})
	}
}

func TestParseFrequency_UnrecognisedValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"every day", "the two-word form, not the vocabulary"},
		{"every day ", "trailing space is trimmed, but the words are wrong"},
		{"every time", "a space instead of a hyphen"},
		{"day", "a prefix of daily; the whole word is required"},
		{"dailly", "a misspelling of daily"},
		{"once-only", "once, decorated"},
		{"never", "the opposite of always, not in the vocabulary"},
		{"2", "a number: frequencies are words on the command line"},
		{"0", "zero; the SDK enum starts at 1, so this cannot be a frequency"},
		{"weekly", "a DCA frequency, not an alert frequency"},
		{"price-rise", "valid for parseCondition in this same file, not for -frequency"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseFrequency(tt.in)
			if err == nil {
				t.Fatalf("parseFrequency(%q) = %d with no error", tt.in, int(got))
			}
			if got != 0 {
				t.Errorf("parseFrequency(%q) = %d alongside an error; the rejected value must be 0", tt.in, int(got))
			}
			if !strings.Contains(err.Error(), "-frequency") {
				t.Errorf("error %q does not name -frequency", err)
			}
		})
	}
}

// A whitespace-only -frequency used to trim to the empty string, which is the
// documented default, so it was accepted as "once" while every other
// misspelling was an error. The alternative to that was silent: the user got a
// once-only notification and no message about the flag they got wrong. It is now
// refused like any other unrecognised value.
//
// Originally pinned as TestParseFrequency_FindingWhitespaceOnlyResolvesToOnce
// NotAnError.
func TestParseFrequency_WhitespaceOnlyIsAnErrorRatherThanTheDefault(t *testing.T) {
	for _, in := range []string{" ", "\t", "  \n "} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseFrequency(in)
			if err == nil {
				t.Fatalf("parseFrequency(%q) = %d with no error; a value that is only whitespace "+
					"must not be indistinguishable from an omitted flag", in, int(got))
			}
			if got != 0 {
				t.Errorf("parseFrequency(%q) = %d alongside an error; the rejected value must be 0", in, int(got))
			}
			if !strings.Contains(err.Error(), "-frequency") {
				t.Errorf("error %q does not name -frequency", err)
			}
			// Go-quoted, so a tab shows up as \t rather than as blank space.
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", in)) {
				t.Errorf("error %q does not quote the value they typed (%q)", err, in)
			}
		})
	}
}

// The line that rejection must not cross: a padded real word is a typo, not a
// value, and parseFrequency still normalises it.
func TestParseFrequency_APaddedRealWordIsStillNormalised(t *testing.T) {
	tests := []struct {
		in   string
		want alert.AlertFrequency
	}{
		{" Once ", alert.AlertFrequencyOnce},
		{"\nDAILY\t", alert.AlertFrequencyDaily},
		{"  every-time  ", alert.AlertFrequencyEveryTime},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseFrequency(tt.in)
			if err != nil {
				t.Fatalf("parseFrequency(%q) = error %v, want %d", tt.in, err, int(tt.want))
			}
			if got != tt.want {
				t.Errorf("parseFrequency(%q) = %d, want %d (%q)", tt.in, int(got), int(tt.want), tt.want)
			}
		})
	}
}

// Both parsers here return 0 alongside their error, and unlike cmd/dca's
// parseFrequency that is safe: the SDK declares AlertCondition and
// AlertFrequency starting at 1, so 0 is not a member of either enum and an
// ignored error would send a condition and a frequency the API has no code for
// rather than a real-looking wrong one. No sentinel is needed, and adding one
// would be noise.
//
// The property is worth pinning anyway, because it is a property of the SDK and
// not of this file: the two parsers are only accidentally safe, and an SDK that
// renumbered either enum to start at iota — the obvious thing to do to tidy up
// a wire protocol — would turn both error returns into valid members with no
// other symptom. So both halves are asserted: the numbering, and that the value
// returned with an error is not a member of it.
func TestAlertParsers_TheErrorValueIsNotAnEnumMember(t *testing.T) {
	conditions := []alert.AlertCondition{
		alert.AlertConditionPriceRise,
		alert.AlertConditionPriceFall,
		alert.AlertConditionPercentRise,
		alert.AlertConditionPercentFall,
	}
	frequencies := []alert.AlertFrequency{
		alert.AlertFrequencyDaily,
		alert.AlertFrequencyEveryTime,
		alert.AlertFrequencyOnce,
	}
	// The numbering first, because everything below is an assertion about it.
	for i, c := range conditions {
		if int(c) != i+1 {
			t.Fatalf("SDK AlertCondition member %d = %d; this enum is only safe while it starts at 1",
				i, int(c))
		}
	}
	for i, f := range frequencies {
		if int(f) != i+1 {
			t.Fatalf("SDK AlertFrequency member %d = %d; this enum is only safe while it starts at 1",
				i, int(f))
		}
	}
	for _, in := range []string{"price", " ", "0", "weekly"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseCondition(in)
			if err == nil {
				t.Fatalf("parseCondition(%q) = %d with no error", in, int(got))
			}
			for _, c := range conditions {
				if got == c {
					t.Errorf("parseCondition(%q) = %d alongside its error, and that is a valid "+
						"condition; the alert would fire on the wrong trigger", in, int(got))
				}
			}
		})
	}
	for _, in := range []string{"every time", " ", "0", "monthly"} {
		t.Run("frequency/"+fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseFrequency(in)
			if err == nil {
				t.Fatalf("parseFrequency(%q) = %d with no error", in, int(got))
			}
			for _, f := range frequencies {
				if got == f {
					t.Errorf("parseFrequency(%q) = %d alongside its error, and that is a valid "+
						"frequency; the alert would fire on the wrong schedule", in, int(got))
				}
			}
		})
	}
}

// findAlert exists because the SDK has no get-alert-by-id: -action update has to
// call List and pick the item out itself. Resolving the wrong id would rewrite
// the wrong alert, so the traversal and its error are pinned against a list
// built here rather than one from the API.
func TestFindAlert_ResolvesTheRequestedIDAndTheSymbolItSitsUnder(t *testing.T) {
	list := &alert.AlertList{Lists: []*alert.AlertSymbolGroup{
		{
			Symbol: "700.HK", Code: "700", Market: "HK", Name: "TENCENT",
			Indicators: []*alert.AlertItem{
				{ID: "a-1", IndicatorID: "1", Frequency: 1, Enabled: true},
				{ID: "a-2", IndicatorID: "3", Frequency: 2, Enabled: false},
			},
		},
		{
			Symbol: "AAPL.US", Code: "AAPL", Market: "US", Name: "APPLE",
			Indicators: []*alert.AlertItem{
				{ID: "b-1", IndicatorID: "2", Frequency: 3, Enabled: true},
			},
		},
	}}

	tests := []struct {
		id         string
		wantItem   string
		wantSymbol string
	}{
		{"a-1", "a-1", "700.HK"},
		{"a-2", "a-2", "700.HK"},
		// The id alone must be enough: the SDK's ids are opaque, so an update
		// cannot supply a symbol to narrow the search with.
		{"b-1", "b-1", "AAPL.US"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			item, sym, err := findAlert(list, tt.id)
			if err != nil {
				t.Fatalf("findAlert(%q) = error %v", tt.id, err)
			}
			if item == nil {
				t.Fatalf("findAlert(%q) returned a nil item with no error", tt.id)
			}
			if item.ID != tt.wantItem {
				t.Errorf("findAlert(%q) returned item %q, want %q", tt.id, item.ID, tt.wantItem)
			}
			if sym != tt.wantSymbol {
				t.Errorf("findAlert(%q) symbol = %q, want %q", tt.id, sym, tt.wantSymbol)
			}
		})
	}
}

// doUpdate mutates the returned item in place (`item.Enabled = enabled`) and
// echoes it back, so the caller must get the list's own pointer and not a copy —
// a copy would leave the SDK's state and this command's view disagreeing.
func TestFindAlert_ReturnsTheListsOwnPointerSoDoUpdatesMutationReachesTheSDK(t *testing.T) {
	item := &alert.AlertItem{ID: "a-1", Enabled: true}
	list := &alert.AlertList{Lists: []*alert.AlertSymbolGroup{
		{Symbol: "700.HK", Indicators: []*alert.AlertItem{item}},
	}}
	got, _, err := findAlert(list, "a-1")
	if err != nil {
		t.Fatalf("findAlert() = error %v", err)
	}
	if got != item {
		t.Fatal("findAlert() returned a different pointer; doUpdate's in-place Enabled change would not reach the SDK")
	}
	got.Enabled = false
	if item.Enabled {
		t.Error("mutating the returned item did not change the list's item")
	}
}

func TestFindAlert_UnknownIDIsAnErrorThatNamesTheIDAndTheListCommand(t *testing.T) {
	list := &alert.AlertList{Lists: []*alert.AlertSymbolGroup{
		{Symbol: "700.HK", Indicators: []*alert.AlertItem{{ID: "a-1"}}},
		// A group with no indicators at all: the inner loop must not fault.
		{Symbol: "9988.HK"},
	}}
	tests := []struct {
		name string
		list *alert.AlertList
		id   string
	}{
		{"id on a different account", list, "zz-9"},
		{"empty id", list, ""},
		{"id that is a prefix of a real one", list, "a"},
		{"no groups at all", &alert.AlertList{}, "a-1"},
		{"a group with no indicators", &alert.AlertList{Lists: []*alert.AlertSymbolGroup{{Symbol: "700.HK"}}}, "a-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, sym, err := findAlert(tt.list, tt.id)
			if err == nil {
				t.Fatalf("findAlert(%q) = (%v, %q, nil); an unresolved id must not be updated", tt.id, item, sym)
			}
			if item != nil || sym != "" {
				t.Errorf("findAlert(%q) = (%v, %q) alongside an error; both must be zero values", tt.id, item, sym)
			}
			if tt.id != "" && !strings.Contains(err.Error(), tt.id) {
				t.Errorf("error %q does not echo the id the user passed", err)
			}
			if !strings.Contains(err.Error(), "-action list") {
				t.Errorf("error %q does not point at the command that shows the valid ids", err)
			}
		})
	}
}

// findAlert used to dereference its list argument, so a nil *AlertList panicked.
// It was unreachable — the SDK's List returns a non-nil *AlertList whenever err
// is nil and doUpdate returns early on err — and the panic test below used to pin
// that. A refactor moving the error check, or an SDK returning nil with a nil
// error, would have turned it into a crash in the middle of a gated write, so it
// is now an ordinary "not found": there is no alert with that id in a nil list
// either, and the caller must not be able to send anything on the strength of it.
//
// Originally pinned as TestFindAlert_FindingANilListPanicsRatherThanReturning
// AnError.
func TestFindAlert_ANilListIsNotFoundRatherThanAPanic(t *testing.T) {
	for _, id := range []string{"a-1", "", "zz-9"} {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			var item *alert.AlertItem
			var sym string
			var err error
			// No recover: a panic here fails the test, which is the point.
			func() {
				item, sym, err = findAlert(nil, id)
			}()
			if err == nil {
				t.Fatalf("findAlert(nil, %q) = (%v, %q, nil)", id, item, sym)
			}
			if item != nil || sym != "" {
				t.Errorf("findAlert(nil, %q) = (%v, %q) alongside an error; both must be zero values", id, item, sym)
			}
			if id != "" && !strings.Contains(err.Error(), id) {
				t.Errorf("error %q does not echo the id the user passed", err)
			}
			if !strings.Contains(err.Error(), "-action list") {
				t.Errorf("error %q does not point at the command that shows the valid ids", err)
			}
		})
	}
}

// Two alerts with the same id would make the update target depend on iteration
// order. The SDK does not promise uniqueness, so the first match wins and that
// is what a test should say.
func TestFindAlert_DuplicateIDsResolveToTheFirstMatchInListOrder(t *testing.T) {
	first := &alert.AlertItem{ID: "dup", IndicatorID: "1"}
	second := &alert.AlertItem{ID: "dup", IndicatorID: "3"}
	list := &alert.AlertList{Lists: []*alert.AlertSymbolGroup{
		{Symbol: "700.HK", Indicators: []*alert.AlertItem{first}},
		{Symbol: "AAPL.US", Indicators: []*alert.AlertItem{second}},
	}}
	item, sym, err := findAlert(list, "dup")
	if err != nil {
		t.Fatalf("findAlert() = error %v", err)
	}
	if item != first || sym != "700.HK" {
		t.Errorf("findAlert(\"dup\") = (%v, %q), want the first match under 700.HK", item, sym)
	}
}

// ------------------------------------------------------------------ rendering

// ValueMap is json.RawMessage in the SDK precisely because the shape is not
// stable, so this is a pretty-printer and not a field mapping. The property that
// matters is that it never loses the raw bytes: an unparsable blob must come
// back verbatim rather than as an empty cell.
func TestPrettyValueMap_RendersAJSONObjectAsSortedKeyEqualsValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single key", `{"price":"600"}`, "price=600"},
		{"keys sorted so two runs match", `{"price":"600","chg":"1.5"}`, "chg=1.5 price=600"},
		{"three keys", `{"c":"3","a":"1","b":"2"}`, "a=1 b=2 c=3"},
		{"numeric value keeps its JSON form", `{"price":600}`, "price=600"},
		{"nested object", `{"k":{"n":1}}`, "k=map[n:1]"},
		{"empty object", `{}`, ""},
		{"null value", `{"price":null}`, "price=<nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prettyValueMap(json.RawMessage(tt.in)); got != tt.want {
				t.Errorf("prettyValueMap(%s) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPrettyValueMap_NeverLosesTheRawBytes(t *testing.T) {
	// Empty and nil: the SDK's "field absent" case. The caller wraps the result
	// in cli.Truncate/OrDash, so "" is meaningful here, not a bug.
	if got := prettyValueMap(nil); got != "" {
		t.Errorf("prettyValueMap(nil) = %q, want \"\"", got)
	}
	if got := prettyValueMap(json.RawMessage("")); got != "" {
		t.Errorf("prettyValueMap(empty) = %q, want \"\"", got)
	}
	// Not JSON at all: the documented fallback is the raw string, because the
	// shape is not stable and hiding it would be worse than showing it.
	for _, in := range []string{`not json`, `[1,2,3]`, `"a bare string"`, `42`, `{`, `{"price":}`} {
		got := prettyValueMap(json.RawMessage(in))
		if got != in {
			t.Errorf("prettyValueMap(%s) = %q, want the input verbatim; the raw bytes are the only "+
				"truth available when the shape is not JSON", in, got)
		}
	}
}

// The rendering must be deterministic: doList truncates the result into a
// ten-column cell, so a non-deterministic order would make the column ragged
// between runs.
func TestPrettyValueMap_IsDeterministicForManyKeys(t *testing.T) {
	raw := json.RawMessage(`{"z":"1","y":"2","x":"3","w":"4","v":"5","u":"6"}`)
	first := prettyValueMap(raw)
	for i := 0; i < 20; i++ {
		if got := prettyValueMap(raw); got != first {
			t.Fatalf("run %d = %q, first run = %q; the output is map-ordered somewhere", i, got, first)
		}
	}
	if first != "u=6 v=5 w=4 x=3 y=2 z=1" {
		t.Errorf("prettyValueMap = %q, want the keys in sorted order", first)
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

// ------------------------------------------------------------------ write paths

func TestWriteActions_RefuseMissingFlagsBeforeAnySDKContextIsCreated(t *testing.T) {
	tests := []struct {
		name    string
		set     alertState
		run     func(*failConnect) error
		wantErr string
	}{
		{
			name: "add without -symbol",
			set:  alertState{condition: "price-rise", triggerValue: "600", frequency: "once"},
			run: func(c *failConnect) error {
				return doAdd(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-symbol",
		},
		{
			name: "add without -value",
			set:  alertState{symbol: "700.HK", condition: "price-rise", frequency: "once"},
			run: func(c *failConnect) error {
				return doAdd(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-value",
		},
		{
			name: "add with an unrecognised -condition",
			set:  alertState{symbol: "700.HK", condition: "sideways", triggerValue: "600", frequency: "once"},
			run: func(c *failConnect) error {
				return doAdd(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-condition",
		},
		{
			name: "add with an unrecognised -frequency",
			set:  alertState{symbol: "700.HK", condition: "price-rise", triggerValue: "600", frequency: "every time"},
			run: func(c *failConnect) error {
				return doAdd(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-frequency",
		},
		{
			name: "update without -id",
			set:  alertState{},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-id",
		},
		{
			name: "delete without -id",
			set:  alertState{},
			run: func(c *failConnect) error {
				return doDelete(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAlertFlags(t, tt.set)
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = tt.run(c) })
			if err == nil {
				t.Fatalf("%s was not refused", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal, want the flag error; an unusable flag must be reported "+
					"as such even when the gate is shut. got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s); validation must precede any client", c.calls)
			}
		})
	}
}

// The key structural promise in this file: update resolves -id through List, and
// the gate runs BEFORE that read. A blocked update that dialled the API to
// resolve an id would break the "a refusal makes no network call" guarantee that
// every guard in this repo makes.
func TestDoUpdate_AnUnconfirmedUpdateIsRefusedBeforeTheListReadThatResolvesTheID(t *testing.T) {
	setAlertFlags(t, alertState{alertID: "a-1", enabled: false})
	c := &failConnect{}
	var err error
	// The refusal block goes to stderr; nothing is previewed, because the body
	// cannot be resolved without a request.
	out := captureStderr(t, func() { err = doUpdate(context.Background(), blockedCfg(), c.connect) })
	if err == nil {
		t.Fatal("doUpdate() = nil with the gate shut")
	}
	if !isBlocked(err) {
		t.Errorf("doUpdate() = %v, want a *config.BlockedError so the exit status is %d", err, appcfg.ExitBlocked)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s); the gate must run before the id-resolving List call", c.calls)
	}
	// The refusal names the id and the flag being toggled, because that is all
	// the command knows without doing the read it deliberately did not do.
	for _, want := range []string{"[DRY-RUN] BLOCKED:", "a-1", "enabled=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal block is missing %q.\nblock was:\n%s", want, out)
		}
	}
	// The resolved body cannot be previewed, and the comment in main.go says so.
	if strings.Contains(out, "indicator_id") {
		t.Errorf("preview shows a resolved body, but resolving it would have required a request "+
			"before the gate:\n%s", out)
	}
}

// An add that passes every flag check reaches the gate and stops. The preview
// reproduces the body the SDK builds, including the price-vs-chg key choice,
// which is derived from the condition inside the SDK and would otherwise be
// invisible to the user.
func TestDoAdd_APreviewShowsTheKeyTheSDKWillUseForTheGivenCondition(t *testing.T) {
	tests := []struct {
		cond       string
		wantKey    string
		notWantKey string
	}{
		{"price-rise", `"price"`, `"chg"`},
		{"price-fall", `"price"`, `"chg"`},
		{"percent-rise", `"chg"`, `"price"`},
		{"percent-fall", `"chg"`, `"price"`},
	}
	for _, tt := range tests {
		t.Run(tt.cond, func(t *testing.T) {
			setAlertFlags(t, alertState{symbol: "700.HK", condition: tt.cond,
				triggerValue: "1.5", frequency: "once"})
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() { err = doAdd(context.Background(), blockedCfg(), c.connect) })
			if err == nil || !isBlocked(err) {
				t.Fatalf("doAdd(%s) = %v, want a *config.BlockedError", tt.cond, err)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) while blocked", c.calls)
			}
			for _, want := range []string{
				"[DRY-RUN]", "POST /v1/notify/reminders", "700.HK", tt.wantKey,
				"no \"id\" in body", "frequency", "3", "state",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
				}
			}
			if strings.Contains(out, tt.notWantKey) {
				t.Errorf("preview contains %s, but a %s alert uses the other key.\npreview was:\n%s",
					tt.notWantKey, tt.cond, out)
			}
		})
	}
}

// The indicator_id in the preview is the decimal form of the condition, so a
// wrong mapping would be visible here as a wrong number.
func TestDoAdd_IndicatorIDInThePreviewIsTheConditionsDecimalCode(t *testing.T) {
	for _, tt := range []struct {
		cond string
		code string
	}{
		{"price-rise", "1"},
		{"price-fall", "2"},
		{"percent-rise", "3"},
		{"percent-fall", "4"},
	} {
		t.Run(tt.cond, func(t *testing.T) {
			setAlertFlags(t, alertState{symbol: "700.HK", condition: tt.cond,
				triggerValue: "600", frequency: "once"})
			c := &failConnect{}
			out := captureStdout(t, func() {
				_ = doAdd(context.Background(), blockedCfg(), c.connect)
			})
			if !strings.Contains(out, "indicator_id") {
				t.Fatalf("preview has no indicator_id line at all:\n%s", out)
			}
			for _, l := range strings.Split(out, "\n") {
				if !strings.Contains(l, "indicator_id") {
					continue
				}
				fields := strings.Fields(l)
				if len(fields) < 2 {
					t.Fatalf("indicator_id line has no value: %q", l)
				}
				if got := fields[len(fields)-1]; got != tt.code {
					t.Errorf("indicator_id = %q, want %q for condition %q", got, tt.code, tt.cond)
				}
				break
			}
		})
	}
}

// Delete is irreversible and there is no undelete endpoint, so the preview has
// to say so rather than leaving the user to discover it afterwards.
func TestDoDelete_PreviewWarnsThatThereIsNoUndelete(t *testing.T) {
	setAlertFlags(t, alertState{alertID: "a-1"})
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doDelete(context.Background(), blockedCfg(), c.connect) })
	if err == nil || !isBlocked(err) {
		t.Fatalf("doDelete() = %v, want a *config.BlockedError", err)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	for _, want := range []string{
		"DELETE /v1/notify/reminders", "a-1", "IRREVERSIBLE", "no undelete endpoint",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
		}
	}
}

func TestGate_RefusalIsExplainedOnStderrAndReturnsABlockedError(t *testing.T) {
	setAlertFlags(t, alertState{})
	var err error
	out := captureStderr(t, func() { err = gate(blockedCfg(), "add a price-rise alert on 700.HK at 600") })
	if err == nil {
		t.Fatal("gate() = nil; a blocked write must be distinguishable from a completed one")
	}
	if !isBlocked(err) {
		t.Errorf("gate() = %v, want a *config.BlockedError", err)
	}
	for _, want := range []string{
		"[DRY-RUN] BLOCKED:", "add a price-rise alert on 700.HK at 600",
		"LONGPORT_ALERT_DRY_RUN", "--confirm-live-alert", "LONGPORT_MODE=live",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal block is missing %q.\nblock was:\n%s", want, out)
		}
	}
}
