package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/trade"
	"github.com/shopspring/decimal"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// The three parsers in this file are the only thing standing between a typo and
// an order the exchange will accept: the SDK's OrderType/OrderSide/TimeType
// are plain strings, so a mistyped word is not a compile error and not a
// runtime failure, it is simply a string the API does not recognise (or worse,
// one it does). Each test therefore asserts the exact SDK constant on success,
// and a non-nil error on rejection — "no error" is never the assertion.

// ------------------------------------------------------------------ helpers

// captureStderr redirects os.Stderr for the duration of fn and returns what was
// written to it. A temp file rather than a pipe: a refusal block is larger than
// a pipe buffer, and a blocked write with nobody draining the pipe would
// deadlock the test instead of failing it.
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

// captureStdout is captureStderr for the dry-run request preview.
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

// tradeState is the package-level flag set the write paths read. main() never
// runs in a test, so the defaults here are the only values the write paths
// would ever see, and a test that forgets to set one is testing a zero value it
// did not mean to.
type tradeState struct {
	symbol      string
	orderID     string
	quantity    uint64
	price       string
	side        string
	orderType   string
	timeInForce string
	remark      string
	confirmLive bool
}

// setTradeFlags installs a complete, valid flag set and restores the previous
// values when the test ends, so tests stay independent under -shuffle.
func setTradeFlags(t *testing.T, s tradeState) {
	t.Helper()
	prev := tradeState{symbol, orderID, quantity, price, side, orderType, timeInForce, remark, confirmLive}
	symbol, orderID, quantity = s.symbol, s.orderID, s.quantity
	price, side, orderType = s.price, s.side, s.orderType
	timeInForce, remark, confirmLive = s.timeInForce, s.remark, s.confirmLive
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		symbol, orderID, quantity = prev.symbol, prev.orderID, prev.quantity
		price, side, orderType = prev.price, prev.side, prev.orderType
		timeInForce, remark, confirmLive = prev.timeInForce, prev.remark, prev.confirmLive
	})
}

// validTradeFlags is the flag set main() would produce for a plain
// `-action submit -symbol 700.HK -qty 100 -price 500` invocation.
func validTradeFlags(t *testing.T) {
	t.Helper()
	setTradeFlags(t, tradeState{
		symbol:      "700.HK",
		orderID:     "ord-1",
		quantity:    100,
		price:       "500.00",
		side:        "Buy",
		orderType:   "LO",
		timeInForce: "Day",
		remark:      "",
		confirmLive: false,
	})
}

// blockedCfg is a Config that cannot authorise a write: dry run on, simulated
// mode. It carries no credentials, so nothing in a test can reach the network
// through it.
func blockedCfg() *appcfg.Config {
	return &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
}

// isBlocked reports whether an error is a guard refusal rather than an ordinary
// failure. The distinction is the whole point of the gate: 3 means "declined to
// act", 1 means "tried and broke".
func isBlocked(err error) bool { return errors.Is(err, appcfg.ErrBlocked) }

// failConnect records an attempt to build an SDK context. Any call to it is a
// test failure: the write paths must refuse before they create a client, because
// a blocked write that dials the API is not a refusal.
type failConnect struct{ calls int }

func (c *failConnect) connect() (*trade.TradeContext, error) {
	c.calls++
	return nil, context.Canceled
}

// ------------------------------------------------------------------ order type

