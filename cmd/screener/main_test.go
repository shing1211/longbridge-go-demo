package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/longbridge/openapi-go/screener"
)

// The screener API takes bare strings for its market and its condition bounds,
// so the SDK offers nothing to catch a typo: "USA" or "pettm:abc:30" would be
// forwarded verbatim and come back empty, which reads as "nothing matched"
// rather than as a mistake. These parsers are the only local check, so the tests
// pin exactly what they do and do not reject.

// ------------------------------------------------------------------ market

// parseMarket here returns a plain string, unlike cmd/market and cmd/reference
// which return the typed openapi.Market. Asserted against the literal the API
// receives, because that string is the whole payload.
func TestParseMarket_EveryAcceptedValueMapsToTheCanonicalString(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"HK", "HK"},
		{"US", "US"},
		{"CN", "CN"},
		{"SG", "SG"},
		{"UK", "UK"},
		// ToUpper+TrimSpace, so the returned string is always the canonical
		// upper-case form and never the user's spelling.
		{"hk", "HK"},
		{"us", "US"},
		{"cn", "CN"},
		{"sg", "SG"},
		{"uk", "UK"},
		{"  hk  ", "HK"},
		{"\tus\n", "US"},
		{"Uk", "UK"},
		{"cN", "CN"},
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

func TestParseMarket_UnknownValueIsAnErrorNotAnEmptyString(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"USA", "near miss on US"},
		{"HKG", "the other common spelling of Hong Kong"},
		{"hk-ex", "suffix the flag help does not promise"},
		{"H", "truncated"},
		{"HKUS", "two markets in one"},
		{"日", "non-ASCII"},
		// An empty market would be forwarded as "" and match nothing, which
		// reads as "no strategies exist" rather than as a typo. That is the
		// exact failure this parser exists to prevent.
		{"-", "the value the SDK substitutes for a blank strategy market"},
		// Values belonging to the other parsers in this file.
		{"search", "an -action value"},
		{"recommend", "an -action value"},
		{"pettm", "a -condition key"},
		{"goldfork", "a -condition tech value"},
		{"capmk", "a -show column"},
		{"0", "a numeric id where a market belongs"},
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

// ------------------------------------------------------------- conditions

func TestParseConditions_OpenAndClosedBoundsBothParse(t *testing.T) {
	// Min and Max are strings where an empty value means unbounded, which is
	// why the fields are separated by ':' at all. Both the "no upper bound"
	// and the "bounded at both ends" spellings must survive intact.
	tests := []struct {
		name string
		in   string
		want screener.ScreenerCondition
	}{
		{"bounded at both ends", "pettm:0:1", screener.ScreenerCondition{Key: "pettm", Min: "0", Max: "1"}},
		{"no lower bound", "pettm::30", screener.ScreenerCondition{Key: "pettm", Min: "", Max: "30"}},
		{"no upper bound", "pettm:5:", screener.ScreenerCondition{Key: "pettm", Min: "5", Max: ""}},
		{"min only, two fields", "pettm:10", screener.ScreenerCondition{Key: "pettm", Min: "10", Max: ""}},
		{"an explicit zero lower bound is kept", "pettm:0:30", screener.ScreenerCondition{Key: "pettm", Min: "0", Max: "30"}},
		{"whitespace around the bounds is trimmed", " pettm : 0 : 1 ",
			screener.ScreenerCondition{Key: "pettm", Min: "0", Max: "1"}},
		{"bounds are strings, not numbers: a decimal survives",
			"pettm:1.5:30.25", screener.ScreenerCondition{Key: "pettm", Min: "1.5", Max: "30.25"}},
		{"bounds are strings, not numbers: a negative survives",
			"pettm:-5:30", screener.ScreenerCondition{Key: "pettm", Min: "-5", Max: "30"}},
		{"bounds are strings, not numbers: an exponent survives",
			"pettm:1e2:", screener.ScreenerCondition{Key: "pettm", Min: "1e2", Max: ""}},
		{"the SDK's filter_ prefix is optional, so it is not stripped",
			"filter_pettm:0:1", screener.ScreenerCondition{Key: "filter_pettm", Min: "0", Max: "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, text, err := parseConditions(tt.in)
			if err != nil {
				t.Fatalf("parseConditions(%q) = error %v, want %+v", tt.in, err, tt.want)
			}
			if len(got) != 1 {
				t.Fatalf("parseConditions(%q) returned %d conditions, want 1", tt.in, len(got))
			}
			if !reflect.DeepEqual(got[0], tt.want) {
				t.Errorf("parseConditions(%q)[0] = %+v, want %+v", tt.in, got[0], tt.want)
			}
			if len(text) != 1 {
				t.Fatalf("parseConditions(%q) returned %d echo strings, want 1", tt.in, len(text))
			}
			// The echo is the entry as typed, minus the surrounding whitespace
			// splitList removed.
			if text[0] != strings.TrimSpace(tt.in) {
				t.Errorf("parseConditions(%q) echoed %q, want %q", tt.in, text[0], strings.TrimSpace(tt.in))
			}
		})
	}
}

