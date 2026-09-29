package config

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/longbridge/openapi-go/oauth"
)

// ---------------------------------------------------------------- helpers --

// clearHeaderEnv unsets every LONGPORT_HEADER_* variable for the duration of a
// test.
//
// sandbox() clears a fixed list of names, and that is enough for everything else
// in this package — but the header source is a prefix scan, so its inputs cannot
// be enumerated in advance. Without this, a developer who exported
// LONGPORT_HEADER_X_TRACE_ID to try the feature would fail the "no headers were
// configured" cases in this file for ever.
func clearHeaderEnv(t *testing.T) {
	t.Helper()
	var names []string
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, HeaderEnvPrefix) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}
	restoreEnv(t, names...)
	for _, n := range names {
		if err := os.Unsetenv(n); err != nil {
			t.Fatalf("clearing %s: %v", n, err)
		}
	}
}

// credentials puts the app-key triple in the environment, which is what Load
// demands before it will do anything else.
func credentials(t *testing.T) {
	t.Helper()
	t.Setenv("LONGBRIDGE_APP_KEY", "key-1")
	t.Setenv("LONGBRIDGE_APP_SECRET", "secret-1")
	t.Setenv("LONGBRIDGE_ACCESS_TOKEN", "token-1")
}

// ------------------------------------------------- the credential headers --

// The central claim of header.go: a caller cannot replace a credential header,
// because the SDK would silently accept the replacement.
//
// Each of the four reserved names is checked in the exact spelling the SDK uses
// AND in a spelling a person would type, since http.Header.Set canonicalises
// and the comparison here therefore has to be case-insensitive.
func TestParseHeaderSpec_EveryReservedHeaderIsRefused(t *testing.T) {
	reserved := ReservedHeaderNames()
	want := []string{"authorization", "x-api-key", "x-api-signature", "x-timestamp"}
	if len(reserved) != len(want) {
		t.Fatalf("ReservedHeaderNames() = %v, want exactly %v; the reserved set is "+
			"the security boundary of this feature and a missing name is a hole in it",
			reserved, want)
	}
	if !sort.StringsAreSorted(reserved) {
		t.Errorf("ReservedHeaderNames() = %v, want them sorted: the refusal and the "+
			"help text both print it in this order", reserved)
	}

	for _, name := range want {
		for _, spelling := range []string{name, strings.ToUpper(name), "X-Api-Key"} {
			if spelling != name && !IsReservedHeader(spelling) {
				continue
			}
			spec := spelling + "=sneaky-value"
			h, err := ParseHeaderSpec("-header", spec)
			if err == nil {
				t.Errorf("ParseHeaderSpec(%q) = %+v, want a refusal: the SDK applies "+
					"extra headers AFTER its own credential headers with Set, so this "+
					"would replace the credential silently", spec, h)
				continue
			}
			if !strings.Contains(err.Error(), spelling) {
				t.Errorf("refusal for %q does not name the header the operator has to "+
					"remove: %v", spec, err)
			}
			// The refusal has to be actionable without being a disclosure: the
			// name, yes; the value, never.
			if strings.Contains(err.Error(), "sneaky-value") {
				t.Errorf("refusal for %q echoed the value:\n%v", spec, err)
			}
		}
	}
}

// A near miss is not a reserved header. Over-reserving would be its own bug —
// `x-api-key-id` is a perfectly ordinary header an operator might want — so the
// boundary is pinned from both sides.
func TestIsReservedHeader_IsTheNameAndNotAPrefix(t *testing.T) {
	for _, name := range []string{
		"x-api-key-id", "x-api-keys", "my-authorization", "x-timestamp-old",
		"x-api-signature-algorithm",
	} {
		if IsReservedHeader(name) {
			t.Errorf("IsReservedHeader(%q) = true; the test is on the whole name, not "+
				"on a prefix, so an unrelated header stays usable", name)
		}
	}
	for _, name := range []string{"  Authorization ", "X-TIMESTAMP"} {
		if !IsReservedHeader(name) {
			t.Errorf("IsReservedHeader(%q) = false; case and surrounding space do not "+
				"make a reserved name a different header", name)
		}
	}
}

