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

	"github.com/longbridge/openapi-go/dca"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// dca.DCAFrequency is a bare int enum, so parseFrequency is the whole of the
// type safety. It is the reason an unrecognised word has to be an error: the
// alternative is a plan created on a schedule nobody asked for, which spends
// money on a recurring basis and is awkward to stop.
//
// schedule() is the second half. It reads package-level flag variables rather
// than taking arguments, so the tests set those variables directly and restore
// them afterwards; there is no flag.FlagSet to drive in a unit test.

// ------------------------------------------------------------------ helpers

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

// dryRunField returns the value describe() rendered for one key, and whether
// that key was in the preview at all. Preview lines look like
// "[DRY-RUN]   day_of_month       15", so the value is whatever follows the key.
func dryRunField(preview, key string) (string, bool) {
	for _, line := range strings.Split(preview, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "[DRY-RUN]" {
			continue
		}
		if fields[1] == key {
			return fields[len(fields)-1], true
		}
	}
	return "", false
}

// dcaState mirrors the flag set schedule() reads. Restored via t.Cleanup so the
// tests stay independent under -shuffle and -count=2.
type dcaState struct {
	symbol         string
	amount         string
	frequency      string
	dayOfWeek      string
	dayOfMonth     uint
	allowMargin    bool
	planID         string
	reminderHours  string
	confirmLiveDCA bool
	limit          int
	page           int
}

func setDCAFlags(t *testing.T, s dcaState) {
	t.Helper()
	prev := dcaState{symbol, amount, frequency, dayOfWeek, dayOfMonth, allowMargin,
		planID, reminderHours, confirmLiveDCA, limit, page}
	symbol, amount, frequency = s.symbol, s.amount, s.frequency
	dayOfWeek, dayOfMonth, planID = s.dayOfWeek, s.dayOfMonth, s.planID
	allowMargin = s.allowMargin
	reminderHours, confirmLiveDCA = s.reminderHours, s.confirmLiveDCA
	limit, page = s.limit, s.page
	timeout = 50 * time.Millisecond
	t.Cleanup(func() {
		symbol, amount, frequency = prev.symbol, prev.amount, prev.frequency
		dayOfWeek, dayOfMonth, planID = prev.dayOfWeek, prev.dayOfMonth, prev.planID
		allowMargin = prev.allowMargin
		reminderHours, confirmLiveDCA = prev.reminderHours, prev.confirmLiveDCA
		limit, page = prev.limit, prev.page
	})
}

func blockedCfg() *appcfg.Config {
	return &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
}

func isBlocked(err error) bool { return errors.Is(err, appcfg.ErrBlocked) }

type failConnect struct{ calls int }

func (c *failConnect) connect() (*dca.DCAContext, error) {
	c.calls++
	return nil, context.Canceled
}

// ------------------------------------------------------------------ frequency

// The SDK's own values are pinned first: parseFrequency compares against these
// constants, and DCAFrequencyDaily is the zero value, so a change there would
// both make a zero-valued error return a valid frequency again and quietly
// renumber dcaFrequencyInvalid's competition for "not a frequency".
func TestParseFrequency_TheSDKConstantValuesArePinned(t *testing.T) {
	for got, want := range map[dca.DCAFrequency]int{
		dca.DCAFrequencyDaily:       0,
		dca.DCAFrequencyWeekly:      1,
		dca.DCAFrequencyFortnightly: 2,
		dca.DCAFrequencyMonthly:     3,
	} {
		if int(got) != want {
			t.Errorf("SDK DCAFrequency %v = %d, pinned %d; the wire mapping in the switch would be wrong", got, int(got), want)
		}
	}
	// The wire strings are what the API sees, so a wrong one is a wrong request.
	for got, want := range map[dca.DCAFrequency]string{
		dca.DCAFrequencyDaily:       "Daily",
		dca.DCAFrequencyWeekly:      "Weekly",
		dca.DCAFrequencyFortnightly: "Fortnightly",
		dca.DCAFrequencyMonthly:     "Monthly",
	} {
		if s := got.String(); s != want {
			t.Errorf("DCAFrequency(%d).String() = %q, want %q", int(got), s, want)
		}
	}
}

func TestParseFrequency_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want dca.DCAFrequency
	}{
		{"daily", dca.DCAFrequencyDaily},
		{"weekly", dca.DCAFrequencyWeekly},
		{"fortnightly", dca.DCAFrequencyFortnightly},
		{"monthly", dca.DCAFrequencyMonthly},
		// The empty string means "the flag default", which the flag layer
		// documents as monthly. Resolving it here rather than erroring keeps
		// doUpdate's `if frequency != ""` guard meaningful.
		{"", dca.DCAFrequencyMonthly},
		// Lower-cased and trimmed, so DAILY and " Weekly " both normalise.
		{"DAILY", dca.DCAFrequencyDaily},
		{"Weekly", dca.DCAFrequencyWeekly},
		{"  fortnightly  ", dca.DCAFrequencyFortnightly},
		{"\nMONTHLY\n", dca.DCAFrequencyMonthly},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseFrequency(tt.in)
			if err != nil {
				t.Fatalf("parseFrequency(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseFrequency(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseFrequency_UnrecognisedValueIsAnError(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"yearly", "a plausible but unsupported word"},
		{"annually", "the monthly analogue, also unsupported"},
		{"biweekly", "the US spelling of fortnightly, not accepted"},
		{"bi-weekly", "hyphenated fortnightly, not accepted"},
		{"day", "a prefix of daily; the whole word is required"},
		{"dayly", "a misspelling of daily"},
		{"wk", "an abbreviation"},
		{"1", "a number: frequencies are words, not ordinals"},
		{"0", "zero is DCAFrequencyDaily, not a name"},
		{"M", "the sortType vocabulary, not a frequency"},
		{"price-rise", "an alert condition, not a frequency"},
		{"Day", "a trade time in force, not a frequency"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseFrequency(tt.in)
			if err == nil {
				t.Fatalf("parseFrequency(%q) = %v with no error; a plan on an unintended frequency spends money", tt.in, got)
			}
			if !strings.Contains(err.Error(), "-frequency") {
				t.Errorf("error %q does not name -frequency", err)
			}
		})
	}
}