// The fourth segment is the tech-value map, and it is the only way to filter on
// a technical indicator, which has no numeric bounds.
func TestParseConditions_TechValueSegmentIsParsedIntoTheMap(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want screener.ScreenerCondition
	}{
		{
			"tech value with no bounds",
			"macd_day:::category=goldenfork",
			screener.ScreenerCondition{
				Key: "macd_day", Min: "", Max: "",
				TechValues: map[string]string{"category": "goldenfork"},
			},
		},
		{
			"tech value alongside bounds",
			"macd_day:1:2:category=goldenfork",
			screener.ScreenerCondition{
				Key: "macd_day", Min: "1", Max: "2",
				TechValues: map[string]string{"category": "goldenfork"},
			},
		},
		{
			"two tech values",
			"macd_day:::category=goldenfork;period=day",
			screener.ScreenerCondition{
				Key:        "macd_day",
				TechValues: map[string]string{"category": "goldenfork", "period": "day"},
			},
		},
		{
			"a fundamental condition carries a nil tech map, not an empty one",
			"pettm:0:1",
			screener.ScreenerCondition{Key: "pettm", Min: "0", Max: "1", TechValues: nil},
		},
		{
			"an empty fourth segment with bounds set yields a nil tech map",
			"pettm:0:1:",
			screener.ScreenerCondition{Key: "pettm", Min: "0", Max: "1", TechValues: nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := parseConditions(tt.in)
			if err != nil {
				t.Fatalf("parseConditions(%q) = error %v", tt.in, err)
			}
			if len(got) != 1 {
				t.Fatalf("parseConditions(%q) returned %d conditions, want 1", tt.in, len(got))
			}
			if !reflect.DeepEqual(got[0], tt.want) {
				t.Errorf("parseConditions(%q)[0] = %+v, want %+v", tt.in, got[0], tt.want)
			}
		})
	}
}

// Multiple entries keep their order, and duplicates are kept because the API,
// not this parser, decides what a repeated filter means.
func TestParseConditions_ListSemanticsAreOrderedWithDuplicatesKept(t *testing.T) {
	got, text, err := parseConditions("pettm:0:1,roe:10:,pb:0:5")
	if err != nil {
		t.Fatalf("parseConditions = error %v", err)
	}
	wantKeys := []string{"pettm", "roe", "pb"}
	if len(got) != len(wantKeys) {
		t.Fatalf("parseConditions returned %d conditions, want %d", len(got), len(wantKeys))
	}
	for i, k := range wantKeys {
		if got[i].Key != k {
			t.Errorf("condition[%d].Key = %q, want %q (order must be preserved)", i, got[i].Key, k)
		}
		if text[i] != got[i].Key+text[i][len(got[i].Key):] {
			t.Errorf("echo[%d] = %q does not match condition %+v", i, text[i], got[i])
		}
	}
	if got[1].Min != "10" || got[1].Max != "" {
		t.Errorf("roe condition = %+v, want min 10 and an unbounded max", got[1])
	}

	dup, _, err := parseConditions("pb:0:5,pb:0:5")
	if err != nil {
		t.Fatalf("parseConditions with a duplicate = error %v", err)
	}
	if len(dup) != 2 || !reflect.DeepEqual(dup[0], dup[1]) {
		t.Errorf("parseConditions with a duplicate = %+v, want two identical conditions", dup)
	}
}