// ReservedHeaderNames hands out a copy. A caller that could sort or truncate the
// package's own slice could make the refusal list stop matching the check.
func TestReservedHeaderNames_ReturnsACopy(t *testing.T) {
	first := ReservedHeaderNames()
	first[0] = "mutated"
	if second := ReservedHeaderNames(); second[0] == "mutated" {
		t.Fatal("ReservedHeaderNames() handed out the package's own slice")
	}
}

// The same refusal has to come out of the environment and out of the YAML file.
// A rule enforced only on the command line would be a rule with a hole exactly
// where the interesting case is.
func TestReservedHeaders_AreRefusedFromEverySource(t *testing.T) {
	t.Run("the environment", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		t.Setenv("LONGPORT_HEADER_AUTHORIZATION", "Bearer stolen")

		_, err := Load("", WithHeaders("x-ok=1"))
		if err == nil {
			t.Fatal("Load accepted LONGPORT_HEADER_AUTHORIZATION; the environment is a " +
				"header source and the reserved check has to apply to it too")
		}
		if !strings.Contains(err.Error(), "LONGPORT_HEADER_AUTHORIZATION") {
			t.Errorf("the refusal does not name the variable to unset:\n%v", err)
		}
		if strings.Contains(err.Error(), "stolen") {
			t.Errorf("the refusal echoed the value:\n%v", err)
		}
	})

	t.Run("the YAML file", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		writeFile(t, "config.yaml",
			"longbridge:\n  app_key: k\n  app_secret: s\n  access_token: t\n"+
				"  headers:\n    x-api-key: replaced\n")

		_, err := Load("")
		if err == nil {
			t.Fatal("Load accepted a `headers:` block naming x-api-key")
		}
		if !strings.Contains(err.Error(), "x-api-key") || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("the refusal must name the header and say why:\n%v", err)
		}
		if !strings.Contains(err.Error(), "config.yaml") {
			t.Errorf("the refusal does not name the file to fix:\n%v", err)
		}
	})

	t.Run("a mixed-case spelling in the file", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		writeFile(t, "config.yaml",
			"longbridge:\n  app_key: k\n  app_secret: s\n  access_token: t\n"+
				"  headers:\n    Authorization: replaced\n")

		if _, err := Load(""); err == nil {
			t.Fatal("Load accepted `Authorization` in a `headers:` block; HTTP header " +
				"names are case-insensitive and the SDK canonicalises them")
		}
	})
}

// ------------------------------------------------------------ the flag form --

// The shape of one -header argument, including the two cases that are easy to
// get wrong: a value that contains '=', and a key with padding around it.
func TestParseHeaderSpec_SplitsOnTheFirstEquals(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		wantKey   string
		wantValue string
	}{
		{"a plain pair", "x-trace-id=abc123", "x-trace-id", "abc123"},
		{"a value containing =", "x-signature=a=b=c", "x-signature", "a=b=c"},
		{"a base64 value, which is full of = padding", "x-key=YWJjZA==", "x-key", "YWJjZA=="},
		{"an empty value", "x-empty=", "x-empty", ""},
		{"a space inside the value", "x-agent=demo 1.0", "x-agent", "demo 1.0"},
		{"padded key", "  x-padded  =v", "x-padded", "v"},
		{"a query string as a value", "x-url=https://h/p?a=1", "x-url", "https://h/p?a=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := ParseHeaderSpec("-header", tt.spec)
			if err != nil {
				t.Fatalf("ParseHeaderSpec(%q) = %v, want it accepted", tt.spec, err)
			}
			if h.Key != tt.wantKey || h.Value != tt.wantValue {
				t.Errorf("ParseHeaderSpec(%q) = %q=%q, want %q=%q",
					tt.spec, h.Key, h.Value, tt.wantKey, tt.wantValue)
			}
			if h.Origin != "-header" {
				t.Errorf("Origin = %q, want %q so the banner can say where it came from",
					h.Origin, "-header")
			}
		})
	}
}

