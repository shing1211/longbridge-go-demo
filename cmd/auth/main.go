// Command auth runs the Longbridge OAuth 2.0 browser flow: it binds a local
// callback port, prints (and where possible opens) the authorization URL,
// waits for the browser to redirect back, exchanges the code, and reports what
// it obtained — the client ID, whether the token was reused from the cache or
// freshly authorized, when it expires, and where the SDK filed it.
//
// # READ-ONLY, WITH ONE EXCEPTION STATED PLAINLY
//
// This command issues no API request at all. It authenticates; it does not
// mutate an account, place an order, edit a watchlist or publish anything. The
// startup banner therefore asserts, on the same grounds as the other read-only
// binaries, that the order gate is still closed.
//
// The exception is a local file. The SDK writes the token it obtained to
// $HOME/.longbridge/openapi/tokens/<client id> with mode 0600 (VERIFIED in
// openapi-go v0.25.2 oauth/oauth.go: saveTokenToPath does MkdirAll 0700 and
// WriteFile 0600). That file is outside this repository, is created by the SDK
// rather than by this command, and is not an API write — but it is a write,
// and calling this binary read-only without saying so would be false. It is
// the only side effect of a successful run. Deleting it does not revoke
// anything: the next run simply authorizes again, and the token is only
// withdrawn at Longbridge.
//
// # WHAT IT DOES NOT DO
//
// It does not switch the rest of this repo onto OAuth. Every other command
// loads the app-key triple through internal/config.Load, and none of them
// hands the SDK a Config that has an OAuthClient set, so an OAuth token
// obtained here is not used by cmd/quote, cmd/trade or any of the other
// fifteen. That is a deliberate boundary, not an oversight: re-plumbing every
// binary onto OAuth would change how all of them authenticate, which is a
// larger question than "demonstrate the flow".
//
// # NOT OBSERVED LIVE
//
// This command has never been run against a working Longbridge account in this
// project. Everything below is written from the SDK's source and from the
// flags, and no claim is made about what a real authorization session looks
// like.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/longbridge/openapi-go/oauth"

	"github.com/shing1211/longbridge-go-demo/internal/cli"
	appcfg "github.com/shing1211/longbridge-go-demo/internal/config"
)

// defaultCallbackPort mirrors the SDK's own default. The constant is
// unexported there, so it is restated here with the flag default agreeing; the
// port is passed explicitly either way, so the SDK's copy cannot silently
// disagree with -h.
const defaultCallbackPort = 60355

// tokenDir is the SDK's token cache directory, relative to the home
// directory: `tokenDir = ".longbridge/openapi/tokens"` in openapi-go v0.25.2
// oauth/oauth.go. The SDK keeps it private and exposes no accessor, so the
// report reconstructs the path in order to describe where the token landed.
// If the two ever disagree the report says the cache file is missing, because
// the file is stat'ed before anything is claimed about it — a wrong path
// cannot produce a wrong claim about a file that was not read.
const tokenDir = ".longbridge/openapi/tokens"

// clientIDEnv is the variable this command reads the OAuth client ID from.
const clientIDEnv = "LONGBRIDGE_CLIENT_ID"

var (
	clientID  string
	port      int
	noBrowser bool
	envName   string
)

