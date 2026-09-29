package config

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"

	sdklog "github.com/longbridge/openapi-go/log"
)

// The interface the SDK's SetLogger accepts. Declared as a variable rather than
// left implicit, so that a change to the SDK's Logger interface is a COMPILE
// error in this file naming the missing method, instead of an install that
// quietly stops filtering.
//
// log.Logger is a type ALIAS for protocol.Logger (openapi-protocol/go v0.5.0
// logger.go), which is what makes this assertion meaningful for SetLogger's
// parameter type as well as for the package-level log.SetLogger.
var _ sdklog.Logger = (*SDKLogger)(nil)

// ---------------------------------------------------------------- levels ---

// The four names the SDK's own logger understands, and nothing else.
//
// `trace` is the interesting rejection: it is accepted silently by
// protocol.DefaultLogger.SetLevel, which has no case for it, so an operator who
// set it got no level at all and no message. Refusing it here is the difference
// between a typo and a shrug.
func TestParseLogLevel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  int
	}{
		{"debug", "debug", levelDebug},
		{"info", "info", levelInfo},
		{"warn", "warn", levelWarn},
		{"error", "error", levelError},
		{"folded case", "DEBUG", levelDebug},
		{"mixed case", "Warn", levelWarn},
		{"padded", "  info  ", levelInfo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLogLevel(tc.input)
			if err != nil {
				t.Fatalf("ParseLogLevel(%q) = %v, want it accepted", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseLogLevel(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}

	for _, bad := range []string{"", "trace", "verbose", "err", "warning", "off", "none"} {
		t.Run("refused: "+bad, func(t *testing.T) {
			got, err := ParseLogLevel(bad)
			if err == nil {
				t.Fatalf("ParseLogLevel(%q) = %d, want a refusal: every other "+
					"enumerated switch in this package errors on a bad value rather "+
					"than falling back to a default", bad, got)
			}
			// The refusal has to be actionable, so it names what is accepted.
			for _, name := range logLevelNames {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("refusal for %q does not mention the accepted name %q:\n%v",
						bad, name, err)
				}
			}
		})
	}
}

// -------------------------------------------------------------- filtering --

// Every method at every level, as a matrix. An adapter that "filters" but lets
// one severity through is worse than no adapter, because the operator asked for
// less noise and got more.
func TestSDKLogger_FiltersOnTheLevel(t *testing.T) {
	calls := []struct {
		name string
		emit func(*SDKLogger)
		want int // the lowest level at which this call must appear
	}{
		{"Debug", func(l *SDKLogger) { l.Debug("m") }, levelDebug},
		{"Debugf", func(l *SDKLogger) { l.Debugf("m") }, levelDebug},
		{"Info", func(l *SDKLogger) { l.Info("m") }, levelInfo},
		{"Infof", func(l *SDKLogger) { l.Infof("m") }, levelInfo},
		{"Warn", func(l *SDKLogger) { l.Warn("m") }, levelWarn},
		{"Warnf", func(l *SDKLogger) { l.Warnf("m") }, levelWarn},
		{"Error", func(l *SDKLogger) { l.Error("m") }, levelError},
		{"Errorf", func(l *SDKLogger) { l.Errorf("m") }, levelError},
	}

	for _, at := range []struct {
		name  string
		level int
	}{
		{"debug", levelDebug},
		{"info", levelInfo},
		{"warn", levelWarn},
		{"error", levelError},
	} {
		t.Run("at "+at.name, func(t *testing.T) {
			for _, c := range calls {
				var buf bytes.Buffer
				l := NewSDKLogger(&buf, at.level)
				c.emit(l)
				appeared := buf.Len() > 0
				want := c.want >= at.level
				if appeared != want {
					t.Errorf("%s at level %s: appeared=%v, want %v (output %q)",
						c.name, at.name, appeared, want, buf.String())
				}
				if want && !strings.Contains(buf.String(), "m") {
					t.Errorf("%s printed %q, want the message", c.name, buf.String())
				}
			}
		})
	}
}

// The formatted variants must interpolate, and the unformatted ones must not
// print a stray format verb.
func TestSDKLogger_FormatsTheMessageAndTagsTheSeverity(t *testing.T) {
	var buf bytes.Buffer
	l := NewSDKLogger(&buf, levelDebug)
	l.Infof("sending %d requests to %s", 3, "example.invalid")
	l.Debug("plain")
	l.Errorf("boom: %v", "why")

	got := buf.String()
	for _, want := range []string{
		"sending 3 requests to example.invalid",
		"plain",
		"boom: why",
		"INFO", "DEBUG", "ERROR",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n") != 3 {
		t.Errorf("want exactly three lines, one per call:\n%s", got)
	}
}

// SetLevel has no error to return, so what it does with a name it does not
// understand is a design decision and it has to be the safe one: change
// nothing. Config.SetLogger calls it with the SDK's own resolved LogLevel,
// which is EMPTY unless LONGBRIDGE_LOG_LEVEL or the YAML log_level field set it,
// so "empty means leave the operator's choice alone" has to be legal.
func TestSDKLogger_SetLevelIgnoresWhatItDoesNotUnderstand(t *testing.T) {
	for _, name := range []string{"", "   ", "trace", "verbose", "loud"} {
		t.Run("name "+name, func(t *testing.T) {
			l := NewSDKLogger(nil, levelWarn)
			l.SetLevel(name)
			if l.Level() != levelWarn {
				t.Errorf("SetLevel(%q) moved the threshold to %s; an unrecognised "+
					"name must not quietly raise or lower what the operator asked for",
					name, l.LevelName())
			}
		})
	}

	t.Run("a recognised name does move it", func(t *testing.T) {
		l := NewSDKLogger(nil, levelWarn)
		l.SetLevel("debug")
		if l.Level() != levelDebug {
			t.Errorf("Level = %s, want debug", l.LevelName())
		}
	})
}

// The SDK logs from websocket read loops while the loading goroutine may still
// be adjusting the level, so this has to be race-free. Run with -race, which the
// Makefile's test target does.
//
// The writer here is locked rather than a bare bytes.Buffer because a
// bytes.Buffer is not safe for concurrent use, and four goroutines writing to one
// is a race in the TEST. The logger itself holds no lock: its production
// destination is os.Stderr, whose Write is safe for concurrent use, and it emits
// each line with a single Fprintf so two lines cannot interleave mid-line.
func TestSDKLogger_IsSafeUnderConcurrentUse(t *testing.T) {
	buf := &syncBuffer{}
	l := NewSDKLogger(buf, levelDebug)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); l.SetLevel("debug") }()
		go func(n int) { defer wg.Done(); l.Infof("worker %d", n) }(i)
	}
	wg.Wait()
	if !strings.Contains(buf.String(), "worker") {
		t.Error("no output; the logger dropped messages under concurrency")
	}
}

