package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/oauth"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// Nothing in this file calls oauth.Build or oauth.AccessToken. Build blocks
// for up to five minutes waiting for a browser callback and would, in a test,
// mean a real network request and a real listener on the operator's port; every
// function exercised below is one that runs either side of it.

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written, so the report lines can be asserted on.
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

// clearClientIDEnv empties every variable resolveClientID reads, so a developer's
// shell cannot decide what these tests see. t.Setenv refuses a parallel test,
// which is why none of them calls t.Parallel.
func clearClientIDEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{clientIDEnv, "LONGBRIDGE_APP_KEY", "LONGPORT_APP_KEY"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("clearing %s: %v", k, err)
		}
	}
}

// ------------------------------------------------------------- client id ---

// The flag wins over every variable, including one that is set to the same
// value: a flag that loses to the environment is not a flag.
func TestResolveClientID_FlagBeatsEveryVariable(t *testing.T) {
	clearClientIDEnv(t)
	t.Setenv(clientIDEnv, "from-env")
	t.Setenv("LONGBRIDGE_APP_KEY", "from-app-key")

	got, err := resolveClientID("  from-flag  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "from-flag" {
		t.Errorf("ID = %q, want the flag value trimmed", got.ID)
	}
	if got.Env != "" {
		t.Errorf("Env = %q, want empty so the report says the flag was used", got.Env)
	}
	if got.Assumed {
		t.Error("a value given as -client-id is not an assumption about anything")
	}
}

func TestResolveClientID_EnvPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		clientID   string
		appKey     string
		portKey    string
		want       string
		wantEnv    string
		wantAssume bool
	}{
		{
			name:     "the documented variable is used and is not an assumption",
			clientID: "oauth-id",
			appKey:   "app-key",
			want:     "oauth-id", wantEnv: clientIDEnv,
		},
		{
			name:   "the app key is the last resort and is flagged as assumed",
			appKey: "app-key",
			want:   "app-key", wantEnv: "LONGBRIDGE_APP_KEY", wantAssume: true,
		},
		{
			name:    "the deprecated app-key spelling is the final fallback",
			portKey: "legacy-key",
			want:    "legacy-key", wantEnv: "LONGPORT_APP_KEY", wantAssume: true,
		},
		{
			name:   "the canonical app key beats the deprecated one",
			appKey: "canonical", portKey: "legacy",
			want: "canonical", wantEnv: "LONGBRIDGE_APP_KEY", wantAssume: true,
		},
		{
			name:     "whitespace is not a value, so the next source is tried",
			clientID: "   ", appKey: "app-key",
			want: "app-key", wantEnv: "LONGBRIDGE_APP_KEY", wantAssume: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearClientIDEnv(t)
			if tt.clientID != "" {
				t.Setenv(clientIDEnv, tt.clientID)
			}
			if tt.appKey != "" {
				t.Setenv("LONGBRIDGE_APP_KEY", tt.appKey)
			}
			if tt.portKey != "" {
				t.Setenv("LONGPORT_APP_KEY", tt.portKey)
			}

			got, err := resolveClientID("")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tt.want || got.Env != tt.wantEnv || got.Assumed != tt.wantAssume {
				t.Errorf("resolveClientID() = %+v, want id %q from %q assumed=%v",
					got, tt.want, tt.wantEnv, tt.wantAssume)
			}
		})
	}
}

// The error is the only thing a user with nothing configured ever sees, so it
// has to name every way out and admit which of them is a guess.
func TestResolveClientID_WithoutAnySourceTheErrorNamesEveryWayOut(t *testing.T) {
	clearClientIDEnv(t)

	_, err := resolveClientID("")
	if err == nil {
		t.Fatal("resolveClientID(\"\") = nil error with no client ID anywhere in the environment")
	}
	msg := err.Error()
	for _, want := range []string{
		"-client-id",
		clientIDEnv,
		"LONGBRIDGE_APP_KEY",
		"assumption",
		"User Center",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error must mention %q so the user has a next step:\n%s", want, msg)
		}
	}
	// It must not read as a missing-credential report: this is a usage
	// problem, and cli.Fail gives a *MissingCredentialError its own exit 2.
	// The remedy text on that type is about the app-key triple, which is not
	// what is missing here, so this stays a plain error.
	if _, ok := err.(*appcfg.MissingCredentialError); ok {
		t.Error("the error is a *MissingCredentialError, whose remedy text is about " +
			"the app-key triple and would exit 2 for a usage problem")
	}
}

