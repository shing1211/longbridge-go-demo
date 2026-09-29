package config

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

// Log levels, in increasing severity. The names are the SDK's own: the logger
// interface Config.SetLogger accepts is a type ALIAS for
// protocol.Logger (openapi-protocol/go v0.5.0 logger.go), and
// protocol.DefaultLogger.SetLevel switches on exactly these four lower-cased
// names and ignores everything else.
//
// The severity order matters and is not the protocol package's: there,
// LevelDebug is -1 and LevelInfo is `iota`, which counts to 1, so the zero
// value of that struct sits BELOW info and a fresh DefaultLogger prints errors
// and nothing else. The thresholds here are spelled out rather than derived
// from an iota so that the zero value of this type means the same thing as a
// constructed one.
const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

// logLevelNames is the set ParseLogLevel accepts, in the order it is reported
// to a reader. Every name here is a name protocol.DefaultLogger.SetLevel
// handles.
//
// `trace` is deliberately absent. It appears in this project's older
// documentation of LONGBRIDGE_LOG_LEVEL and the SDK accepts it without
// complaint, but protocol.DefaultLogger.SetLevel has no case for it, so it
// selects no level at all: an operator who set it believes they asked for
// something and got silence. Refusing it here turns that into a message naming
// the four that work.
var logLevelNames = []string{"debug", "info", "warn", "error"}

// ParseLogLevel resolves a level name to the threshold the logger filters at.
//
// An unrecognised or empty name is an ERROR, not a default. This is the same
// rule loadModeAndDryRun applies to LONGPORT_MODE and the guards apply to their
// dry-run switches: a typo in a switch is a startup failure the operator can
// see, rather than a value that quietly means something else. The alternative —
// falling back to a default — would make `-log-level debugd` indistinguishable
// from `-log-level error` in the only output there is.
//
// Case and surrounding whitespace are tolerated, matching how every other
// enumerated switch in this repo parses (LONGPORT_MODE, -sections, -period).
func ParseLogLevel(name string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return levelDebug, nil
	case "info":
		return levelInfo, nil
	case "warn":
		return levelWarn, nil
	case "error":
		return levelError, nil
	}
	return 0, fmt.Errorf(
		"invalid SDK log level %q: want one of %s. Note that the SDK's own logger "+
			"honours only these four; \"trace\", which older docs of this project "+
			"list, selects no level and is silently ignored by the library",
		name, strings.Join(logLevelNames, ", "))
}

// SDKLogger is the adapter that satisfies the logger interface the SDK's config
// takes, and sends everything the SDK logs to standard error.
//
// # WHY STDERR AND NOT STDOUT
//
// This repo's contract is that standard output is data: every section heading
// and every row goes there, and a reader pipes stdout into jq or a file. A
// library that printed its diagnostics into that stream would corrupt it. The
// SDK's own default logger writes through the standard library `log` package,
// which also defaults to stderr, so this matches what an operator already sees
// when no flag is passed at all.
//
// # SetLevel IGNORES WHAT IT DOES NOT UNDERSTAND
//
// The interface method returns nothing, so there is no way to report a bad
// level from inside it. An unrecognised or empty name therefore leaves the
// current threshold alone — the same thing protocol.DefaultLogger.SetLevel does,
// and the same reason: Config.SetLogger calls SetLevel with the SDK's own
// resolved value, which is EMPTY unless LONGBRIDGE_LOG_LEVEL or the YAML
// log_level field set it, and "empty means leave it as the operator asked" has
// to be a legal input or the flag could never win. A name this logger does not
// recognise is likewise not allowed to change what the operator asked for.
// The value the operator typed is validated separately, by ParseLogLevel, so a
// typo fails loudly there and cannot reach this method.
type SDKLogger struct {
	// w is the destination. It is a field rather than a hard-coded os.Stderr so
	// that the test can capture what the SDK would have printed without
	// capturing the test binary's own stderr.
	w io.Writer
	// level is an int32 rather than an int so it can be swapped atomically.
	// The SDK logs from websocket read loops, and Config.SetLogger calls
	// SetLevel from the goroutine that loaded the config; without the atomic
	// this is a data race that the race detector would report from inside the
	// library, not from this line.
	//
	// There is no lock around w, and that is deliberate: the destination is
	// os.Stderr, whose Write is safe for concurrent use, and each line is emitted
	// with a single Fprintf so two lines cannot interleave halfway through one.
	level atomic.Int32
}

// NewSDKLogger returns a logger writing to w at the given threshold. w is only
// standard error in production; see the SDKLogger comment.
func NewSDKLogger(w io.Writer, level int) *SDKLogger {
	l := &SDKLogger{w: w}
	l.level.Store(int32(level))
	return l
}

// SetLevel implements the SDK's logger interface. See the SDKLogger comment for
// why an unknown name changes nothing.
func (l *SDKLogger) SetLevel(name string) {
	level, err := ParseLogLevel(name)
	if err != nil {
		return
	}
	l.level.Store(int32(level))
}

// Level reports the threshold currently in force, so a caller can say what a
// run actually got rather than what it asked for.
func (l *SDKLogger) Level() int { return int(l.level.Load()) }

// LevelName is Level rendered as the name ParseLogLevel accepts.
func (l *SDKLogger) LevelName() string { return logLevelNames[l.Level()] }

func (l *SDKLogger) enabled(level int) bool { return level >= l.Level() }

func (l *SDKLogger) emit(level int, tag, msg string) {
	if !l.enabled(level) {
		return
	}
	// One Fprintf, not two: interleaved writes from the websocket read loops
	// would otherwise be able to split a line in half.
	fmt.Fprintf(l.w, "[longbridge] %s %s\n", tag, msg)
}

// The nine methods below are the whole of protocol.Logger (openapi-protocol/go
// v0.5.0 logger.go), which log.Logger is an alias for. Each filters on its own
// threshold, which is the point of having an adapter at all: the library's own
// DefaultLogger also filters, but only if a level was ever set, and with nothing
// set it starts from a zero value that prints errors and nothing else.

func (l *SDKLogger) Info(msg string) { l.emit(levelInfo, "INFO", msg) }

func (l *SDKLogger) Warn(msg string) { l.emit(levelWarn, "WARN", msg) }

func (l *SDKLogger) Error(msg string) { l.emit(levelError, "ERROR", msg) }

func (l *SDKLogger) Debug(msg string) { l.emit(levelDebug, "DEBUG", msg) }

func (l *SDKLogger) Infof(format string, args ...interface{}) {
	l.emitf(levelInfo, "INFO", format, args...)
}

func (l *SDKLogger) Warnf(format string, args ...interface{}) {
	l.emitf(levelWarn, "WARN", format, args...)
}

func (l *SDKLogger) Errorf(format string, args ...interface{}) {
	l.emitf(levelError, "ERROR", format, args...)
}

func (l *SDKLogger) Debugf(format string, args ...interface{}) {
	l.emitf(levelDebug, "DEBUG", format, args...)
}

func (l *SDKLogger) emitf(level int, tag, format string, args ...interface{}) {
	if !l.enabled(level) {
		return
	}
	fmt.Fprintf(l.w, "[longbridge] %s %s\n", tag, fmt.Sprintf(format, args...))
}

// stderrLogger is the destination used in production, named so that the one
// place that decides where SDK diagnostics go says so out loud.
var stderrLogger io.Writer = os.Stderr
