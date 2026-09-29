package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/sharelist"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// The -symbols check in this command is the only thing standing between a
// malformed symbol and an API rejection, because the SDK converts "700.HK" to
// the counter_id "ST/HK/700" and passes anything without a dot straight through
// as the symbol itself. sharelist/context.go symbolToCounterID returns the
// input unchanged when there is no dot, so a bare "700" becomes the counter_id
// "700" and the API refuses it with a message that names neither the flag nor
// the format.
//
// The same helper is what makes the *other* shapes dangerous: it splits on the
// last dot and upper-cases the tail without checking either half, so ".HK",
// "700.", "700.HK.US" and "700 .HK" all produce an id no instrument can match
// while passing a local check that only asked whether a dot was present. The
// local check is therefore exactly CODE.MARKET, and the tests below pin both
// what it must refuse and what it must keep accepting.

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

type sharelistState struct {
	action          string
	count           int
	detailID        int64
	popular         bool
	stockLimit      int
	listName        string
	listDescription string
	symbols         string
	confirmLive     bool
	showState       bool
}

func setSharelistFlags(t *testing.T, s sharelistState) {
	t.Helper()
	prev := sharelistState{action, count, detailID, popular, stockLimit,
		listName, listDescription, symbols, confirmLive, showState}
	action, count, detailID, popular, stockLimit = s.action, s.count, s.detailID, s.popular, s.stockLimit
	listName, listDescription, symbols = s.listName, s.listDescription, s.symbols
	confirmLive, showState = s.confirmLive, s.showState
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		action, count, detailID, popular, stockLimit = prev.action, prev.count, prev.detailID, prev.popular, prev.stockLimit
		listName, listDescription, symbols = prev.listName, prev.listDescription, prev.symbols
		confirmLive, showState = prev.confirmLive, prev.showState
	})
}

func validSharelistFlags(t *testing.T) {
	t.Helper()
	setSharelistFlags(t, sharelistState{
		action:      "add",
		count:       20,
		detailID:    12345,
		stockLimit:  20,
		listName:    "My list",
		symbols:     "700.HK",
		showState:   false,
		confirmLive: false,
	})
}

func blockedCfg() *appcfg.Config {
	return &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
}

func isBlocked(err error) bool { return errors.Is(err, appcfg.ErrBlocked) }

type failConnect struct{ calls int }

func (c *failConnect) connect() (*sharelist.SharelistContext, error) {
	c.calls++
	return nil, context.Canceled
}

func (c *failConnect) reached() bool { return c.calls > 0 }

// ------------------------------------------------------------------ symbol format