func TestClientIDSourceString(t *testing.T) {
	tests := []struct {
		in        clientIDSource
		wantParts []string
	}{
		{clientIDSource{ID: "x"}, []string{"-client-id"}},
		{clientIDSource{ID: "x", Env: clientIDEnv}, []string{clientIDEnv}},
		{clientIDSource{ID: "x", Env: "LONGBRIDGE_APP_KEY", Assumed: true},
			[]string{"LONGBRIDGE_APP_KEY", "ASSUMED"}},
	}
	for _, tt := range tests {
		got := tt.in.String()
		for _, want := range tt.wantParts {
			if !strings.Contains(got, want) {
				t.Errorf("String() = %q, want it to contain %q", got, want)
			}
		}
	}
	// The assumed form must not be indistinguishable from the documented one:
	// a reader who did not spot the difference would file a bug report about
	// the wrong credential.
	a := clientIDSource{ID: "x", Env: "LONGBRIDGE_APP_KEY", Assumed: true}.String()
	b := clientIDSource{ID: "x", Env: "LONGBRIDGE_APP_KEY"}.String()
	if a == b {
		t.Errorf("the assumed and documented renderings are identical (%q); the "+
			"fallback would be indistinguishable from a documented source", a)
	}
}

// ------------------------------------------------------------------ flags ---

func TestValidateFlags_Port(t *testing.T) {
	tests := []struct {
		in      int
		wantErr bool
	}{
		{defaultCallbackPort, false},
		{1, false},
		{65535, false},
		{0, true},
		{-1, true},
		{65536, true},
	}
	for _, tt := range tests {
		port = tt.in
		err := validateFlags()
		if (err != nil) != tt.wantErr {
			t.Errorf("validateFlags() with -port %d = %v, want error: %v", tt.in, err, tt.wantErr)
		}
		if tt.wantErr && !strings.Contains(err.Error(), "-port") {
			t.Errorf("the error for -port %d must name the flag: %v", tt.in, err)
		}
	}
	port = defaultCallbackPort
}

func TestValidateFlags_Env(t *testing.T) {
	// Only "staging" means anything to the SDK; anything else is a typo that
	// would otherwise be silently ignored and produce a production login on a
	// user who believed they were testing.
	for _, in := range []string{"", "staging", "STAGING", "  Staging  "} {
		envName = in
		if err := validateFlags(); err != nil {
			t.Errorf("validateFlags() with -env %q = %v, want nil", in, err)
		}
	}
	for _, in := range []string{"prod", "live", "test", "staging.x", "true"} {
		envName = in
		err := validateFlags()
		if err == nil {
			t.Errorf("validateFlags() with -env %q = nil, want an error", in)
			continue
		}
		if !strings.Contains(err.Error(), "-env") {
			t.Errorf("the error for -env %q must name the flag: %v", in, err)
		}
	}
	envName = ""
}

// oauth.New reads LONGBRIDGE_ENV once, at construction, and keeps the base URL
// it chose. Setting it afterwards cannot move an already-built client, which is
// why this has to run first in main(); the test is here so that a future
// reordering has something to fail.
func TestApplyEnvOverride_StagingIsSetInTheProcessEnv(t *testing.T) {
	for _, in := range []string{"staging", "STAGING", " Staging "} {
		t.Setenv("LONGBRIDGE_ENV", "")
		if err := os.Unsetenv("LONGBRIDGE_ENV"); err != nil {
			t.Fatalf("clearing LONGBRIDGE_ENV: %v", err)
		}
		applyEnvOverride(in)
		if got := os.Getenv("LONGBRIDGE_ENV"); got != "staging" {
			t.Errorf("applyEnvOverride(%q) left LONGBRIDGE_ENV = %q, want \"staging\"", in, got)
		}
	}
}