// The counterpart of the finding that used to be pinned here: parseFrequency
// trims before switching, so a whitespace-only -frequency used to become the
// empty string and resolve to monthly with no error. It is now refused like any
// other unrecognised value, which is the only reading that keeps it
// distinguishable from not passing the flag — on update, not passing it means the
// plan's schedule is left alone, so silently reading " " as "monthly" was the
// one input that could change a plan's schedule without being asked to.
//
// This was originally pinned as TestParseFrequency_FindingWhitespaceOnly
// ResolvesToMonthlyNotAnError, back when that was the observed behaviour.
func TestParseFrequency_WhitespaceOnlyIsAnErrorRatherThanTheDefault(t *testing.T) {
	for _, in := range []string{" ", "\t", "  \n ", "   "} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseFrequency(in)
			if err == nil {
				t.Fatalf("parseFrequency(%q) = %v with no error; a value that is only whitespace "+
					"is indistinguishable from an omitted flag, which on update means "+
					"\"leave the schedule alone\"", in, got)
			}
			if !strings.Contains(err.Error(), "-frequency") {
				t.Errorf("error %q does not name -frequency", err)
			}
			// The value is echoed (Go-quoted, so a tab is visible as \t) so the
			// user can see what they passed rather than what it trimmed to.
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", in)) {
				t.Errorf("error %q does not quote the value they typed (%q)", err, in)
			}
		})
	}
}

// A padded real word is a typo, not a value, so it still normalises. This is the
// line the whitespace rejection must not cross: `freqFlag` trims before
// comparing, and only refuses what is left empty.
func TestParseFrequency_APaddedRealWordIsStillNormalised(t *testing.T) {
	tests := []struct {
		in   string
		want dca.DCAFrequency
	}{
		{" Weekly ", dca.DCAFrequencyWeekly},
		{"\nFORtnightly\t", dca.DCAFrequencyFortnightly},
		{" daily ", dca.DCAFrequencyDaily},
		{" monthly ", dca.DCAFrequencyMonthly},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			got, err := parseFrequency(tt.in)
			if err != nil {
				t.Fatalf("parseFrequency(%q) = error %v, want %v", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseFrequency(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// FINDING, now fixed. The error return value used to be 0, which is
// DCAFrequencyDaily — a valid, meaningful constant, and the one value that would
// create a daily plan if a future caller forgot to check the error. It is now
// dcaFrequencyInvalid, a value outside the enum, so an ignored error sends
// something the API must reject rather than a plan to invest every trading day.
//
// This asserts MEMBERSHIP rather than "it errors", because a test that only
// checked the error would have passed on the old code too and proved nothing
// about the hazard.
func TestParseFrequency_TheErrorValueIsNotAFrequency(t *testing.T) {
	// The valid set, spelled out rather than derived from a range, so the
	// assertion below cannot be satisfied by a change to the SDK constants
	// themselves: if the enum ever grows a negative member, this is where it
	// shows up.
	valid := []dca.DCAFrequency{
		dca.DCAFrequencyDaily,
		dca.DCAFrequencyWeekly,
		dca.DCAFrequencyFortnightly,
		dca.DCAFrequencyMonthly,
	}
	for _, in := range []string{"yearly", " ", "biweekly", "0", "Day"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			got, err := parseFrequency(in)
			if err == nil {
				t.Fatalf("parseFrequency(%q) = %d with no error", in, int(got))
			}
			for _, f := range valid {
				if got == f {
					t.Errorf("parseFrequency(%q) = %d alongside its error, and that is %v; "+
						"a caller that ignored the error would send a real frequency. "+
						"The error return must be outside the enum", in, int(got), f)
				}
			}
			if got != dcaFrequencyInvalid {
				t.Errorf("parseFrequency(%q) = %d alongside its error, want the sentinel %d "+
					"(dcaFrequencyInvalid), so the reason the value is unsendable is one line, "+
					"not a coincidence", in, int(got), int(dcaFrequencyInvalid))
			}
		})
	}
}

// The sentinel's safety rests on the SDK's numbering, so the numbering is
// asserted here as well: -1 is outside the enum only because the SDK declares
// all four members with iota and none of them negative. TestParseFrequency_
// TheSDKConstantValuesArePinned covers the same four numbers as a wire mapping;
// this one states the property the sentinel depends on.
func TestDCAFrequencyInvalid_IsOutsideTheEnum(t *testing.T) {
	if int(dcaFrequencyInvalid) >= 0 {
		t.Fatalf("dcaFrequencyInvalid = %d, which is inside the 0..3 range the SDK declares; "+
			"it would have to be renumbered below zero", int(dcaFrequencyInvalid))
	}
	for _, f := range []dca.DCAFrequency{
		dca.DCAFrequencyDaily, dca.DCAFrequencyWeekly,
		dca.DCAFrequencyFortnightly, dca.DCAFrequencyMonthly,
	} {
		if f == dcaFrequencyInvalid {
			t.Errorf("dcaFrequencyInvalid collides with the SDK constant %v", f)
		}
	}
}

// ------------------------------------------------------------------ schedule

// The README states the rule this test exists for: weekly/fortnightly without a
// weekday is rejected, and supplying both a weekday and a day-of-month is
// rejected, so the user gets a local error instead of an opaque one from the API.
func TestSchedule_WeeklyAndFortnightlyRequireAWeekday(t *testing.T) {
	for _, freq := range []string{"weekly", "fortnightly", "Weekly", " FORTNIGHTLY "} {
		t.Run(freq, func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: freq})
			day, month, err := schedule()
			if err == nil {
				t.Fatalf("schedule() with -frequency %q and no -day-of-week = (%q, %v, nil); "+
					"the API requires the field matching the frequency, so this is a request that cannot succeed", freq, day, month)
			}
			if !strings.Contains(err.Error(), "-day-of-week") {
				t.Errorf("error %q does not name the missing flag -day-of-week", err)
			}
			if !strings.Contains(err.Error(), freq) {
				t.Errorf("error %q does not echo the -frequency the user passed", err)
			}
		})
	}
}

func TestSchedule_WeeklyWithAWeekdayIsAcceptedAndReturnsOnlyTheWeekday(t *testing.T) {
	for _, freq := range []string{"weekly", "fortnightly"} {
		t.Run(freq, func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: freq, dayOfWeek: "Monday"})
			day, month, err := schedule()
			if err != nil {
				t.Fatalf("schedule() = error %v, want the weekday accepted", err)
			}
			if day != "Monday" {
				t.Errorf("schedule() day-of-week = %q, want \"Monday\"", day)
			}
			if month != nil {
				t.Errorf("schedule() day-of-month = %v, want nil: %s takes a weekday, not a day of month", *month, freq)
			}
		})
	}
}