// The SDK's own values are pinned first. parseOrderType compares against these
// constants, so a swap inside the SDK's declaration would otherwise make two
// cases in the switch equivalent and silently drop one of them.
func TestParseOrderType_TheSDKConstantValuesArePinned(t *testing.T) {
	for got, want := range map[trade.OrderType]string{
		trade.OrderTypeLO:      "LO",
		trade.OrderTypeELO:     "ELO",
		trade.OrderTypeMO:      "MO",
		trade.OrderTypeAO:      "AO",
		trade.OrderTypeALO:     "ALO",
		trade.OrderTypeODD:     "ODD",
		trade.OrderTypeLIT:     "LIT",
		trade.OrderTypeMIT:     "MIT",
		trade.OrderTypeTSLPAMT: "TSLPAMT",
		trade.OrderTypeTSLPPCT: "TSLPPCT",
		trade.OrderTypeTSMAMT:  "TSMAMT",
		trade.OrderTypeTSMPCT:  "TSMPCT",
		trade.OrderTypeSLO:     "SLO",
	} {
		if string(got) != want {
			t.Errorf("SDK OrderType %q != pinned %q; parseOrderType's switch cases are now ambiguous", got, want)
		}
	}
}

func TestParseOrderType_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	all := []trade.OrderType{
		trade.OrderTypeLO, trade.OrderTypeELO, trade.OrderTypeMO,
		trade.OrderTypeAO, trade.OrderTypeALO, trade.OrderTypeODD,
		trade.OrderTypeLIT, trade.OrderTypeMIT, trade.OrderTypeTSLPAMT,
		trade.OrderTypeTSLPPCT, trade.OrderTypeTSMAMT, trade.OrderTypeTSMPCT,
		trade.OrderTypeSLO,
	}
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
		// The parser upper-cases, so lower case and padding are normalised
		// rather than rejected. That is deliberate: the SDK's values are
		// upper-case, the user's are not obliged to be.
		{"lo", trade.OrderTypeLO},
		{"  slo  ", trade.OrderTypeSLO},
		{"\tElo\n", trade.OrderTypeELO},
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
	// Every SDK constant must be reachable, or a documented order type becomes
	// unusable with no way to tell that from a typo.
	for _, want := range all {
		got, err := parseOrderType(string(want))
		if err != nil {
			t.Errorf("parseOrderType(%q) = error %v, but it is an SDK OrderType constant", want, err)
			continue
		}
		if got != want {
			t.Errorf("parseOrderType(%q) = %q, want the same constant back", want, got)
		}
	}
}

func TestParseOrderType_UnrecognisedValueIsAnErrorNotAnEmptyOrderType(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string: the zero OrderType is \"\", which the API would reject"},
		{"   ", "whitespace only, after TrimSpace this is the empty string again"},
		{"XX", "no such order type"},
		{"LIMIT", "the long name, not the code"},
		{"LO1", "a real code with a stray digit"},
		{"LO-", "a real code with a stray punctuation"},
		{"ELOO", "a real code with a doubled trailing letter"},
		{"Buy", "valid for parseSide, not for -type"},
		{"Day", "valid for parseTimeInForce, not for -type"},
		{"GTC", "valid for parseTimeInForce, not for -type"},
		{"0", "a number: order types are codes, not ordinals"},
		{"L O", "a real code split by a space; TrimSpace removes surrounding whitespace, not interior"},
		{"LO-", "a real code with a stray punctuation"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseOrderType(tt.in)
			if err == nil {
				t.Fatalf("parseOrderType(%q) = %q with no error; an unrecognised order type must be an error, "+
					"not a string the exchange will reject on our behalf", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseOrderType(%q) = %q alongside an error; the rejected value must be the zero value", tt.in, got)
			}
			if !strings.Contains(err.Error(), "-type") {
				t.Errorf("error %q does not name the flag the user has to fix", err)
			}
		})
	}
}

// ------------------------------------------------------------------ side

func TestParseSide_TheSDKConstantValuesArePinned(t *testing.T) {
	if trade.OrderSideBuy != "Buy" {
		t.Errorf("SDK OrderSideBuy = %q, want \"Buy\"", trade.OrderSideBuy)
	}
	if trade.OrderSideSell != "Sell" {
		t.Errorf("SDK OrderSideSell = %q, want \"Sell\"", trade.OrderSideSell)
	}
}