// An empty -condition is not an error: it selects no conditions, which is a
// valid Mode B search.
func TestParseConditions_EmptyInputIsNoFilterNotAnError(t *testing.T) {
	for _, in := range []string{"", "   ", ",", " , , ", "\t\n"} {
		got, text, err := parseConditions(in)
		if err != nil {
			t.Fatalf("parseConditions(%q) = error %v, want (nil, nil, nil)", in, err)
		}
		if got != nil {
			t.Errorf("parseConditions(%q) = %+v, want nil", in, got)
		}
		if text != nil {
			t.Errorf("parseConditions(%q) echoed %v, want nil", in, text)
		}
	}
}

func TestParseConditions_MalformedEntryIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"pettm", "a bare key with no bounds and no tech value"},
		{"pettm::", "both bounds empty and no tech value"},
		{"pettm: : ", "whitespace-only bounds and no tech value"},
		{":1:2", "the key is empty"},
		{":", "the key is empty and nothing else is set"},
		{"   :1:2", "the key trims to empty"},
		{"pe ttm:0:1", "the key contains a space"},
		{"pe\tttm:0:1", "the key contains a tab"},
		{"pettm:0:1:2:3", "four colons"},
		{"pettm:0:1:2:3:4", "five colons"},
		{"pettm:0:1:category=x:extra", "a tech segment that itself contains a colon"},
		{"pettm:0:1,roe", "one good entry does not excuse one bad entry"},
		{"bogus:0:1", "an unknown key is not rejected here: the API owns the key vocabulary"},
	}
	for _, tt := range tests {
		// The last row is a documented non-rejection, not an error case.
		if tt.in == "bogus:0:1" {
			t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
				got, _, err := parseConditions(tt.in)
				if err != nil {
					t.Fatalf("parseConditions(%q) = error %v, want it forwarded to the API", tt.in, err)
				}
				if len(got) != 1 || got[0].Key != "bogus" {
					t.Errorf("parseConditions(%q) = %+v, want one condition keyed bogus", tt.in, got)
				}
			})
			continue
		}
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, text, err := parseConditions(tt.in)
			if err == nil {
				t.Fatalf("parseConditions(%q) = %+v with no error, want an error", tt.in, got)
			}
			if got != nil {
				t.Errorf("parseConditions(%q) = %+v alongside the error, want nil so no partial filter is sent", tt.in, got)
			}
			if text != nil {
				t.Errorf("parseConditions(%q) echoed %v alongside the error, want nil", tt.in, text)
			}
		})
	}
}

// parseConditions is the only local check on the condition grammar, so its
// error message must name the offending entry: a screener search with a typo
// would otherwise return an empty result set and look like a market fact.
func TestParseConditions_ErrorMessageNamesTheOffendingEntry(t *testing.T) {
	// "roe:10:" is fine (a min is set); "roe" on its own is not, because a
	// condition with no bound and no tech value filters nothing.
	_, _, err := parseConditions("pettm:0:1,roe")
	if err == nil {
		t.Fatal("parseConditions = nil error, want an error for the entry with no bound")
	}
	if !strings.Contains(err.Error(), `"roe"`) {
		t.Errorf("error %q does not name the offending entry", err)
	}
	if strings.Contains(err.Error(), `"pettm:0:1"`) {
		t.Errorf("error %q blames the wrong entry: %q is well-formed", err, "pettm:0:1")
	}
}

// --------------------------------------------------------- tech values