func TestSchedule_MonthlyRejectsAWeekdayAndADayOfMonthTogether(t *testing.T) {
	for _, freq := range []string{"monthly", "", "MONTHLY"} {
		t.Run(fmt.Sprintf("%q", freq), func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: freq, dayOfWeek: "Monday", dayOfMonth: 15})
			_, _, err := schedule()
			if err == nil {
				t.Fatal("schedule() = nil with both -day-of-week and -day-of-month; " +
					"the README says supplying both is rejected")
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Errorf("error %q does not explain that the two flags contradict each other", err)
			}
			for _, want := range []string{"-day-of-week", "-day-of-month"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %s", err, want)
				}
			}
		})
	}
}

func TestSchedule_MonthlyWithADayOfMonthAloneIsAccepted(t *testing.T) {
	for _, dom := range []uint{1, 15, 28, 31} {
		t.Run(fmt.Sprint(dom), func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: "monthly", dayOfMonth: dom})
			day, month, err := schedule()
			if err != nil {
				t.Fatalf("schedule() = error %v, want -day-of-month %d accepted", err, dom)
			}
			if day != "" {
				t.Errorf("schedule() day-of-week = %q, want \"\"", day)
			}
			if month == nil {
				t.Fatal("schedule() day-of-month = nil, want the value to be returned")
			}
			if *month != uint32(dom) {
				t.Errorf("schedule() day-of-month = %d, want %d", *month, dom)
			}
		})
	}
}

func TestSchedule_DayOfMonthAbove31IsRejectedWhateverTheFrequency(t *testing.T) {
	// dayOfMonth is a uint, so 0 means "not supplied" and 32 is the first
	// impossible value. The bound is checked before the frequency switch, so it
	// applies to every frequency including ones the switch does not handle.
	for _, freq := range []string{"monthly", "weekly", "daily", "", "yearly"} {
		for _, dom := range []uint{32, 100, 4294967295} {
			t.Run(fmt.Sprintf("%s/%d", freq, dom), func(t *testing.T) {
				setDCAFlags(t, dcaState{frequency: freq, dayOfMonth: dom, dayOfWeek: "Monday"})
				_, _, err := schedule()
				if err == nil {
					t.Fatalf("schedule() with -day-of-month %d = nil error", dom)
				}
				if !strings.Contains(err.Error(), "-day-of-month") {
					t.Errorf("error %q does not name -day-of-month", err)
				}
			})
		}
	}
}

func TestSchedule_MonthlyWithNeitherDayFlagIsAcceptedForTheServerToDefault(t *testing.T) {
	setDCAFlags(t, dcaState{frequency: "monthly"})
	day, month, err := schedule()
	if err != nil {
		t.Fatalf("schedule() = error %v; a monthly plan with no day is a server default, not a contradiction", err)
	}
	if day != "" || month != nil {
		t.Errorf("schedule() = (%q, %v), want (\"\", nil)", day, month)
	}
}

// The mutual-exclusion check now runs for every frequency, not only monthly, so
// this asserts the rejection rather than the acceptance it used to pin: for a
// weekday frequency, -day-of-month was accepted and returned in the month
// pointer, so CreateOptions carried DayOfWeek "Monday" and DayOfMonth 5 at the
// same time. The contradiction was harmless only because the API ignores the
// field that does not match the frequency — which is exactly the "harmless" a
// local check exists to stop relying on.
//
// Originally pinned as TestSchedule_FindingWeeklyAcceptsBothDayFlagsAtOnce.
func TestSchedule_WeeklyRejectsADayOfMonthAlongsideTheWeekday(t *testing.T) {
	for _, freq := range []string{"weekly", "fortnightly", "Weekly", " FORTNIGHTLY "} {
		t.Run(fmt.Sprintf("%q", freq), func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: freq, dayOfWeek: "Monday", dayOfMonth: 5})
			day, month, err := schedule()
			if err == nil {
				t.Fatalf("schedule() = (%q, %v, nil) with both day flags set under -frequency %q; "+
					"the README says supplying both is rejected", day, month, freq)
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Errorf("error %q does not explain that the two flags contradict each other", err)
			}
			for _, want := range []string{"-day-of-week", "-day-of-month"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %s", err, want)
				}
			}
			// The message has to say which of the two is the wrong one for THIS
			// frequency, or the user cannot tell which flag to delete.
			if !strings.Contains(err.Error(), strings.ToLower(strings.TrimSpace(freq))) {
				t.Errorf("error %q does not name the frequency in force (%q)", err, freq)
			}
			if !strings.Contains(err.Error(), "drop -day-of-month") {
				t.Errorf("error %q does not say which flag to drop, and a %s plan takes no day of month:\n%s",
					err, freq, err)
			}
		})
	}
}

// daily is a frequency parseFrequency accepts, so schedule() has to have an
// opinion about its day fields. It takes neither: a daily plan runs every
// trading day. The switch used to have no case for daily at all, so
// -frequency daily -day-of-month 5 was accepted and the day-of-month was sent.
//
// Originally pinned as TestSchedule_FindingDailyIsNotCrossValidatedAtAll.
func TestSchedule_DailyRejectsEitherDayFlag(t *testing.T) {
	tests := []struct {
		name    string
		set     dcaState
		wantErr string
	}{
		{
			name:    "day of month",
			set:     dcaState{frequency: "daily", dayOfMonth: 5},
			wantErr: "-day-of-month",
		},
		{
			name:    "weekday",
			set:     dcaState{frequency: "daily", dayOfWeek: "Monday"},
			wantErr: "-day-of-week",
		},
		{
			name:    "both, reported by the flag it names first",
			set:     dcaState{frequency: "daily", dayOfWeek: "Monday", dayOfMonth: 5},
			wantErr: "-day-of-week",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDCAFlags(t, tt.set)
			day, month, err := schedule()
			if err == nil {
				t.Fatalf("schedule() = (%q, %v, nil) for -frequency daily with %s; a daily plan "+
					"takes no day field, so the value would be ignored or rejected by the API",
					day, month, tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name the offending flag %s", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), "daily") {
				t.Errorf("error %q does not name the frequency the user passed", err)
			}
		})
	}
}