func TestApplyEnvOverride_LeavesTheVariableAloneOtherwise(t *testing.T) {
	// -env unset must not clear a staging the user exported, and must not set
	// a value the SDK would not recognise.
	for _, pre := range []string{"", "staging"} {
		t.Run("pre-set to "+pre, func(t *testing.T) {
			if pre == "" {
				t.Setenv("LONGBRIDGE_ENV", "")
				if err := os.Unsetenv("LONGBRIDGE_ENV"); err != nil {
					t.Fatalf("clearing LONGBRIDGE_ENV: %v", err)
				}
			} else {
				t.Setenv("LONGBRIDGE_ENV", pre)
			}
			applyEnvOverride("")
			if got := os.Getenv("LONGBRIDGE_ENV"); got != pre {
				t.Errorf("applyEnvOverride(\"\") changed LONGBRIDGE_ENV to %q, want %q", got, pre)
			}
		})
	}
}

func TestDescribeEnv_ReportsTheRuleTheSDKFollows(t *testing.T) {
	t.Setenv("LONGBRIDGE_ENV", "staging")
	if got := describeEnv(); !strings.Contains(got, "staging") {
		t.Errorf("describeEnv() = %q, want it to name staging", got)
	}
	for _, in := range []string{"", "STAGING", "production"} {
		t.Setenv("LONGBRIDGE_ENV", in)
		got := describeEnv()
		if !strings.Contains(got, "production") {
			t.Errorf("describeEnv() with LONGBRIDGE_ENV=%q = %q, want the production host", in, got)
		}
	}
}

// ---------------------------------------------------------------- browser ---

func TestBrowserLaunchers_PerPlatform(t *testing.T) {
	tests := []struct {
		goos   string
		name   string // the launcher that is tried first
		hasArg bool   // whether it needs an argument before the URL
	}{
		{"linux", "xdg-open", false},
		{"freebsd", "xdg-open", false},
		{"darwin", "open", false},
		{"windows", "rundll32", true},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := browserLaunchers(tt.goos)
			if len(got) == 0 {
				t.Fatalf("browserLaunchers(%q) is empty; the user would be told "+
					"no browser could be launched, which is the one outcome that "+
					"must not happen by default", tt.goos)
			}
			if got[0][0] != tt.name {
				t.Errorf("browserLaunchers(%q)[0] = %q, want %q", tt.goos, got[0][0], tt.name)
			}
			// Exactly one %s in the whole vector, and it is the last element:
			// argv[0] is the launcher name looked up on PATH, so a template in
			// it would be looked up verbatim and never found.
			n := 0
			for _, arg := range got[0] {
				n += strings.Count(arg, "%s")
			}
			if n != 1 {
				t.Errorf("browserLaunchers(%q)[0] = %q, want exactly one %%s", tt.goos, got[0])
			}
			if last := got[0][len(got[0])-1]; last != "%s" {
				t.Errorf("browserLaunchers(%q)[0] = %q, want the URL last", tt.goos, got[0])
			}
			if tt.hasArg && len(got[0]) != 3 {
				t.Errorf("browserLaunchers(%q)[0] = %q, want the FileProtocolHandler "+
					"argument between the launcher and the URL", tt.goos, got[0])
			}
		})
	}
}

// fakeLauncher puts a script called name at the front of PATH that appends its
// arguments to a file, so a real launch can be observed without a browser, a
// display or a network.
func fakeLauncher(t *testing.T, name, record string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake launcher is a POSIX shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + record + "; done\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatalf("writing the fake launcher: %v", err)
	}
	t.Setenv("PATH", dir)
}