func TestParseSide_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want trade.OrderSide
	}{
		{"Buy", trade.OrderSideBuy},
		{"Sell", trade.OrderSideSell},
		// The parser lower-cases, so "BUY" and "SELL" are normalised to the
		// SDK's own mixed-case constants rather than passed through.
		{"buy", trade.OrderSideBuy},
		{"sell", trade.OrderSideSell},
		{"BUY", trade.OrderSideBuy},
		{"  Sell  ", trade.OrderSideSell},
		{"\tbuy\n", trade.OrderSideBuy},
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

func TestParseSide_UnrecognisedValueIsAnErrorNotAnEmptyOrderSide(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"  ", "whitespace only"},
		{"B", "one letter: looks like a typo for Buy"},
		{"SEN", "a bare prefix"},
		{"Buy to Open", "a phrase"},
		{"long", "the other side of the trade, in words"},
		{"short", "ditto"},
		{"hold", "a third verb, not a side"},
		{"LO", "valid for parseOrderType, not for -side"},
		{"Day", "valid for parseTimeInForce, not for -side"},
		{"0", "a number: sides are not ordinals"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseSide(tt.in)
			if err == nil {
				t.Fatalf("parseSide(%q) = %q with no error; a wrong side on a real order is a real-money mistake", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseSide(%q) = %q alongside an error; the rejected value must be the zero value", tt.in, got)
			}
			if !strings.Contains(err.Error(), "-side") {
				t.Errorf("error %q does not name the flag the user has to fix", err)
			}
		})
	}
}

// ------------------------------------------------------------------ time in force

// The documented past bug: the SDK's TimeTypeDay is "Day", not "DAY". An
// earlier version of this file compared the upper-cased input against "DAY" and
// returned the literal string "DAY" for the default, which the API rejected
// with `unknown -tif "Day"`. Both halves of that are pinned here: the input
// spellings that must be accepted, and the exact constant that comes back.
func TestParseTimeInForce_DayIsTheSDKSpellingAndNotDAY(t *testing.T) {
	if trade.TimeTypeDay != "Day" {
		t.Fatalf("SDK TimeTypeDay = %q; the demo docs and this test both say \"Day\". "+
			"If the SDK changed, every -tif Day order in this repo is now sending a wrong value", trade.TimeTypeDay)
	}
	if trade.TimeTypeGTC != "GTC" || trade.TimeTypeGTD != "GTD" {
		t.Fatalf("SDK TimeType values moved: GTC=%q GTD=%q", trade.TimeTypeGTC, trade.TimeTypeGTD)
	}
	for _, in := range []string{"Day", "day", "DAY", "  Day  ", "\tday\n"} {
		t.Run(in, func(t *testing.T) {
			got, err := parseTimeInForce(in)
			if err != nil {
				t.Fatalf("parseTimeInForce(%q) = error %v, but the -tif default is \"Day\"", in, err)
			}
			if got != trade.TimeTypeDay {
				t.Errorf("parseTimeInForce(%q) = %q, want the SDK constant %q — "+
					"returning the upper-cased input instead is the exact bug this test exists for", in, got, trade.TimeTypeDay)
			}
		})
	}
}

func TestParseTimeInForce_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want trade.TimeType
	}{
		{"Day", trade.TimeTypeDay},
		{"GTC", trade.TimeTypeGTC},
		{"GTD", trade.TimeTypeGTD},
		{"gtc", trade.TimeTypeGTC},
		{"gtd", trade.TimeTypeGTD},
		{" GTC ", trade.TimeTypeGTC},
		{"\tDay\n", trade.TimeTypeDay},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseTimeInForce(tt.in)
			if err != nil {
				t.Fatalf("parseTimeInForce(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseTimeInForce(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseTimeInForce_UnrecognisedValueIsAnErrorNotAnEmptyTimeType(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string"},
		{"   ", "whitespace only"},
		{"GTE", "one letter from a real constant"},
		{"GTX", "one letter from a real constant"},
		{"DAY ORDERS", "the long name"},
		{"Good Til Canceled", "the long name"},
		{"D", "a prefix of the default"},
		{"DAYS", "the default with a trailing letter"},
		{"BUY", "valid for parseSide, not for -tif"},
		{"LO", "valid for parseOrderType, not for -tif"},
		{"weekly", "a DCA frequency, not a time in force"},
		{"0", "a number: time in forces are not ordinals"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseTimeInForce(tt.in)
			if err == nil {
				t.Fatalf("parseTimeInForce(%q) = %q with no error; an order with a made-up time in force "+
					"is never filled and never explained", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseTimeInForce(%q) = %q alongside an error; the rejected value must be the zero value", tt.in, got)
			}
			if !strings.Contains(err.Error(), "-tif") {
				t.Errorf("error %q does not name the flag the user has to fix", err)
			}
		})
	}
}