// A daily plan with no day field at all is the shape schedule() is meant to
// accept, and the regression guard for the rejections above.
func TestSchedule_DailyWithNoDayFieldIsAccepted(t *testing.T) {
	setDCAFlags(t, dcaState{frequency: "daily"})
	day, month, err := schedule()
	if err != nil {
		t.Fatalf("schedule() = error %v; a daily plan needs no day field", err)
	}
	if day != "" || month != nil {
		t.Errorf("schedule() = (%q, %v), want (\"\", nil)", day, month)
	}
}

// The other half of "each frequency takes exactly one day field": monthly takes
// a day of the month, so a weekday contradicts it even with nothing to
// contradict. The monthly case used to fire only when BOTH flags were set, so
// -frequency monthly -day-of-week Monday reached CreateOptions on its own.
func TestSchedule_MonthlyRejectsAWeekdayOnItsOwn(t *testing.T) {
	for _, freq := range []string{"monthly", "", "MONTHLY"} {
		t.Run(fmt.Sprintf("%q", freq), func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: freq, dayOfWeek: "Monday"})
			day, month, err := schedule()
			if err == nil {
				t.Fatalf("schedule() = (%q, %v, nil); monthly uses -day-of-month, so a weekday "+
					"cannot be sent alongside it", day, month)
			}
			if !strings.Contains(err.Error(), "-day-of-week") {
				t.Errorf("error %q does not name the offending flag", err)
			}
			// "say what to do instead" is part of the error contract, and the
			// frequency quoted for the empty flag has to be the documented
			// default rather than the empty string the user never typed.
			if !strings.Contains(err.Error(), "-frequency weekly") {
				t.Errorf("error %q does not suggest the frequency to use instead:\n%s", err, err)
			}
			if !strings.Contains(err.Error(), "monthly") {
				t.Errorf("error %q does not name the frequency in force:\n%s", err, err)
			}
		})
	}
}

// The flag defaults, through the real flag set rather than by reading the source.
// -frequency is the one that matters: it defaults to EMPTY, not to "monthly".
// With a non-empty default every `-action update -amount 2000` — the example in
// the README — would carry invest_frequency and silently convert a weekly plan to
// a monthly one, because doUpdate sends a frequency for any value the flag layer
// produced. No unit test of doUpdate alone can catch that, because it bypasses
// the flag layer, so the default is pinned here.
func TestRegisterFlags_TheDefaultsLeaveAnUpdateableScheduleUnspecified(t *testing.T) {
	// resetDCAFlags zeroes the package-level variables, because flag.StringVar
	// only writes the ones the command line actually names.
	prev := dcaState{symbol, amount, frequency, dayOfWeek, dayOfMonth, allowMargin,
		planID, reminderHours, confirmLiveDCA, limit, page}
	t.Cleanup(func() {
		symbol, amount, frequency = prev.symbol, prev.amount, prev.frequency
		dayOfWeek, dayOfMonth, planID = prev.dayOfWeek, prev.dayOfMonth, prev.planID
		allowMargin = prev.allowMargin
		reminderHours, confirmLiveDCA = prev.reminderHours, prev.confirmLiveDCA
		limit, page = prev.limit, prev.page
	})

	tests := []struct {
		name       string
		args       []string
		wantFreq   string
		wantAction string
	}{
		{
			name:       "no flags at all",
			args:       nil,
			wantFreq:   "",
			wantAction: "list",
		},
		{
			name:       "the README's amount-only update",
			args:       []string{"-action", "update", "-plan-id", "1", "-amount", "2000"},
			wantFreq:   "",
			wantAction: "update",
		},
		{
			name:       "a create that says nothing about the schedule",
			args:       []string{"-action", "create", "-symbol", "700.HK", "-amount", "10"},
			wantFreq:   "",
			wantAction: "create",
		},
		{
			name:       "an explicit monthly create",
			args:       []string{"-action", "create", "-symbol", "700.HK", "-amount", "10", "-frequency", "monthly"},
			wantFreq:   "monthly",
			wantAction: "create",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, amount, frequency = "", "", ""
			dayOfWeek, dayOfMonth, planID = "", 0, ""
			reminderHours = ""
			u := cli.NewUsage("dca", "test")
			registerFlags(u)
			if err := u.FS.Parse(tt.args); err != nil {
				t.Fatalf("parsing %v: %v", tt.args, err)
			}
			if frequency != tt.wantFreq {
				t.Errorf("-frequency = %q, want %q. A non-empty default makes every update send "+
					"invest_frequency, which changes the plan's schedule rather than leaving it alone",
					frequency, tt.wantFreq)
			}
			if action != tt.wantAction {
				t.Errorf("-action = %q, want %q", action, tt.wantAction)
			}
			// The default day fields are unset on both sides, which is what lets
			// schedule() tell "monthly with the server's default day" from
			// "nothing to change".
			if dayOfWeek != "" || dayOfMonth != 0 {
				t.Errorf("default -day-of-week = %q and -day-of-month = %d, want both unset",
					dayOfWeek, dayOfMonth)
			}
		})
	}
}

// And the end-to-end consequence, through the flag layer: the amount-only update
// reaches the gate with an UpdateOptions that leaves the schedule untouched.
func TestDoUpdate_TheFlagLayerAnAmountOnlyUpdateSendsNoFrequency(t *testing.T) {
	prevFreq := frequency
	t.Cleanup(func() { frequency = prevFreq })
	frequency = ""
	u := cli.NewUsage("dca", "test")
	registerFlags(u)
	if err := u.FS.Parse([]string{"-action", "update", "-plan-id", "1", "-amount", "2000"}); err != nil {
		t.Fatalf("parsing the update flags: %v", err)
	}
	planID, amount, confirmLiveDCA, allowMargin = "1", "2000", false, false
	dayOfWeek, dayOfMonth = "", 0

	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doUpdate(context.Background(), blockedCfg(), c.connect) })
	if err == nil || !isBlocked(err) {
		t.Fatalf("doUpdate() = %v, want a *config.BlockedError: the update is valid", err)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	if !strings.Contains(out, "invest_frequency") {
		t.Fatalf("preview has no invest_frequency line at all:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "invest_frequency") {
			continue
		}
		fields := strings.Fields(line)
		if got := fields[len(fields)-1]; got != "-" {
			t.Errorf("invest_frequency = %q, want \"-\": the plan's frequency must be left as it is, "+
				"because -frequency was not passed", got)
		}
	}
}