// A launcher that starts must actually receive the URL. The failure this
// catches is a vector built for Windows being run on Linux, where the URL would
// be consumed as an argument to something else — or not passed at all.
func TestTryOpenBrowser_PassesTheURLToALauncherItFinds(t *testing.T) {
	record := filepath.Join(t.TempDir(), "argv")
	fakeLauncher(t, "xdg-open", record)

	const url = "https://openapi.longbridge.com/oauth2/authorize?client_id=abc&state=xyz"
	if err := tryOpenBrowser(url); err != nil {
		t.Fatalf("tryOpenBrowser() = %v, want nil with xdg-open on PATH", err)
	}

	// The script is a child process reaped in a goroutine, so poll briefly
	// rather than assume it has run by the time Start returned.
	var got string
	for i := 0; i < 100; i++ {
		b, err := os.ReadFile(record)
		if err == nil && len(b) > 0 {
			got = string(b)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(got, url) {
		t.Errorf("the launcher did not receive the URL; its arguments were %q", got)
	}
}

// A missing browser must come back as an error the caller can print, naming
// every launcher that was tried — and must not be fatal, which is the caller's
// contract rather than this function's (see urlSink.show).
func TestTryOpenBrowser_WithoutALauncherItNamesEveryOneItTried(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := tryOpenBrowser("https://example.invalid/")
	if err == nil {
		t.Fatal("tryOpenBrowser() = nil with an empty PATH, so a headless run would " +
			"claim to have opened a browser")
	}
	msg := err.Error()
	for _, name := range browserLaunchers(runtime.GOOS) {
		if !strings.Contains(msg, name[0]) {
			t.Errorf("the error must name %q so the user can install it or work "+
				"around it: %s", name[0], msg)
		}
	}
}

// The URL is printed whatever the browser does, because a launcher that starts
// and opens nothing is the normal case on a headless Linux box. Each of the
// three paths has to print it.
func TestURLSink_AlwaysPrintsTheURLAndNeverFails(t *testing.T) {
	const url = "https://openapi.longbridge.com/oauth2/authorize?client_id=abc&state=xyz"

	t.Run("no-browser waits and says how to reach the host", func(t *testing.T) {
		noBrowser, port = true, defaultCallbackPort
		sink := &urlSink{}
		out := captureStdout(t, func() { sink.show(url) })

		if !sink.called {
			t.Error("show() must record that the SDK asked for a URL; that is the " +
				"only exact signal the report has about which Build path ran")
		}
		if !strings.Contains(out, url) {
			t.Errorf("the URL must be printed with -no-browser:\n%s", out)
		}
		if !strings.Contains(out, "ssh -L") {
			t.Errorf("the headless path must say how to reach the callback host:\n%s", out)
		}
	})

	t.Run("no launcher found is a warning, not a failure", func(t *testing.T) {
		noBrowser, port = false, defaultCallbackPort
		t.Setenv("PATH", t.TempDir())
		sink := &urlSink{}
		out := captureStdout(t, func() { sink.show(url) })

		if !strings.Contains(out, url) {
			t.Errorf("the URL must be printed before the browser is attempted:\n%s", out)
		}
		if !strings.Contains(out, "by hand") {
			t.Errorf("a missing browser must tell the user to open the URL "+
				"themselves rather than implying the flow is over:\n%s", out)
		}
	})

	t.Run("a launcher that starts is reported as launched", func(t *testing.T) {
		noBrowser, port = false, defaultCallbackPort
		fakeLauncher(t, "xdg-open", filepath.Join(t.TempDir(), "argv"))
		sink := &urlSink{}
		out := captureStdout(t, func() { sink.show(url) })

		if !strings.Contains(out, "A browser was launched") {
			t.Errorf("a successful launch should say so:\n%s", out)
		}
	})
	noBrowser, port = false, defaultCallbackPort
}

// ------------------------------------------------------------ token cache ---

// A token file as the SDK writes it: three JSON fields, mode 0600, named after
// the client ID. The shape is from openapi-go v0.25.2 oauth/oauth.go's
// oauthToken struct; only expires_at is read back.
const (
	testAccessToken  = "ACCESS-TOKEN-VALUE-MUST-NEVER-BE-PRINTED"
	testRefreshToken = "REFRESH-TOKEN-VALUE-MUST-NEVER-BE-PRINTED"
)

func writeTokenFile(t *testing.T, access, refresh string, expiresAt int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client")
	body := `{"access_token": "` + access + `", "refresh_token": "` + refresh +
		`", "expires_at": ` + strconv.FormatInt(expiresAt, 10) + `}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the token file: %v", err)
	}
	return path
}

func TestTokenPath_IsTheSDKCacheLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this one on Windows

	got, err := tokenPath("client-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, ".longbridge", "openapi", "tokens", "client-id")
	if got != want {
		t.Errorf("tokenPath() = %q, want %q\n"+
			"the SDK builds this from tokenDir = \".longbridge/openapi/tokens\"; "+
			"if the two disagree the report says the cache file is missing", got, want)
	}
}

// A home directory the SDK cannot resolve is fatal here, because the SDK's own
// Build calls the same lookup and would fail with a bare "cannot get home dir".
// The command's version says which path was being reconstructed.
func TestTokenPath_NoHomeDirectoryIsAnErrorNotAPath(t *testing.T) {
	t.Setenv("HOME", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", "")
	}

	got, err := tokenPath("client-id")
	if err == nil {
		t.Fatalf("tokenPath() = %q with no home directory, want an error", got)
	}
	if got != "" {
		t.Errorf("tokenPath() returned %q alongside an error; a half-built path "+
			"would be stat'ed and reported as a missing token", got)
	}
	if !strings.Contains(err.Error(), "home") {
		t.Errorf("error = %q, want it to say the home directory is the problem", err)
	}
}

// The file name IS the client ID, so printing the path in full would print a
// credential on the line that exists to reassure the reader about credentials.
func TestMaskTokenPath_MasksTheClientIDAndKeepsTheDirectory(t *testing.T) {
	const clientID = "client-id-abcdefghij"
	path := filepath.Join("/home", "somebody", ".longbridge", "openapi", "tokens", clientID)

	got := maskTokenPath(path)
	if strings.Contains(got, clientID) {
		t.Errorf("maskTokenPath() = %q, which contains the client ID in clear", got)
	}
	if !strings.HasPrefix(got, filepath.Join("/home", "somebody", ".longbridge", "openapi", "tokens")+string(os.PathSeparator)) {
		t.Errorf("maskTokenPath() = %q, want the directory preserved exactly: the "+
			"user needs it in order to find or delete the file", got)
	}
	if !strings.Contains(got, appcfg.Redact(clientID)) {
		t.Errorf("maskTokenPath() = %q, want the name masked the way every other "+
			"credential in this repo is masked", got)
	}
}

func TestSnapshotToken_UnchangedByComparesContentAndMetadata(t *testing.T) {
	path := writeTokenFile(t, "a", "r", 1000)
	first := snapshotToken(path)
	if !first.read || first.stat == nil {
		t.Fatalf("snapshotToken() did not read the file it just wrote: %+v", first)
	}

	t.Run("an identical file is unchanged", func(t *testing.T) {
		if !first.unchangedBy(snapshotToken(path)) {
			t.Error("two snapshots of the same file must compare unchanged, or " +
				"every reuse would be reported as a refresh")
		}
	})

	t.Run("a rewritten token of the same length is not unchanged", func(t *testing.T) {
		// Same length and, on a coarse filesystem, possibly the same
		// modification time. Only the bytes give it away — which is why they
		// are compared as well as the size.
		// A different file on purpose: unchangedBy compares size, modification
		// time and bytes, and never the path, because the SDK always writes the
		// same one and a second path means a different file entirely.
		other := writeTokenFile(t, "b", "r", 2000)
		if first.unchangedBy(snapshotToken(other)) {
			t.Error("two snapshots of different files compared unchanged; the " +
				"origin line would report a refresh that never happened")
		}
	})

	t.Run("an absent file is not unchanged", func(t *testing.T) {
		if first.unchangedBy(snapshotToken(filepath.Join(t.TempDir(), "absent"))) {
			t.Error("a missing file must never compare unchanged; that is the " +
				"difference between \"reused\" and \"written for the first time\"")
		}
	})
}

// The origin line is the part of the report that is an inference, so each of
// Build's paths has to be distinguishable and none may be overclaimed.
func TestPrintOrigin_EveryBuildPathIsReportedDistinctly(t *testing.T) {
	path := writeTokenFile(t, "access", "refresh", 1000)
	before := snapshotToken(path)

	t.Run("a browser flow is a fact, not an inference", func(t *testing.T) {
		out := captureStdout(t, func() { printOrigin(&urlSink{called: true}, before, before) })
		if !strings.Contains(out, "authorized just now") {
			t.Errorf("the browser path must be reported as the full flow:\n%s", out)
		}
		if strings.Contains(out, "inferred") {
			t.Errorf("nothing is inferred when the SDK opened the browser, so "+
				"the caveat should not be printed:\n%s", out)
		}
	})

	t.Run("an untouched file is a reuse", func(t *testing.T) {
		out := captureStdout(t, func() { printOrigin(&urlSink{}, before, before) })
		if !strings.Contains(out, "reused") {
			t.Errorf("want the reuse wording:\n%s", out)
		}
		if !strings.Contains(out, "inferred") {
			t.Errorf("the reuse answer is inferred from the file, so the "+
				"caveat has to say so:\n%s", out)
		}
	})

	t.Run("a changed file is a silent refresh", func(t *testing.T) {
		other := writeTokenFile(t, "access2", "refresh", 2000)
		out := captureStdout(t, func() { printOrigin(&urlSink{}, before, snapshotToken(other)) })
		if !strings.Contains(out, "refreshed") {
			t.Errorf("want the refresh wording:\n%s", out)
		}
	})

	t.Run("a file that did not exist is new", func(t *testing.T) {
		missing := snapshotToken(filepath.Join(t.TempDir(), "absent"))
		out := captureStdout(t, func() { printOrigin(&urlSink{}, missing, before) })
		if !strings.Contains(out, "newly written") {
			t.Errorf("want the first-write wording:\n%s", out)
		}
	})

	t.Run("an unreadable file says so instead of guessing", func(t *testing.T) {
		missing := snapshotToken(filepath.Join(t.TempDir(), "absent"))
		out := captureStdout(t, func() { printOrigin(&urlSink{}, missing, missing) })
		if !strings.Contains(out, "unknown") {
			t.Errorf("with nothing readable to compare, the report must say "+
				"unknown rather than pick a branch:\n%s", out)
		}
	})
}

func TestPrintExpiry_ReadsTheOneFieldTheSDKWrote(t *testing.T) {
	expires := time.Now().Add(90 * time.Minute).Unix()
	path := writeTokenFile(t, testAccessToken, testRefreshToken, expires)

	out := captureStdout(t, func() { printExpiry(snapshotToken(path)) })
	if !strings.Contains(out, cli.FmtTime(expires)) {
		t.Errorf("printExpiry() must render the expiry as a local time:\n%s", out)
	}
	if !strings.Contains(out, "in 1h") {
		t.Errorf("printExpiry() should also say how long is left:\n%s", out)
	}
}

// Every way the expiry can be unavailable has to produce "unknown" rather than
// a wrong time or a crash: the file is written by another program, and this
// command reads it on a best-effort basis.
func TestPrintExpiry_UnavailableIsSaidRatherThanGuessed(t *testing.T) {
	tests := []struct {
		name string
		body string
		path string
	}{
		{name: "no file at all", path: filepath.Join(t.TempDir(), "absent")},
		{name: "not JSON", body: "this is not json"},
		{name: "no expires_at field", body: `{"access_token": "x"}`},
		{name: "expires_at is zero", body: `{"access_token": "x", "expires_at": 0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if tt.body != "" {
				path = filepath.Join(t.TempDir(), "token")
				if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
					t.Fatalf("writing the token file: %v", err)
				}
			}
			out := captureStdout(t, func() { printExpiry(snapshotToken(path)) })
			if !strings.Contains(out, "unknown") {
				t.Errorf("want an explicit \"unknown\", never a made-up time:\n%s", out)
			}
		})
	}
}