// The three vocabularies are separate. A word that is valid for one parser must
// not be accepted by another, or -type would accept "-side Buy" and quietly
// send a Buy side as the order type.
func TestTradeParsers_VocabulariesDoNotOverlap(t *testing.T) {
	otherParsers := map[string]string{
		"Buy": "parseSide", "Sell": "parseSide", "buy": "parseSide",
		"Day": "parseTimeInForce", "GTC": "parseTimeInForce", "GTD": "parseTimeInForce",
	}
	for word, owner := range otherParsers {
		if got, err := parseOrderType(word); err == nil {
			t.Errorf("parseOrderType(%q) = %q, but %q is the vocabulary of %s", word, got, word, owner)
		}
	}
	for _, word := range []string{"LO", "SLO", "MIT", "lo"} {
		if got, err := parseSide(word); err == nil {
			t.Errorf("parseSide(%q) = %q, but that is an order type", word, got)
		}
		if got, err := parseTimeInForce(word); err == nil {
			t.Errorf("parseTimeInForce(%q) = %q, but that is an order type", word, got)
		}
	}
}

// ------------------------------------------------------------------ gate

// A write is authorised only by both --confirm-live and a Config that allows
// it. Each alone must refuse, and the refusal must be a *BlockedError so the
// process exits 3 rather than 0.
func TestGuard_BothSwitchesAreRequiredAndNeitherAloneOpensTheGate(t *testing.T) {
	liveButConfirmed := &appcfg.Config{Mode: appcfg.ModeLive, DryRun: true}
	confirmedButDryRun := &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
	openGate := &appcfg.Config{Mode: appcfg.ModeLive, DryRun: false}

	tests := []struct {
		name      string
		cfg       *appcfg.Config
		confirmed bool
		wantBlock bool
	}{
		{"neither switch set", blockedCfg(), false, true},
		{"flag passed but dry run still on", liveButConfirmed, true, true},
		{"dry run off but flag missing", confirmedButDryRun, false, true},
		{"simulated mode with dry run off still blocks", confirmedButDryRun, true, true},
		{"both switches set", openGate, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guard(tt.cfg, tt.confirmed, "submit an order")
			if tt.wantBlock {
				if err == nil {
					t.Fatal("guard() = nil; a write was authorised without all three conditions")
				}
				if !isBlocked(err) {
					t.Errorf("guard() = %v, want a *config.BlockedError so the process exits %d", err, appcfg.ExitBlocked)
				}
			} else if err != nil {
				t.Errorf("guard() = %v, want nil", err)
			}
		})
	}
}

// guard("") must still be refused by GuardWrite: an empty action description
// would put a blank "refusing to : DRY RUN is active" in front of the user.
func TestGuard_EmptyActionDescriptionIsRefused(t *testing.T) {
	openGate := &appcfg.Config{Mode: appcfg.ModeLive, DryRun: false}
	if err := guard(openGate, true, ""); err == nil {
		t.Fatal("guard(cfg, true, \"\") = nil; GuardWrite requires an action description")
	}
}