// Every way of getting the argument wrong, and the property that matters about
// the message: it never contains the value.
func TestParseHeaderSpec_RejectsAndNeverEchoesTheValue(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr string
		secret  string
	}{
		{"no equals sign at all", "supersecretvalue", `has no "="`, "supersecretvalue"},
		{"an empty name", "=value", "name is empty", "value"},
		{"a whitespace-only name", "   =value", "name is empty", "value"},
		{"a name with a space in it", "bad name=sup3rs3cr3t", "not a valid HTTP header name", "sup3rs3cr3t"},
		{"a name with a colon", "x:a=sup3rs3cr3t", "not a valid HTTP header name", "sup3rs3cr3t"},
		{"a non-ASCII name", "x-café=sup3rs3cr3t", "not a valid HTTP header name", "sup3rs3cr3t"},
		{"a value carrying a newline", "x-a=one\ntwo", "net/http refuses to send", "two"},
		{"a value carrying a carriage return", "x-a=one\rtwo", "net/http refuses to send", "two"},
		{"a value carrying a NUL", "x-a=one\x00two", "net/http refuses to send", "two"},
		{"a value carrying a DEL", "x-a=one\x7ftwo", "net/http refuses to send", "two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := ParseHeaderSpec("-header", tt.spec)
			if err == nil {
				t.Fatalf("ParseHeaderSpec(%q) = %+v, want a refusal", tt.spec, h)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("refusal = %q, want it to contain %q so the operator knows "+
					"what to change", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Errorf("refusal for %q leaked %q into the message:\n%v",
					tt.spec, tt.secret, err)
			}
			if h.Key != "" || h.Value != "" {
				t.Errorf("a refused header must come back empty, got %+v", h)
			}
		})
	}
}

// The name table is the one place this package re-implements something net/http
// also has, so it is checked against the alphabet rather than against a handful
// of examples: every one of the 256 byte values is classified, and the accepted
// set must be exactly RFC 7230's tchar.
//
// This is also why the check is here rather than delegated to
// textproto.CanonicalMIMEHeaderKey: measured, that function returns an invalid
// name UNCHANGED and a valid one canonicalised, so "did it change" is not a
// validity test. golang.org/x/net/http/httpguts.ValidHeaderFieldName is the
// function net/http actually uses and it is not reachable from here — it is not
// in go.sum at all — so the table is spelled out against the RFC instead of
// adding a dependency for one function.
func TestValidHeaderName_IsExactlyRFC7230Token(t *testing.T) {
	for c := 0; c < 256; c++ {
		b := byte(c)
		got := validHeaderName(string([]byte{b}))
		want := false
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
			want = true
		case strings.IndexByte(tchar, b) >= 0:
			want = true
		}
		if got != want {
			t.Errorf("validHeaderName(%q) = %v, want %v", string(b), got, want)
		}
	}
	if validHeaderName("") {
		t.Error("validHeaderName(\"\") = true; net/http rejects an empty field name")
	}
	// The composed forms have to work too, since that is the shape an operator
	// actually types.
	for _, name := range []string{"x-trace-id", "X-Trace-Id", "user_agent", "a!#$%&'*+-.^_`|~z"} {
		if !validHeaderName(name) {
			t.Errorf("validHeaderName(%q) = false, want true", name)
		}
	}
}

// The value rule mirrors net/http's: no control characters except tab. Tab is
// allowed, space is not a control character and is obviously allowed, and bytes
// at or above 0x80 are allowed because net/http allows them.
func TestValidHeaderValue_MirrorsNetHTTP(t *testing.T) {
	for c := 0; c < 256; c++ {
		b := byte(c)
		got := validHeaderValue(string([]byte{b}))
		want := !((b < 0x20 && b != '\t') || b == 0x7f)
		if got != want {
			t.Errorf("validHeaderValue(%q) = %v, want %v", string(b), got, want)
		}
	}
	if !validHeaderValue("") {
		t.Error("validHeaderValue(\"\") = false; an empty value is not a malformed one")
	}
	if !validHeaderValue("a\tb c") {
		t.Error("validHeaderValue rejected a tab or a space, which net/http allows")
	}
	// The reason the value rule exists at all: a CR/LF in a value is one header
	// in the operator's intent and two on the wire.
	if validHeaderValue("x-a\r\nx-b: injected") {
		t.Error("validHeaderValue accepted a CRLF sequence, which would split the header")
	}
}

// ---------------------------------------------------------- the env source --

