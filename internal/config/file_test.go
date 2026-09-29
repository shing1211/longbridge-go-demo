package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This file is deliberately in-package: loadFileInto is unexported, and the
// candidate-resolution order it implements is a safety-relevant contract (a
// developer's real config.yaml must be the one that gets read).

const yamlCreds = "longbridge:\n  app_key: FILE_KEY\n  app_secret: FILE_SECRET\n  access_token: FILE_TOKEN\n"

func emptyVals() map[string]string {
	return map[string]string{}
}

func TestCandidateFileOrderIsExact(t *testing.T) {
	// The order is part of the contract: config.yaml wins over
	// config.local.yaml. Changing it silently changes which credentials are
	// used.
	want := []string{"config.yaml", "config.local.yaml"}
	if !reflect.DeepEqual(candidateFiles, want) {
		t.Fatalf("candidateFiles = %q, want %q", candidateFiles, want)
	}
}

// TestCandidateFilesAreYamlOnly is the assertion behind dropping config.yml and
// config.toml: the auto-detected list must contain nothing this loader cannot
// honour. Re-adding either name is a regression, because the SDK's
// configTypeMap rejects .yml ("config type:.yml not support") and .toml was
// never parsed here, so a TOML file supplied credentials to nobody.
func TestCandidateFilesAreYamlOnly(t *testing.T) {
	for _, name := range candidateFiles {
		if filepath.Ext(name) != yamlExt {
			t.Errorf("candidate %q is not a %s file; this loader only parses YAML", name, yamlExt)
		}
	}
	for _, gone := range []string{"config.yml", "config.toml"} {
		for _, name := range candidateFiles {
			if name == gone {
				t.Errorf("%q must not be auto-detected: it cannot be loaded, so its presence is a startup failure waiting to happen", gone)
			}
		}
	}
}

func TestLoadFileInto_NoExplicitPathAndNoCandidates(t *testing.T) {
	sandbox(t)
	// The file is optional: its absence is not an error, and the caller must
	// be told that no file was used.
	got, err := loadFileInto(emptyVals(), "")
	if err != nil {
		t.Fatalf("absence is not an error, got %v", err)
	}
	if got != "" {
		t.Fatalf("used path = %q, want empty", got)
	}
}

func TestLoadFileInto_ExplicitPathThatDoesNotExistIsAnError(t *testing.T) {
	// A typo in --config must not silently fall back to the environment, which
	// would look like "my file is being ignored" or worse, "my file was used".
	sandbox(t)
	_, err := loadFileInto(emptyVals(), "no-such-file.yaml")
	if err == nil {
		t.Fatal("want an error for a missing explicit path")
	}
	if !strings.Contains(err.Error(), "no-such-file.yaml") {
		t.Errorf("error must name the path, got %q", err)
	}
}