// gate prints why it refused, on stderr, before returning the BlockedError: a
// silent refusal looks like a crash, and "nothing happened" is exactly what a
// safety gate must not leave unexplained.
func TestGate_RefusalIsExplainedOnStderrAndReturnedNotSwallowed(t *testing.T) {
	var err error
	out := captureStderr(t, func() { err = gate(blockedCfg(), false, "cancel an order") })
	if err == nil {
		t.Fatal("gate() = nil, so a blocked write would exit 0 and be indistinguishable from a placed order")
	}
	if !isBlocked(err) {
		t.Errorf("gate() = %v, want a *config.BlockedError", err)
	}
	for _, want := range []string{"BLOCKED:", "DRY RUN", "--confirm-live", "LONGPORT_DRY_RUN=0", "LONGPORT_MODE=live"} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal block is missing %q; the user is not told which switch to change.\nblock was:\n%s", want, out)
		}
	}

	// Once the flag is passed the refusal comes from GuardWrite instead, and
	// that message must name the action — "refusing to cancel an order" is what
	// tells the user which of the three write paths stopped them.
	var err2 error
	out2 := captureStderr(t, func() {
		err2 = gate(&appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}, true, "cancel an order")
	})
	if err2 == nil || !isBlocked(err2) {
		t.Fatalf("gate() with --confirm-live and dry run on = %v, want a *config.BlockedError", err2)
	}
	if !strings.Contains(out2, "cancel an order") {
		t.Errorf("refusal block does not name the action the user asked for:\n%s", out2)
	}
}

// ------------------------------------------------------------------ write paths

// doSubmit must refuse a missing -symbol, a zero -qty and each bad enum before
// it creates an SDK context, and must never create one at all while the gate is
// shut. The connect counter is the assertion that matters: it is the only way
// to see that no request was attempted.
func TestDoSubmit_RefusesBadInputBeforeAnySDKContextIsCreated(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func()
		wantErr string
	}{
		{"missing symbol", func() { symbol = "" }, "-symbol"},
		{"zero quantity", func() { quantity = 0 }, "-qty"},
		{"unrecognised order type", func() { orderType = "LIMIT" }, "-type"},
		{"unrecognised side", func() { side = "long" }, "-side"},
		{"unrecognised time in force", func() { timeInForce = "GTE" }, "-tif"},
		{"unparsable price", func() { price = "five hundred" }, "-price"},
		// A price of only whitespace must not be read as "no price given":
		// that would quietly turn a limit order into one at decimal.Zero.
		{"whitespace-only price", func() { price = "   " }, "-price"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validTradeFlags(t)
			tt.mutate()
			c := &failConnect{}
			var err error
			captureStdout(t, func() {
				err = doSubmit(context.Background(), blockedCfg(), c.connect)
			})
			if err == nil {
				t.Fatalf("doSubmit() = nil; %s should have been refused", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("doSubmit() = %v, want the flag error, not a gate refusal: "+
					"an unparsable flag must be reported as such even when the gate is shut", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s); flag validation must happen before any client exists", c.calls)
			}
		})
	}
}

// doSubmit used to test `symbol == ""` rather than strings.TrimSpace, so a
// whitespace-only -symbol passed validation and was stopped by the gate, which
// reported it as a safety-gate refusal. That message names the three switches
// and not the flag the user actually got wrong. A bad flag has to be reported as
// a bad flag even when the gate is shut, so the symbol is trimmed first.
//
// Originally pinned as
// TestDoSubmit_FindingWhitespaceOnlySymbolIsNotNamedAsABadFlag.
func TestDoSubmit_WhitespaceOnlySymbolIsAFlagErrorNotAGateRefusal(t *testing.T) {
	for _, bad := range []string{" ", "\t", "  \n ", "   "} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			validTradeFlags(t)
			symbol = bad
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = doSubmit(context.Background(), blockedCfg(), c.connect) })
			if err == nil {
				t.Fatalf("doSubmit() = nil with -symbol %q and a shut gate", bad)
			}
			if isBlocked(err) {
				t.Errorf("doSubmit() = %v, a gate refusal; the whole point is that a blank -symbol "+
					"is a bad flag, not something the gate should be asked about", err)
			}
			if !strings.Contains(err.Error(), "-symbol") {
				t.Errorf("error %q does not name -symbol", err)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) while blocked", c.calls)
			}
		})
	}
}