// ------------------------------------------------------------------ write paths

// Every mutating action must refuse its missing flags before the gate, and none
// of them may create an SDK context. The connect counter is the assertion that
// matters: it is the only way to see that nothing was sent.
func TestWriteActions_RefuseMissingFlagsBeforeAnySDKContextIsCreated(t *testing.T) {
	tests := []struct {
		name    string
		set     dcaState
		run     func(*failConnect) error
		wantErr string
	}{
		{
			name: "create without -symbol",
			set:  dcaState{amount: "100", frequency: "monthly"},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-symbol",
		},
		{
			name: "create without -amount",
			set:  dcaState{symbol: "700.HK", frequency: "monthly"},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-amount",
		},
		{
			name: "create with an unrecognised -frequency",
			set:  dcaState{symbol: "700.HK", amount: "100", frequency: "yearly"},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-frequency",
		},
		{
			name: "create weekly without -day-of-week",
			set:  dcaState{symbol: "700.HK", amount: "100", frequency: "weekly"},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-day-of-week",
		},
		{
			name: "update without -plan-id",
			set:  dcaState{amount: "100", frequency: "monthly"},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-plan-id",
		},
		{
			name: "update with an unrecognised -frequency",
			set:  dcaState{planID: "plan-1", frequency: "yearly"},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-frequency",
		},
		{
			name: "pause without -plan-id",
			set:  dcaState{},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusSuspended, "pause")
			},
			wantErr: "-plan-id",
		},
		{
			name: "resume without -plan-id",
			set:  dcaState{},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusActive, "resume")
			},
			wantErr: "-plan-id",
		},
		{
			name: "stop without -plan-id",
			set:  dcaState{},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusFinished, "stop")
			},
			wantErr: "-plan-id",
		},
		{
			name: "set-reminder without -reminder-hours",
			set:  dcaState{},
			run: func(c *failConnect) error {
				return doSetReminder(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-reminder-hours",
		},
		{
			name: "history without -plan-id",
			set:  dcaState{},
			run: func(c *failConnect) error {
				return doHistory(context.Background(), c.connect)
			},
			wantErr: "-plan-id",
		},
		{
			name: "calc-date without -symbol",
			set:  dcaState{frequency: "monthly"},
			run: func(c *failConnect) error {
				return doCalcDate(context.Background(), c.connect)
			},
			wantErr: "-symbol",
		},
		{
			name: "check-support without -symbol",
			set:  dcaState{},
			run: func(c *failConnect) error {
				return doCheckSupport(context.Background(), c.connect)
			},
			wantErr: "-symbol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDCAFlags(t, tt.set)
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = tt.run(c) })
			if err == nil {
				t.Fatalf("%s was not refused", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal, want the flag error: an unusable flag must be "+
					"reported as such even when the gate is shut. got %v", err)
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

// A fully specified create reaches the gate, which is shut, and stops there.
func TestDoCreate_AValidCreateIsStillRefusedByTheGateWithNoNetworkCall(t *testing.T) {
	setDCAFlags(t, dcaState{symbol: "700.HK", amount: "100", frequency: "monthly", dayOfMonth: 15})
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doCreate(context.Background(), blockedCfg(), c.connect) })
	if err == nil {
		t.Fatal("doCreate() = nil with the gate shut")
	}
	if !isBlocked(err) {
		t.Errorf("doCreate() = %v, want a *config.BlockedError so the exit status is %d", err, appcfg.ExitBlocked)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	// The dry run prints the exact body it would send, prefixed, so a blocked
	// transcript can never be mistaken for a live one.
	for _, want := range []string{
		"[DRY-RUN]", "POST /v1/dailycoins/create", "700.HK", "100",
		"Monthly", "day_of_month", "15",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run preview is missing %q.\npreview was:\n%s", want, out)
		}
	}
}

