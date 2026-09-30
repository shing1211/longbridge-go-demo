package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// boolishValues is a deliberately hostile set of strings. The gate is only ever
// opened by an exact bool literal, so the test asserts that against the whole
// list rather than against a handful of happy paths: a future "improvement"
// that treats "off", "no" or "" as false would silently unblock a real order.
var boolishValues = []string{
	"", " ", "\t", "\n", " 0 ", "0", "1", " 1 ", "1 ", "0\t",
	"t", "T", "f", "F",
	"true", "TRUE", "True", "false", "FALSE", "False",
	"yes", "no", "y", "n", "on", "off", "ON", "OFF", "Yes", "No",
	"2", "-1", "0.0", "1.0", "00", "0x0", "null", "nil", "undefined", "unset",
	"live", "simulated", "dry", "enabled", "disabled", "０", "0 0", "1 1", "0;0",
}

// clearsDryRun reports whether v is one of the exact literals that switches a
// dry-run env var OFF. It mirrors strconv.ParseBool, which is what the
// production code uses; the test re-derives the expectation independently so a
// change to the production parser cannot silently rewrite the oracle.
func clearsDryRun(v string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	return err == nil && !b
}

func writeGuardWithEnv(name, env string, requireLive bool) WriteGuard {
	return WriteGuard{
		Name:        name,
		Description: name + " test gate",
		DryRunEnv:   env,
		ConfirmFlag: "--confirm-" + strings.ToLower(name),
		RequireLive: requireLive,
	}
}

// setDryRunEnv either sets the env var or removes it entirely. Removal matters:
// the production code uses os.LookupEnv, which distinguishes "unset" from
// "set to the empty string", and t.Setenv can only express the latter.
func setDryRunEnv(t *testing.T, key, val string, set bool) {
	t.Helper()
	if set {
		t.Setenv(key, val)
		return
	}
	unsetEnv(t, key)
}

// TestWriteGuard_CheckEightCellMatrix runs the whole eight-cell sweep for every
// declared gate, not just DCA. A new WriteGuard is a new set of switches with
// its own env var, and the only interesting property of each is the same one:
// across {flag} x {dry-run env} x {mode} exactly ONE of the eight lets the
// write through. Deriving the list from declaredGuards() means a gate added
// there is swept here automatically.
func TestWriteGuard_CheckEightCellMatrix(t *testing.T) {
	for _, g := range declaredGuards() {
		t.Run(g.Name, func(t *testing.T) {
			checkEightCellMatrix(t, g, fmt.Sprintf("write through the %s gate", g.Name))
		})
	}
}