// The other direction: trimming is normalisation, not rejection, so a symbol
// with a stray space is the symbol the user meant. Without this the fix above
// would have been a stricter parser, not a consistent one.
func TestDoSubmit_APaddedSymbolIsNormalisedRatherThanRefused(t *testing.T) {
	validTradeFlags(t)
	symbol = "  700.HK  "
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doSubmit(context.Background(), blockedCfg(), c.connect) })
	if err == nil || !isBlocked(err) {
		t.Fatalf("doSubmit() = %v, want a *config.BlockedError: a padded symbol is a valid order "+
			"and must reach the gate", err)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	// The request, and so the API, must carry the trimmed symbol.
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "symbol ") {
			continue
		}
		if got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "symbol")); got != "700.HK" {
			t.Errorf("preview sends symbol %q, want the trimmed \"700.HK\"", got)
		}
	}
}

// The happy path reaches the gate, is refused, and stops there. A dry run is
// the default, so a correct submit in a default environment must not dial.
func TestDoSubmit_AValidOrderIsStillRefusedByTheGateWithNoNetworkCall(t *testing.T) {
	validTradeFlags(t)
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() {
		err = doSubmit(context.Background(), blockedCfg(), c.connect)
	})
	if err == nil {
		t.Fatal("doSubmit() = nil with the gate shut")
	}
	if !isBlocked(err) {
		t.Errorf("doSubmit() = %v, want a *config.BlockedError so the exit status is %d", err, appcfg.ExitBlocked)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	// The preview is printed BEFORE the gate on purpose, so a dry run is
	// informative rather than just a refusal. It must show the SDK's own
	// spelling of the time in force: "Day", never "DAY".
	for _, want := range []string{"SubmitOrder", "order_type", "LO", "side", "Buy", "time_force", "Day"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run preview is missing %q; the user asked for a preview, not a refusal.\npreview was:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DAY") {
		t.Errorf("preview contains the upper-cased \"DAY\"; the SDK's TimeTypeDay is \"Day\":\n%s", out)
	}
}

func TestDoReplace_RefusesMissingOrderIDAndZeroQuantityBeforeConnecting(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func()
		wantErr string
	}{
		{"missing order id", func() { orderID = "" }, "-order-id"},
		{"whitespace-only order id", func() { orderID = "   " }, "-order-id"},
		{"zero quantity", func() { quantity = 0; orderID = "ord-1" }, "-qty"},
		{"unparsable price", func() { price = "1.2.3" }, "-price"},
		{"whitespace-only price", func() { price = " " }, "-price"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validTradeFlags(t)
			tt.mutate()
			c := &failConnect{}
			var err error
			captureStdout(t, func() {
				err = doReplace(context.Background(), blockedCfg(), c.connect)
			})
			if err == nil {
				t.Fatalf("doReplace() = nil; %s should have been refused", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) during flag validation", c.calls)
			}
		})
	}
}

func TestDoCancel_RequiresAnOrderIDAndNeverConnectsWhileBlocked(t *testing.T) {
	for _, bad := range []string{"", "   ", "\t"} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			validTradeFlags(t)
			orderID = bad
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = doCancel(context.Background(), blockedCfg(), c.connect) })
			if err == nil {
				t.Fatalf("doCancel() = nil with -order-id %q", bad)
			}
			// A blank id is a bad flag, not a gate refusal: a BLOCKED message here
			// would name three switches and not the one the user got wrong.
			if isBlocked(err) {
				t.Errorf("doCancel() = %v, a gate refusal; want the missing -order-id", err)
			}
			if !strings.Contains(err.Error(), "-order-id") {
				t.Errorf("error %q does not name -order-id", err)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) during flag validation", c.calls)
			}
		})
	}
	validTradeFlags(t)

	// With an id it reaches the gate, which is still shut.
	orderID = "ord-1"
	c2 := &failConnect{}
	var err2 error
	captureStdout(t, func() { err2 = doCancel(context.Background(), blockedCfg(), c2.connect) })
	if err2 == nil || !isBlocked(err2) {
		t.Errorf("doCancel() with an order id = %v, want a *config.BlockedError", err2)
	}
	if c2.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c2.calls)
	}
}