// FINDING, now fixed. Every string flag here used to be tested with `== ""` and
// no trim, so a value of only whitespace sailed through validation and was
// reported by the SAFETY GATE instead: "BLOCKED: missing --confirm-live-dca",
// exit 3. That is the most misleading diagnosis this command can give, because
// a user who sees exit 3 reasonably concludes a guard protected them, when in
// fact they left a required flag blank and no guard was ever asked. cmd/trade's
// doSubmit/doReplace/doCancel had the same bug and already trim.
//
// The assertions are the ones that distinguish the two: the error must NOT be
// a *BlockedError, it must name the flag, and connect() must never run. A test
// that only checked "some error" would have passed on the old code too.
func TestWriteActions_WhitespaceOnlyStringFlagsAreFlagErrorsNotGateRefusals(t *testing.T) {
	tests := []struct {
		name    string
		set     dcaState
		run     func(*failConnect) error
		wantErr string
	}{
		{
			name: "create with a blank -symbol",
			set:  dcaState{symbol: "   ", amount: "100", frequency: "monthly"},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-symbol",
		},
		{
			name: "create with a blank -amount",
			set:  dcaState{symbol: "700.HK", amount: "\t", frequency: "monthly"},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-amount",
		},
		{
			name: "create weekly with a blank -day-of-week",
			set:  dcaState{symbol: "700.HK", amount: "100", frequency: "weekly", dayOfWeek: "   "},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-day-of-week",
		},
		{
			name: "calc-date with a blank -symbol",
			set:  dcaState{frequency: "monthly", symbol: "  "},
			run: func(c *failConnect) error {
				return doCalcDate(context.Background(), c.connect)
			},
			wantErr: "-symbol",
		},
		{
			name: "update with a blank -plan-id",
			set:  dcaState{amount: "100", planID: "\t"},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-plan-id",
		},
		{
			// The sparse-update case: trimming the blank into "not supplied"
			// would have made this a valid amount-only update, and the plan
			// would have kept the amount the user was trying to change.
			name: "update with a blank -amount, which is not the same as leaving it out",
			set:  dcaState{planID: "plan-1", amount: "   "},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-amount",
		},
		{
			name: "stats with a blank -symbol filter",
			set:  dcaState{symbol: "  "},
			run: func(c *failConnect) error {
				return doStats(context.Background(), c.connect)
			},
			wantErr: "-symbol",
		},
		{
			name: "history with a blank -plan-id",
			set:  dcaState{planID: "  "},
			run: func(c *failConnect) error {
				return doHistory(context.Background(), c.connect)
			},
			wantErr: "-plan-id",
		},
		{
			name: "pause with a blank -plan-id",
			set:  dcaState{planID: "   "},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusSuspended, "pause")
			},
			wantErr: "-plan-id",
		},
		{
			name: "resume with a blank -plan-id",
			set:  dcaState{planID: "   "},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusActive, "resume")
			},
			wantErr: "-plan-id",
		},
		{
			name: "stop with a blank -plan-id",
			set:  dcaState{planID: "   "},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusFinished, "stop")
			},
			wantErr: "-plan-id",
		},
		{
			name: "set-reminder with a blank -reminder-hours",
			set:  dcaState{reminderHours: "  "},
			run: func(c *failConnect) error {
				return doSetReminder(context.Background(), blockedCfg(), c.connect)
			},
			wantErr: "-reminder-hours",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDCAFlags(t, tt.set)
			c := &failConnect{}
			var err error
			captureStdout(t, func() { err = tt.run(c) })
			if err == nil {
				t.Fatalf("%s was not refused", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("error is a gate refusal (%v), want the flag error: exit 3 tells the user "+
					"a guard stopped them, and the thing that actually needs fixing is a blank flag",
					err)
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

// The other half, and the reason the fix has to be a trim rather than a
// rejection of anything with a space in it: a padded real value is a typo, not
// a bad flag. Every one of these must still reach the gate — with the padding
// gone, because the trimmed value is what the SDK puts in the request body (the
// symbol becomes a counter_id, the amount and the hours are sent verbatim, and
// invest_day_of_week is taken as typed). Over-rejecting here would be a
// regression in the other direction, so the preview is checked for the trimmed
// value rather than merely for reaching the gate.
func TestWriteActions_PaddedStringFlagsAreTrimmedAndStillReachTheGate(t *testing.T) {
	tests := []struct {
		name    string
		set     dcaState
		run     func(*failConnect) error
		wantKey string
		want    string
	}{
		{
			name: "create with a padded symbol and amount",
			set:  dcaState{symbol: "  700.HK ", amount: " 100 ", frequency: "monthly", dayOfMonth: 15},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantKey: "symbol",
			want:    "700.HK",
		},
		{
			name: "create weekly with a padded weekday",
			set:  dcaState{symbol: "700.HK", amount: "100", frequency: "weekly", dayOfWeek: "  Monday  "},
			run: func(c *failConnect) error {
				return doCreate(context.Background(), blockedCfg(), c.connect)
			},
			wantKey: "day_of_week",
			want:    "Monday",
		},
		{
			name: "update with a padded amount",
			set:  dcaState{planID: " plan-1 ", amount: " 250 "},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantKey: "per_invest_amount",
			want:    "250",
		},
		{
			name: "update with a padded plan id",
			set:  dcaState{planID: " plan-1 ", amount: "250"},
			run: func(c *failConnect) error {
				return doUpdate(context.Background(), blockedCfg(), c.connect)
			},
			wantKey: "plan_id",
			want:    "plan-1",
		},
		{
			name: "stop with a padded plan id",
			set:  dcaState{planID: " plan-1 "},
			run: func(c *failConnect) error {
				return doToggle(context.Background(), blockedCfg(), c.connect, dca.DCAStatusFinished, "stop")
			},
			wantKey: "plan_id",
			want:    "plan-1",
		},
		{
			name: "set-reminder with a padded hours value",
			set:  dcaState{reminderHours: " 6 "},
			run: func(c *failConnect) error {
				return doSetReminder(context.Background(), blockedCfg(), c.connect)
			},
			wantKey: "alter_hours",
			want:    "6",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDCAFlags(t, tt.set)
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() { err = tt.run(c) })
			if err == nil || !isBlocked(err) {
				t.Fatalf("error = %v, want a *config.BlockedError: a padded value is a typo to be "+
					"normalised, not a flag to be refused", err)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) while blocked", c.calls)
			}
			got, ok := dryRunField(out, tt.wantKey)
			if !ok {
				t.Fatalf("preview has no %q line at all.\npreview was:\n%s", tt.wantKey, out)
			}
			if got != tt.want {
				t.Errorf("preview %s = %q, want %q: the preview has to be the request that WOULD be "+
					"sent, and the padded text would not be", tt.wantKey, got, tt.want)
			}
		})
	}
}