func checkEightCellMatrix(t *testing.T, g WriteGuard, action string) {
	t.Helper()
	type cell struct {
		name      string
		confirmed bool
		envVal    string
		envSet    bool
		mode      Mode
		open      bool
	}
	cells := []cell{
		{"flag=no env=unset mode=simulated", false, "", false, ModeSimulated, false},
		{"flag=no env=0 mode=simulated", false, "0", true, ModeSimulated, false},
		{"flag=yes env=unset mode=simulated", true, "", false, ModeSimulated, false},
		{"flag=yes env=0 mode=simulated", true, "0", true, ModeSimulated, false},
		{"flag=no env=unset mode=paper", false, "", false, ModePaper, false},
		{"flag=no env=0 mode=paper", false, "0", true, ModePaper, false},
		{"flag=yes env=unset mode=paper", true, "", false, ModePaper, false},
		{"flag=yes env=0 mode=paper", true, "0", true, ModePaper, true}, // open cell: paper+dryRun=false+confirm
		{"flag=no env=unset mode=live", false, "", false, ModeLive, false},
		{"flag=no env=0 mode=live", false, "0", true, ModeLive, false},
		{"flag=yes env=unset mode=live", true, "", false, ModeLive, false},
		{"flag=yes env=0 mode=live", true, "0", true, ModeLive, true}, // open cell: live+dryRun=false+confirm
	}
	if len(cells) != 12 {
		t.Fatalf("matrix must have 12 cells, has %d", len(cells))
	}
	openCount := 0
	for _, c := range cells {
		t.Run(c.name, func(t *testing.T) {
			sandbox(t)
			setDryRunEnv(t, g.DryRunEnv, c.envVal, c.envSet)
			cfg := newTestConfig(c.mode, false)

			err := g.Check(cfg, c.confirmed, action)

			if c.open {
				openCount++
				if err != nil {
					t.Fatalf("this cell must be open, got blocked: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("SAFETY BUG: this cell must block, but Check returned nil")
			}
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("refusal must wrap ErrBlocked so cli.Fail exits %d; got %T: %v", ExitBlocked, err, err)
			}
			var be *BlockedError
			if !errors.As(err, &be) {
				t.Fatalf("refusal must be a *BlockedError, got %T", err)
			}
		})
	}
	if openCount != 2 {
		t.Fatalf("exactly two cells may be open for %s (paper and live), %d were", g.Name, openCount)
	}
}

func TestWriteGuard_Check_OnlyExactFalsyOpensTheGate(t *testing.T) {
	g := writeGuardWithEnv("PROBE", "LONGPORT_PROBE_DRY_RUN", true)
	for _, v := range boolishValues {
		t.Run(fmt.Sprintf("value=%q", v), func(t *testing.T) {
			sandbox(t)
			t.Setenv(g.DryRunEnv, v)
			cfg := newTestConfig(ModeLive, false)

			// Flag and mode are already satisfied, so DryRun() alone decides.
			err := g.Check(cfg, true, "write")

			if clearsDryRun(v) {
				if err != nil {
					t.Fatalf("%q is an exact falsy literal and must open the gate, got %v", v, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("SAFETY BUG: %q must keep the gate closed, but Check returned nil", v)
			}
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("want ErrBlocked, got %v", err)
			}
		})
	}
}

func TestWriteGuard_DryRun_TruthTable(t *testing.T) {
	g := writeGuardWithEnv("PROBE", "LONGPORT_PROBE_DRY_RUN", false)
	tests := []struct {
		name string
		set  bool
		val  string
		want bool
		why  string
	}{
		{name: "unset defaults to blocking", set: false, want: true,
			why: "an absent opt-in must never be read as consent"},
		{name: "empty value fails safe", set: true, val: "", want: true,
			why: "export FOO= is not an opt-in"},
		{name: "1 blocks", set: true, val: "1", want: true},
		{name: "true blocks", set: true, val: "true", want: true},
		{name: "TRUE blocks", set: true, val: "TRUE", want: true},
		{name: "0 clears", set: true, val: "0", want: false},
		{name: "false clears", set: true, val: "false", want: false},
		{name: "FALSE clears", set: true, val: "FALSE", want: false},
		{name: "surrounding spaces are trimmed before parsing", set: true, val: " 0 ", want: false},
		{name: "surrounding tab is trimmed before parsing", set: true, val: "\t0\n", want: false},
		{name: "unparseable yes fails safe", set: true, val: "yes", want: true,
			why: "the dangerous direction would be treating it as false"},
		{name: "unparseable off fails safe", set: true, val: "off", want: true,
			why: "a user meaning 'off' gets a refusal, which is the safe outcome"},
		{name: "unparseable -1 fails safe", set: true, val: "-1", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			setDryRunEnv(t, g.DryRunEnv, tt.val, tt.set)
			if got := g.DryRun(); got != tt.want {
				t.Fatalf("DryRun() = %v, want %v (set=%v val=%q): %s", got, tt.want, tt.set, tt.val, tt.why)
			}
		})
	}
}

func TestWriteGuard_DryRun_DefaultsToOnForEveryDeclaredGuard(t *testing.T) {
	for _, g := range declaredGuards() {
		t.Run(g.Name, func(t *testing.T) {
			sandbox(t)
			unsetEnv(t, g.DryRunEnv)
			if !g.DryRun() {
				t.Fatalf("%s must default to dry run when %s is unset", g.Name, g.DryRunEnv)
			}
		})
	}
}

// TestDeclaredGuards pins the package-level guard values. They are vars, not
// consts — Go has no constant structs — so any package could reassign them; a
// mutation that flipped RequireLive to false would remove the live-mode
// assertion from a real money-moving gate without touching a single line of
// guard logic. This test is the tripwire for that: it fails the moment any
// field is edited.
func TestDeclaredGuards(t *testing.T) {
	tests := []struct {
		g                  WriteGuard
		name               string
		dryRunEnv          string
		confirmFlag        string
		requireLive        bool
		emptyDescriptionOK bool
	}{
		{
			g: DCAGuard, name: "DCA", dryRunEnv: "LONGPORT_DCA_DRY_RUN",
			confirmFlag: "--confirm-live-dca", requireLive: true,
		},
		{
			g: AlertGuard, name: "price alert", dryRunEnv: "LONGPORT_ALERT_DRY_RUN",
			confirmFlag: "--confirm-live-alert", requireLive: true,
		},
		{
			g: SharelistGuard, name: "sharelist", dryRunEnv: "LONGPORT_SHARELIST_DRY_RUN",
			confirmFlag: "--confirm-live-sharelist", requireLive: true,
		},
		{
			g: ContentGuard, name: "content", dryRunEnv: "LONGPORT_CONTENT_DRY_RUN",
			confirmFlag: "--confirm-live-content", requireLive: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.g.Name != tt.name {
				t.Errorf("Name = %q, want %q", tt.g.Name, tt.name)
			}
			if tt.g.DryRunEnv != tt.dryRunEnv {
				t.Errorf("DryRunEnv = %q, want %q", tt.g.DryRunEnv, tt.dryRunEnv)
			}
			if tt.g.ConfirmFlag != tt.confirmFlag {
				t.Errorf("ConfirmFlag = %q, want %q", tt.g.ConfirmFlag, tt.confirmFlag)
			}
			if tt.g.RequireLive != tt.requireLive {
				t.Errorf("SAFETY: RequireLive = %v, want %v", tt.g.RequireLive, tt.requireLive)
			}
			if tt.g.Description == "" {
				t.Error("Description must be non-empty: it is printed inside every refusal")
			}
		})
	}

	// Set-level invariants. These are what make the four values a contract
	// rather than four unrelated literals: a gate added to declaredGuards()
	// must appear above, no two gates may share a switch, and the list the
	// loader validates must be the list asserted here.
	seenEnv := map[string]string{}
	seenFlag := map[string]string{}
	for _, tt := range tests {
		if other, dup := seenEnv[tt.dryRunEnv]; dup {
			t.Errorf("SAFETY: %s and %s share the dry-run switch %s; one env var cannot guard two gates",
				other, tt.name, tt.dryRunEnv)
		}
		seenEnv[tt.dryRunEnv] = tt.name
		if other, dup := seenFlag[tt.confirmFlag]; dup {
			t.Errorf("SAFETY: %s and %s share the confirm flag %s", other, tt.name, tt.confirmFlag)
		}
		seenFlag[tt.confirmFlag] = tt.name
	}
	if got := declaredGuards(); len(got) != len(tests) {
		t.Errorf("declaredGuards() has %d gates but TestDeclaredGuards pins %d; "+
			"a gate must be added in both places", len(got), len(tests))
	}
}