// syncBuffer is a bytes.Buffer with the one property a bytes.Buffer lacks and
// this test needs: being written from several goroutines at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Where the diagnostics go. stdout is data in this repo — every section and row
// — so a library writing into it would corrupt the output of a pipe, and this
// is the one assertion that keeps that from happening.
func TestSDKLogger_ProductionDestinationIsStderr(t *testing.T) {
	if stderrLogger != os.Stderr {
		t.Error("the SDK logger's destination is not os.Stderr; standard output is " +
			"data in this repo and diagnostics must not go there")
	}
}

// ------------------------------------------------------------- installLogger -

// An unset flag must change nothing at all: no adapter, and the SDK keeps the
// logger it installs for itself.
func TestInstallLogger_AnUnsetFlagInstallsNothing(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	for _, name := range []string{"", "   "} {
		cfg, err := Load("", WithLogLevel(name))
		if err != nil {
			t.Fatalf("Load with log level %q = %v, want it accepted as \"not asked for\"", name, err)
		}
		if cfg.Logger != nil {
			t.Errorf("log level %q installed %v; leaving the flag off must leave the "+
				"SDK's own logger in place", name, cfg.Logger)
		}
		if cfg.SDK.Logger() != nil {
			t.Error("the SDK config has a logger installed; Config.SetLogger was " +
				"called when the operator did not ask for it")
		}
		if strings.Contains(cfg.String(), "sdk_log=") {
			t.Errorf("the banner claims a log level for a logger nobody installed:\n%s", cfg.String())
		}
	}
}