// optionalFlag is the shared shape for the two optional string flags, and the
// three inputs it keeps apart are the whole reason it exists. Pinned directly,
// because the sparse-update reading of "" is invisible from any single action.
func TestOptionalFlag_AbsentPaddedAndBlankAreThreeDifferentInputs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "absent is the only way to say not supplied", in: "", want: ""},
		{name: "padded is a typo and is trimmed", in: "  250 ", want: "250"},
		{name: "padded symbol", in: "\t700.HK\n", want: "700.HK"},
		{name: "blank is refused, not read as an omission", in: "   ", wantErr: true},
		{name: "a tab is blank too", in: "\t", wantErr: true},
		{name: "a newline is blank too", in: "\n ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := optionalFlag("amount", tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("optionalFlag(\"amount\", %q) = %q with no error; reading it as an "+
						"omission changes nothing about the plan while looking as though it did",
						tt.in, got)
				}
				if !strings.Contains(err.Error(), "-amount") {
					t.Errorf("error %q does not name the flag", err)
				}
				// The user has to see what they typed, or a stray space is a
				// mystery; Go-quoted so a tab shows up as \t.
				if !strings.Contains(err.Error(), fmt.Sprintf("%q", tt.in)) {
					t.Errorf("error %q does not quote the value they typed", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("optionalFlag(\"amount\", %q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("optionalFlag(\"amount\", %q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// dayOfWeekFlag has the same three-way split as freqFlag, and the blank case is
// load-bearing for a second reason: under a monthly or daily frequency the
// weekday is not sent at all, so a blank one trimmed away would be dropped
// without a word. schedule() is the entry point, so it is driven through that.
func TestSchedule_WhitespaceOnlyDayOfWeekIsAnErrorForEveryFrequency(t *testing.T) {
	for _, freq := range []string{"weekly", "fortnightly", "monthly", "daily", ""} {
		t.Run(fmt.Sprintf("%q", freq), func(t *testing.T) {
			setDCAFlags(t, dcaState{frequency: freq, dayOfWeek: "   ", dayOfMonth: 15})
			day, month, err := schedule()
			if err == nil {
				t.Fatalf("schedule() = (%q, %v, nil) for -day-of-week %q under -frequency %q; a "+
					"blank weekday is a flag the user got wrong, and under monthly/daily it is "+
					"dropped without a word", day, month, "   ", freq)
			}
			if !strings.Contains(err.Error(), "-day-of-week") {
				t.Errorf("error %q does not name -day-of-week", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", "   ")) {
				t.Errorf("error %q does not quote the value they typed", err)
			}
		})
	}
}

// And the line that rejection must not cross, in both directions: a padded
// weekday is a real weekday, under either the frequency that uses one or the
// frequency that ignores one.
func TestSchedule_APaddedDayOfWeekIsNormalisedNotRefused(t *testing.T) {
	setDCAFlags(t, dcaState{frequency: "weekly", dayOfWeek: "  Monday  "})
	day, month, err := schedule()
	if err != nil {
		t.Fatalf("schedule() = error %v, want the padded weekday normalised", err)
	}
	if day != "Monday" {
		t.Errorf("schedule() day-of-week = %q, want \"Monday\"", day)
	}
	if month != nil {
		t.Errorf("schedule() day-of-month = %v, want nil", *month)
	}
}

// Stop is the one DCA action that cannot be undone, so its preview has to say
// so. The other two share the endpoint and must not claim the same thing.
func TestDoToggle_OnlyStopWarnsThatItIsIrreversible(t *testing.T) {
	tests := []struct {
		verb       string
		status     dca.DCAStatus
		wantStatus string
		wantWarn   bool
	}{
		{"pause", dca.DCAStatusSuspended, "Suspended", false},
		{"resume", dca.DCAStatusActive, "Active", false},
		{"stop", dca.DCAStatusFinished, "Finished", true},
	}
	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			setDCAFlags(t, dcaState{planID: "plan-1"})
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() {
				err = doToggle(context.Background(), blockedCfg(), c.connect,
					tt.status, tt.verb)
			})
			if err == nil || !isBlocked(err) {
				t.Fatalf("doToggle(%s) = %v, want a *config.BlockedError", tt.verb, err)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) while blocked", c.calls)
			}
			if !strings.Contains(out, tt.wantStatus) {
				t.Errorf("preview does not show status %s:\n%s", tt.wantStatus, out)
			}
			warned := strings.Contains(out, "IRREVERSIBLE")
			if warned != tt.wantWarn {
				t.Errorf("doToggle(%s) preview IRREVERSIBLE warning = %v, want %v.\npreview was:\n%s",
					tt.verb, warned, tt.wantWarn, out)
			}
			if !strings.Contains(out, "POST /v1/dailycoins/toggle") {
				t.Errorf("preview does not name the shared toggle endpoint:\n%s", out)
			}
		})
	}
}

// doUpdate turns the individual flags into a sparse *dca.UpdateOptions. The
// pointers decide what the API is asked to change, so each one is checked.
func TestDoUpdate_BuildsASparseUpdateFromOnlyTheFlagsThatWereSet(t *testing.T) {
	tests := []struct {
		name    string
		set     dcaState
		want    []string
		notWant []string
	}{
		{
			name:    "amount only",
			set:     dcaState{planID: "plan-1", amount: "250"},
			want:    []string{"per_invest_amount", "250"},
			notWant: []string{"invest_frequency", "day_of_week", "day_of_month", "allow_margin"},
		},
		{
			name:    "frequency only",
			set:     dcaState{planID: "plan-1", frequency: "weekly", dayOfWeek: "Friday"},
			want:    []string{"Weekly", "day_of_week", "Friday"},
			notWant: []string{"per_invest_amount", "day_of_month", "allow_margin"},
		},
		{
			name:    "day of month only",
			set:     dcaState{planID: "plan-1", dayOfMonth: 21},
			want:    []string{"day_of_month", "21"},
			notWant: []string{"per_invest_amount", "invest_frequency", "day_of_week"},
		},
		{
			name:    "allow-margin only",
			set:     dcaState{planID: "plan-1", allowMargin: true},
			want:    []string{"allow_margin", "true"},
			notWant: []string{"per_invest_amount", "day_of_week"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDCAFlags(t, tt.set)
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() { err = doUpdate(context.Background(), blockedCfg(), c.connect) })
			if err == nil || !isBlocked(err) {
				t.Fatalf("doUpdate() = %v, want a *config.BlockedError", err)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s) while blocked", c.calls)
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("preview is missing %q.\npreview was:\n%s", want, out)
				}
			}
			// A field that must not be sent is not missing from the preview —
			// describe() renders every key — it is rendered as OrDash's "-". The
			// previous check compared a line against the bare key and so could
			// never match, since every line carries the [DRY-RUN] prefix: it
			// passed whatever the preview said.
			for _, notWant := range tt.notWant {
				got, ok := dryRunField(out, notWant)
				if !ok {
					t.Errorf("preview has no %q line at all, so it cannot be shown to be absent.\npreview was:\n%s",
						notWant, out)
					continue
				}
				if got != "-" {
					t.Errorf("preview sends %s = %q but only %q was set: a sparse update must leave "+
						"the other fields alone.\npreview was:\n%s", notWant, got, tt.name, out)
				}
			}
		})
	}
}

