package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// WriteGuard is a reusable, self-contained safety gate for a family of
// mutating SDK methods that are neither orders nor watchlist edits.
//
// # WHY THIS EXISTS
//
// The repo already had two hand-rolled gates, GuardWrite (orders) and
// GuardWatchlist, and adding a third and fourth for DCA and alert writes would
// have meant a third and fourth copy of the same loop:
//
//	if !flag { refuse }
//	if dryRunEnv { refuse }
//	if requireLive && mode != live { refuse }
//
// That copy-paste is where safety bugs live: a variant that forgets the exit
// code, or forgets that the two switches must be INDEPENDENT, is easy to write
// and hard to notice. So the loop is written once here and each new family of
// mutations just declares its own switches.
//
// # THE CONTRACT
//
// A mutation guarded by g proceeds only when ALL of the following hold:
//
//  1. confirmed is true — the caller passed g.ConfirmFlag, AND
//  2. g.DryRunEnv is set to 0/false — otherwise it defaults to ON, AND
//  3. if g.RequireLive, the config Mode is live
//
// Conditions 1 and 2 are INDEPENDENT and that is deliberate: a missing flag
// refuses even when the env is cleared, and the env refuses even when the flag
// is present. Neither alone can open the gate.
//
// # WHY REQUIRE LIVE IS PER-GATE
//
// GuardWrite requires live because it moves real money. GuardWatchlist does
// not, because a saved ticker list is a preference. A third kind of mutation —
// a recurring investment plan, a notification rule — is neither exactly, so
// this struct makes the choice explicit per gate instead of assuming.
//
// # NO NETWORK CALL
//
// Check() never touches the network and never reads a config file. On refusal
// it returns a *BlockedError, and the caller MUST return that error up to
// cli.Fail rather than swallowing it, so the process exits 3. A command that
// catches the error and returns nil reintroduces the very bug this file
// documents.
type WriteGuard struct {
	// Name is the short label used in messages, e.g. "DCA".
	Name string
	// Description explains in one sentence what this gate protects, shown in
	// the refusal so the user knows which gate they are up against.
	Description string
	// DryRunEnv is the environment variable that turns dry run OFF, e.g.
	// "LONGPORT_DCA_DRY_RUN". It defaults to ON when unset or unparseable.
	DryRunEnv string
	// ConfirmFlag is the command-line flag that must accompany the env, e.g.
	// "--confirm-live-dca". This is documentation only; the command owns the
	// flag and passes the bool in.
	ConfirmFlag string
	// RequireLive demands LONGPORT_MODE=live. Set it for anything that
	// commits real money or publishes something irreversible.
	RequireLive bool
}

// The gates this repo actually uses. Declaring them as package-level values
// means the env-var names and flag names exist in exactly one place.
var (
	// DCAGuard guards the six mutating DCA methods. A DCA plan spends real
	// money on a recurring schedule and Stop is not reversible, so it requires
	// an explicit live assertion.
	DCAGuard = WriteGuard{
		Name:        "DCA",
		Description: "a DCA plan invests real money on a recurring schedule, and Stop cannot be undone",
		DryRunEnv:   "LONGPORT_DCA_DRY_RUN",
		ConfirmFlag: "--confirm-live-dca",
		RequireLive: true,
	}

	// AlertGuard guards the three mutating price-alert methods. Alerts do not
	// move money, but Delete removes server-side state, so they get a real
	// gate. RequireLive is set anyway because an alert attached to a live
	// account is a real notification someone may act on.
	AlertGuard = WriteGuard{
		Name:        "price alert",
		Description: "price alerts are account state that other devices and people act on",
		DryRunEnv:   "LONGPORT_ALERT_DRY_RUN",
		ConfirmFlag: "--confirm-live-alert",
		RequireLive: true,
	}
)

// DryRun reports whether this guard is currently blocking. It is exported so a
// command can print the effective state in its startup banner without
// duplicating the lookup.
//
// It fails SAFE: an unset variable, or one that does not parse as a bool, both
// mean "still in dry run". Guessing the other way would be the dangerous
// default.
func (g WriteGuard) DryRun() bool {
	v, ok := os.LookupEnv(g.DryRunEnv)
	if !ok {
		return true
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return true
	}
	return b
}

// Unsatisfied returns every condition that is not met, in a fixed order, so a
// refusal can list all of them at once rather than making the user discover
// them one run at a time. An empty result means the gate is open.
//
// This is also what makes the two switches verifiably independent: a user who
// passes the flag but forgets the env is told about the env, and vice versa,
// in a single run.
func (g WriteGuard) Unsatisfied(cfg *Config, confirmed bool) []string {
	var out []string
	if !confirmed {
		out = append(out, fmt.Sprintf(
			"%s was not passed (pass %s)", g.ConfirmFlag, g.ConfirmFlag))
	}
	if g.DryRun() {
		out = append(out, fmt.Sprintf(
			"%s is on (default 1; set it to 0 to allow %s writes)",
			g.DryRunEnv, g.Name))
	}
	if g.RequireLive && cfg.Mode != ModeLive {
		out = append(out, fmt.Sprintf(
			"LONGPORT_MODE=%s (%s writes require LONGPORT_MODE=live)",
			cfg.Mode, g.Name))
	}
	return out
}

// Check returns nil only when the gate is open. On refusal it returns a
// *BlockedError (so cli.Fail exits 3) and the caller must make NO network call.
//
// The action is a human phrase such as "pause DCA plan 12345", used verbatim
// in the message.
func (g WriteGuard) Check(cfg *Config, confirmed bool, action string) error {
	if action == "" {
		return fmt.Errorf("WriteGuard(%s): action description is required", g.Name)
	}
	missing := g.Unsatisfied(cfg, confirmed)
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "refusing to %s.\n", action)
	fmt.Fprintf(&b, "No change was sent, because %s.\n", g.Description)
	b.WriteString("\nUnsatisfied condition(s):\n")
	for _, m := range missing {
		fmt.Fprintf(&b, "  - %s\n", m)
	}
	b.WriteString("\nNOTHING was sent to Longbridge. To actually perform this write, set:\n")
	if g.RequireLive {
		fmt.Fprintf(&b, "  %s=0  +  %s  +  LONGPORT_MODE=live\n",
			g.DryRunEnv, g.ConfirmFlag)
	} else {
		fmt.Fprintf(&b, "  %s=0  +  %s\n", g.DryRunEnv, g.ConfirmFlag)
	}
	b.WriteString("All of them are required; any one alone still blocks the write.")
	return Blockedf("%s", b.String())
}

// ValidateDryRunEnv reports a bad value for this guard's env var as a startup
// error rather than silently treating it as "on". Commands call this during
// config load so the operator finds out immediately rather than at the moment a
// write is refused.
func (g WriteGuard) ValidateDryRunEnv() error {
	v, ok := os.LookupEnv(g.DryRunEnv)
	if !ok {
		return nil
	}
	if _, err := strconv.ParseBool(strings.TrimSpace(v)); err != nil {
		return fmt.Errorf("invalid %s=%q: want 1, true, 0 or false", g.DryRunEnv, v)
	}
	return nil
}

// ValidateAllDryRunEnvs checks every declared guard, for commands that expose
// more than one. It exists so adding a gate does not also mean remembering to
// add a validation call.
func ValidateAllDryRunEnvs(guards ...WriteGuard) error {
	for _, g := range guards {
		if err := g.ValidateDryRunEnv(); err != nil {
			return err
		}
	}
	return nil
}