// The documented reason for the local check, restated as a test: the SDK turns
// "700.HK" into "ST/HK/700" and leaves a bare code alone, so a bare code is
// rejected by the API rather than converted. The other rows record what the
// same helper does with entries that merely contain a dot — it splits on the
// LAST one and never checks either half — which is why the local check has to
// be stricter than "contains a dot".
func TestSharelistSymbolFormat_TheSDKConvertsDottedSymbolsAndPassesBareOnesThrough(t *testing.T) {
	// This mirrors the SDK's own helper, which is unexported, so it is spelled
	// out here: the point is the asymmetry the local check exists to catch.
	convert := func(symbol string) string {
		idx := strings.LastIndex(symbol, ".")
		if idx < 0 {
			return symbol
		}
		return fmt.Sprintf("ST/%s/%s", strings.ToUpper(symbol[idx+1:]), symbol[:idx])
	}
	for _, tt := range []struct{ in, want string }{
		{"700.HK", "ST/HK/700"},
		{"700.hk", "ST/HK/700"},
		{"700", "700"},
		// Split on the last dot: the code keeps its own dot and the market is
		// whatever follows, so "700.HK.US" is not rejected locally either.
		{"700.HK.US", "ST/US/700.HK"},
		{".HK", "ST/HK/"},
		{"700.", "ST//700"},
		{"700 .HK", "ST/HK/700 "},
	} {
		if got := convert(tt.in); got != tt.want {
			t.Errorf("the SDK's conversion of %q is %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A bare code has no dot, so the local check catches it. This is the case the
// check exists for and it must be reported by name, with the format shown.
func TestDoSecurities_ASymbolWithNoMarketIsRejectedBeforeAnyRequest(t *testing.T) {
	for _, mode := range []string{"add", "remove", "sort"} {
		t.Run(mode, func(t *testing.T) {
			for _, bad := range []string{"700", "AAPL", "12345", "00700"} {
				t.Run(bad, func(t *testing.T) {
					validSharelistFlags(t)
					symbols = bad
					c := &failConnect{}
					var err error
					captureStdout(t, func() {
						err = doSecurities(context.Background(), blockedCfg(), c.connect, mode)
					})
					if err == nil {
						t.Fatalf("doSecurities(%s, -symbols %q) = nil; the SDK would send %q as the "+
							"counter_id itself and the API would refuse it", mode, bad, bad)
					}
					if isBlocked(err) {
						t.Errorf("error is a gate refusal, want the symbol error: a symbol the API "+
							"cannot resolve must be reported as such even when the gate is shut. got %v", err)
					}
					if !strings.Contains(err.Error(), "CODE.MARKET") {
						t.Errorf("error %q does not state the required format", err)
					}
					if !strings.Contains(err.Error(), bad) {
						t.Errorf("error %q does not name the offending symbol %q", err, bad)
					}
					if !strings.Contains(err.Error(), "counter_id") {
						t.Errorf("error %q does not explain why the form matters", err)
					}
					if c.reached() {
						t.Error("connect() was called; the symbol check must precede any client")
					}
				})
			}
		})
	}
}

// An empty -symbols never reaches the format check: splitList drops empty
// entries, so the list is empty and the required-flag error is the right one.
func TestDoSecurities_AnEmptySymbolListIsReportedAsAMissingFlag(t *testing.T) {
	for _, mode := range []string{"add", "remove", "sort"} {
		t.Run(mode, func(t *testing.T) {
			for _, in := range []string{"", "   ", ",", ", ,", " , , "} {
				t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
					validSharelistFlags(t)
					symbols = in
					c := &failConnect{}
					var err error
					captureStdout(t, func() {
						err = doSecurities(context.Background(), blockedCfg(), c.connect, mode)
					})
					if err == nil {
						t.Fatalf("doSecurities(%s, -symbols %q) = nil", mode, in)
					}
					if !strings.Contains(err.Error(), "-symbols") {
						t.Errorf("error %q does not name -symbols", err)
					}
					if !strings.Contains(err.Error(), mode) {
						t.Errorf("error %q does not name the -action it applies to", err)
					}
					if c.reached() {
						t.Error("connect() was called with no symbols at all")
					}
				})
			}
		})
	}
}

// One bad symbol in a list is enough: the check runs over every entry and names
// the offender, so the user fixes the right one rather than bisecting the list.
func TestDoSecurities_OneBareCodeInAListIsRejectedAndNamed(t *testing.T) {
	validSharelistFlags(t)
	// Deliberately not 700.HK: the error message uses it as its example, so it
	// would be impossible to tell a blame from an example.
	symbols = "9988.HK,3690.HK,AAPL"
	c := &failConnect{}
	var err error
	captureStdout(t, func() { err = doSecurities(context.Background(), blockedCfg(), c.connect, "add") })
	if err == nil {
		t.Fatal("doSecurities() = nil with a bare code in the list")
	}
	if !strings.Contains(err.Error(), "AAPL") {
		t.Errorf("error %q does not name the bare code, so the user cannot tell which entry to fix", err)
	}
	for _, valid := range []string{"9988.HK", "3690.HK"} {
		if strings.Contains(err.Error(), valid) {
			t.Errorf("error %q blames the valid symbol %q: %q", err, valid, err)
		}
	}
}

// Every entry must be exactly CODE.MARKET. The SDK does not validate symbols:
// it splits on the LAST dot and builds the counter_id from the two halves, so
// anything with a dot in it used to pass the local check and produce an id the
// API cannot resolve — ".HK" as a counter_id with no code, "700." with no
// market, "700.HK.US" split across the wrong dot, and "700 .HK" with a space
// carried into the id. Each is now refused, and each error names the part that
// is wrong rather than reporting a generic "wrong shape".
func TestDoSecurities_EachMalformedEntryIsRejectedAndNamedForItsOwnDefect(t *testing.T) {
	tests := []struct {
		sym    string
		defect string
	}{
		{".HK", "empty code"},
		{"700.", "empty market"},
		{"700.HK.US", "2 dots"},
		{"700 .HK", "whitespace"},
		{"700.H K", "whitespace"},
		{"700.\tHK", "whitespace"},
		{".", "empty code"},
		{"..", "2 dots"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.sym), func(t *testing.T) {
			validSharelistFlags(t)
			symbols = tt.sym
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() {
				err = doSecurities(context.Background(), blockedCfg(), c.connect, "add")
			})
			if err == nil {
				t.Fatalf("doSecurities(-symbols %q) = nil; want the malformed entry refused", tt.sym)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal, want the symbol error: %q must be reported as "+
					"unusable even while the gate is shut. got %v", tt.sym, err)
			}
			// The messages quote the entry, so a control character comes back
			// escaped; compare against the same quoting.
			if quoted := strconv.Quote(tt.sym); !strings.Contains(err.Error(), quoted) {
				t.Errorf("error %q does not name the offending entry %s", err, quoted)
			}
			if !strings.Contains(err.Error(), tt.defect) {
				t.Errorf("error %q does not say what is wrong with %q (want %q)", err, tt.sym, tt.defect)
			}
			if !strings.Contains(err.Error(), "CODE.MARKET") {
				t.Errorf("error %q does not state the required format", err)
			}
			if c.reached() {
				t.Error("connect() was called; the symbol check must precede any client")
			}
			// The check runs before the preview too, so nothing claims a
			// request would be sent for an entry that cannot be sent.
			if strings.Contains(out, "[DRY-RUN]") {
				t.Errorf("the dry-run preview was printed for a rejected entry:\n%s", out)
			}
		})
	}
}