// The prefix scan, the underscore-to-hyphen mapping and the lower-casing, plus
// the refusal of a variable that cannot name a header at all.
func TestHeadersFromEnv(t *testing.T) {
	t.Run("every prefixed variable becomes a header", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		t.Setenv(HeaderEnvPrefix+"X_TRACE_ID", "abc123")
		t.Setenv(HeaderEnvPrefix+"USER_AGENT", "demo/1.0")

		got, err := headersFromEnv()
		if err != nil {
			t.Fatalf("headersFromEnv() = %v", err)
		}
		want := []Header{
			{Key: "user-agent", Value: "demo/1.0", Origin: HeaderEnvPrefix + "USER_AGENT"},
			{Key: "x-trace-id", Value: "abc123", Origin: HeaderEnvPrefix + "X_TRACE_ID"},
		}
		if len(got) != len(want) {
			t.Fatalf("headersFromEnv() = %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("headersFromEnv()[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("an unrelated variable is not a header", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		t.Setenv("LONGPORT_HEADERISH", "nope")
		t.Setenv("LONGPORT_HEADER", "nope")

		got, err := headersFromEnv()
		if err != nil {
			t.Fatalf("headersFromEnv() = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("headersFromEnv() = %+v, want nothing: the prefix is %q exactly",
				got, HeaderEnvPrefix)
		}
	})

	t.Run("a variable that cannot name a header is an error, not a skip", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		t.Setenv(HeaderEnvPrefix, "no-name")

		_, err := headersFromEnv()
		if err == nil {
			t.Fatal("headersFromEnv() accepted a variable with no name after the prefix; " +
				"a silent skip would look exactly like the variable being ignored")
		}
		if !strings.Contains(err.Error(), HeaderEnvPrefix) {
			t.Errorf("the refusal does not name the variable:\n%v", err)
		}
	})

	t.Run("a value with a newline in it is refused", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		t.Setenv(HeaderEnvPrefix+"X_A", "one\ntwo")

		if _, err := headersFromEnv(); err == nil {
			t.Error("headersFromEnv() accepted a value containing a newline")
		}
	})
}

// -------------------------------------------------------------- precedence --

// File, then environment, then flag; last one wins within a source. One test for
// all of it, because the rule is one rule and the three sources failing
// differently is what would make it useless.
func TestResolveHeaders_PrecedenceAndFolding(t *testing.T) {
	tests := []struct {
		name  string
		file  []Header
		env   []Header
		flag  []Header
		want  map[string]string
		order []string
	}{
		{
			name: "the file alone",
			file: []Header{{Key: "x-a", Value: "from-file"}},
			want: map[string]string{"x-a": "from-file"},
		},
		{
			name: "the environment beats the file",
			file: []Header{{Key: "x-a", Value: "from-file"}},
			env:  []Header{{Key: "x-a", Value: "from-env"}},
			want: map[string]string{"x-a": "from-env"},
		},
		{
			name: "the flag beats both",
			file: []Header{{Key: "x-a", Value: "from-file"}},
			env:  []Header{{Key: "x-a", Value: "from-env"}},
			flag: []Header{{Key: "x-a", Value: "from-flag"}},
			want: map[string]string{"x-a": "from-flag"},
		},
		{
			name: "the last entry within one source wins",
			flag: []Header{{Key: "x-a", Value: "first"}, {Key: "x-a", Value: "second"}},
			want: map[string]string{"x-a": "second"},
		},
		{
			name: "spellings that differ only in case are one header",
			file: []Header{{Key: "x-a", Value: "from-file"}},
			flag: []Header{{Key: "X-A", Value: "from-flag"}},
			want: map[string]string{"x-a": "from-flag"},
		},
		{
			name:  "unrelated names from every source all survive",
			file:  []Header{{Key: "x-file", Value: "1"}},
			env:   []Header{{Key: "x-env", Value: "2"}},
			flag:  []Header{{Key: "x-flag", Value: "3"}},
			want:  map[string]string{"x-file": "1", "x-env": "2", "x-flag": "3"},
			order: []string{"x-env", "x-file", "x-flag"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveHeaders(tt.file, tt.env, tt.flag)
			if len(got) != len(tt.want) {
				t.Fatalf("resolveHeaders() = %+v, want %d headers %v", got, len(tt.want), tt.want)
			}
			for _, h := range got {
				if want, ok := tt.want[h.Key]; !ok {
					t.Errorf("resolveHeaders() produced %q, which is not one of %v", h.Key, tt.want)
				} else if h.Value != want {
					t.Errorf("%s = %q, want %q", h.Key, h.Value, want)
				}
			}
			if tt.order != nil {
				var keys []string
				for _, h := range got {
					keys = append(keys, h.Key)
				}
				if strings.Join(keys, ",") != strings.Join(tt.order, ",") {
					t.Errorf("resolveHeaders() order = %v, want %v; the banner prints "+
						"these, so their order has to be stable", keys, tt.order)
				}
			}
		})
	}
}

// The reason for the case folding is a nondeterminism in the SDK, and this is
// what it looks like from here: two spellings must never both reach the map,
// because the SDK iterates it and the last Set wins.
func TestResolveHeaders_NeverLeavesTwoSpellingsOfOneHeader(t *testing.T) {
	got := resolveHeaders(
		[]Header{{Key: "x-a", Value: "lower"}},
		nil,
		[]Header{{Key: "X-A", Value: "upper"}},
	)
	if len(got) != 1 {
		t.Fatalf("resolveHeaders() = %+v, want exactly one header; the SDK stores "+
			"these in a map and iterates it, so two spellings would race", got)
	}
	if got[0].Key != "x-a" || got[0].Value != "upper" {
		t.Errorf("resolveHeaders() = %+v, want x-a=upper", got[0])
	}
}

// -------------------------------------------------------------- end to end --

// The flag reaches the SDK. This is the whole feature: the header is not merely
// recorded on the Config, it is in the SDK's ExtraHeaders map, which is what
// http.Client.Call iterates for every request.
func TestLoad_FlagHeadersReachTheSDKConfig(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	cfg, err := Load("", WithHeaders("x-trace-id=abc123", "x-tenant=demo"))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	want := map[string]string{"x-trace-id": "abc123", "x-tenant": "demo"}
	if cfg.SDK.ExtraHeaders == nil {
		t.Fatal("SDK.ExtraHeaders is nil; WithHeader was never called")
	}
	if len(cfg.SDK.ExtraHeaders) != len(want) {
		t.Fatalf("SDK.ExtraHeaders = %v, want %v", cfg.SDK.ExtraHeaders, want)
	}
	for k, v := range want {
		if cfg.SDK.ExtraHeaders[k] != v {
			t.Errorf("SDK.ExtraHeaders[%q] = %q, want %q", k, cfg.SDK.ExtraHeaders[k], v)
		}
	}
	if len(cfg.Headers) != 2 {
		t.Errorf("Config.Headers = %+v, want the two that were passed", cfg.Headers)
	}
	for _, h := range cfg.Headers {
		if h.Origin != "-header" {
			t.Errorf("header %q has Origin %q, want %q", h.Key, h.Origin, "-header")
		}
	}
}

// The three sources, end to end, with the precedence visible in what the SDK
// ends up holding. No client is built and no request is made: Load only
// configures.
func TestLoad_AllThreeHeaderSourcesAndTheirPrecedence(t *testing.T) {
	t.Run("the YAML headers block", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		writeFile(t, "config.yaml", "longbridge:\n  headers:\n    x-tenant: from-file\n")

		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load = %v", err)
		}
		if got := cfg.SDK.ExtraHeaders["x-tenant"]; got != "from-file" {
			t.Errorf("x-tenant = %q, want the file's value", got)
		}
		if len(cfg.Headers) != 1 || cfg.Headers[0].Origin != "config.yaml" {
			t.Errorf("Config.Headers = %+v, want one header sourced from config.yaml", cfg.Headers)
		}
	})

	t.Run("the environment", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		t.Setenv(HeaderEnvPrefix+"X_TENANT", "from-env")

		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load = %v", err)
		}
		if got := cfg.SDK.ExtraHeaders["x-tenant"]; got != "from-env" {
			t.Errorf("x-tenant = %q, want the environment's value", got)
		}
		if len(cfg.Headers) != 1 || cfg.Headers[0].Origin != HeaderEnvPrefix+"X_TENANT" {
			t.Errorf("Config.Headers = %+v, want one header sourced from the variable", cfg.Headers)
		}
	})

	t.Run("the environment beats the file, and the flag beats both", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		writeFile(t, "config.yaml", "longbridge:\n  headers:\n    x-tenant: from-file\n")
		t.Setenv(HeaderEnvPrefix+"X_TENANT", "from-env")

		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load = %v", err)
		}
		if got := cfg.SDK.ExtraHeaders["x-tenant"]; got != "from-env" {
			t.Errorf("x-tenant = %q, want the environment's value; the documented rule "+
				"is that the environment beats the file", got)
		}

		cfg, err = Load("", WithHeaders("x-tenant=from-flag"))
		if err != nil {
			t.Fatalf("Load with a flag = %v", err)
		}
		if got := cfg.SDK.ExtraHeaders["x-tenant"]; got != "from-flag" {
			t.Errorf("x-tenant = %q, want the flag's value", got)
		}
	})

	t.Run("a headers entry with no value is refused", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)
		writeFile(t, "config.yaml", "longbridge:\n  headers:\n    x-tenant: \"  \"\n")

		_, err := Load("")
		if err == nil {
			t.Fatal("Load accepted a headers entry with a blank value; sending an empty " +
				"header is not the same as sending none, so the mistake has to be named")
		}
		if !strings.Contains(err.Error(), "x-tenant") {
			t.Errorf("the refusal does not name the entry:\n%v", err)
		}
	})

	t.Run("a malformed -header argument is a startup error, not a credential one", func(t *testing.T) {
		sandbox(t)
		clearHeaderEnv(t)
		credentials(t)

		_, err := Load("", WithHeaders("nonsense"))
		if err == nil {
			t.Fatal("Load accepted -header nonsense")
		}
		var missing *MissingCredentialError
		if asMissingCredentialError(err, &missing) {
			t.Errorf("err = %T; a malformed header is a plain error and must not exit 2",
				err)
		}
	})
}