func TestLoadFileInto_ExplicitYamlFillsOnlyEmptyKeys(t *testing.T) {
	sandbox(t)
	writeFile(t, "cfg.yaml", yamlCreds)

	t.Run("empty map is filled completely", func(t *testing.T) {
		vals := emptyVals()
		got, err := loadFileInto(vals, "cfg.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "cfg.yaml" {
			t.Errorf("used path = %q, want cfg.yaml", got)
		}
		want := map[string]string{"app_key": "FILE_KEY", "app_secret": "FILE_SECRET", "access_token": "FILE_TOKEN"}
		if !reflect.DeepEqual(vals, want) {
			t.Fatalf("vals = %v, want %v", vals, want)
		}
	})

	t.Run("a key already set by the environment is not overwritten", func(t *testing.T) {
		vals := map[string]string{"app_key": "ENV_KEY", "access_token": "ENV_TOKEN"}
		if _, err := loadFileInto(vals, "cfg.yaml"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vals["app_key"] != "ENV_KEY" {
			t.Errorf("app_key = %q, want the env value to win", vals["app_key"])
		}
		if vals["access_token"] != "ENV_TOKEN" {
			t.Errorf("access_token = %q, want the env value to win", vals["access_token"])
		}
		if vals["app_secret"] != "FILE_SECRET" {
			t.Errorf("app_secret = %q, want the file to fill the gap", vals["app_secret"])
		}
	})

	t.Run("a whitespace-only existing value counts as empty", func(t *testing.T) {
		vals := map[string]string{"app_key": "  \t "}
		if _, err := loadFileInto(vals, "cfg.yaml"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vals["app_key"] != "FILE_KEY" {
			t.Errorf("app_key = %q, want the file to replace a blank value", vals["app_key"])
		}
	})

	t.Run("a whitespace-only file value is not stored", func(t *testing.T) {
		writeFile(t, "blank.yaml", "longbridge:\n  app_key: \"  \"\n  app_secret: FILE_SECRET\n")
		vals := emptyVals()
		if _, err := loadFileInto(vals, "blank.yaml"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := vals["app_key"]; ok {
			t.Errorf("a blank credential must stay missing, got %q", vals["app_key"])
		}
		if vals["app_secret"] != "FILE_SECRET" {
			t.Errorf("app_secret = %q", vals["app_secret"])
		}
	})
}

func TestLoadFileInto_PartialFile(t *testing.T) {
	sandbox(t)
	writeFile(t, "partial.yaml", "longbridge:\n  app_key: ONLY_KEY\n")
	vals := emptyVals()
	if _, err := loadFileInto(vals, "partial.yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vals["app_key"] != "ONLY_KEY" {
		t.Errorf("app_key = %q", vals["app_key"])
	}
	if len(vals) != 1 {
		t.Errorf("only the key present in the file may be added, got %v", vals)
	}
}

// TestLoadFileInto_ExplicitNonYamlPathIsRejected pins the YAML-only rule at the
// unit that enforces it. Every non-.yaml extension the SDK understands, plus a
// few it does not, is refused with an error that names the file — and the file
// must be rejected whether or not it exists, so a typo is reported as a typo.
func TestLoadFileInto_ExplicitNonYamlPathIsRejected(t *testing.T) {
	for _, name := range []string{
		"cfg.toml", "creds.yml", ".env", "creds.conf", "config", "creds.yaml.bak",
	} {
		t.Run(name, func(t *testing.T) {
			sandbox(t)
			writeFile(t, name, yamlCreds)

			got, err := loadFileInto(emptyVals(), name)
			if err == nil {
				t.Fatalf("SAFETY: %q must be refused, but it was accepted and returned %q", name, got)
			}
			if got != "" {
				t.Errorf("used path = %q, want empty on failure", got)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error must name the file, got %q", err)
			}
			// The message has to be actionable: it must say which formats are
			// supported, not merely refuse.
			for _, want := range []string{".yaml", "YAML only", "TOML"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error must mention %q, got %q", want, err)
				}
			}
		})
	}
}

// TestLoadFileInto_NonYamlIsCheckedBeforeTheFileExists keeps the format check
// ahead of the stat, so "you gave me a .toml" is the answer even when the path
// is also wrong. A missing-file message about a .toml would send the user
// looking in the wrong place.
func TestLoadFileInto_NonYamlIsCheckedBeforeTheFileExists(t *testing.T) {
	sandbox(t)
	got, err := loadFileInto(emptyVals(), "no-such-file.toml")
	if err == nil {
		t.Fatalf("want an error, got %q", got)
	}
	if !strings.Contains(err.Error(), "YAML only") {
		t.Fatalf("the format must be reported before existence, got %q", err)
	}
}

// TestLoad_TomlIsNotACandidate pins that a config.toml in the working directory
// is now simply not there as far as Load is concerned: it supplies no
// credentials and, more importantly, it no longer reaches sdk.New, which used
// to be the only situation in which it was harmless.
//
// Before: TOML was a candidate and was never parsed, so a TOML-only user was
// told all three credentials were missing while the error text pointed at
// config.example.yaml. Re-adding it to candidateFiles breaks this test.
func TestLoad_TomlIsNotACandidate(t *testing.T) {
	sandbox(t)
	writeFile(t, "config.toml", "[longbridge]\napp_key = \"TOML_KEY\"\napp_secret = \"TOML_SECRET\"\naccess_token = \"TOML_TOKEN\"\n")

	// Environment complete, so the only thing that could fail is the file. The
	// TOML must be invisible.
	t.Run("complete environment ignores it and reports no source", func(t *testing.T) {
		sandbox(t)
		writeFile(t, "config.toml", "[longbridge]\napp_key = \"TOML_KEY\"\n")
		setCredentials(t, "key-1", "secret-1", "token-1")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("a config.toml in the tree must no longer affect the load: %v", err)
		}
		if cfg.Source != "" {
			t.Errorf("Source = %q, want empty: config.toml is not a candidate any more", cfg.Source)
		}
		if cfg.AppKey != "key-1" {
			t.Errorf("AppKey = %q, want the env value", cfg.AppKey)
		}
	})

	t.Run("it cannot supply a credential either", func(t *testing.T) {
		sandbox(t)
		writeFile(t, "config.toml", "[longbridge]\napp_key = \"TOML_KEY\"\napp_secret = \"TOML_SECRET\"\naccess_token = \"TOML_TOKEN\"\n")
		_, err := Load("")
		mce := requireMissingCredentials(t, err,
			"LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_KEY", "LONGBRIDGE_APP_SECRET")
		if strings.Contains(mce.Source, "config.toml") {
			t.Errorf("the file must not be named as a source: %q", mce.Source)
		}
	})
}

// TestLoad_ExplicitNonYamlPathIsAPlainError pins the user-visible contract of
// the new restriction: -config accepts .yaml and nothing else, and getting it
// wrong is an ordinary error (exit 1), NOT a missing-credential error. Telling
// a user with a perfectly good config.toml that their credentials are missing
// was the second half of the original defect.
func TestLoad_ExplicitNonYamlPathIsAPlainError(t *testing.T) {
	for _, name := range []string{"creds.toml", "creds.yml", ".env"} {
		t.Run(name, func(t *testing.T) {
			sandbox(t)
			writeFile(t, name, yamlCreds)

			cfg, err := Load(name)
			if err == nil {
				t.Fatalf("BUG REINTRODUCED: Load accepted %q, cfg = %+v", name, cfg)
			}
			if cfg != nil {
				t.Errorf("no config may be returned alongside an error, got %+v", cfg)
			}
			var mce *MissingCredentialError
			if asMissingCredentialError(err, &mce) {
				t.Fatalf("a format complaint must not be reported as missing credentials: %q", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error must name the file, got %q", err)
			}
			if !strings.Contains(err.Error(), ".yaml") {
				t.Fatalf("error must state the supported format, got %q", err)
			}
		})
	}
}

// TestLoad_YmlIsNotACandidate pins the other half: config.yml is neither
// auto-detected nor acceptable via -config.
//
// Before: config.yml WAS a candidate and was in .gitignore, so users were told
// to create it — and then Load failed with the SDK's "config type:.yml not
// support" even when the environment already held every credential, because
// the SDK derives the config type from the file extension. Adding either name
// back breaks this test.
func TestLoad_YmlIsNotACandidate(t *testing.T) {
	sandbox(t)
	writeFile(t, "config.yml", yamlCreds)
	setCredentials(t, "key-1", "secret-1", "token-1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("BUG REINTRODUCED: a config.yml in the tree broke the load: %v", err)
	}
	if cfg.Source != "" {
		t.Errorf("Source = %q, want empty: config.yml is not a candidate any more", cfg.Source)
	}
	if cfg.AppKey != "key-1" || cfg.AppSecret != "secret-1" || cfg.AccessToken != "token-1" {
		t.Errorf("credentials must come from the environment: %+v", cfg)
	}

	t.Run("an explicit .yml path is refused with a clear error", func(t *testing.T) {
		sandbox(t)
		writeFile(t, "creds.yml", yamlCreds)
		setCredentials(t, "key-1", "secret-1", "token-1")
		cfg, err := Load("creds.yml")
		if err == nil {
			t.Fatalf("BUG REINTRODUCED: an explicit .yml path was accepted, cfg = %+v", cfg)
		}
		if cfg != nil {
			t.Errorf("no config may be returned alongside an error, got %+v", cfg)
		}
		if strings.Contains(err.Error(), "config type:.yml not support") {
			t.Fatalf("the SDK's confusing message must not reach the user any more, got %q", err)
		}
		if !strings.Contains(err.Error(), "creds.yml") || !strings.Contains(err.Error(), ".yaml") {
			t.Fatalf("error must name the file and the supported format, got %q", err)
		}
	})
}

// TestLoad_MissingExplicitFileFailsBeforeAnythingElse pins that the explicit
// path is checked before credentials are collected. A typo in --config must
// surface as itself, not as "missing credentials", and must never quietly fall
// back to the environment.
func TestLoad_MissingExplicitFileFailsBeforeAnythingElse(t *testing.T) {
	sandbox(t)
	// Credentials present: if the file were optional, this would succeed.
	setCredentials(t, "key-1", "secret-1", "token-1")

	cfg, err := Load("no-such-file.yaml")
	if err == nil {
		t.Fatalf("a missing --config path must be an error, got cfg %+v", cfg)
	}
	if cfg != nil {
		t.Errorf("no config may be returned alongside an error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "no-such-file.yaml") {
		t.Fatalf("error must name the file, got %q", err)
	}
	var mce *MissingCredentialError
	if asMissingCredentialError(err, &mce) {
		t.Fatalf("the file error must not be reported as a credential problem: %q", err)
	}
}

func TestLoad_ExplicitYamlFileSuppliesCredentials(t *testing.T) {
	sandbox(t)
	writeFile(t, "creds.yaml", yamlCreds)

	cfg, err := Load("creds.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "FILE_KEY" || cfg.AppSecret != "FILE_SECRET" || cfg.AccessToken != "FILE_TOKEN" {
		t.Fatalf("credentials were not read from the file: %+v", cfg)
	}
	if cfg.Source != "creds.yaml" {
		t.Errorf("Source = %q, want creds.yaml so the banner can name the source", cfg.Source)
	}
}

func TestLoad_EnvironmentBeatsTheFile(t *testing.T) {
	sandbox(t)
	writeFile(t, "config.yaml", yamlCreds)
	setCredentials(t, "key-1", "secret-1", "token-1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppKey != "key-1" || cfg.AppSecret != "secret-1" || cfg.AccessToken != "token-1" {
		t.Fatalf("the environment must win over the file: %+v", cfg)
	}
	if cfg.Source != "config.yaml" {
		t.Errorf("Source = %q; the file was still read and must be reported", cfg.Source)
	}
}

func TestLoadFileInto_CandidatePrecedence(t *testing.T) {
	// The outer sandbox only supplies a clean starting point; each subtest
	// builds its own directory containing a known prefix of the candidate
	// list, so the winner can be identified by the file it came from.
	names := candidateFiles
	sandbox(t)
	for i, n := range names {
		t.Run("first present is "+n, func(t *testing.T) {
			// Names[i:] exist and names[:i] do not, so exactly one candidate is
			// the first present one and the winner must be it.
			dir := t.TempDir()
			t.Chdir(dir)
			for j := i; j < len(names); j++ {
				writeFile(t, names[j], "longbridge:\n  app_key: FROM_"+names[j]+"\n")
			}
			vals := emptyVals()
			got, err := loadFileInto(vals, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != n {
				t.Fatalf("used %q, want the highest-priority present file %q", got, n)
			}
			if want := "FROM_" + n; vals["app_key"] != want {
				t.Fatalf("app_key = %q, want %q (got creds from the wrong file)", vals["app_key"], want)
			}
		})
	}
}

func TestLoadFileInto_MalformedYamlWithAnExplicitPathIsAnError(t *testing.T) {
	// Explicit means the user asked for this file; failing loudly is correct.
	sandbox(t)
	writeFile(t, "broken.yaml", "longbridge:\n  app_key: [unclosed\n")

	vals := emptyVals()
	got, err := loadFileInto(vals, "broken.yaml")
	if err == nil {
		t.Fatal("want a parse error")
	}
	if !strings.Contains(err.Error(), "broken.yaml") {
		t.Errorf("error must name the file, got %q", err)
	}
	if got != "" {
		t.Errorf("used path = %q, want empty on failure", got)
	}
}

func TestLoadFileInto_MalformedYamlWithoutAnExplicitPathFallsThrough(t *testing.T) {
	// With no explicit path the candidates are best-effort, so a broken file
	// is skipped and the next candidate gets its turn.
	sandbox(t)
	writeFile(t, "config.yaml", "longbridge:\n  app_key: [unclosed\n")
	writeFile(t, "config.local.yaml", yamlCreds)

	vals := emptyVals()
	got, err := loadFileInto(vals, "")
	if err != nil {
		t.Fatalf("a broken candidate must not be fatal, got %v", err)
	}
	if got != "config.local.yaml" {
		t.Fatalf("used %q, want the next candidate", got)
	}
	if vals["app_key"] != "FILE_KEY" {
		t.Fatalf("app_key = %q, want the good file to be used", vals["app_key"])
	}
}

func TestLoadFileInto_MalformedCandidateThenNoOthers(t *testing.T) {
	sandbox(t)
	writeFile(t, "config.yaml", "\tnot: [valid\n\t\t yaml: - -")
	got, err := loadFileInto(emptyVals(), "")
	if err != nil {
		t.Fatalf("a broken candidate with no fallback must still be silent, got %v", err)
	}
	if got != "" {
		t.Fatalf("used path = %q, want empty: every candidate was unusable", got)
	}
}

func TestLoadFileInto_UnknownExtensionIsNotSilentlyParsedAsYaml(t *testing.T) {
	// Guessing a format from its contents is how a .conf or a .env ended up
	// being handed to the SDK and rejected there with a message about
	// credentials. The extension is the contract: only .yaml is loaded.
	sandbox(t)
	writeFile(t, "creds.conf", yamlCreds)

	got, err := loadFileInto(emptyVals(), "creds.conf")
	if err == nil {
		t.Fatalf("SAFETY: an unknown extension must be refused, but it returned %q", got)
	}
	if !strings.Contains(err.Error(), yamlExt) {
		t.Errorf("error must name the supported extension, got %q", err)
	}
}

func TestLoadFileInto_YamlWithoutALongbridgeBlock(t *testing.T) {
	sandbox(t)
	writeFile(t, "other.yaml", "something_else:\n  key: value\n")

	vals := emptyVals()
	got, err := loadFileInto(vals, "other.yaml")
	if err != nil {
		t.Fatalf("a file with no longbridge block is not a parse error, got %v", err)
	}
	if got != "other.yaml" {
		t.Errorf("used path = %q, want the file to still be reported", got)
	}
	if len(vals) != 0 {
		t.Errorf("vals = %v, want no credentials invented", vals)
	}
}

func TestLoadFileInto_EmptyAndNullLongbridgeBlock(t *testing.T) {
	for _, tt := range []struct{ name, body string }{
		{"empty block", "longbridge:\n"},
		{"null block", "longbridge: null\n"},
		{"blank keys", "longbridge:\n  app_key: \"\"\n  app_secret: \"\"\n  access_token: \"\"\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sandbox(t)
			writeFile(t, "empty.yaml", tt.body)
			vals := emptyVals()
			if _, err := loadFileInto(vals, "empty.yaml"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(vals) != 0 {
				t.Fatalf("vals = %v, want nothing filled", vals)
			}
		})
	}
}

func TestLoadFileInto_ExtraKeysAreIgnored(t *testing.T) {
	sandbox(t)
	writeFile(t, "extra.yaml", "longbridge:\n  app_key: K\n  app_secret: S\n  access_token: T\n  http_url: https://example.invalid\n  region: cn\n")

	vals := emptyVals()
	if _, err := loadFileInto(vals, "extra.yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"app_key": "K", "app_secret": "S", "access_token": "T"}
	if !reflect.DeepEqual(vals, want) {
		t.Fatalf("vals = %v, want only the three credentials", vals)
	}
}

func TestLoadFileInto_DirectoryAsTheExplicitPathIsAReadError(t *testing.T) {
	// A directory whose name ends in .yaml passes the format check and then
	// fails to read; that failure has to stay distinguishable from a parse
	// failure, which is the line after it.
	sandbox(t)
	if err := os.Mkdir("adir.yaml", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := loadFileInto(emptyVals(), "adir.yaml")
	if err == nil {
		t.Fatal("a directory is not a config file")
	}
	if !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("error = %q, want the read failure to be distinguished from a parse failure", err)
	}
	if got != "" {
		t.Errorf("used path = %q, want empty", got)
	}
}

// TestLoad_MissingCredentialWithAFileInPlay pins the two-line Source message,
// which is the only way a user learns that the loader looked somewhere other
// than the environment. Losing this branch is how "my config.yaml is being
// ignored" becomes unanswerable.
func TestLoad_MissingCredentialWithAFileInPlay(t *testing.T) {
	sandbox(t)
	writeFile(t, "config.yaml", "longbridge:\n  app_key: FILE_KEY\n")

	_, err := Load("")
	mce := requireMissingCredentials(t, err, "LONGBRIDGE_ACCESS_TOKEN", "LONGBRIDGE_APP_SECRET")
	if want := "environment and config.yaml"; mce.Source != want {
		t.Fatalf("Source = %q, want %q", mce.Source, want)
	}
	if !strings.Contains(mce.Error(), "config.example.yaml") {
		t.Errorf("the fix text must still point at the YAML template:\n%s", mce.Error())
	}
}

// TestLoad_StrictSdkFieldsInAYamlFileStillReachTheSdk pins the one place the
// SDK's own parser is still authoritative: a field this package ignores but the
// SDK does not, with a value the SDK cannot decode. It must surface as the
// SDK's error, not as "missing credentials" and not silently as success.
func TestLoad_StrictSdkFieldsInAYamlFileStillReachTheSdk(t *testing.T) {
	sandbox(t)
	// timeout is a time.Duration in the SDK and a string here, so this package
	// parses the file happily and the SDK does not.
	writeFile(t, "config.yaml", "longbridge:\n  timeout: \"not-a-duration\"\n")
	setCredentials(t, "key-1", "secret-1", "token-1")

	cfg, err := Load("")
	if err == nil {
		t.Fatalf("want the SDK to reject the file, got cfg %+v", cfg)
	}
	if !strings.Contains(err.Error(), "building SDK config") {
		t.Fatalf("error = %q, want it attributed to the SDK's own parse", err)
	}
	var mce *MissingCredentialError
	if asMissingCredentialError(err, &mce) {
		t.Fatalf("credentials were present; this must not be a credential error: %q", err)
	}
}