// One malformed entry in an otherwise good list is enough, and the error has to
// name that entry — not the flag value, and not a neighbour the user typed
// correctly.
func TestDoSecurities_OneMalformedEntryInAListIsRejectedAndNamesTheEntry(t *testing.T) {
	for _, tt := range []struct {
		syms  string
		blame string
		ok    []string
	}{
		{"9988.HK,.US,3690.HK", ".US", []string{"9988.HK", "3690.HK"}},
		{"700.HK,700.HK.US", "700.HK.US", []string{"700.HK"}},
		{"700.HK,700 .HK", "700 .HK", []string{"700.HK"}},
	} {
		t.Run(tt.syms, func(t *testing.T) {
			validSharelistFlags(t)
			symbols = tt.syms
			c := &failConnect{}
			var err error
			captureStdout(t, func() {
				err = doSecurities(context.Background(), blockedCfg(), c.connect, "add")
			})
			if err == nil {
				t.Fatalf("doSecurities(-symbols %q) = nil", tt.syms)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal, want the symbol error. got %v", err)
			}
			if !strings.Contains(err.Error(), strconv.Quote(tt.blame)) {
				t.Errorf("error %q does not name the offending entry %q", err, tt.blame)
			}
			for _, good := range tt.ok {
				if strings.Contains(err.Error(), strconv.Quote(good)) {
					t.Errorf("error %q blames the valid entry %q", err, good)
				}
			}
			if c.reached() {
				t.Error("connect() was called; the symbol check must precede any client")
			}
		})
	}
}