func main() {
	u := cli.NewUsage("auth", "OAuth 2.0 browser login: obtain a token and report where it is cached")
	// Added before Parse, so that -h and a usage error both carry it: the
	// headless path is the one a user hits over SSH, where nobody can afford
	// to read past the flag list to discover it.
	addOAuthUsageNotes(u)
	u.FS.StringVar(&clientID, "client-id", "",
		"OAuth client ID (default $"+clientIDEnv+", else $LONGBRIDGE_APP_KEY)")
	u.FS.IntVar(&port, "port", defaultCallbackPort,
		"local port for the OAuth callback, 1-65535")
	u.FS.BoolVar(&noBrowser, "no-browser", false,
		"print the URL and wait instead of launching a browser (headless, SSH)")
	u.FS.StringVar(&envName, "env", "",
		"\"staging\" to authorize against *.longbridge.xyz instead of the production host")
	u.Parse(os.Args[1:])

	if err := validateFlags(); err != nil {
		cli.Fail(err)
	}
	id, err := resolveClientID(clientID)
	if err != nil {
		cli.Fail(err)
	}
	// ORDERING, AND IT IS LOAD-BEARING: oauth.New reads LONGBRIDGE_ENV right
	// here, once, to choose between https://openapi.longbridge.com and
	// https://openapi.longbridge.xyz. Setting the variable after New has no
	// effect on the client that was already built, so the flag is applied
	// before that call and not inside it.
	applyEnvOverride(envName)

	sink := &urlSink{}
	o := oauth.New(id.ID).WithCallbackPort(port)
	// OnOpenURL is registered here rather than after Build: Build is what
	// triggers the flow, so a nil hook means the URL is never surfaced at all
	// and the command would sit for five minutes looking hung.
	o.OnOpenURL(sink.show)

	// The config is loaded before Build on purpose, so the order-gate
	// assertion runs before the user is sent to a browser and a
	// misconfigured environment costs nothing. The SDK's own doc comment says
	// to call Build before config.New, and that ordering only matters for a
	// request made afterwards: the SDK config needs the OAuth *client*, and
	// the token is fetched lazily by the HTTP client — which this command
	// never uses, because it issues no requests.
	cfg := u.LoadOAuth(o)
	fmt.Fprintf(os.Stderr, "[config] %s\n", cfg)
	// SAFETY: read-only binary. It authenticates and makes no API call, so
	// the order gate must still refuse it; an open gate here is a
	// misconfigured environment, not permission to start writing. The one
	// write it does perform is the SDK's own token cache under $HOME, which
	// is not an API write and not a file in this repository.
	cli.AssertReadOnly(cfg, "auth", "run the OAuth login")

	cli.Run(func(ctx context.Context) error {
		return authorize(ctx, o, sink, id)
	})
}

// authorize runs the flow and prints an account of what came back.
//
// The three things reported afterwards are kept apart on purpose. "A token
// exists" is proved by the SDK (AccessToken). "Where it is, and until when" is
// read from the cache file, which is the only place the SDK records an expiry.
// "Whether it was reused or freshly obtained" is neither: the SDK does not say,
// so it is inferred from the one signal it does give — whether it asked us to
// open a browser — corroborated by comparing the cache file across the call.
func authorize(ctx context.Context, o *oauth.OAuth, sink *urlSink, id clientIDSource) error {
	cli.Section("OAuth 2.0")
	// The fixed facts come first, before anything is attempted, so that a run
	// which then fails says which client ID and which host it was aiming at.
	fmt.Printf("   client id     %s  (%s)\n", appcfg.Redact(id.ID), id)
	fmt.Printf("   environment   %s\n", describeEnv())
	fmt.Printf("   callback      http://localhost:%d/callback\n", port)

	path, err := tokenPath(id.ID)
	if err != nil {
		return err
	}
	// Read before Build so the report can say whether Build touched the file.
	// The bytes are compared, never printed: a fresh token is a credential,
	// and comparing content is immune to the coarse modification-time
	// granularity that would let a same-second rewrite look untouched.
	before := snapshotToken(path)
	if err := o.Build(ctx); err != nil {
		return fmt.Errorf("authorizing: %w", err)
	}

	// AccessToken is the proof of life, not decoration: it is what the SDK's
	// HTTP client calls before every request, it refreshes a token that is
	// expired or expiring within the hour, and it errors if Build was never
	// called. A successful Build followed by a successful AccessToken is the
	// strongest claim available without making a request, and this command
	// makes none. It takes the same context, so Ctrl-C during a refresh aborts
	// it rather than hanging the terminal.
	token, err := o.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("obtaining a usable access token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("the SDK returned an empty access token; nothing was cached")
	}
	// token is a live credential. It is not printed, not logged, not
	// abbreviated into a prefix, and its length is not reported either: a
	// fixed-width mask is the only form in which this repo prints anything
	// sensitive (internal/config.Redact). The emptiness check above is the
	// only use made of the string.

	after := snapshotToken(path)

	cli.Section("Result")
	fmt.Printf("   token         present, never printed (AccessToken returned one)\n")
	printExpiry(after)
	printOrigin(sink, before, after)
	printCacheFile(path, after)
	return nil
}