func TestPrintCacheFile_ReportsThePathAndItsPermissions(t *testing.T) {
	path := writeTokenFile(t, "a", "r", 1000)
	snap := snapshotToken(path)

	out := captureStdout(t, func() { printCacheFile(path, snap) })
	if !strings.Contains(out, filepath.Dir(path)) {
		t.Errorf("the directory must be printed exactly:\n%s", out)
	}
	if !strings.Contains(out, "600") && !strings.Contains(out, "666") {
		t.Errorf("the file's mode must be reported, since the whole point is "+
			"that the token is not world-readable (expected '0600' on Unix, '0666' on Windows):\n%s", out)
	}
	if !strings.Contains(out, "SDK") {
		t.Errorf("the line must say the SDK wrote the file, not this command:\n%s", out)
	}
}

// The perms are the reassurance, so an unreadable file must say so rather than
// print an empty one: "cache file: 0600" with nothing after it would be a
// claim about a file that was never read.
func TestPrintCacheFile_UnreadableSaysSoRatherThanClaimingAMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	out := captureStdout(t, func() { printCacheFile(path, snapshotToken(path)) })

	if !strings.Contains(out, "not readable") {
		t.Errorf("want an explicit \"not readable\" for a file that is not there:\n%s", out)
	}
	if strings.Contains(out, "0600") {
		t.Errorf("a mode was reported for a file that does not exist:\n%s", out)
	}
}