// The whole point of the flag: the SDK's logging ends up on stderr, filtered by
// the requested level, and the banner says which level actually took effect.
func TestInstallLogger_RoutesTheSDKsOwnLoggingToTheAdapter(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	var buf bytes.Buffer
	old := stderrLogger
	stderrLogger = &buf
	t.Cleanup(func() { stderrLogger = old })

	cfg, err := Load("", WithLogLevel("info"))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.Logger == nil {
		t.Fatal("Config.Logger is nil; -log-level installed nothing")
	}
	if cfg.SDK.Logger() != sdklog.Logger(cfg.Logger) {
		t.Error("the adapter was not handed to the SDK config, so the SDK would keep " +
			"logging to its own logger")
	}
	if got := cfg.Logger.LevelName(); got != "info" {
		t.Errorf("level = %s, want info", got)
	}
	if !strings.Contains(cfg.String(), "sdk_log=info") {
		t.Errorf("the banner does not report the level in force:\n%s", cfg.String())
	}

	// Through the SDK's own package-level helpers, which is the path the
	// websocket layer uses. This also pins the footgun: SetLogger replaced the
	// library's GLOBAL default logger, so a call from anywhere in the process
	// now lands here.
	sdklog.Debug("a debug line")
	sdklog.Info("an info line")
	if strings.Contains(buf.String(), "a debug line") {
		t.Errorf("a debug line reached the output at level info:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "an info line") {
		t.Errorf("the SDK's own log helpers do not reach the adapter, which means "+
			"SetLogger did not replace the package-global logger:\n%s", buf.String())
	}
}

// A typo must not reach the SDK: Load re-validates even though the flag layer
// validates too, because Load is a package API rather than a CLI front end.
func TestLoad_AnUnknownLogLevelIsAStartupError(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	cfg, err := Load("", WithLogLevel("trace"))
	if err == nil {
		t.Fatalf("Load accepted -log-level trace, returning %+v", cfg)
	}
	if cfg != nil {
		t.Errorf("no config may come back alongside an error, got %+v", cfg)
	}
	var missing *MissingCredentialError
	if asMissingCredentialError(err, &missing) {
		t.Error("a bad log level must be a plain error; it is a flag value, and a flag " +
			"value that cannot be used exits 1 rather than 2")
	}
}

// The flag beats the SDK's own LONGBRIDGE_LOG_LEVEL, because Config.SetLogger
// applies that value first and an explicit request should not be undone by a
// variable left in a .env from another session.
func TestInstallLogger_TheFlagBeatsTheSDKsOwnLogLevel(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)
	t.Setenv("LONGBRIDGE_LOG_LEVEL", "error")

	var buf bytes.Buffer
	old := stderrLogger
	stderrLogger = &buf
	t.Cleanup(func() { stderrLogger = old })

	cfg, err := Load("", WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if got := cfg.Logger.LevelName(); got != "debug" {
		t.Errorf("level = %s, want debug; SetLogger applies LONGBRIDGE_LOG_LEVEL "+
			"(error here) and the flag has to win over it", got)
	}

	// And with no flag, the SDK's own value is what takes effect — the adapter
	// is not installed, so this is the SDK's DefaultLogger reading it, exactly
	// as it did before this feature existed.
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.Logger != nil {
		t.Errorf("Config.Logger = %v, want nil: leaving the flag off must not change "+
			"which logger the SDK uses", cfg.Logger)
	}
	if cfg.SDK.LogLevel != "error" {
		t.Errorf("SDK config LogLevel = %q, want the environment's value to reach the "+
			"SDK unchanged", cfg.SDK.LogLevel)
	}
}

// Nothing in this file, or in logger.go, builds a client or opens a connection:
// the adapter is a filter over a writer, and every assertion above is made
// against a bytes.Buffer.
func TestSDKLogger_NeedsNoClientAndNoConnection(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	var buf bytes.Buffer
	cfg, err := Load("", WithLogLevel("debug"))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.SDK.Client != nil {
		t.Error("Load installed an HTTP client; nothing about logging should")
	}
	old := stderrLogger
	stderrLogger = &buf
	t.Cleanup(func() { stderrLogger = old })
	if buf.Len() != 0 {
		t.Errorf("loading produced output on the logger's destination: %q", buf.String())
	}
}

// The adapter is constructed by the loader, but the type is exported so a
// command that wants to send the SDK's logging somewhere else — a file, a test —
// can build one itself. Its threshold is set through SetLevel, not by passing an
// unknown name in.
func TestNewSDKLogger_ExposesTheLevelItWasBuiltWith(t *testing.T) {
	l := NewSDKLogger(os.Stderr, levelWarn)
	if l.Level() != levelWarn {
		t.Errorf("Level = %s, want warn", l.LevelName())
	}
	if l.LevelName() != "warn" {
		t.Errorf("LevelName = %q, want %q; the name has to round-trip through "+
			"ParseLogLevel", l.LevelName(), "warn")
	}
	if _, err := ParseLogLevel(l.LevelName()); err != nil {
		t.Errorf("LevelName() = %q is not a name ParseLogLevel accepts: %v", l.LevelName(), err)
	}
}

// WHAT IS DELIBERATELY NOT PINNED HERE, and why.
//
// The SDK's own default logger — what a run gets when -log-level is absent —
// prints error, warn and info and suppresses only debug, because its filter is
// `severity >= threshold` and its zero threshold sits below info. That was
// measured with fake credentials and no request, and it is recorded in the
// comment on installLogger, but it is not asserted here: reaching the library's
// default requires the package-global logger to still BE the default, and
// Config.SetLogger replaces that global for the rest of the process. Any test
// that installed this package's adapter first would therefore make the answer
// depend on test order. Asserting the SDK's default behaviour is not worth an
// order-dependent test in this package; asserting that OUR logger filters is,
// and TestSDKLogger_FiltersOnTheLevel does it at every level.