// cacheSnapshot is the state of the token cache file at one moment: whether it
// was there, what it contained, and what the filesystem said about it.
type cacheSnapshot struct {
	read  bool
	bytes []byte
	stat  os.FileInfo
	err   error
}

func snapshotToken(path string) cacheSnapshot {
	s := cacheSnapshot{}
	s.bytes, s.err = os.ReadFile(path)
	s.read = s.err == nil
	if s.read {
		s.stat, _ = os.Stat(path)
	}
	return s
}

// unchangedBy reports whether other is the same file, untouched, as this
// snapshot saw it: same length, same modification time, same bytes.
//
// The byte comparison is what makes the answer trustworthy. A rewrite inside
// the same clock tick leaves the modification time looking unchanged on a
// filesystem with coarse timestamps, and a length comparison alone cannot tell
// a rewritten token from an old one; neither can a timestamp alone. The SDK's
// only two non-interactive paths either leave the file alone (reuse) or
// replace it (refresh), so an exactly-equal file means it was left alone.
func (s cacheSnapshot) unchangedBy(other cacheSnapshot) bool {
	return s.read && other.read && s.stat != nil && other.stat != nil &&
		s.stat.Size() == other.stat.Size() &&
		s.stat.ModTime().Equal(other.stat.ModTime()) &&
		string(s.bytes) == string(other.bytes)
}

// printOrigin reports which of Build's three paths it took.
//
// Build has three: reuse a cached token, silently refresh an expired one, or
// run the browser flow. VERIFIED in openapi-go v0.25.2 oauth/oauth.go: only
// the third calls openURL, and only the first and second can leave the cache
// file untouched. So the two questions below answer it, and the output says
// which is which rather than asserting a fact the SDK never reported.
func printOrigin(sink *urlSink, before, after cacheSnapshot) {
	switch {
	case sink.called:
		fmt.Printf("   origin        authorized just now in the browser; the SDK had no " +
			"usable cached token, so it ran the full flow\n")
		return
	case !before.read && !after.read:
		fmt.Printf("   origin        unknown: the token cache file was not readable "+
			"before or after the call (%v)\n", after.err)
	case !before.read:
		// The file was absent, so the token in it is new whatever the
		// timestamps say; the browser hook not firing is the odd part.
		fmt.Printf("   origin        newly written to the cache: the file did not exist " +
			"before the call, and no browser flow ran\n")
	case before.unchangedBy(after):
		fmt.Printf("   origin        reused the cached token; the cache file is " +
			"byte-for-byte unchanged\n")
	default:
		fmt.Printf("   origin        refreshed the cached token silently; the cache file " +
			"changed and no browser flow ran\n")
	}
	fmt.Printf("   note          Build does not report which path it took, so every answer " +
		"here except\n")
	fmt.Printf("                 the browser one is inferred from the cache file, not known " +
		"from the SDK\n")
}

// printExpiry reads the one field of the cache file worth showing. The token's
// own expiry is not exposed by the SDK — its token type is unexported and
// OAuth has no accessor for it — so it is read out of the file the SDK wrote.
func printExpiry(after cacheSnapshot) {
	if !after.read {
		fmt.Printf("   expires       unknown (the token cache file is not readable: %v)\n", after.err)
		return
	}
	// Only expires_at is declared. encoding/json still scans the token fields
	// in order to skip them — it has no partial-read mode — but they never
	// reach a variable here, let alone the output.
	var cached struct {
		ExpiresAt int64 `json:"expires_at"`
	}
	if err := json.Unmarshal(after.bytes, &cached); err != nil {
		fmt.Printf("   expires       unknown (the token cache file did not parse: %v)\n", err)
		return
	}
	if cached.ExpiresAt == 0 {
		fmt.Printf("   expires       unknown (the cache file carries no expires_at)\n")
		return
	}
	fmt.Printf("   expires       %s  (in %s, from the cache file; the SDK does not "+
		"report it)\n", cli.FmtTime(cached.ExpiresAt),
		time.Until(time.Unix(cached.ExpiresAt, 0)).Round(time.Second))
}