// The report's central promise: no token, not even a prefix, and no client ID
// in clear. Every report line is produced with the same three calls, so driving
// all of them with a realistic cache file covers the whole surface.
func TestTheReportNeverPrintsATokenOrTheClientID(t *testing.T) {
	const clientID = "client-id-abcdefghijklmno"
	path := writeTokenFile(t, testAccessToken, testRefreshToken,
		time.Now().Add(time.Hour).Unix())
	before := snapshotToken(path)
	sink := &urlSink{}
	id, err := resolveClientID(clientID)
	if err != nil {
		t.Fatalf("resolving the client ID: %v", err)
	}

	out := captureStdout(t, func() {
		fmt.Printf("   client id     %s  (%s)\n", appcfg.Redact(id.ID), id)
		printExpiry(snapshotToken(path))
		printOrigin(sink, before, snapshotToken(path))
		printCacheFile(path, snapshotToken(path))
	})

	for _, secret := range []string{testAccessToken, testRefreshToken, clientID} {
		if strings.Contains(out, secret) {
			t.Errorf("the report leaked %q:\n%s", secret, out)
		}
	}
	// A prefix would be the subtler leak, and is worth its own assertion.
	if strings.Contains(out, testAccessToken[:8]) {
		t.Errorf("the report leaked the first 8 characters of the token:\n%s", out)
	}
	if !strings.Contains(out, appcfg.Redact(clientID)) {
		t.Errorf("the report should show the client ID masked:\n%s", out)
	}
}

