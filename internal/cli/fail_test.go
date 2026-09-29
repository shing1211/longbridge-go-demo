package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/longbridge/openapi-go/oauth"

	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// ---------------------------------------------------------------------------
// Re-exec harness
//
// Fail and Run both end in os.Exit, so their exit status cannot be observed
// from inside the test process: the first os.Exit(1) would take the whole test
// binary down. Each case therefore runs in a re-executed copy of this binary,
// which is why the scenarios below live in a helper test that is skipped
// unless the guard environment variable is set.
// ---------------------------------------------------------------------------

const helperEnv = "GO_CLI_WANT_HELPER_PROCESS"

type helperResult struct {
	code   int
	stdout string
	stderr string
}

// runHelper re-executes the test binary, asks the helper to perform scenario,
// and reports how the child process terminated. The scenario is passed in the
// environment rather than on the command line so no argument parsing is
// involved, and the child is never given -test.paniconexit0, so a scenario
// that legitimately exits 0 does not turn into a panic.
func runHelper(t *testing.T, scenario string, env ...string) helperResult {
	t.Helper()
	return runHelperWith(t, scenario, nil, env...)
}

// runHelperWith is runHelper with the ability to remove variables from the
// child's environment, so a case can be made independent of whatever the
// developer's shell happens to export.
func runHelperWith(t *testing.T, scenario string, unset []string, env ...string) helperResult {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")

	drop := map[string]bool{}
	for _, name := range unset {
		drop[name] = true
	}
	kept := os.Environ()
	base := make([]string, 0, len(kept)+len(env)+1)
	for _, kv := range kept {
		if drop[strings.SplitN(kv, "=", 2)[0]] {
			continue
		}
		base = append(base, kv)
	}
	cmd.Env = append(base, helperEnv+"="+scenario)
	cmd.Env = append(cmd.Env, env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	res := helperResult{stdout: stdout.String(), stderr: stderr.String()}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.code = 0
	case errors.As(err, &exitErr):
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("could not run the helper process for %q: %v", scenario, err)
	}
	return res
}

// credentialEnv is every name config.Load consults, both spellings, so a
// "no credentials" case is reproducible on a machine that has them set.
var credentialEnv = []string{
	"LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET", "LONGBRIDGE_ACCESS_TOKEN",
	"LONGPORT_APP_KEY", "LONGPORT_APP_SECRET", "LONGPORT_ACCESS_TOKEN",
}