// ------------------------------------------------------------------ small helpers

// describe sorts its keys because the dry-run output is meant to be diffed
// against a previous run. Map iteration order would make every run differ.
func TestDescribe_KeysAreSortedSoThePreviewIsDiffable(t *testing.T) {
	out := captureStdout(t, func() {
		describe("SubmitOrder", map[string]string{"zeta": "1", "alpha": "2", "mid": "3"})
	})
	iAlpha := strings.Index(out, "alpha")
	iMid := strings.Index(out, "mid")
	iZeta := strings.Index(out, "zeta")
	if iAlpha < 0 || iMid < 0 || iZeta < 0 {
		t.Fatalf("preview is missing a key:\n%s", out)
	}
	if !(iAlpha < iMid && iMid < iZeta) {
		t.Errorf("preview keys are not in sorted order, so two identical requests render differently:\n%s", out)
	}
}

func TestDec_NilIsAbsentAndZeroIsARealFigure(t *testing.T) {
	if got := dec(nil); got != "-" {
		t.Errorf("dec(nil) = %q, want \"-\": the SDK models absent amounts as nil pointers", got)
	}
	zero, five := decimal.NewFromInt(0), decimal.RequireFromString("1234.5")
	if got := dec(&zero); got != "0.00" {
		t.Errorf("dec(0) = %q, want \"0.00\": zero is a value, not an absence", got)
	}
	if got := dec(&five); got != "1234.50" {
		t.Errorf("dec(1234.5) = %q, want \"1234.50\"", got)
	}
}

func TestTruncate_ShortensAndMarksTheCut(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"700.HK", 24, "700.HK"},
		{"", 10, ""},
		{"12345", 5, "12345"},
		{"123456", 5, "1234…"},
		{"TENCENT", 4, "TEN…"},
		// n == 1 keeps the ellipsis and drops the content: a one-column cell has
		// no room for both, and an empty cell would read as "no name".
		{"ab", 1, "…"},
		{"", 1, ""},
		// A string that already fits is returned untouched, including when the
		// width is generous or the string is exactly as wide as the column.
		{"700.HK", 100, "700.HK"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := truncate(tt.in, tt.n); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

// truncate indexed s[:n-1], so n <= 0 sliced at a negative index and panicked.
// Every call site passes a compile-time constant of 24, 20 or 10, so it was
// unreachable — which is why the boundary used to be pinned as a panic rather
// than a value. A width computed at run time would have panicked in front of a
// user instead, so n <= 0 now returns the same thing n == 1 does: the ellipsis
// on its own, which cli.Truncate already did for every other binary in this repo.
//
// Originally pinned as TestTruncate_ZeroWidthPanicsAndNoCallSiteCanReachIt.
func TestTruncate_ANonPositiveWidthIsSafeAndYieldsTheEllipsis(t *testing.T) {
	for _, n := range []int{0, -1, -20} {
		t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
			// No recover: a panic here fails the test, which is the point.
			if got := truncate("700.HK", n); got != "…" {
				t.Errorf("truncate(\"700.HK\", %d) = %q, want the ellipsis alone: a width with no "+
					"room for a character still has to say the cell was cut", n, got)
			}
			// An empty string is an absence, not a cut, whatever the width.
			if got := truncate("", n); got != "" {
				t.Errorf("truncate(\"\", %d) = %q, want \"\"", n, got)
			}
		})
	}
}