// ----------------------------------------------------------------- usage ---

// -no-browser is the flag a user over SSH needs, and the standard envelope
// knows nothing about authorization, so the notes have to be the thing that
// makes it findable.
func TestUsage_ExplainsTheHeadlessPath(t *testing.T) {
	u := cli.NewUsage("auth", "OAuth 2.0 browser login")
	addOAuthUsageNotes(u)
	var buf bytes.Buffer
	u.FS.SetOutput(&buf)
	u.FS.Usage()
	out := buf.String()

	for _, want := range []string{
		"-no-browser", // named as the headless path, not just in the flag list
		"ssh -L",      // how the callback reaches the host when it is not local
		"5 minutes",   // how long it waits, so a hung browser is diagnosable
		"0600",        // what the SDK does with the token file
		"HEADLESS",    // the section a user scans for
		"masked",      // the promise about the client ID
		"Exit codes:", // the shared envelope is still there
		"Usage:",      // ...and still comes first
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the usage text must contain %q:\n%s", want, out)
		}
	}
	// The envelope comes before the notes, so the notes read as an addition
	// rather than as a replacement of the shared contract.
	if strings.Index(out, "Usage:") > strings.Index(out, "How this works:") {
		t.Errorf("the OAuth notes must follow the standard envelope:\n%s", out)
	}
}

// The notes name a port. Which one must be the port this run will use, or a
// user who passed -port is told to forward the wrong one over SSH.
func TestUsage_NotesQuoteThePortThisRunWillUse(t *testing.T) {
	port = 0
	u := cli.NewUsage("auth", "OAuth 2.0 browser login")
	addOAuthUsageNotes(u)
	var buf bytes.Buffer
	u.FS.SetOutput(&buf)
	u.FS.Usage()
	if !strings.Contains(buf.String(), "ssh -L 60355:localhost:60355") {
		t.Errorf("with no -port parsed, the notes must quote the default:\n%s", buf.String())
	}

	port = 7000
	buf.Reset()
	u.FS.Usage()
	out := buf.String()
	if !strings.Contains(out, "ssh -L 7000:localhost:7000") {
		t.Errorf("with -port 7000, the notes must quote 7000:\n%s", out)
	}
	if strings.Contains(out, "60355") {
		t.Errorf("the default port is still quoted after -port 7000:\n%s", out)
	}
	port = defaultCallbackPort
}