// With nothing configured there is nothing to apply, and the SDK's own field
// stays nil so its request loop skips the extra-header loop entirely.
func TestLoad_NoHeadersMeansTheSDKFieldIsUntouched(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(cfg.Headers) != 0 {
		t.Errorf("Config.Headers = %+v, want none", cfg.Headers)
	}
	if cfg.SDK.ExtraHeaders != nil {
		t.Errorf("SDK.ExtraHeaders = %v, want nil; the SDK only allocates it when "+
			"WithHeader is called", cfg.SDK.ExtraHeaders)
	}
	if strings.Contains(cfg.String(), "header[") {
		t.Errorf("the banner reports headers that were never configured:\n%s", cfg.String())
	}
}

// The OAuth loader resolves headers the same way, because a command that
// authenticates differently still talks to the same API with the same headers.
func TestLoadOAuth_ResolvesHeadersToo(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	t.Setenv("LONGPORT_HEADER_X_TRACE_ID", "abc")

	cfg, err := LoadOAuth(oauth.New("client-id"), "", WithHeaders("x-tenant=demo"))
	if err != nil {
		t.Fatalf("LoadOAuth = %v", err)
	}
	if got := cfg.SDK.ExtraHeaders["x-trace-id"]; got != "abc" {
		t.Errorf("x-trace-id = %q, want the environment's value", got)
	}
	if got := cfg.SDK.ExtraHeaders["x-tenant"]; got != "demo" {
		t.Errorf("x-tenant = %q, want the flag's value", got)
	}
	// And the reserved check is not a property of the credential path.
	if _, err := LoadOAuth(oauth.New("client-id"), "", WithHeaders("authorization=x")); err == nil {
		t.Error("LoadOAuth accepted a reserved header; the check belongs to the " +
			"configuration, not to one way of authenticating")
	}
}