// The tightened check must not narrow what the SDK can actually use: anything
// that is one non-empty code, one dot and one non-empty market is left alone,
// including a lower-case market (the SDK upper-cases it) and codes that are
// neither numeric nor three characters long.
func TestDoSecurities_AnyCorrectlyShapedEntryIsStillAccepted(t *testing.T) {
	for _, in := range []string{
		"700.HK", "700.hk", "AAPL.US", "HSI.HK", "000001.SZ",
		"9988.HK,3690.HK,700.HK",
	} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			validSharelistFlags(t)
			symbols = in
			c := &failConnect{}
			var err error
			captureStdout(t, func() {
				err = doSecurities(context.Background(), blockedCfg(), c.connect, "add")
			})
			if err == nil || !isBlocked(err) {
				t.Fatalf("doSecurities(-symbols %q) = %v, want the gate refusal", in, err)
			}
			if c.reached() {
				t.Error("connect() was called while blocked")
			}
		})
	}
}

// DELIBERATELY STRICTER THAN THE SDK. The SDK's helper splits on the LAST dot,
// so it would also convert a code that contains a dot — "BRK.B.US" becomes
// ST/US/BRK.B and a leading-dot index like ".DJI.US" becomes ST/US/.DJI. The
// local check refuses both, because it cannot tell those apart from a mistyped
// market ("700.HK.US") and one bad entry fails the whole request server-side.
// Pinned so the trade-off is a decision on record rather than an accident.
func TestCheckSymbolShape_CodesContainingADotAreRefusedToo(t *testing.T) {
	for _, in := range []string{"BRK.B.US", ".DJI.US", "700.HK.US", "700.HK.extra"} {
		err := checkSymbolShape(in)
		if err == nil {
			t.Errorf("checkSymbolShape(%q) = nil; the CLI accepts only one dot, so a code with a "+
				"dot in it must be refused rather than guessed at", in)
			continue
		}
		if !strings.Contains(err.Error(), strconv.Quote(in)) {
			t.Errorf("error %q does not name %q", err, in)
		}
	}
}

// Whitespace around an entry is trimmed by splitList, so a normally-typed flag
// value with a trailing space is handled correctly. That is the case worth
// pinning, and it is distinct from the interior-whitespace finding above.
func TestDoSecurities_WhitespaceAroundAnEntryIsTrimmedNotRejected(t *testing.T) {
	for _, in := range []string{" 700.HK ", "\t700.HK\n", " 700.HK, 9988.HK "} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			validSharelistFlags(t)
			symbols = in
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() {
				err = doSecurities(context.Background(), blockedCfg(), c.connect, "add")
			})
			if err == nil || !isBlocked(err) {
				t.Fatalf("doSecurities(-symbols %q) = %v, want the gate refusal", in, err)
			}
			if c.reached() {
				t.Error("connect() was called while blocked")
			}
			// The preview shows the trimmed symbols, not the padded ones.
			for _, line := range strings.Split(out, "\n") {
				if !strings.Contains(line, "counter_ids") {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) < 2 {
					t.Fatalf("counter_ids line has no value: %q", line)
				}
				for _, f := range fields {
					if f != "counter_ids" && strings.Contains(f, " ") {
						t.Errorf("counter_ids value %q carries whitespace the split should have removed:\n%s", f, out)
					}
				}
			}
			if !strings.Contains(out, "700.HK") {
				t.Errorf("preview is missing the symbol:\n%s", out)
			}
		})
	}
}

// ------------------------------------------------------------------ dispatch

// The three securities verbs share one function and differ by an SDK method. A
// mis-wired dispatch would send a removal when an addition was asked for, so
// each verb is pinned to the endpoint and verb the preview shows.
func TestDoSecurities_EachModeMapsToItsOwnEndpointAndMethod(t *testing.T) {
	tests := []struct {
		mode    string
		want    []string
		notWant []string
	}{
		{
			mode:    "add",
			want:    []string{"AddSecurities", "POST /v1/sharelists/{id}/items", "12345", "700.HK"},
			notWant: []string{"RemoveSecurities", "SortSecurities", "WARNING"},
		},
		{
			mode:    "remove",
			want:    []string{"RemoveSecurities", "DELETE /v1/sharelists/{id}/items", "take effect"},
			notWant: []string{"AddSecurities", "SortSecurities", "REPLACES"},
		},
		{
			mode:    "sort",
			want:    []string{"SortSecurities", "POST /v1/sharelists/{id}/items/sort", "REPLACES", "pass the full intended order"},
			notWant: []string{"AddSecurities", "RemoveSecurities", "takes effect"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			validSharelistFlags(t)
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() {
				err = doSecurities(context.Background(), blockedCfg(), c.connect, tt.mode)
			})
			if err == nil || !isBlocked(err) {
				t.Fatalf("doSecurities(%s) = %v, want a *config.BlockedError", tt.mode, err)
			}
			if c.reached() {
				t.Error("connect() was called while blocked")
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(out, notWant) {
					t.Errorf("preview contains %q, which belongs to a different mode.\npreview was:\n%s",
						notWant, out)
				}
			}
		})
	}
}

