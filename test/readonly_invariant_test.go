// Package test holds the checks that belong to no single package: they read the
// source of the binaries themselves.
//
// It exists for one gap. Every read-only binary asserts at startup that the
// order gate is still closed, and that assertion lives in main(), which no
// in-process test can reach — running main() would need credentials, a network
// and a live gate, and would exit the test binary besides. So the eight copies
// that existed when this package was written went untested for as long as they
// did, mutation testing confirmed it (deleting any one of the eight left the
// suite green), and one of them had
// already drifted from the documented contract while nobody was looking. This
// package closes the gap from the outside: it parses the source and asserts the
// shape of the startup sequence.
package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// readOnlyCommands maps each binary that issues no writes to the action string
// it hands to AssertReadOnly — the same phrase GuardWrite would print had that
// binary attempted a write, which is why the nine differ ("quote a symbol" is
// not "run the market reader"). The list is pinned against the cmd/ directory
// listing below, so a tenth binary cannot be added without saying which kind it
// is. internal/cli/cli_test.go holds the same nine pairs and drives them
// through the helper itself.
//
// cmd/auth is in this list and not the other one, and the distinction is worth
// stating because it is the first entry here that is not a market-data reader.
// It authenticates: it makes no API request at all, so it cannot mutate an
// account, and it therefore belongs with the readers. It does cause the SDK to
// write ONE local file — the token cache at $HOME/.longbridge/openapi/tokens/
// <client id>, mode 0600, written by the SDK and not by the command. That is
// not an API write and not a file in this repository, and it is why the order
// gate is still the right thing for this binary to assert: there is still no
// write the gate could legitimately permit. AssertReadOnly only asks that
// GuardWrite refuses; it says nothing about the filesystem.
var readOnlyCommands = map[string]string{
	"quote":        "quote a symbol",
	"watch":        "run the watch streamer",
	"warrant":      "run the warrant reader",
	"reference":    "run the reference reader",
	"portfolio":    "run the portfolio reader",
	"fundamentals": "run the fundamentals reader",
	"market":       "run the market reader",
	"screener":     "run the screener reader",
	"auth":         "run the OAuth login",
}

// writeCommands are the binaries that can change something, and therefore
// consult a write gate of their own instead of asserting that none is open:
// trade places and replaces orders, executions withdraws one, watchlist edits
// account state under GuardWatchlist, and dca, alert, sharelist and content
// each have their own WriteGuard. The list is used to prove every cmd/
// directory is accounted for, and to keep AssertReadOnly out of files that
// genuinely write.
var writeCommands = []string{
	"alert", "content", "dca", "executions", "sharelist", "trade", "watchlist",
}

// cmdDir is the repository's command directory, relative to this package's
// working directory (a test binary runs in the directory of its package).
const cmdDir = "../cmd"

// parseMain parses cmd/<name>/main.go and returns its file and its main
// function. Comments are kept, because the SAFETY note above the assertion is
// part of what is being checked.
func parseMain(t *testing.T, name string) (*ast.File, *ast.FuncDecl) {
	t.Helper()
	path := cmdDir + "/" + name + "/main.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "main" {
			return f, fn
		}
	}
	t.Fatalf("%s has no main function; the file this test parses is not a command", path)
	return nil, nil
}

// qualifiedName renders a function expression as package.Selector, or "" for
// anything that is not one, so a call can be recognised without caring how the
// package was imported.
func qualifiedName(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}

// stringArg returns the value of the i-th argument when it is a string literal.
func stringArg(t *testing.T, call *ast.CallExpr, i int) string {
	t.Helper()
	lit, ok := call.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("argument %d of cli.AssertReadOnly is %T, want a string literal", i, call.Args[i])
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("argument %d of cli.AssertReadOnly is not a valid string literal %s: %v",
			i, lit.Value, err)
	}
	return v
}