func TestParseTechValues_EveryAcceptedSpecIsParsed(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want map[string]string
	}{
		{"one pair", "category=goldenfork", map[string]string{"category": "goldenfork"}},
		{"two pairs", "category=goldenfork;period=day", map[string]string{"category": "goldenfork", "period": "day"}},
		{"whitespace around keys and values is trimmed", "  category = goldenfork ; period = day ",
			map[string]string{"category": "goldenfork", "period": "day"}},
		{"a trailing separator is dropped", "category=goldenfork;", map[string]string{"category": "goldenfork"}},
		{"doubled separators are skipped", "category=a;;period=b", map[string]string{"category": "a", "period": "b"}},
		{"an empty value is kept as an empty string", "category=", map[string]string{"category": ""}},
		{"a value containing an equals sign keeps the tail", "expr=a=b", map[string]string{"expr": "a=b"}},
		{"the last value wins for a repeated key", "period=day;period=week", map[string]string{"period": "week"}},
		{"an empty spec yields a nil map", "", nil},
		{"a whitespace-only spec yields a nil map", "   \t\n ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTechValues("entry", tt.spec)
			if err != nil {
				t.Fatalf("parseTechValues(%q) = error %v, want %v", "entry", err, tt.want)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTechValues(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestParseTechValues_MalformedSpecIsAnErrorNotAnEmptyMap(t *testing.T) {
	tests := []struct {
		spec string
		note string
	}{
		{"category", "no equals sign"},
		{"category=goldenfork;period", "the second pair has no equals sign"},
		{"=goldenfork", "an empty key with a value"},
		{"  =goldenfork  ", "a whitespace-only key"},
		{"=", "neither key nor value"},
		{";", "only separators, so no key=value pair at all"},
		{" ; ; ", "only separators with whitespace"},
		{"a=1;;", "a valid pair followed by separators is still valid, so this is NOT an error"},
	}
	for _, tt := range tests {
		if tt.spec == "a=1;;" {
			t.Run(tt.spec+"/"+tt.note, func(t *testing.T) {
				got, err := parseTechValues("entry", tt.spec)
				if err != nil {
					t.Fatalf("parseTechValues(%q) = error %v, want {a:1}", tt.spec, err)
				}
				if !reflect.DeepEqual(got, map[string]string{"a": "1"}) {
					t.Errorf("parseTechValues(%q) = %v, want map[a:1]", tt.spec, got)
				}
			})
			continue
		}
		t.Run(tt.spec+"/"+tt.note, func(t *testing.T) {
			got, err := parseTechValues("entry", tt.spec)
			if err == nil {
				t.Fatalf("parseTechValues(%q) = %v with no error, want an error", tt.spec, got)
			}
			if got != nil {
				t.Errorf("parseTechValues(%q) = %v alongside the error, want nil", tt.spec, got)
			}
		})
	}
}

// The error must name both the whole condition and the offending pair, because
// the pair is the fourth colon-separated segment and is otherwise hard to see.
func TestParseTechValues_ErrorMessageNamesTheEntryAndThePair(t *testing.T) {
	_, err := parseTechValues("macd_day:::category", "category")
	if err == nil {
		t.Fatal("parseTechValues returned no error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"macd_day:::category"`) {
		t.Errorf("error %q does not name the whole condition entry", msg)
	}
	if !strings.Contains(msg, `"category"`) {
		t.Errorf("error %q does not name the offending pair", msg)
	}
}

// ------------------------------------------------------ shared splitList

// splitList is the -condition/-show splitter. Its trimming and empty-entry
// handling is what makes "pettm::30, roe:1:2" work, so it is pinned once.
func TestSplitList_TrimsAndDropsEmptyEntries(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "a", []string{"a"}},
		{"several", "a,b,c", []string{"a", "b", "c"}},
		{"whitespace around entries", " a , b ", []string{"a", "b"}},
		{"empty entries are dropped", "a,,b,", []string{"a", "b"}},
		{"whitespace-only entries are dropped", "a, ,b", []string{"a", "b"}},
		{"colons are kept, since they are the condition grammar", "pettm::30", []string{"pettm::30"}},
		{"empty input yields an empty slice", "", nil},
		{"whitespace only yields an empty slice", "  ", nil},
		{"commas only yield an empty slice", ",,,", nil},
		{"order is preserved", "c,a,b", []string{"c", "a", "b"}},
		{"duplicates are kept", "a,a", []string{"a", "a"}},
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

// ---------------------------------------------------------------- plan

// buildPlan is the only local validation of the screener flags. It reads
// package flag variables, so they are saved and restored around every case.
func resetScreenerFlags() {
	action, market, conditions, show = "indicators", "HK", "", ""
	strategyID, strategyIDSet, page, size = 0, false, 0, 20
}

// The -action vocabulary is closed, and an unknown action would otherwise fall
// through to the SDK and produce a confusing server error.
func TestBuildPlan_RejectsAnUnknownAction(t *testing.T) {
	for _, in := range []string{"", "   ", "list", "quote", "indicators2", "screener", "SEARCHS", "*"} {
		t.Run(in, func(t *testing.T) {
			resetScreenerFlags()
			t.Cleanup(resetScreenerFlags)
			action = in
			if _, err := buildPlan(); err == nil {
				t.Errorf("buildPlan with -action %q returned no error, want an error", in)
			}
		})
	}
	// The case is normalised, so every action is reachable in any case.
	for _, in := range []string{"indicators", "INDICATORS", "Recommend", " mine ", "STRATEGY", "Search"} {
		t.Run(in, func(t *testing.T) {
			resetScreenerFlags()
			t.Cleanup(resetScreenerFlags)
			action = in
			// "strategy" is the one action that cannot be planned without an id.
			if strings.EqualFold(strings.TrimSpace(in), "strategy") {
				strategyID, strategyIDSet = 7, true
			}
			p, err := buildPlan()
			if err != nil {
				t.Fatalf("buildPlan with -action %q = error %v", in, err)
			}
			if p.action != strings.ToLower(strings.TrimSpace(in)) {
				t.Errorf("-action %q normalised to %q, want %q", in, p.action, strings.ToLower(strings.TrimSpace(in)))
			}
		})
	}
}

// -market is validated for every action, even the ones that never use it, so a
// typo is caught by the same code path regardless of -action.
func TestBuildPlan_ValidatesMarketForEveryAction(t *testing.T) {
	for _, act := range []string{"indicators", "recommend", "mine", "strategy", "search"} {
		t.Run(act, func(t *testing.T) {
			resetScreenerFlags()
			t.Cleanup(resetScreenerFlags)
			action = act
			market = "USA"
			// The market is checked before anything else, so the market typo
			// is the reported error even for the actions that also need a
			// strategy id.
			if _, err := buildPlan(); err == nil {
				t.Errorf("buildPlan with -action %s -market USA returned no error, want an error", act)
			} else if !strings.Contains(err.Error(), "market") {
				t.Errorf("error %v does not mention the market", err)
			}
		})
	}
}

// -strategy-id 0 is refused rather than treated as unset, because a nil
// *int64 is the SDK's "no strategy" signal and a pointer to 0 would silently
// switch search from Mode A to Mode B and return a different result set.
func TestBuildPlan_StrategyIDZeroIsRejectedNotTreatedAsUnset(t *testing.T) {
	resetScreenerFlags()
	t.Cleanup(resetScreenerFlags)
	action, market, strategyID, strategyIDSet = "search", "HK", 0, true

	p, err := buildPlan()
	if err == nil {
		t.Fatalf("buildPlan with -strategy-id 0 = %+v with no error, want an error", p)
	}
	if !strings.Contains(err.Error(), "strategy-id 0") {
		t.Errorf("error %v does not explain that 0 is not a strategy id", err)
	}
	// Omitting the flag entirely is the documented way to search with
	// -condition, so the same value must be accepted when it was not typed.
	action, strategyIDSet = "search", false
	p, err = buildPlan()
	if err != nil {
		t.Fatalf("buildPlan with -strategy-id omitted = error %v, want it accepted", err)
	}
	if p.strategyID != nil {
		t.Errorf("an omitted -strategy-id produced %d, want nil so the SDK stays in Mode B", *p.strategyID)
	}
}

// A real id must be copied, never aliased to the flag variable, or a later
// mutation of the flag would rewrite the plan the SDK is holding.
func TestBuildPlan_RealStrategyIDIsCopiedNotAliased(t *testing.T) {
	resetScreenerFlags()
	t.Cleanup(resetScreenerFlags)
	action, strategyID, strategyIDSet = "strategy", 42, true

	p, err := buildPlan()
	if err != nil {
		t.Fatalf("buildPlan = error %v", err)
	}
	if p.strategyID == nil || *p.strategyID != 42 {
		t.Fatalf("plan strategy id = %v, want 42", p.strategyID)
	}
	if p.strategyID == &strategyID {
		t.Error("the plan aliases the flag variable; mutating the flag would rewrite the plan")
	}
	strategyID = 99
	if *p.strategyID != 42 {
		t.Errorf("mutating the flag changed the plan to %d, want 42", *p.strategyID)
	}
}

func TestBuildPlan_RejectsImpossibleStrategyIDs(t *testing.T) {
	resetScreenerFlags()
	t.Cleanup(resetScreenerFlags)

	t.Run("negative id", func(t *testing.T) {
		action, strategyID, strategyIDSet = "search", -1, true
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -strategy-id -1 returned no error, want an error")
		}
	})
	t.Run("strategy action without an id", func(t *testing.T) {
		action, strategyID, strategyIDSet = "strategy", 0, false
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -action strategy and no -strategy-id returned no error, want an error")
		}
	})
	t.Run("id on an action that cannot use it", func(t *testing.T) {
		action, strategyID, strategyIDSet = "indicators", 7, true
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -action indicators -strategy-id 7 returned no error, want an error")
		}
	})
}

func TestBuildPlan_RejectsSearchOnlyFlagsOnOtherActions(t *testing.T) {
	resetScreenerFlags()
	t.Cleanup(resetScreenerFlags)
	action = "recommend"

	t.Run("-condition", func(t *testing.T) {
		conditions = "pettm:0:1"
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -action recommend -condition returned no error, want an error")
		}
	})
	t.Run("-show", func(t *testing.T) {
		conditions, show = "", "capmk"
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -action recommend -show returned no error, want an error")
		}
	})
	t.Run("whitespace-only values are not treated as set", func(t *testing.T) {
		conditions, show = "   ", "\t"
		if _, err := buildPlan(); err != nil {
			t.Errorf("buildPlan with whitespace-only -condition/-show = error %v, want them treated as unset", err)
		}
	})
}