// printCacheFile shows where the token landed and how the file is protected.
// The client ID is the file name, so the name is masked for the same reason
// the value is: a full path would print a credential in clear on the very line
// meant to reassure the reader that none is. The directory is shown exactly,
// because that is the part the user needs in order to find or delete the file.
func printCacheFile(path string, after cacheSnapshot) {
	if after.stat == nil {
		fmt.Printf("   cache file    %s  (not readable: %v)\n", maskTokenPath(path), after.err)
		return
	}
	dir := filepath.Dir(path)
	dirMode := "unknown"
	if d, err := os.Stat(dir); err == nil {
		dirMode = fmt.Sprintf("%04o", d.Mode().Perm())
	}
	fmt.Printf("   cache file    %s\n", maskTokenPath(path))
	// Octal, because that is how the modes are written in the SDK's own source
	// (MkdirAll 0700, WriteFile 0600) and in this command's -h text; a
	// "-rw-------" rendering would not be recognisable as either.
	fmt.Printf("   permissions   file %04o, directory %s  (written by the SDK, not by "+
		"this command)\n", after.stat.Mode().Perm(), dirMode)
}

// urlSink receives the authorization URL from the SDK and puts it in front of
// the user, which is the entire reason OnOpenURL exists.
type urlSink struct {
	// called records that the SDK asked for an authorization URL, which is the
	// one exact signal there is about which of Build's three paths it is on:
	// only the browser flow calls the hook.
	called bool
}

// show prints the URL and, unless -no-browser was given, asks the desktop to
// open it.
//
// The URL is printed FIRST and unconditionally. A launcher being started
// proves nothing on a headless Linux box, where xdg-open is frequently
// installed, exits zero and opens nothing; printing only after a successful
// launch would leave that user in a silent five-minute wait.
//
// The URL carries the client ID and the CSRF state in its query string. It
// cannot be hidden — the user has to open it — which is exactly why it is
// printed to the terminal and not, say, appended to a log file.
//
// The SDK calls this synchronously inside Build, before it starts waiting for
// the callback, so Build returning is the happens-before edge for reading
// called afterwards. No lock is needed, and adding one would imply a
// concurrency that is not there.
func (s *urlSink) show(authURL string) {
	s.called = true

	fmt.Printf("\nOpen this URL in a browser to authorize:\n\n  %s\n\n", authURL)
	if noBrowser {
		fmt.Println("   -no-browser: not launching anything, waiting for the callback here.")
		fmt.Println("   The redirect has to reach this host, so run the command on the machine")
		fmt.Println("   the browser will come back to, or forward the port over SSH:")
		fmt.Printf("       ssh -L %d:localhost:%d <this-host>\n", port, port)
	} else if err := tryOpenBrowser(authURL); err != nil {
		fmt.Printf("   Could not launch a browser: %v\n", err)
		fmt.Println("   That is not fatal — open the URL above by hand. The callback is")
		fmt.Println("   served here either way, and this command is waiting for it.")
	} else {
		fmt.Println("   A browser was launched. Waiting for the callback (up to 5 minutes).")
	}
	fmt.Printf("   Waiting on http://localhost:%d/callback ...\n", port)
}

// browserLaunchers lists the commands that can open a URL, most likely first,
// for one operating system. In every vector the launcher is argv[0] and the URL
// is the last element, with only the Windows form carrying anything in between.
//
// The GOOS is a parameter rather than runtime.GOOS so the mapping can be
// asserted for platforms this test happens not to be running on — the failure
// this guards against is a Windows user discovering a Linux launcher name.
func browserLaunchers(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"open", "%s"}}
	case "windows":
		// rundll32 with the shell's URL handler is how Windows opens the
		// default browser without taking a dependency.
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", "%s"}}
	default:
		// Linux and every other unix: the freedesktop helper first, then the
		// names Debian and RedHat install for browsers that register
		// themselves as an alternative.
		return [][]string{{"xdg-open", "%s"}, {"sensible-browser", "%s"}, {"x-www-browser", "%s"}}
	}
}