// TestReadOnlyCommands_AssertReadOnlyIsCalledInMain replaces the coverage the
// nine copies never had. For each read-only binary it asserts, from the parsed
// source, that main() calls cli.AssertReadOnly exactly once, as a top-level
// statement, on the configuration it just loaded and with the name and action
// recorded above — and that it does so in the one position where the assertion
// means anything: after the [config] banner, so the user can see the state that
// tripped it, and before the first cli.Run, so nothing has been sent yet.
func TestReadOnlyCommands_AssertReadOnlyIsCalledInMain(t *testing.T) {
	for name, wantAction := range readOnlyCommands {
		t.Run(name, func(t *testing.T) {
			file, mainFn := parseMain(t, name)

			// Every call in the file, not just the top-level one: a second call
			// hidden inside the cli.Run closure would be a copy of the
			// duplication this helper removed, and one that no longer runs
			// before the first request.
			var calls []*ast.CallExpr
			var banner, firstRun token.Pos
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch qualifiedName(call.Fun) {
				case "cli.AssertReadOnly":
					calls = append(calls, call)
				case "cli.Run":
					if firstRun == token.NoPos {
						firstRun = call.Pos()
					}
				case "fmt.Fprintf":
					if banner == token.NoPos && printsConfigBanner(call) {
						banner = call.Pos()
					}
				}
				return true
			})

			if len(calls) != 1 {
				t.Fatalf("cmd/%s/main.go contains %d cli.AssertReadOnly calls, want exactly 1; "+
					"the assertion must exist once, in main(), or it is not being enforced at all",
					name, len(calls))
			}
			call := calls[0]

			// A top-level statement of main(), not nested in the cli.Run closure:
			// inside the closure the gate would be consulted after the client is
			// already connected and the first subscription already sent.
			topLevel := false
			for _, stmt := range mainFn.Body.List {
				expr, ok := stmt.(*ast.ExprStmt)
				if !ok {
					continue
				}
				if inner, ok := expr.X.(*ast.CallExpr); ok && inner == call {
					topLevel = true
				}
			}
			if !topLevel {
				t.Errorf("cmd/%s/main.go calls cli.AssertReadOnly somewhere other than a "+
					"top-level statement of main(); it must run before the first request, "+
					"not inside the cli.Run closure", name)
			}

			if len(call.Args) != 3 {
				t.Fatalf("cli.AssertReadOnly in cmd/%s/main.go takes %d arguments, want 3 "+
					"(cfg, name, action)", name, len(call.Args))
			}
			if cfgArg, ok := call.Args[0].(*ast.Ident); !ok || cfgArg.Name != "cfg" {
				t.Errorf("first argument of cli.AssertReadOnly in cmd/%s/main.go is %v, want "+
					"the loaded config (cfg)", name, call.Args[0])
			}
			if got := stringArg(t, call, 1); got != name {
				t.Errorf("cmd/%s/main.go reports itself as %q in the refusal; that name is "+
					"what the user sees, and it should be the binary's own", name, got)
			}
			if got := stringArg(t, call, 2); got != wantAction {
				t.Errorf("cmd/%s/main.go passes action %q to AssertReadOnly, want %q; this is "+
					"the text GuardWrite would print had the binary attempted a write",
					name, got, wantAction)
			}

			// The SAFETY note above the call is the only place a reader of the
			// source learns why a reader checks a write gate, so it is part of
			// the shape being pinned.
			if !hasSafetyCommentAbove(file, call) {
				t.Errorf("cmd/%s/main.go has no // SAFETY note directly above "+
					"cli.AssertReadOnly", name)
			}

			// Both position checks are relative, so both need their anchor. A
			// binary that prints no banner and calls no cli.Run has no state to
			// show and no first request to precede, and the honest answer there
			// is "nothing to compare" rather than a failure: whether those two
			// conventions still hold is a different test's business. Every
			// read-only binary today has both, so for the nine these checks
			// always run.
			if banner == token.NoPos {
				t.Logf("cmd/%s/main.go prints no [config] banner, so there is nothing "+
					"for the assertion to follow; skipping the position check", name)
			} else if call.Pos() < banner {
				t.Errorf("cmd/%s/main.go asserts the order gate before printing the [config] "+
					"banner, so the user cannot see the mode and dry-run state that "+
					"violated the invariant", name)
			}
			if firstRun == token.NoPos {
				t.Logf("cmd/%s/main.go never calls cli.Run, so there is no first request "+
					"for the assertion to precede; skipping the position check", name)
			} else if call.Pos() > firstRun {
				t.Errorf("cmd/%s/main.go asserts the order gate after the first cli.Run call; "+
					"by then it has already connected and sent something", name)
			}
		})
	}
}