func TestBuildPlan_SearchValidatesPagingAndCarriesTheConditions(t *testing.T) {
	resetScreenerFlags()
	t.Cleanup(resetScreenerFlags)
	action, conditions, show, page, size = "search", "pettm:0:1,roe:5:", "capmk,roe", 0, 50

	p, err := buildPlan()
	if err != nil {
		t.Fatalf("buildPlan = error %v", err)
	}
	if p.page != 0 || p.size != 50 {
		t.Errorf("page/size = %d/%d, want 0/50", p.page, p.size)
	}
	if len(p.conditions) != 2 {
		t.Fatalf("plan has %d conditions, want 2", len(p.conditions))
	}
	if p.conditions[0].Key != "pettm" || p.conditions[1].Key != "roe" {
		t.Errorf("plan conditions = %+v, want pettm then roe", p.conditions)
	}
	if len(p.condText) != 2 || p.condText[0] != "pettm:0:1" || p.condText[1] != "roe:5:" {
		t.Errorf("plan condText = %v, want the two entries as typed", p.condText)
	}
	if len(p.show) != 2 || p.show[0] != "capmk" || p.show[1] != "roe" {
		t.Errorf("plan show = %v, want [capmk roe]", p.show)
	}

	t.Run("negative page", func(t *testing.T) {
		page = -1
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -page -1 returned no error, want an error")
		}
	})
	t.Run("non-positive size", func(t *testing.T) {
		page, size = 0, 0
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -size 0 returned no error, want an error")
		}
		page, size = 0, -5
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with -size -5 returned no error, want an error")
		}
	})
	t.Run("page 0 and size 1 are the boundaries and must be accepted", func(t *testing.T) {
		page, size = 0, 1
		if _, err := buildPlan(); err != nil {
			t.Errorf("buildPlan with -page 0 -size 1 = error %v, want it accepted", err)
		}
	})
	t.Run("a malformed condition aborts the plan", func(t *testing.T) {
		page, size, conditions = 0, 20, "pettm"
		if _, err := buildPlan(); err == nil {
			t.Error("buildPlan with a malformed -condition returned no error, want an error")
		}
	})
}

// The screener's page 0 is the first page, unlike every other command in the
// repo, and the plan must hand the 0-indexed value to the SDK unchanged.
func TestBuildPlan_PageZeroIsPassedThroughUnchanged(t *testing.T) {
	resetScreenerFlags()
	t.Cleanup(resetScreenerFlags)
	action, conditions, page = "search", "pettm::30", 0

	p, err := buildPlan()
	if err != nil {
		t.Fatalf("buildPlan = error %v", err)
	}
	if p.page != 0 {
		t.Errorf("plan page = %d, want 0 (0-indexed, page 0 is the first page)", p.page)
	}
	if got := ordinal(p.page + 1); got != "1st" {
		t.Errorf("ordinal(plan.page+1) = %q, want %q", got, "1st")
	}
}