// The counter_ids line is annotated "(derived)", because the symbols the user
// typed are not what the API receives: the SDK converts 700.HK into ST/HK/700.
// Showing them unqualified would be a lie about the request body.
func TestDoSecurities_ThePreviewMarksTheCounterIDsAsDerived(t *testing.T) {
	validSharelistFlags(t)
	symbols = "700.HK,9988.HK"
	c := &failConnect{}
	out := captureStdout(t, func() {
		_ = doSecurities(context.Background(), blockedCfg(), c.connect, "add")
	})
	for _, want := range []string{"counter_ids", "700.HK,9988.HK", "(derived)"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
		}
	}
}

// The mode switch has a default that is unreachable from main() — the dispatch
// in main only ever passes add, remove or sort — so it is tested directly with
// a mode the command cannot produce. It must be an error, not a silent default
// that would pick a method.
func TestDoSecurities_AnUnreachableModeIsAnInternalErrorNotASilentDefault(t *testing.T) {
	for _, mode := range []string{"replace", "delete", "ADD", "", "sort "} {
		t.Run(fmt.Sprintf("%q", mode), func(t *testing.T) {
			validSharelistFlags(t)
			c := &failConnect{}
			var err error
			captureStdout(t, func() {
				err = doSecurities(context.Background(), blockedCfg(), c.connect, mode)
			})
			if err == nil {
				t.Fatalf("doSecurities(%q) = nil; a mode the command cannot produce must be an "+
					"internal error, not a silent fallthrough to some other SDK method", mode)
			}
			if !strings.Contains(err.Error(), "bad securities mode") {
				t.Errorf("error %q does not identify the problem as a programming error", err)
			}
			if !strings.Contains(err.Error(), mode) {
				t.Errorf("error %q does not echo the mode it was given", err)
			}
			if c.reached() {
				t.Error("connect() was called for an impossible mode")
			}
		})
	}
}

// ------------------------------------------------------------------ required flags

func TestWriteActions_RefuseMissingFlagsBeforeAnySDKContextIsCreated(t *testing.T) {
	tests := []struct {
		name    string
		set     func(t *testing.T)
		run     func(*failConnect) error
		wantErr string
	}{
		{
			name: "add without -id",
			set:  func(t *testing.T) { validSharelistFlags(t); detailID = 0 },
			run: func(c *failConnect) error {
				return doSecurities(context.Background(), blockedCfg(), c.connect, "add")
			},
			wantErr: "-id",
		},
		{
			name: "remove without -id",
			set:  func(t *testing.T) { validSharelistFlags(t); detailID = 0 },
			run: func(c *failConnect) error {
				return doSecurities(context.Background(), blockedCfg(), c.connect, "remove")
			},
			wantErr: "-id",
		},
		{
			name: "sort without -id",
			set:  func(t *testing.T) { validSharelistFlags(t); detailID = 0 },
			run: func(c *failConnect) error {
				return doSecurities(context.Background(), blockedCfg(), c.connect, "sort")
			},
			wantErr: "-id",
		},
		{
			name: "sort without -id and without -symbols",
			set:  func(t *testing.T) { validSharelistFlags(t); detailID, symbols = 0, "" },
			run: func(c *failConnect) error {
				return doSecurities(context.Background(), blockedCfg(), c.connect, "sort")
			},
			// -id is checked first, so that is the one reported.
			wantErr: "-id",
		},
		{
			name: "create without -name",
			set:  func(t *testing.T) { validSharelistFlags(t); listName = "" },
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-name",
		},
		{
			name: "create with a whitespace-only -name",
			set:  func(t *testing.T) { validSharelistFlags(t); listName = "   " },
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-name",
		},
		{
			name: "delete without -id",
			set:  func(t *testing.T) { validSharelistFlags(t); detailID = 0 },
			run: func(c *failConnect) error {
				return doDelete(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set(t)
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = tt.run(c) })
			if err == nil {
				t.Fatalf("%s was not refused", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal, want the flag error. got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
			if c.reached() {
				t.Errorf("connect() was called; validation must precede any client")
			}
		})
	}
}

// Delete is irreversible and takes the constituents with it, so the preview has
// to say so.
func TestDoDelete_PreviewWarnsThatThereIsNoUndelete(t *testing.T) {
	validSharelistFlags(t)
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doDelete(context.Background(), blockedCfg(), c.connect) })
	if err == nil || !isBlocked(err) {
		t.Fatalf("doDelete() = %v, want a *config.BlockedError", err)
	}
	if c.reached() {
		t.Error("connect() was called while blocked")
	}
	for _, want := range []string{
		"Delete", "DELETE /v1/sharelists/{id}", "12345",
		"IRREVERSIBLE", "no undelete endpoint", "constituents",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
		}
	}
}