func TestHelperProcess(t *testing.T) {
	scenario := os.Getenv(helperEnv)
	if scenario == "" {
		t.Skip("not a helper-process invocation")
	}

	switch scenario {

	// --- Fail: the three statuses it distinguishes -------------------------
	case "fail-nil":
		Fail(nil)

	case "fail-blocked":
		Fail(&appcfg.BlockedError{Reason: "refusing to submit an order: DRY RUN is active"})

	case "fail-blocked-empty-reason":
		// A guard that refused without a message is still a refusal, and must
		// still be distinguishable from success by exit code alone.
		Fail(&appcfg.BlockedError{})

	case "fail-blocked-wrapped":
		Fail(fmt.Errorf("placing order 42: %w",
			appcfg.Blockedf("DRY RUN is active (mode=%s)", appcfg.ModeSimulated)))

	case "fail-missing-credentials":
		Fail(&appcfg.MissingCredentialError{
			Missing: []string{"LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_KEY"},
			Source:  "environment",
		})

	case "fail-missing-credentials-wrapped":
		Fail(fmt.Errorf("loading config: %w", &appcfg.MissingCredentialError{
			Missing: []string{"LONGBRIDGE_APP_SECRET"},
			Source:  "config.yaml",
		}))

	case "fail-generic":
		Fail(errors.New("creating trade context: httpStatus:401 code:401004 message:token invalid"))

	case "fail-generic-wrapped-blocked-lookalike":
		// Mentions "blocked" but does not wrap the sentinel: a real failure.
		Fail(errors.New("upstream said: blocked by safety guard"))

	// --- the guards themselves, end to end -------------------------------
	case "guard-order-write-refused":
		// The exact chain the whole repo rests on: a guard refuses, the caller
		// propagates, cli.Fail maps it to 3.
		cfg := &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
		Fail(cfg.GuardWrite("submit an order"))

	case "guard-dca-refused":
		cfg := &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
		Fail(appcfg.DCAGuard.Check(cfg, false, "stop DCA plan 12345"))

	case "guard-alert-refused":
		cfg := &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
		Fail(appcfg.AlertGuard.Check(cfg, true, "delete price alert 99"))

	// --- the invariant the read-only binaries share -------------------------
	case "read-only-assertion-open-gate":
		// Their startup assertion, with the order gate open. Name and action
		// arrive in the environment so that one scenario can stand in for all
		// of them; it defaults to market so it still means something if
		// it is ever run on its own. openGate is the single config the closed-
		// gate table in cli_test.go proves GuardWrite admits.
		name, action := os.Getenv("HELPER_NAME"), os.Getenv("HELPER_ACTION")
		if name == "" || action == "" {
			name, action = "market", "run the market reader"
		}
		AssertReadOnly(openGate, name, action)

	// --- Parse ------------------------------------------------------------
	case "parse-unknown-flag":
		NewUsage("demo", "a summary line").Parse([]string{"-nope"})

	case "parse-help":
		NewUsage("demo", "a summary line").Parse([]string{os.Getenv("HELPER_FLAG")})

	case "load-with-no-credentials":
		// The path every one of the commands takes on a fresh machine. It must
		// name all three missing variables at once and exit 2.
		NewUsage("demo", "s").Load()

	case "load-oauth-bad-mode":
		// cmd/auth's loader, which asks for no credential at all — so the way
		// it can fail at startup is a bad switch, and that has to be a plain
		// exit 1 rather than the missing-credentials exit 2, which would be
		// about the app-key triple this path never needed.
		NewUsage("demo", "s").LoadOAuth(oauth.New("client-id"))

	// --- Parse: the shared flags ------------------------------------------
	case "parse-reserved-header":
		// The security case. The flag layer has to refuse it on its own, with
		// no credentials present, because a credential header that silently
		// replaced the real one would not fail until the first request — and it
		// would fail at the far end instead of here.
		NewUsage("demo", "s").Parse([]string{"-header", os.Getenv("HELPER_HEADER")})

	case "parse-bad-log-level":
		NewUsage("demo", "s").Parse([]string{"-log-level", os.Getenv("HELPER_LEVEL")})

	// --- Run --------------------------------------------------------------
	case "run-blocked":
		Run(func(_ context.Context) error { return appcfg.Blockedf("watchlist write refused") })

	case "run-generic":
		Run(func(_ context.Context) error { return errors.New("boom") })

	case "run-panic-string":
		Run(func(_ context.Context) error { panic("the SDK exploded") })

	case "run-panic-error":
		Run(func(_ context.Context) error { panic(errors.New("the SDK exploded")) })

	default:
		t.Fatalf("unknown helper scenario %q", scenario)
	}

	// Reaching this line means the call under test returned instead of
	// exiting, which is itself a meaningful outcome for Fail(nil).
	fmt.Println("returned without exiting")
}

// ---------------------------------------------------------------------------
// Fail(nil)
// ---------------------------------------------------------------------------

func TestFail_NilReturnsWithoutExiting(t *testing.T) {
	// Safe to call in-process precisely because it must not exit: if the nil
	// guard were dropped, this test would take the binary down with it and
	// every other test in the package would report a failure.
	Fail(nil)
	Fail(error(nil))
}