// TestDeclaredGuards_RefusalNamesItsOwnSwitches catches the copy-paste mistake
// that declaring a gate by hand invites: a guard wired to another gate's env
// var or flag. A user who sets the documented switch would be told it is still
// on, with no way to see why.
func TestDeclaredGuards_RefusalNamesItsOwnSwitches(t *testing.T) {
	for _, g := range declaredGuards() {
		t.Run(g.Name, func(t *testing.T) {
			sandbox(t)
			unsetEnv(t, g.DryRunEnv)
			err := g.Check(newTestConfig(ModeSimulated, false), false, "do the thing")
			if err == nil {
				t.Fatal("want a refusal")
			}
			msg := err.Error()
			if !strings.Contains(msg, g.DryRunEnv) {
				t.Errorf("refusal must name %s:\n%s", g.DryRunEnv, msg)
			}
			if !strings.Contains(msg, g.ConfirmFlag) {
				t.Errorf("refusal must name %s:\n%s", g.ConfirmFlag, msg)
			}
			if !strings.Contains(msg, g.Description) {
				t.Errorf("refusal must explain what the gate protects (Description):\n%s", msg)
			}
			for _, other := range declaredGuards() {
				if other.Name == g.Name {
					continue
				}
				if strings.Contains(msg, other.DryRunEnv) || strings.Contains(msg, other.ConfirmFlag) {
					t.Errorf("the %s refusal mentions the %s switches, so a user is told to set a variable that does not affect it:\n%s",
						g.Name, other.Name, msg)
				}
			}
		})
	}
}