// The SDK substitutes the name when the description is empty, so the preview
// shows the name rather than a misleading "-": a dry run has to be truthful
// about the body that would be sent.
func TestDoCreate_PreviewShowsTheNameWhenNoDescriptionIsGiven(t *testing.T) {
	tests := []struct {
		desc        string
		wantDesc    string
		wantNoDescr bool
	}{
		{"", "My list", true},
		{"   ", "My list", true},
		{"A real description", "A real description", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.desc), func(t *testing.T) {
			validSharelistFlags(t)
			listName, listDescription = "My list", tt.desc
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() { err = doCreate(context.Background(), blockedCfg(), c.connect) })
			if err == nil || !isBlocked(err) {
				t.Fatalf("doCreate() = %v, want a *config.BlockedError", err)
			}
			if c.reached() {
				t.Error("connect() was called while blocked")
			}
			for _, want := range []string{
				"Create", "POST /v1/sharelists", "name", "My list", "cover",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
				}
			}
			if tt.wantNoDescr {
				if !strings.Contains(out, "description") {
					t.Errorf("preview omits the description line entirely.\npreview was:\n%s", out)
				}
			}
		})
	}
}

// ------------------------------------------------------------------ -show-state

// -show-state is the one opt-in that makes a network call inside a blocked run,
// which is why it is off by default. Every other test in this file depends on
// connect() never being reached while blocked; this one pins that directly.
func TestShowCurrentState_DoesNothingUnlessItIsOptedIn(t *testing.T) {
	tests := []struct {
		name        string
		showState   bool
		detailID    int64
		wantReached bool
		note        string
	}{
		{"off by default", false, 12345, false, "the default; a refused write makes no request at all"},
		{"opt-in", true, 12345, true, "the documented exception: this one does read"},
		{"opt-in with no -id", true, 0, false, "nothing to fetch without an id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validSharelistFlags(t)
			showState, detailID = tt.showState, tt.detailID
			c := &failConnect{}
			showCurrentState(context.Background(), c.connect)
			if c.reached() != tt.wantReached {
				t.Errorf("connect() reached = %v, want %v: %s", c.reached(), tt.wantReached, tt.note)
			}
		})
	}
}

// A failed state fetch is reported, never fatal, and never changes the gate's
// verdict. A read that errors must not turn a refusal into a crash.
func TestShowCurrentState_AFailedFetchIsReportedAndNotFatal(t *testing.T) {
	validSharelistFlags(t)
	showState = true
	c := &failConnect{}
	out := captureStderr(t, func() { showCurrentState(context.Background(), c.connect) })
	if !c.reached() {
		t.Fatal("showCurrentState never called connect(); the opt-in was not honoured")
	}
	if !strings.Contains(out, "current state unavailable") {
		t.Errorf("stderr does not report the failed fetch.\nstderr was:\n%s", out)
	}
	if !strings.Contains(out, context.Canceled.Error()) {
		t.Errorf("stderr does not include the reason.\nstderr was:\n%s", out)
	}
}