// ---------------------------------------------------------------- masking ---

// Exactly which names are masked, and that nothing else is. This is the rule a
// reader of the banner has to be able to apply to it.
func TestSensitiveHeaderKey_MatchesTheDocumentedFragments(t *testing.T) {
	masked := []string{
		"x-token", "token", "X-ACCESS-TOKEN", "x_trace_token",
		"x-secret", "SECRET", "x-api-key", "monkey-bag", // "key" as a substring
		"authorization", "x-auth-thing", "auth",
		"pass", "x-passwd", "password",
		"credential", "x-credential-id", "credentials",
		"cookie", "x-cookie",
		// Over-masking is the safe direction and is deliberate: a name that
		// merely CONTAINS a fragment is treated as credential-bearing even when
		// it is not — x-passenger contains "pass", x-keynote contains "key",
		// x-author contains "auth". The cost is a masked line in the banner; the
		// alternative is a printed credential when somebody names a header
		// something nobody predicted.
		"x-passenger", "x-keynote", "x-author",
	}
	for _, key := range masked {
		if !SensitiveHeaderKey(key) {
			t.Errorf("SensitiveHeaderKey(%q) = false, want true; the name carries a "+
				"credential fragment and must never be printed in full", key)
		}
	}

	shown := []string{
		"x-trace-id", "x-tenant", "x-env", "user-agent", "accept-language",
		"x-request-source", "x-idea", "x-locale", "content-language",
	}
	for _, key := range shown {
		if SensitiveHeaderKey(key) {
			t.Errorf("SensitiveHeaderKey(%q) = true, want false; a name with no "+
				"credential fragment is printed in full, which is the other half of "+
				"the documented rule", key)
		}
	}
}