// The child half of the same fact, so the assertion does not rely on the
// in-process call above surviving.
func TestFail_NilExitsZeroInAChildProcess(t *testing.T) {
	res := runHelper(t, "fail-nil")
	if res.code != 0 {
		t.Errorf("Fail(nil) exited %d, want 0.\nstderr:\n%s", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "returned without exiting") {
		t.Errorf("Fail(nil) did not return.\nstdout:\n%s", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("Fail(nil) wrote %q to stderr, want nothing", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// The exit-code contract
// ---------------------------------------------------------------------------

func TestExitBlockedConstantMatchesTheDocumentedPromise(t *testing.T) {
	// The README and every guarded command promise 3. 0, 1 and 2 are already
	// spoken for (success, real failure, missing credentials), so 3 is the only
	// status that can mean "the gate declined".
	if appcfg.ExitBlocked != 3 {
		t.Errorf("config.ExitBlocked = %d, want 3; 0, 1 and 2 already mean "+
			"success, real failure and missing credentials, and a refusal must "+
			"not collapse into any of them", appcfg.ExitBlocked)
	}
}

func TestFail_ExitCodeContract(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		want     int
		env      []string
		wantErr  []string
	}{
		{
			name:     "a guard refusal is BLOCKED, not a failure",
			scenario: "fail-blocked",
			want:     3,
			wantErr:  []string{"error: ", "refusing to submit an order: DRY RUN is active"},
		},
		{
			name:     "a refusal with no message is still BLOCKED",
			scenario: "fail-blocked-empty-reason",
			want:     3,
			wantErr:  []string{"error: "},
		},
		{
			name:     "a refusal survives being wrapped for context",
			scenario: "fail-blocked-wrapped",
			want:     3,
			wantErr:  []string{"placing order 42: ", "DRY RUN is active (mode=simulated)"},
		},
		{
			name:     "GuardWrite refusing an order is BLOCKED",
			scenario: "guard-order-write-refused",
			want:     3,
			wantErr:  []string{"refusing to submit an order", "No order was sent"},
		},
		{
			name:     "DCAGuard refusing a plan is BLOCKED",
			scenario: "guard-dca-refused",
			want:     3,
			wantErr:  []string{"stop DCA plan 12345", "NOTHING was sent"},
		},
		{
			name:     "AlertGuard refusing an alert is BLOCKED",
			scenario: "guard-alert-refused",
			want:     3,
			wantErr:  []string{"delete price alert 99", "LONGPORT_ALERT_DRY_RUN"},
		},
		{
			name:     "missing credentials are a usage problem",
			scenario: "fail-missing-credentials",
			want:     2,
			wantErr: []string{
				"missing Longbridge credentials",
				"(checked via environment)",
				"LONGBRIDGE_ACCESS_TOKEN",
				"LONGBRIDGE_APP_KEY",
			},
		},
		{
			name:     "a wrapped credential error is still 2",
			scenario: "fail-missing-credentials-wrapped",
			want:     2,
			wantErr:  []string{"missing Longbridge credentials", "config.yaml", "LONGBRIDGE_APP_SECRET"},
		},
		{
			name:     "anything else is an ordinary failure",
			scenario: "fail-generic",
			want:     1,
			wantErr:  []string{"error: creating trade context: httpStatus:401"},
		},
		{
			name:     "text that merely mentions the sentinel is still 1",
			scenario: "fail-generic-wrapped-blocked-lookalike",
			want:     1,
			wantErr:  []string{"blocked by safety guard"},
		},
		{
			name:     "an open gate in a read-only binary is a misconfiguration",
			scenario: "read-only-assertion-open-gate",
			want:     1,
			wantErr: []string{
				"internal invariant violated: market is read-only but the order gate is open",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runHelper(t, tt.scenario, tt.env...)
			if res.code != tt.want {
				t.Errorf("exit code = %d, want %d.\nstdout:\n%s\nstderr:\n%s",
					res.code, tt.want, res.stdout, res.stderr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(res.stderr, want) {
					t.Errorf("stderr does not contain %q.\nstderr:\n%s", want, res.stderr)
				}
			}
			if !strings.HasPrefix(res.stderr, "error: ") {
				t.Errorf("stderr = %q, want every failure report to start with %q",
					res.stderr, "error: ")
			}
			if !strings.HasSuffix(res.stderr, "\n") {
				t.Errorf("stderr = %q, want a trailing newline", res.stderr)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want failures reported on stderr only", res.stdout)
			}
		})
	}
}

func TestFail_EveryStatusIsDistinct(t *testing.T) {
	// The whole point of the convention is that a wrapper script can tell the
	// cases apart, so no two may collapse onto the same status.
	seen := map[int]string{}
	for _, tc := range []struct {
		scenario string
		what     string
	}{
		{"fail-nil", "success"},
		{"fail-generic", "real failure"},
		{"fail-missing-credentials", "missing credentials"},
		{"fail-blocked", "guard refusal"},
	} {
		res := runHelper(t, tc.scenario)
		if prev, dup := seen[res.code]; dup {
			t.Errorf("%s and %s both exit %d; the statuses must stay distinct",
				prev, tc.what, res.code)
		}
		seen[res.code] = tc.what
	}
	for _, want := range []int{0, 1, 2, 3} {
		if _, ok := seen[want]; !ok {
			t.Errorf("no scenario exits %d; got %v", want, seen)
		}
	}
}

// A BlockedError is a perfectly ordinary error value. Nothing about the type
// forces cli.Fail to look at it first, so the only thing keeping a refusal out
// of the generic branch is the order of the two checks in Fail. These tests
// make that dependency explicit.
func TestFail_BlockedCheckMustPrecedeTheGenericBranch(t *testing.T) {
	t.Run("a bare BlockedError satisfies every generic test", func(t *testing.T) {
		var err error = &appcfg.BlockedError{Reason: "refusing to submit an order"}

		// All three of these are true of a refusal, and every one of them
		// would route to the generic branch if the sentinel check were moved
		// below it:
		if err.Error() == "" {
			t.Error("BlockedError.Error() is empty; nothing to print")
		}
		if !errors.Is(err, appcfg.ErrBlocked) {
			t.Error("BlockedError must unwrap to ErrBlocked")
		}
		var blocked *appcfg.BlockedError
		if !errors.As(err, &blocked) {
			t.Error("BlockedError must be recoverable with errors.As")
		}
		var missing *appcfg.MissingCredentialError
		if errors.As(err, &missing) {
			t.Error("a refusal must not be mistaken for a credential problem")
		}
		// A refusal is deliberately indistinguishable from a plain error at the
		// type level, so the exit code is the only signal left.
		var plain error = err
		if plain == nil {
			t.Fatal("unreachable")
		}
	})

	t.Run("a BlockedError still exits 3, not 1", func(t *testing.T) {
		// This is the regression the README documents in reverse: a blocked
		// write once returned nil and exited 0, which a script could not tell
		// from a filled order. Exiting 1 would be almost as bad, because 1 is
		// what a real failure looks like.
		res := runHelper(t, "fail-blocked")
		if res.code != appcfg.ExitBlocked {
			t.Errorf("a guard refusal exited %d, want %d (config.ExitBlocked). "+
				"If this fails, the errors.Is(err, ErrBlocked) check in Fail has "+
				"moved below the generic branch, or the guard stopped wrapping the "+
				"sentinel.\nstderr:\n%s", res.code, appcfg.ExitBlocked, res.stderr)
		}
	})

	t.Run("a refusal is not swallowed on the way to Fail", func(t *testing.T) {
		cfg := &appcfg.Config{Mode: appcfg.ModeSimulated, DryRun: true}
		err := cfg.GuardWrite("submit an order")
		if err == nil {
			t.Fatal("GuardWrite returned nil in dry run")
		}
		if !errors.Is(err, appcfg.ErrBlocked) {
			t.Fatalf("GuardWrite refusal does not wrap ErrBlocked: %v", err)
		}
		res := runHelper(t, "guard-order-write-refused")
		if res.code != 3 {
			t.Errorf("the real guard chain exits %d, want 3", res.code)
		}
		if strings.Contains(res.stderr, "No order was sent") == false {
			t.Error("the refusal text must tell the user nothing was sent")
		}
	})
}

func TestFail_BlockedErrorMessageReachesTheUser(t *testing.T) {
	// A silent refusal is as bad as a wrong exit code: exit 3 with no
	// explanation reads like a crash.
	res := runHelper(t, "guard-dca-refused")
	if len(res.stderr) < 100 {
		t.Errorf("a blocked write produced only %d bytes of explanation:\n%s",
			len(res.stderr), res.stderr)
	}
	for _, want := range []string{
		"Unsatisfied condition",
		"LONGPORT_DCA_DRY_RUN",
		"--confirm-live-dca",
		"LONGPORT_MODE=live",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("the refusal does not mention %q.\nstderr:\n%s", want, res.stderr)
		}
	}
}

// All three branches of Fail report err, so a caller that wrapped a credential
// error to say where the load came from ("loading config from %s") keeps that
// context in the output. The exit code is unaffected (still 2); what this pins
// is the diagnostic, since losing the prefix leaves the user with a bare
// "missing Longbridge credentials" and no idea which load path failed.
func TestFail_WrappedCredentialErrorKeepsItsWrapContext(t *testing.T) {
	res := runHelper(t, "fail-missing-credentials-wrapped")
	if res.code != 2 {
		t.Errorf("exit code = %d, want 2", res.code)
	}

	const wrapContext = "loading config: "
	if !strings.Contains(res.stderr, wrapContext) {
		t.Errorf("stderr drops the caller's wrap context %q; Fail must print err, "+
			"not the unwrapped *MissingCredentialError.\nstderr:\n%s", wrapContext, res.stderr)
	}
	// The reason must still be there, and exactly once: printing err already
	// spells it out, so appending missing.Error() would duplicate it.
	if n := strings.Count(res.stderr, "missing Longbridge credentials"); n != 1 {
		t.Errorf("stderr states the reason %d times, want 1.\nstderr:\n%s", n, res.stderr)
	}
	if !strings.Contains(res.stderr, "LONGBRIDGE_APP_SECRET") {
		t.Errorf("stderr does not name the missing variable.\nstderr:\n%s", res.stderr)
	}

	// errors.As still has to find the inner error; that is what routes the
	// branch at all.
	err := fmt.Errorf("loading config from %s: %w", "config.yaml",
		&appcfg.MissingCredentialError{Missing: []string{"LONGBRIDGE_APP_KEY"}, Source: "environment"})
	var found *appcfg.MissingCredentialError
	if !errors.As(err, &found) {
		t.Fatal("errors.As did not find the wrapped credential error")
	}
}

// Usage.Load is what every command calls to reach exit 2. On a machine with no
// credentials it must name all three at once, and it must do so without
// touching the network.
func TestUsage_LoadWithoutCredentialsExitsTwo(t *testing.T) {
	res := runHelperWith(t, "load-with-no-credentials", credentialEnv)
	if res.code != 2 {
		t.Errorf("Load() with no credentials exited %d, want 2.\nstdout:\n%s\nstderr:\n%s",
			res.code, res.stdout, res.stderr)
	}
	for _, want := range []string{
		"missing Longbridge credentials",
		"LONGBRIDGE_APP_KEY",
		"LONGBRIDGE_APP_SECRET",
		"LONGBRIDGE_ACCESS_TOKEN",
		"How to fix",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not contain %q; every missing variable must be "+
				"listed at once, not one per run.\nstderr:\n%s", want, res.stderr)
		}
	}
	// The banner must not leak a value it never had.
	for _, secret := range []string{"app_secret", "access_token"} {
		if strings.Contains(res.stderr, secret+"=") {
			t.Errorf("stderr looks like it echoes a credential (%s=).\nstderr:\n%s", secret, res.stderr)
		}
	}
}

// cmd/auth loads its configuration through a different loader, because OAuth
// needs no app-key triple. The two must not be confused with each other: a bad
// switch is a usage error (exit 1) whichever loader found it, and the
// missing-credentials exit 2 must stay reserved for the triple the OAuth path
// does not ask for.
func TestUsage_LoadOAuthReportsABadSwitchAsExitOne(t *testing.T) {
	res := runHelperWith(t, "load-oauth-bad-mode", credentialEnv,
		"LONGPORT_MODE=sandbox")
	if res.code != 1 {
		t.Errorf("LoadOAuth() with a bad LONGPORT_MODE exited %d, want 1.\nstdout:\n%s\nstderr:\n%s",
			res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, `invalid LONGPORT_MODE="sandbox"`) {
		t.Errorf("stderr does not name the variable to fix:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, "missing Longbridge credentials") {
		t.Errorf("the OAuth loader must not report a missing credential; it asks "+
			"for none:\n%s", res.stderr)
	}
}

// An open order gate in a read-only binary is a misconfiguration, not a
// refusal, so it exits 1 — nothing was blocked, because nothing was ever
// attempted, and 3 would tell a wrapping script that a write had been declined.
// The stderr is pinned byte for byte rather than by substring, because the
// whole reason this assertion became a helper is that eight copies of the
// string had already drifted: cmd/quote said "the write gate is open" where the
// contract in README.md says "the order gate", and no test noticed.
func TestAssertReadOnly_OpenGateExitsOneMisconfiguration(t *testing.T) {
	for _, bin := range readOnlyBinaries {
		t.Run(bin.name, func(t *testing.T) {
			res := runHelper(t, "read-only-assertion-open-gate",
				"HELPER_NAME="+bin.name, "HELPER_ACTION="+bin.action)
			if res.code != 1 {
				t.Errorf("exit code = %d, want 1; 3 is reserved for a guard that "+
					"refused a write, and nothing was attempted here.\nstdout:\n%s\nstderr:\n%s",
					res.code, res.stdout, res.stderr)
			}
			want := fmt.Sprintf("error: internal invariant violated: %s is read-only "+
				"but the order gate is open\n", bin.name)
			if res.stderr != want {
				t.Errorf("stderr = %q, want exactly %q", res.stderr, want)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want empty: the report belongs on stderr", res.stdout)
			}
			// The child must have exited from Fail, not fallen out of the bottom
			// of the switch, so the harness's fallthrough marker must be absent.
			if strings.Contains(res.stdout, "returned without exiting") {
				t.Errorf("AssertReadOnly returned instead of exiting.\nstdout:\n%s", res.stdout)
			}
		})
	}
}

func TestFail_RunPropagatesARefusalToExit3(t *testing.T) {
	res := runHelper(t, "run-blocked")
	if res.code != 3 {
		t.Errorf("Run returned a refusal and the process exited %d, want 3.\nstdout:\n%s\nstderr:\n%s",
			res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "watchlist write refused") {
		t.Errorf("stderr = %q, want the guard's reason", res.stderr)
	}
}

// ---------------------------------------------------------------------------
// -header and -log-level at the exit-code level
// ---------------------------------------------------------------------------

// A reserved header is a usage error, and it has to be caught at flag-parse
// time rather than at load time: with no credentials in the environment, a
// load-time check would report the missing credential first and exit 2, which
// says nothing about the header the operator actually got wrong.
func TestUsage_AReservedHeaderExitsOneWithNoCredentialsPresent(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"authorization, the credential itself", "authorization=stolen-token-value", "authorization"},
		{"x-api-key", "x-api-key=stolen-token-value", "x-api-key"},
		{"x-api-signature", "x-api-signature=stolen-token-value", "x-api-signature"},
		{"x-timestamp", "x-timestamp=stolen-token-value", "x-timestamp"},
		{"a spelling that only differs in case", "AUTHORIZATION=stolen-token-value", "AUTHORIZATION"},
		{"no equals sign", "nonsense-value", `has no "="`},
		{"an empty name", "=value", "name is empty"},
		{"a name with a space", "bad name=value", "not a valid HTTP header name"},
		{"a value carrying a newline", "x-a=one\ntwo", "net/http refuses to send"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runHelperWith(t, "parse-reserved-header", credentialEnv,
				"HELPER_HEADER="+tt.header)
			if res.code != 1 {
				t.Errorf("-header %q exited %d, want 1.\nstdout:\n%s\nstderr:\n%s",
					tt.header, res.code, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, tt.want) {
				t.Errorf("stderr does not contain %q, so the operator is not told what "+
					"to change.\nstderr:\n%s", tt.want, res.stderr)
			}
			// The masking rule applies to the error path as well as the banner.
			// This is not hypothetical: the flag package's own wrapper for a Set
			// error is `invalid value %q for flag -%s`, which WOULD have printed
			// the whole argument — which is why the flag records the rejection
			// and reports it here instead.
			for _, secret := range []string{"stolen-token-value", "nonsense-value", "two"} {
				if strings.Contains(res.stderr, secret) {
					t.Errorf("stderr echoed %q.\nstderr:\n%s", secret, res.stderr)
				}
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want empty: a usage error is a diagnostic", res.stdout)
			}
		})
	}
}

// The same property for the log level, including the one name that looks
// plausible and is not honoured by the SDK's own logger.
func TestUsage_AnUnusableLogLevelExitsOne(t *testing.T) {
	for _, level := range []string{"trace", "verbose", "off", "warninG"} {
		t.Run(level, func(t *testing.T) {
			res := runHelperWith(t, "parse-bad-log-level", credentialEnv,
				"HELPER_LEVEL="+level)
			if res.code != 1 {
				t.Errorf("-log-level %q exited %d, want 1.\nstdout:\n%s\nstderr:\n%s",
					level, res.code, res.stdout, res.stderr)
			}
			for _, want := range []string{"-log-level", level, "debug", "error"} {
				if !strings.Contains(res.stderr, want) {
					t.Errorf("stderr does not contain %q.\nstderr:\n%s", want, res.stderr)
				}
			}
			if strings.Contains(res.stderr, "missing Longbridge credentials") {
				t.Errorf("a bad flag value must not be reported as a missing "+
					"credential; that would exit 2 and send the operator to the "+
					"wrong place.\nstderr:\n%s", res.stderr)
			}
		})
	}
}