// The gate's verdict must survive showCurrentState, whatever it does. A refused
// add with -show-state is still a refusal and still exits 3.
func TestDoSecurities_ShowStateDoesNotOpenTheGate(t *testing.T) {
	validSharelistFlags(t)
	showState = true
	c := &failConnect{}
	var err error
	captureStdout(t, func() { err = doSecurities(context.Background(), blockedCfg(), c.connect, "add") })
	if err == nil {
		t.Fatal("doSecurities() = nil with -show-state and the gate shut")
	}
	if !isBlocked(err) {
		t.Errorf("doSecurities() = %v, want a *config.BlockedError; -show-state must not change the verdict", err)
	}
}

// ------------------------------------------------------------------ gate

func TestGate_RefusalIsExplainedOnStderrAndReturnsABlockedError(t *testing.T) {
	validSharelistFlags(t)
	var err error
	out := captureStderr(t, func() { err = gate(blockedCfg(), "add securities on sharelist 12345") })
	if err == nil {
		t.Fatal("gate() = nil; a blocked write must be distinguishable from a completed one")
	}
	if !isBlocked(err) {
		t.Errorf("gate() = %v, want a *config.BlockedError", err)
	}
	for _, want := range []string{
		"[DRY-RUN] BLOCKED:", "add securities on sharelist 12345",
		"LONGPORT_SHARELIST_DRY_RUN", "--confirm-live-sharelist", "LONGPORT_MODE=live",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal block is missing %q.\nblock was:\n%s", want, out)
		}
	}
}

// ------------------------------------------------------------------ renderers

// SharelistType is a bare int32 with a gap: the SDK declares 0, 3 and 4 and
// nothing for 1 or 2. Those two values render as unknown(1) and unknown(2)
// rather than as a blank, which is the whole point of the fallback.
func TestSharelistTypeName_TheSDKConstantValuesArePinned(t *testing.T) {
	for got, want := range map[sharelist.SharelistType]int32{
		sharelist.SharelistTypeRegular:  0,
		sharelist.SharelistTypeOfficial: 3,
		sharelist.SharelistTypeIndustry: 4,
	} {
		if int32(got) != want {
			t.Errorf("SDK SharelistType %d, pinned %d; sharelistTypeName's cases depend on it", int32(got), want)
		}
	}
}

func TestSharelistTypeName_EverySDKConstantRendersToItsOwnName(t *testing.T) {
	tests := []struct {
		in   sharelist.SharelistType
		want string
	}{
		{sharelist.SharelistTypeRegular, "regular"},
		{sharelist.SharelistTypeOfficial, "official"},
		{sharelist.SharelistTypeIndustry, "industry"},
	}
	seen := map[string]int32{}
	for _, tt := range tests {
		got := sharelistTypeName(tt.in)
		if got != tt.want {
			t.Errorf("sharelistTypeName(%d) = %q, want %q", int32(tt.in), got, tt.want)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("constants %d and %d both render as %q", prev, int32(tt.in), got)
		}
		seen[got] = int32(tt.in)
	}
}