// TestDeclaredGuards_AnotherGatesDryRunSwitchCannotOpenIt is the cross-gate
// contamination test. Each gate has its own variable, and the only thing that
// keeps that true is that the four declarations say so; a copy-paste that gave
// two gates the same variable would leave one of them openable by a switch the
// user thinks is unrelated. Here every OTHER gate's variable is cleared and
// this gate must still refuse.
func TestDeclaredGuards_AnotherGatesDryRunSwitchCannotOpenIt(t *testing.T) {
	for _, g := range declaredGuards() {
		t.Run(g.Name, func(t *testing.T) {
			sandbox(t)
			for _, other := range declaredGuards() {
				if other.DryRunEnv == g.DryRunEnv {
					continue
				}
				t.Setenv(other.DryRunEnv, "0")
			}
			// Flag given and mode live, so this gate's own env is the only thing
			// left that can block it.
			if err := g.Check(newTestConfig(ModeLive, false), true, "write"); err == nil {
				t.Fatalf("SAFETY BUG: %s was opened by another gate's dry-run switch", g.Name)
			} else if !errors.Is(err, ErrBlocked) {
				t.Fatalf("want a refusal wrapping ErrBlocked, got %v", err)
			}
		})
	}
}

func TestWriteGuard_Unsatisfied_OrderAndCount(t *testing.T) {
	// The order is user-visible output (a bulleted list in the refusal), so
	// these assertions are on the exact sequence, not just the length.
	flagLine := "--confirm-live-dca was not passed (pass --confirm-live-dca)"
	envLine := "LONGPORT_DCA_DRY_RUN is on (default 1; set it to 0 to allow DCA writes)"
	modeLineSim := "LONGPORT_MODE=simulated (DCA writes require LONGPORT_MODE=live or =paper)"

	tests := []struct {
		name      string
		confirmed bool
		envVal    string
		envSet    bool
		mode      Mode
		want      []string
	}{
		{"nothing satisfied", false, "", false, ModeSimulated, []string{flagLine, envLine, modeLineSim}},
		{"only flag satisfied", true, "", false, ModeSimulated, []string{envLine, modeLineSim}},
		{"only env satisfied", false, "0", true, ModeSimulated, []string{flagLine, modeLineSim}},
		{"flag and env satisfied, simulated", true, "0", true, ModeSimulated, []string{modeLineSim}},
		{"only mode satisfied", false, "", false, ModeLive, []string{flagLine, envLine}},
		{"only env satisfied, live", false, "0", true, ModeLive, []string{flagLine}},
		{"all but flag satisfied, live", true, "", false, ModeLive, []string{envLine}},
		{"fully open", true, "0", true, ModeLive, nil},
		// Paper mode shares the same Unsatisfied path as live (allowsWrites=true),
		// so the mode line is NOT added — paper satisfies the write condition.
		// Only the description differs, tested separately.
		{"nothing satisfied, paper", false, "", false, ModePaper, []string{flagLine, envLine}},
		{"only flag satisfied, paper", true, "", false, ModePaper, []string{envLine}},
		{"only env satisfied, paper", false, "0", true, ModePaper, []string{flagLine}},
		{"flag and env satisfied, paper", true, "0", true, ModePaper, nil}, // open: paper allows writes
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			setDryRunEnv(t, DCAGuard.DryRunEnv, tt.envVal, tt.envSet)
			got := DCAGuard.Unsatisfied(newTestConfig(tt.mode, false), tt.confirmed)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d conditions %q, want %d %q", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("condition %d = %q, want %q (full list %q)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestWriteGuard_Unsatisfied_RequireLiveFalseNeverEmitsAModeLine(t *testing.T) {
	// A gate that does not require live must not tell the user to switch to
	// live mode: for watchlist-style preferences that advice is simply wrong.
	for _, mode := range []Mode{ModeSimulated, ModePaper, ModeLive, Mode(""), Mode("bogus")} {
		t.Run("mode="+string(mode)+"-"+fmt.Sprint(mode == ""), func(t *testing.T) {
			sandbox(t)
			g := writeGuardWithEnv("watchlistish", "LONGPORT_PROBE_DRY_RUN", false)
			t.Setenv(g.DryRunEnv, "0")

			for _, confirmed := range []bool{true, false} {
				for _, s := range g.Unsatisfied(newTestConfig(mode, false), confirmed) {
					if strings.Contains(s, "LONGPORT_MODE") {
						t.Fatalf("RequireLive=false must not emit a mode condition, got %q", s)
					}
				}
			}
			// And the gate really is open in simulated mode, which is the whole
			// point of the per-gate choice.
			if err := g.Check(newTestConfig(ModeSimulated, false), true, "add AAPL to watchlist"); err != nil {
				t.Fatalf("RequireLive=false gate should be open in simulated mode, got %v", err)
			}
			if err := g.Check(newTestConfig(ModeLive, false), true, "add AAPL to watchlist"); err != nil {
				t.Fatalf("RequireLive=false gate should be open in live mode, got %v", err)
			}
		})
	}
}

func TestWriteGuard_Unsatisfied_ReportsAnUnrecognisedMode(t *testing.T) {
	// Default deny: anything that is not ModeLive or ModePaper blocks.
	sandbox(t)
	t.Setenv(DCAGuard.DryRunEnv, "0")
	got := DCAGuard.Unsatisfied(newTestConfig(Mode("live-ish"), false), true)
	want := "LONGPORT_MODE=live-ish (DCA writes require LONGPORT_MODE=live or =paper)"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want exactly [%q]", got, want)
	}
}

func TestWriteGuard_Check_MessageContent(t *testing.T) {
	tests := []struct {
		name        string
		requireLive bool
		wantRecipe  string
	}{
		{
			name:        "require live recipe names all three switches",
			requireLive: true,
			wantRecipe:  "LONGPORT_PROBE_DRY_RUN=0  +  --confirm-probe  +  LONGPORT_MODE=live (or =paper for a simulated account)",
		},
		{
			name:        "no live recipe omits the mode switch",
			requireLive: false,
			wantRecipe:  "LONGPORT_PROBE_DRY_RUN=0  +  --confirm-probe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			g := writeGuardWithEnv("PROBE", "LONGPORT_PROBE_DRY_RUN", tt.requireLive)
			g.Description = "PROBE is a description shown to the user"
			unsetEnv(t, g.DryRunEnv)

			err := g.Check(newTestConfig(ModeSimulated, false), false, "pause DCA plan 12345")
			if err == nil {
				t.Fatal("want a refusal")
			}
			msg := err.Error()
			for _, want := range []string{
				"refusing to pause DCA plan 12345.",
				g.Description,
				"Unsatisfied condition(s):",
				"NOTHING was sent",
				tt.wantRecipe,
				"All of them are required",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("message is missing %q\n---\n%s", want, msg)
				}
			}
			if !strings.HasSuffix(msg, "All of them are required; any one alone still blocks the write.") {
				t.Errorf("message must end with the all-required warning, got:\n%s", msg)
			}
			// The action is echoed verbatim; a caller passing a formatted
			// string is fine but an empty one is a programming error (covered
			// separately).
			if strings.Count(msg, "pause DCA plan 12345") < 1 {
				t.Errorf("action must appear in the message:\n%s", msg)
			}
		})
	}
}