// What the operator actually sees. The masked form must be Redact's fixed-width
// one — the same shape the app key uses on the same line — and the unmasked one
// must be the value, byte for byte.
func TestRedactHeaderValue(t *testing.T) {
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

	got := RedactHeaderValue("x-access-token", secret)
	if got == secret {
		t.Fatal("a credential-looking name was printed in full")
	}
	if want := Redact(secret); got != want {
		t.Errorf("RedactHeaderValue = %q, want %q — the same mask the app key uses, so "+
			"one reader knows both", got, want)
	}
	if strings.Contains(got, "012345") || strings.Contains(got, "ghp_a") {
		t.Errorf("the mask leaked a recognisable part of the value: %q", got)
	}

	if got := RedactHeaderValue("x-trace-id", "abc123"); got != "abc123" {
		t.Errorf("RedactHeaderValue on a plain name = %q, want the value unchanged", got)
	}
	// An empty value still renders as the unset marker rather than as nothing,
	// so "set to empty" and "not set" stay distinguishable in the banner.
	if got := RedactHeaderValue("x-token", ""); got != "<unset>" {
		t.Errorf("RedactHeaderValue on an empty credential value = %q, want %q", got, "<unset>")
	}
}

// The banner is the one line every command prints before it does anything, so it
// is where a header has to be visible — and where the masking decision is
// checkable.
func TestConfigString_ReportsHeadersWithTheMaskingApplied(t *testing.T) {
	cfg := newTestConfig(ModeSimulated, true)
	cfg.Headers = []Header{
		{Key: "x-trace-id", Value: "abc123", Origin: "-header"},
		{Key: "x-access-token", Value: "ghp_abcdefghijklmnopqrstuvwxyz", Origin: HeaderEnvPrefix + "X_ACCESS_TOKEN"},
	}
	msg := cfg.String()

	if !strings.Contains(msg, "header[x-trace-id]=abc123(from -header)") {
		t.Errorf("the banner does not show the plain header with its source:\n%s", msg)
	}
	if !strings.Contains(msg, "header[x-access-token]="+Redact(cfg.Headers[1].Value)) {
		t.Errorf("the banner does not show the masked header:\n%s", msg)
	}
	if strings.Contains(msg, "ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("the banner printed a credential-bearing header value in full:\n%s", msg)
	}
	// The source is what makes a surprising header explainable, so it is printed
	// rather than only recorded.
	if !strings.Contains(msg, HeaderEnvPrefix+"X_ACCESS_TOKEN") {
		t.Errorf("the banner does not say which variable set the header:\n%s", msg)
	}
}

// Nothing here may make a request, and no header may reach a request without
// going through the SDK config Load built.
func TestLoad_HeaderWorkNeverBuildsAClient(t *testing.T) {
	sandbox(t)
	clearHeaderEnv(t)
	credentials(t)

	cfg, err := Load("", WithHeaders("x-trace-id=abc123"))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.SDK.Client != nil {
		t.Error("Load installed an HTTP client; that is the caller's job and no test " +
			"here should be able to make a request")
	}
	if len(cfg.SDK.ExtraHeaders) != 1 {
		t.Errorf("SDK.ExtraHeaders = %v, want exactly the one header", cfg.SDK.ExtraHeaders)
	}
}