// oauth.New is exercised here without Build: it is the one call that has a
// side effect (reading LONGBRIDGE_ENV) and no I/O, and its result is what
// every later stage is built from.
func TestOAuthNewAndClientID_RoundTripWithoutARequest(t *testing.T) {
	t.Setenv("LONGBRIDGE_ENV", "")
	if err := os.Unsetenv("LONGBRIDGE_ENV"); err != nil {
		t.Fatalf("clearing LONGBRIDGE_ENV: %v", err)
	}
	o := oauth.New("client-id-abcdefghijklmno").WithCallbackPort(12345)
	if got := o.ClientID(); got != "client-id-abcdefghijklmno" {
		t.Errorf("ClientID() = %q, want the value passed to New", got)
	}
	// AccessToken must refuse before Build rather than reaching for the
	// network: that is the failure this command would hit if the Build step
	// were ever skipped, and it is safe to observe because the SDK checks
	// its own state first.
	if _, err := o.AccessToken(context.Background()); err == nil {
		t.Error("AccessToken() = nil error before Build; the SDK documents that " +
			"it errors, and this command reports that error verbatim")
	}
}

// ------------------------------------------------------- the whole report --

// A run that finds a valid cached token performs no request of any kind: Build
// reads the file, sees a token that is neither expired nor close to expiry, and
// returns; AccessToken finds the same token and returns it. That makes it the
// one path through this command that can be driven end to end offline — and it
// is the path a real user hits on every run after the first, so the report is
// worth having tested against the real SDK rather than against a hand-built
// stand-in for one.
//
// Nothing else is exercised here, deliberately. The browser flow and the
// silent refresh both need a token endpoint to talk to, and a test that stands
// one up would be a network listener in a suite that has none. Those two paths
// are the SDK's to get right; what is this command's to get right is what it
// reports, and that is what the calls below cover.
func TestAuthorize_ReusesACachedTokenAndReportsItHonestly(t *testing.T) {
	const clientID = "client-id-abcdefghijklmno"
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LONGBRIDGE_ENV", "")
	if err := os.Unsetenv("LONGBRIDGE_ENV"); err != nil {
		t.Fatalf("clearing LONGBRIDGE_ENV: %v", err)
	}
	port = defaultCallbackPort

	// Seeded exactly as the SDK seeds it: three fields, mode 0600, named after
	// the client ID, two hours of validity so neither Build nor AccessToken
	// considers a refresh due.
	expires := time.Now().Add(2 * time.Hour).Unix()
	dir := filepath.Join(home, ".longbridge", "openapi", "tokens")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating the token cache: %v", err)
	}
	seed := `{"access_token": "` + testAccessToken + `", "refresh_token": "` + testRefreshToken +
		`", "expires_at": ` + strconv.FormatInt(expires, 10) + `}`
	if err := os.WriteFile(filepath.Join(dir, clientID), []byte(seed), 0o600); err != nil {
		t.Fatalf("seeding the token cache: %v", err)
	}

	id, err := resolveClientID(clientID)
	if err != nil {
		t.Fatalf("resolving the client ID: %v", err)
	}
	// The base URL points at a port nothing is listening on, so any request
	// this run attempted would fail rather than pass quietly.
	o := oauth.NewWithBaseURL(clientID, "http://127.0.0.1:1")
	sink := &urlSink{}

	out := captureStdout(t, func() {
		if err := authorize(t.Context(), o, sink, id); err != nil {
			t.Errorf("authorize() = %v, want nil for a cached token that needs no refresh", err)
		}
	})

	if sink.called {
		t.Error("the SDK asked to open a browser for a valid cached token, so the " +
			"reuse path was not taken")
	}
	for _, want := range []string{
		"present, never printed",  // the token exists and is not shown
		"reused the cached token", // the origin line, from the real SDK behaviour
		"inferred",                // ...and labelled as an inference
		cli.FmtTime(expires),      // the expiry, read out of the cache file
		"production",              // which host oauth.New would have chosen
		"client id     " + appcfg.Redact(clientID),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "file 0600") && !strings.Contains(out, "file 0666") {
		t.Errorf("the report is missing the file permissions line (expected 'file 0600' on Unix, 'file 0666' on Windows):\n%s", out)
	}
	for _, secret := range []string{testAccessToken, testRefreshToken, clientID} {
		if strings.Contains(out, secret) {
			t.Errorf("the report leaked %q:\n%s", secret, out)
		}
	}
}