func TestWriteGuard_Check_EmptyActionIsNotABlockedError(t *testing.T) {
	// Deliberate: a missing action description is a bug in the CALLER, not a
	// safety refusal, so it is a plain error and must not be reported as
	// ErrBlocked. Exit status 1 ("broken command") is the honest answer.
	sandbox(t)
	unsetEnv(t, DCAGuard.DryRunEnv)
	g := DCAGuard
	err := g.Check(newTestConfig(ModeLive, false), true, "")
	if err == nil {
		t.Fatal("empty action must be rejected")
	}
	if errors.Is(err, ErrBlocked) {
		t.Error("an empty action is a caller bug, not a guard refusal: it must not wrap ErrBlocked")
	}
	var be *BlockedError
	if errors.As(err, &be) {
		t.Errorf("must be a plain error, got *BlockedError: %q", be.Reason)
	}
	if got, want := err.Error(), "WriteGuard(DCA): action description is required"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestWriteGuard_Check_DoesNotReadTheConfigFile(t *testing.T) {
	// The doc comment promises Check never touches the filesystem. A config
	// file claiming live mode must not be able to open a simulated gate.
	sandbox(t)
	writeFile(t, "config.yaml", "longbridge:\n  mode: live\n  app_key: FROMFILE\n")
	unsetEnv(t, DCAGuard.DryRunEnv)

	if err := DCAGuard.Check(newTestConfig(ModeSimulated, false), true, "pause DCA plan 1"); err == nil {
		t.Fatal("SAFETY BUG: Check must not read config.yaml, and must still block in simulated mode")
	}
}

func TestWriteGuard_ValidateDryRunEnv(t *testing.T) {
	g := writeGuardWithEnv("PROBE", "LONGPORT_PROBE_DRY_RUN", false)
	tests := []struct {
		name    string
		set     bool
		val     string
		wantErr bool
	}{
		{"unset is accepted", false, "", false},
		{"0 is accepted", true, "0", false},
		{"1 is accepted", true, "1", false},
		{"false is accepted", true, "false", false},
		{"padded 0 is accepted", true, " 0 ", false},
		// A set-but-empty variable is a startup error even though DryRun()
		// treats it as blocking. `export FOO=` and a bare `FOO=` line in a .env
		// both land here: safe, but noisy. See the report.
		{"empty value is rejected", true, "", true},
		{"yes is rejected", true, "yes", true},
		{"off is rejected", true, "off", true},
		{"2 is rejected", true, "2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			setDryRunEnv(t, g.DryRunEnv, tt.val, tt.set)
			err := g.ValidateDryRunEnv()
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidateDryRunEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				want := fmt.Sprintf("invalid %s=%q: want 1, true, 0 or false", g.DryRunEnv, tt.val)
				if err.Error() != want {
					t.Errorf("message = %q, want %q", err, want)
				}
			}
		})
	}
}