func TestSharelistTypeName_TheSDKGapsAndOutOfRangeValuesAreVisiblyMarked(t *testing.T) {
	// 1 and 2 are the gap the SDK leaves; 5 does not exist yet. All must be
	// marked, and none may render as an empty cell.
	for _, in := range []int32{1, 2, 5, 99, -1} {
		want := fmt.Sprintf("unknown(%d)", in)
		if got := sharelistTypeName(sharelist.SharelistType(in)); got != want {
			t.Errorf("sharelistTypeName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestOrNameAsDescription_FallsBackToTheNameAsTheSDKDoes(t *testing.T) {
	tests := []struct {
		name, desc, want string
	}{
		{"My list", "", "My list"},
		{"My list", "   ", "My list"},
		{"My list", "\t\n", "My list"},
		{"My list", "A description", "A description"},
		// A description that is only whitespace is replaced, but a padded real
		// one is used verbatim: the SDK trims only to decide whether to
		// substitute, and the preview must show what it will send.
		{"My list", "  padded  ", "  padded  "},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q/%q", tt.name, tt.desc), func(t *testing.T) {
			if got := orNameAsDescription(tt.name, tt.desc); got != tt.want {
				t.Errorf("orNameAsDescription(%q, %q) = %q, want %q", tt.name, tt.desc, got, tt.want)
			}
		})
	}
}

func TestSymbolNames_CapsTheListAndSaysHowManyWereDropped(t *testing.T) {
	mk := func(n int) []sharelist.SharelistStock {
		out := make([]sharelist.SharelistStock, n)
		for i := range out {
			out[i] = sharelist.SharelistStock{Symbol: fmt.Sprintf("S%d.HK", i)}
		}
		return out
	}
	tests := []struct {
		name string
		n    int
		max  int
		want []string
		note string
	}{
		{"under the cap", 3, 20, []string{"S0.HK", "S1.HK", "S2.HK"}, "all shown"},
		{"exactly the cap", 2, 2, []string{"S0.HK", "S1.HK"}, "all shown, no overflow marker"},
		{"one over the cap", 3, 2, []string{"S0.HK", "S1.HK", "...+1 more"}, "the marker replaces the tail"},
		{"far over the cap", 30, 5, []string{"S0.HK", "S1.HK", "S2.HK", "S3.HK", "S4.HK", "...+25 more"}, ""},
		{"zero means all", 3, 0, []string{"S0.HK", "S1.HK", "S2.HK"}, "no cap"},
		{"negative means all", 3, -1, []string{"S0.HK", "S1.HK", "S2.HK"}, "no cap"},
		{"no stocks at all", 0, 20, []string{}, "an empty list is not an error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := symbolNames(mk(tt.n), tt.max)
			if len(got) != len(tt.want) {
				t.Fatalf("symbolNames(%d stocks, %d) = %v, want %v", tt.n, tt.max, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("symbolNames[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIntStrAndBoolStr_NilIsAbsent(t *testing.T) {
	if got := intStr(nil); got != "-" {
		t.Errorf("intStr(nil) = %q, want \"-\"", got)
	}
	if got := boolStr(nil); got != "-" {
		t.Errorf("boolStr(nil) = %q, want \"-\"", got)
	}
	i, zero := int32(42), int32(0)
	if got := intStr(&i); got != "42" {
		t.Errorf("intStr(42) = %q, want \"42\"", got)
	}
	// Zero is a value, not an absence, so it must not render as the dash.
	if got := intStr(&zero); got != "0" {
		t.Errorf("intStr(0) = %q, want \"0\"", got)
	}
	for _, b := range []bool{true, false} {
		want := "false"
		if b {
			want = "true"
		}
		if got := boolStr(&b); got != want {
			t.Errorf("boolStr(%v) = %q, want %q", b, got, want)
		}
	}
}

func TestFmtTime_ZeroTimeRendersAsAbsent(t *testing.T) {
	if got := fmtTime(time.Time{}); got != "-" {
		t.Errorf("fmtTime(zero) = %q, want \"-\"", got)
	}
	when := time.Date(2025, 3, 1, 12, 30, 45, 0, time.UTC)
	if got := fmtTime(when); got != "2025-03-01 12:30:45" {
		t.Errorf("fmtTime = %q, want the formatted value", got)
	}
}

func TestSplitList_SplitsOnCommasAndDropsBlanks(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{",,", nil},
		{"700.HK", []string{"700.HK"}},
		{"700.HK,9988.HK", []string{"700.HK", "9988.HK"}},
		{" 700.HK , 9988.HK ", []string{"700.HK", "9988.HK"}},
		{"700.HK,,9988.HK,", []string{"700.HK", "9988.HK"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
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