// doUpdate used to be the one DCA path that never called schedule(), so it sent
// whatever the flags said: -day-of-month 99, and a weekday and a day of the
// month together under -frequency monthly, neither of which create accepts. It
// now runs the same validation, and the refusal is a flag error rather than a
// gate refusal, because an unusable flag has to be reported as one even when the
// gate is shut.
//
// Originally pinned as TestDoUpdate_FindingNoCrossValidationOnTheUpdatePath.
func TestDoUpdate_AppliesTheSameScheduleValidationAsCreate(t *testing.T) {
	tests := []struct {
		name    string
		set     dcaState
		wantErr string
	}{
		{
			name:    "out-of-range day of month with a contradictory weekday",
			set:     dcaState{planID: "plan-1", frequency: "monthly", dayOfWeek: "Monday", dayOfMonth: 99},
			wantErr: "-day-of-month must be 1-31",
		},
		{
			name:    "both day fields under a monthly frequency",
			set:     dcaState{planID: "plan-1", frequency: "monthly", dayOfWeek: "Monday", dayOfMonth: 15},
			wantErr: "mutually exclusive",
		},
		{
			name:    "weekday with no frequency at all, which reads as monthly",
			set:     dcaState{planID: "plan-1", dayOfWeek: "Monday"},
			wantErr: "-day-of-week",
		},
		{
			name:    "a weekly frequency with no weekday to go with it",
			set:     dcaState{planID: "plan-1", frequency: "weekly"},
			wantErr: "-day-of-week is required",
		},
		{
			name:    "a daily frequency carrying a day of the month",
			set:     dcaState{planID: "plan-1", frequency: "daily", dayOfMonth: 5},
			wantErr: "-day-of-month",
		},
		{
			name:    "a day of the month on its own, out of range",
			set:     dcaState{planID: "plan-1", dayOfMonth: 32},
			wantErr: "-day-of-month must be 1-31",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDCAFlags(t, tt.set)
			c := &failConnect{}
			var err error
			out := captureStdout(t, func() { err = doUpdate(context.Background(), blockedCfg(), c.connect) })
			if err == nil {
				t.Fatalf("doUpdate() = nil for %s", tt.name)
			}
			if isBlocked(err) {
				t.Errorf("doUpdate() = %v, a gate refusal; an unusable flag must be reported as "+
					"such even when the gate is shut", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not name %q", err, tt.wantErr)
			}
			if c.calls != 0 {
				t.Errorf("connect() was called %d time(s); validation must precede any client", c.calls)
			}
			// A refused flag prints no request at all, which is how a user tells
			// "your flags are wrong" from "the gate is shut".
			if strings.Contains(out, "POST /v1/dailycoins/update") {
				t.Errorf("a request was previewed despite a flag error:\n%s", out)
			}
		})
	}
}

// The other direction, and the reason the validation cannot simply be stricter
// than create's: UpdateOptions is sparse, so an update that names no schedule
// flag at all must change nothing about the schedule. An amount-only change is
// the documented example.
func TestDoUpdate_AnAmountOnlyUpdateLeavesTheScheduleAlone(t *testing.T) {
	setDCAFlags(t, dcaState{planID: "plan-1", amount: "250"})
	c := &failConnect{}
	var err error
	out := captureStdout(t, func() { err = doUpdate(context.Background(), blockedCfg(), c.connect) })
	if err == nil || !isBlocked(err) {
		t.Fatalf("doUpdate() = %v, want a *config.BlockedError: the amount-only update is valid "+
			"and must reach the gate", err)
	}
	if c.calls != 0 {
		t.Errorf("connect() was called %d time(s) while blocked", c.calls)
	}
	// Every schedule field has to be absent from the preview, because absent is
	// what tells the SDK not to send it.
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "invest_frequency:", "day_of_week:", "day_of_month:":
			t.Errorf("preview carries %s, but no schedule flag was set:\n%s", fields[0], out)
		}
	}
	if !strings.Contains(out, "250") {
		t.Errorf("preview is missing the amount that WAS set:\n%s", out)
	}
}

// ------------------------------------------------------------------ gate

func TestGate_RefusalIsExplainedOnStderrAndReturnsABlockedError(t *testing.T) {
	setDCAFlags(t, dcaState{})
	var err error
	out := captureStderr(t, func() { err = gate(blockedCfg(), "create a DCA plan for 700.HK") })
	if err == nil {
		t.Fatal("gate() = nil; a blocked write must be distinguishable from a completed one")
	}
	if !isBlocked(err) {
		t.Errorf("gate() = %v, want a *config.BlockedError", err)
	}
	for _, want := range []string{
		"[DRY-RUN] BLOCKED:", "create a DCA plan for 700.HK",
		"LONGPORT_DCA_DRY_RUN", "--confirm-live-dca", "LONGPORT_MODE=live",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal block is missing %q.\nblock was:\n%s", want, out)
		}
	}
}

// ------------------------------------------------------------------ helpers

func TestSplitList_SplitsOnCommasAndDropsBlanks(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{",,,", nil},
		{"700.HK", []string{"700.HK"}},
		{"700.HK,9988.HK", []string{"700.HK", "9988.HK"}},
		{" 700.HK , 9988.HK ", []string{"700.HK", "9988.HK"}},
		{"700.HK,,9988.HK,", []string{"700.HK", "9988.HK"}},
		// An empty entry is dropped rather than sent as an empty symbol, which
		// the API would reject for a reason that names neither the flag nor the
		// position of the stray comma.
		{"700.HK, ,9988.HK", []string{"700.HK", "9988.HK"}},
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

func TestSymbolsOr_ReturnsTheSymbolFlagUnchanged(t *testing.T) {
	setDCAFlags(t, dcaState{symbol: "700.HK,9988.HK"})
	if got := symbolsOr(); got != "700.HK,9988.HK" {
		t.Errorf("symbolsOr() = %q, want the raw -symbol; splitting is splitList's job", got)
	}
}

func TestOptUint_NilIsEmptyAndAValueIsDecimal(t *testing.T) {
	if got := optUint(nil); got != "" {
		t.Errorf("optUint(nil) = %q, want \"\"", got)
	}
	one, max := uint32(1), uint32(4294967295)
	if got := optUint(&one); got != "1" {
		t.Errorf("optUint(1) = %q, want \"1\"", got)
	}
	if got := optUint(&max); got != "4294967295" {
		t.Errorf("optUint(max) = %q, want \"4294967295\"", got)
	}
}

func TestDerefHelpers_NilIsEmptyAndValuesAreRendered(t *testing.T) {
	if got := derefStr(nil); got != "" {
		t.Errorf("derefStr(nil) = %q, want \"\"", got)
	}
	if got := derefFreq(nil); got != "" {
		t.Errorf("derefFreq(nil) = %q, want \"\"", got)
	}
	if got := derefBool(nil); got != "" {
		t.Errorf("derefBool(nil) = %q, want \"\"", got)
	}
	s := "  spaced  "
	if got := derefStr(&s); got != "  spaced  " {
		t.Errorf("derefStr = %q, want the string verbatim; trimming is the caller's decision", got)
	}
	f := dca.DCAFrequencyFortnightly
	if got := derefFreq(&f); got != "Fortnightly" {
		t.Errorf("derefFreq(Fortnightly) = %q, want the SDK wire string", got)
	}
	for _, b := range []bool{true, false} {
		want := "false"
		if b {
			want = "true"
		}
		if got := derefBool(&b); got != want {
			t.Errorf("derefBool(%v) = %q, want %q", b, got, want)
		}
	}
}