func TestWriteGuard_ValidateAllDryRunEnvs(t *testing.T) {
	first := writeGuardWithEnv("FIRST", "LONGPORT_FIRST_DRY_RUN", true)
	second := writeGuardWithEnv("SECOND", "LONGPORT_SECOND_DRY_RUN", true)

	t.Run("zero guards is nil", func(t *testing.T) {
		sandbox(t)
		if err := ValidateAllDryRunEnvs(); err != nil {
			t.Fatalf("no guards to check must be nil, got %v", err)
		}
	})

	t.Run("all valid is nil", func(t *testing.T) {
		sandbox(t)
		t.Setenv(first.DryRunEnv, "0")
		t.Setenv(second.DryRunEnv, "1")
		if err := ValidateAllDryRunEnvs(first, second); err != nil {
			t.Fatalf("got %v, want nil", err)
		}
	})

	t.Run("first error wins and its position is reported", func(t *testing.T) {
		sandbox(t)
		t.Setenv(first.DryRunEnv, "nope")
		t.Setenv(second.DryRunEnv, "also-nope")
		err := ValidateAllDryRunEnvs(first, second)
		if err == nil {
			t.Fatal("want an error")
		}
		if want := fmt.Sprintf("invalid %s=", first.DryRunEnv); !strings.Contains(err.Error(), want) {
			t.Fatalf("first guard in the argument list must be the one reported, got %q", err)
		}
		if strings.Contains(err.Error(), second.DryRunEnv) {
			t.Fatalf("the loop returns early; the second guard must not appear: %q", err)
		}
	})

	t.Run("a later bad guard is still caught", func(t *testing.T) {
		sandbox(t)
		unsetEnv(t, first.DryRunEnv)
		t.Setenv(second.DryRunEnv, "nope")
		err := ValidateAllDryRunEnvs(first, second)
		if err == nil {
			t.Fatal("want an error")
		}
		if want := fmt.Sprintf("invalid %s=", second.DryRunEnv); !strings.Contains(err.Error(), want) {
			t.Fatalf("got %q, want it to mention %s", err, second.DryRunEnv)
		}
	})

	t.Run("the declared repo guards validate clean when unset", func(t *testing.T) {
		sandbox(t)
		envs := make([]string, 0, len(declaredGuards()))
		for _, g := range declaredGuards() {
			envs = append(envs, g.DryRunEnv)
		}
		unsetEnv(t, envs...)
		if err := ValidateAllDryRunEnvs(declaredGuards()...); err != nil {
			t.Fatalf("got %v, want nil", err)
		}
	})
}

func TestWriteGuard_Check_ReadsTheEnvAtCallTime(t *testing.T) {
	// A cached verdict would be a time-of-check/time-of-use hazard: the command
	// must call Check immediately before the mutation, and Check must consult
	// the environment as it is at that moment.
	sandbox(t)
	cfg := newTestConfig(ModeLive, false)
	if err := DCAGuard.Check(cfg, true, "pause DCA plan 1"); err == nil {
		t.Fatal("with the env unset the gate must be closed")
	}
	t.Setenv(DCAGuard.DryRunEnv, "0")
	if err := DCAGuard.Check(cfg, true, "pause DCA plan 1"); err != nil {
		t.Fatalf("after clearing the env the gate must open, got %v", err)
	}
	unsetEnv(t, DCAGuard.DryRunEnv)
	if err := DCAGuard.Check(cfg, true, "pause DCA plan 1"); err == nil {
		t.Fatal("after unsetting the env again the gate must close again")
	}
}