// tryOpenBrowser asks the desktop to open url and reports whether a launcher
// was found and started.
//
// A missing browser is never fatal to the caller: the callback is served by
// this process either way, so a user who cannot launch one can still finish
// the flow by hand.
func tryOpenBrowser(url string) error {
	var tried []string
	for _, argv := range browserLaunchers(runtime.GOOS) {
		name := argv[0]
		path, err := exec.LookPath(name)
		if err != nil {
			tried = append(tried, name)
			continue
		}
		args := make([]string, 0, len(argv))
		for _, a := range argv {
			args = append(args, strings.ReplaceAll(a, "%s", url))
		}
		cmd := exec.Command(path, args...)
		if err := cmd.Start(); err != nil {
			tried = append(tried, fmt.Sprintf("%s (%v)", name, err))
			continue
		}
		// Reaped in the background: xdg-open exits as soon as it has handed
		// the URL on, and this process may be waiting on that browser for
		// another five minutes.
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return fmt.Errorf("no browser launcher on PATH (tried %s)", strings.Join(tried, ", "))
}

// clientIDSource is the resolved OAuth client ID together with where it came
// from, because "which value did you use" is the first question after a
// rejected authorization, and an unlabelled answer does not answer it.
type clientIDSource struct {
	ID  string
	Env string // the variable it came from, or "" when -client-id was used
	// Assumed records the app-key fallback, which is a bet about how
	// Longbridge issues IDs rather than a documented mapping.
	Assumed bool
}

func (s clientIDSource) String() string {
	switch {
	case s.Env == "":
		return "from -client-id"
	case s.Assumed:
		return "from " + s.Env + " — ASSUMED to be the OAuth client ID, see -h"
	default:
		return "from " + s.Env
	}
}

// resolveClientID finds the OAuth client ID, in this order: the -client-id
// flag, then $LONGBRIDGE_CLIENT_ID, then the app key.
//
// THE FALLBACK IS AN ASSUMPTION. Longbridge issues the OAuth client ID from
// the same User Center page as the app key — that is what the operator who
// asked for this command stated — but nothing in the SDK, and nothing this
// project has observed, documents the two as the same string. So the fallback
// is labelled as assumed everywhere it is used, and a rejected authorization
// with an app key set is the symptom that would disprove it.
func resolveClientID(flagValue string) (clientIDSource, error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return clientIDSource{ID: v}, nil
	}
	if v := strings.TrimSpace(os.Getenv(clientIDEnv)); v != "" {
		return clientIDSource{ID: v, Env: clientIDEnv}, nil
	}
	for _, env := range []string{"LONGBRIDGE_APP_KEY", "LONGPORT_APP_KEY"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return clientIDSource{ID: v, Env: env, Assumed: true}, nil
		}
	}
	return clientIDSource{}, fmt.Errorf(
		"no OAuth client ID.\n"+
			"Set it in one of these ways:\n"+
			"  export %s=<your client id>    (read by this command)\n"+
			"  export LONGBRIDGE_APP_KEY=... (read as a fallback, ASSUMED to be the\n"+
			"                                 same value; see the note below)\n"+
			"  auth -client-id <your client id>\n"+
			"\n"+
			"The client ID comes from the same User Center page as the app key\n"+
			"(https://open.longbridge.com/ -> User Center). That app-key fallback is an\n"+
			"assumption about how Longbridge issues the two, not a documented mapping:\n"+
			"if the authorization below is rejected, pass -client-id explicitly.\n"+
			"\n"+
			"Neither the client ID nor any token is printed by this command.", clientIDEnv)
}