// printsConfigBanner reports whether call is the startup banner: an Fprintf
// whose format string carries the [config] marker.
func printsConfigBanner(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		if v, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(v, "[config]") {
			return true
		}
	}
	return false
}

// hasSafetyCommentAbove reports whether the comment group ending closest above
// call mentions SAFETY.
func hasSafetyCommentAbove(file *ast.File, call *ast.CallExpr) bool {
	var nearest *ast.CommentGroup
	for _, group := range file.Comments {
		if group.End() >= call.Pos() {
			continue
		}
		if nearest == nil || group.End() > nearest.End() {
			nearest = group
		}
	}
	if nearest == nil {
		return false
	}
	for _, c := range nearest.List {
		if strings.Contains(c.Text, "SAFETY") {
			return true
		}
	}
	return false
}

// commandDirs returns the names of the directories under cmd/.
func commandDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		t.Fatalf("reading %s: %v", cmdDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestCommandDirectories_AreAllClassifiedAsReadOnlyOrWriting is what makes the
// lists above self-policing: a new command directory arrives unclassified and
// fails, so nobody can add a ninth read-only binary — or a third way of being a
// command — without a human recording which kind it is.
//
// WHAT THIS TEST CANNOT CATCH: whether a newly classified command really is
// read-only. That judgement is still a human's, made by reading its SDK calls,
// and a lie in either list is invisible here. What it can catch is the thing
// that actually went wrong before — an assertion nobody ever checked. Once a
// binary is in readOnlyCommands, the test above holds its file to the call, the
// arguments, the SAFETY note and the position in main().
func TestCommandDirectories_AreAllClassifiedAsReadOnlyOrWriting(t *testing.T) {
	dirs := commandDirs(t)
	if len(dirs) == 0 {
		t.Fatalf("%s contains no command directories; the premise of this test is gone", cmdDir)
	}

	var unclassified []string
	for _, name := range dirs {
		if _, ok := readOnlyCommands[name]; ok {
			continue
		}
		if slices.Contains(writeCommands, name) {
			continue
		}
		unclassified = append(unclassified, name)
	}
	if len(unclassified) > 0 {
		t.Errorf("cmd/ contains %v, which is in neither list. If it issues no writes, add it "+
			"to readOnlyCommands and call cli.AssertReadOnly in its main(); if it can change "+
			"something, add it to writeCommands and give it a write gate. Either way it must "+
			"be recorded, because these two lists are the only thing keeping the read-only "+
			"set honest.", unclassified)
	}

	// The other direction: a name in a list whose directory is gone is a typo,
	// and a typo in readOnlyCommands would silently stop testing a real binary.
	for name := range readOnlyCommands {
		if !slices.Contains(dirs, name) {
			t.Errorf("readOnlyCommands lists %q but cmd/%s does not exist", name, name)
		}
	}
	for _, name := range writeCommands {
		if !slices.Contains(dirs, name) {
			t.Errorf("writeCommands lists %q but cmd/%s does not exist", name, name)
		}
	}
}

// The mirror image of the check above: a binary that really does write must not
// carry the read-only assertion, because AssertReadOnly exits 1 whenever the
// order gate is open — which is precisely the configuration a writer is trying
// to reach. A writer that wants the same reassurance about a different gate
// belongs in the guard family (config.WriteGuard and friends), not here.
func TestWritingCommands_DoNotAssertReadOnly(t *testing.T) {
	for _, name := range writeCommands {
		t.Run(name, func(t *testing.T) {
			file, _ := parseMain(t, name)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if ok && qualifiedName(call.Fun) == "cli.AssertReadOnly" {
					t.Errorf("cmd/%s/main.go calls cli.AssertReadOnly, but this binary can "+
						"write, so it has to be able to run with the order gate open. Move it "+
						"to readOnlyCommands if it is in fact read-only", name)
				}
				return true
			})
		})
	}
}