// validateFlags rejects the values a flag can hold that the command cannot
// use, before anything is bound or launched. A usage error is exit 1, which is
// also what the flag package's own errors produce (see cli.Fail).
func validateFlags() error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("-port must be between 1 and 65535, got %d", port)
	}
	switch strings.ToLower(strings.TrimSpace(envName)) {
	case "", "staging":
	default:
		return fmt.Errorf("invalid -env %q: the only value the SDK understands is "+
			"\"staging\", which points at *.longbridge.xyz; omit it for the "+
			"production host", envName)
	}
	return nil
}

// applyEnvOverride sets LONGBRIDGE_ENV from -env. It must run before
// oauth.New, which reads the variable once at construction and never again.
func applyEnvOverride(envName string) {
	if strings.EqualFold(strings.TrimSpace(envName), "staging") {
		_ = os.Setenv("LONGBRIDGE_ENV", "staging")
	}
}

// describeEnv reports which OAuth host the SDK will use. The choice is made
// inside oauth.New from LONGBRIDGE_ENV and cannot be read back out of the
// client, so this reports the documented rule and the variable it read — not an
// observation.
func describeEnv() string {
	if os.Getenv("LONGBRIDGE_ENV") == "staging" {
		return "staging (LONGBRIDGE_ENV=staging, so oauth.New chose openapi.longbridge.xyz)"
	}
	return "production (LONGBRIDGE_ENV is not \"staging\", so oauth.New chose openapi.longbridge.com)"
}

// tokenPath is the SDK's own cache location, reconstructed. See tokenDir.
func tokenPath(clientID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory, so the token cache "+
			"path is unknown: %w", err)
	}
	return filepath.Join(home, tokenDir, clientID), nil
}

// maskTokenPath masks the file name — which IS the client ID — and leaves the
// directory exact. Printing the raw path would put a credential in clear on
// the one line whose purpose is to say where a credential landed safely.
func maskTokenPath(path string) string {
	dir, base := filepath.Split(path)
	return dir + appcfg.Redact(base)
}

// portOrDefault is -port, or the default when the help text is rendered before
// the flag layer has parsed anything. Quoting the default unconditionally would
// tell a user who passed -port 7000 to forward the wrong port over SSH.
func portOrDefault() int {
	if port == 0 {
		return defaultCallbackPort
	}
	return port
}

// addOAuthUsageNotes appends this flow's own preamble and its headless
// instructions to the standard -h envelope, which knows nothing about
// authorization. The envelope is the shared part; what goes below it is what
// makes -no-browser findable for the user who has no browser to point at.
func addOAuthUsageNotes(u *cli.Usage) {
	base := u.FS.Usage
	u.FS.Usage = func() {
		base()
		out := u.FS.Output()
		fmt.Fprintf(out, "\nHow this works:\n")
		fmt.Fprintf(out, "  1. Binds 127.0.0.1 on -port and prints an authorization URL.\n")
		fmt.Fprintf(out, "  2. Approve the app in the browser; it redirects back to the local\n")
		fmt.Fprintf(out, "     callback, which completes the flow. Waits up to 5 minutes.\n")
		fmt.Fprintf(out, "  3. The SDK caches the token under $HOME/%s/<client id>, mode 0600.\n", tokenDir)
		fmt.Fprintf(out, "     That local file is the only thing a successful run writes; no API\n")
		fmt.Fprintf(out, "     request is made at all.\n")
		fmt.Fprintf(out, "\nHEADLESS, OR OVER SSH — use -no-browser:\n")
		fmt.Fprintf(out, "  The URL is printed and this command waits, so it can be opened on\n")
		fmt.Fprintf(out, "  another machine. The redirect has to come back to the host running\n")
		fmt.Fprintf(out, "  this command, so forward the port when they are not the same machine:\n")
		fmt.Fprintf(out, "      ssh -L %d:localhost:%d <this-host>\n", portOrDefault(), portOrDefault())
		fmt.Fprintf(out, "  A missing browser is never a failure: the URL is printed first and the\n")
		fmt.Fprintf(out, "  callback is served here either way.\n")
		fmt.Fprintf(out, "\nNo token is printed, not even a prefix. The client ID is shown masked,\n")
		fmt.Fprintf(out, "and it is also the token file's name, so the path is masked too.\n")
	}
}
