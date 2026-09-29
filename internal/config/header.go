package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// HeaderEnvPrefix is the prefix an environment variable must carry to become an
// extra HTTP header. Everything after the prefix is the header name with each
// underscore read as a hyphen, lower-cased:
//
//	LONGPORT_HEADER_X_TRACE_ID=abc123  ->  x-trace-id: abc123
//
// A prefix scan rather than one variable per possible header, because the set
// of headers an operator may want to add is not knowable in advance: adding a
// new one must not mean editing this file. It is the same reason -header is a
// repeatable flag rather than a fixed set of string flags.
//
// The mapping is lossy in exactly one direction — a header name that genuinely
// contains an underscore cannot be spelled through the environment, because an
// underscore is read as a hyphen. -header and the YAML `headers:` block can both
// spell one, and HTTP header names containing "_" are rare enough that the
// environment form is a convenience rather than the only way in.
const HeaderEnvPrefix = "LONGPORT_HEADER_"

// Header is one extra HTTP header the SDK will attach to every request it makes.
//
// Origin names the source that supplied it — the flag `-header`, the environment
// variable, or the file path — and appears in the startup banner, because a
// header is a thing an operator did not necessarily ask for in this particular
// invocation and the source is the first question worth answering when one
// misbehaves. It is also the prefix of any rejection, so a header that cannot be
// used says where it came from without having to say what it carried.
type Header struct {
	Key    string
	Value  string
	Origin string
}

// reservedHeaders are the header names no caller may set.
//
// # WHY, WHICH IS THE WHOLE POINT OF THIS FILE
//
// openapi-go v0.25.2 http/client.go, Call(), builds each request like this:
//
//	req.Header.Add("x-api-key", appKey)
//	req.Header.Add("authorization", accessToken)
//	for k, v := range c.opts.ExtraHeaders {
//		req.Header.Set(k, v)
//	}
//	…
//	signature(req, appSecret, bb)   // sets x-timestamp and x-api-signature
//
// `Set` replaces every value already stored under that name, and it runs AFTER
// the SDK has added the credential headers. So an extra header called
// `authorization` is not a second credential, it is the only one the request
// would carry — silently, with no error anywhere and no way for the caller to
// tell from the response which account was addressed. A caller that "just
// wanted to pass a header" could therefore sign a request with a credential the
// operator never loaded, or send a real one to an environment they did not
// choose.
//
// x-api-signature and x-timestamp are reserved for the same `Set`-after-`Add`
// reason and with a different consequence. http/signature.go only adds
// x-timestamp when it is empty — it reads the outgoing header first and skips
// the assignment when a value is already there — and computes the HMAC over
// authorization, x-api-key and x-timestamp, so a
// caller-supplied x-timestamp is signed as if it were real: the request is
// rejected rather than mis-authenticated, which is the good case, and either way
// the header is the SDK's to own.
//
// Case does not save you. http.Header.Set canonicalises the name, so
// `-header Authorization=x` and `-header authorization=x` land in the same slot;
// the comparison here is therefore case-insensitive.
//
// Deliberately NOT reserved, with the reason for each, because an unexamined
// list is indistinguishable from an incomplete one:
//   - content-type. The SDK `Add`s its own AFTER the extra-header loop, so a
//     caller's value stays and the request ends up carrying two content types.
//     Wrong, but not a credential and not a signature.
//   - accept-language. The SDK `Add`s it before the loop, so a caller's value
//     replaces it. That is the documented purpose of LONGPORT_LANGUAGE, so this
//     is the one shadowable header an operator might legitimately want.
var reservedHeaders = []string{
	"authorization",
	"x-api-key",
	"x-api-signature",
	"x-timestamp",
}

// ReservedHeaderNames returns the reserved header names, sorted, for help text
// and for the refusal message. It returns a copy: the caller cannot reach the
// package-level slice and mutate the list.
func ReservedHeaderNames() []string {
	out := make([]string, len(reservedHeaders))
	copy(out, reservedHeaders)
	sort.Strings(out)
	return out
}

// IsReservedHeader reports whether key names a header the SDK owns. The
// comparison is on the lower-cased name, because HTTP header names are
// case-insensitive and the SDK canonicalises them before use.
func IsReservedHeader(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, r := range reservedHeaders {
		if lower == r {
			return true
		}
	}
	return false
}

// sensitiveHeaderKeyParts are the substrings that make a header NAME look like
// it carries a credential. The test is on the name only, never on the value:
// the value is by definition opaque, so there is nothing in it to inspect, and
// guessing from content would be both unreliable and a way to leak by accident.
//
// The list is a list of name fragments rather than a rule ("anything not
// credential-bearing is fine"), so a name that matches is masked even if it
// carries something harmless, and a name that does not match is printed even if
// it does carry a credential. The second half of that sentence is why the
// environment and YAML sources exist: a secret you cannot name well should not
// be on a command line, where it lands in the shell history and in ps.
var sensitiveHeaderKeyParts = []string{
	"token", "secret", "key", "auth", "pass", "credential", "cookie",
}

// SensitiveHeaderKey reports whether a header name looks like it carries a
// credential, and is therefore never printed with its value in full.
//
// VERIFIED against the same rule the rest of this file uses: the name is
// lower-cased and then searched for any of sensitiveHeaderKeyParts as a
// substring, so "X-Trace-Token", "x-trace_token" and "SECRET" all match, and
// "x-trace-id", "x-env" and "x-tenant" do not.
func SensitiveHeaderKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range sensitiveHeaderKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// RedactHeaderValue returns the printable form of a header value.
//
// What it does, precisely:
//   - a name that looks credential-bearing gets Redact(value): at most four
//     leading and two trailing characters, a fixed run of six mask characters,
//     and never the whole value. An empty value renders as the unset marker.
//   - any other name gets the value unchanged, in full.
//
// So the rule is a pure function of the NAME, and the name is printed next to
// the result. A reader holding a `[config]` banner can therefore tell exactly
// which values were shortened and which were not, by reading the name — no
// guessing from the shape of the value, which a real credential can imitate.
func RedactHeaderValue(key, value string) string {
	if SensitiveHeaderKey(key) {
		return Redact(value)
	}
	return value
}

// ParseHeaderSpec turns one `NAME=VALUE` argument into a Header.
//
// Split on the FIRST '=' only, because values legitimately contain one: a
// signature, a base64 blob, a query string. Trimming is on the name and not on
// the value, since leading or trailing spaces in a header value are content.
//
// source is both the prefix of any rejection and the Origin recorded in the
// banner, so a header that misbehaves can be traced to the switch, the variable
// or the file that set it. Callers pass "-header", the variable name, or the
// file path accordingly.
//
// # WHY THE ERROR NEVER QUOTES THE ARGUMENT
//
// The argument holds the value, and the value may be a credential. The flag
// package's own wrapper for a Set error is
// `invalid value %q for flag -%s: %v`, which would echo it, so every message
// here names the header and the problem and quotes neither side's value. The
// one thing that is quoted is the NAME, which is not a secret and is what the
// operator has to go and fix.
func ParseHeaderSpec(source, spec string) (Header, error) {
	eq := strings.Index(spec, "=")
	if eq < 0 {
		return Header{}, fmt.Errorf(
			"%s: want NAME=VALUE with exactly one header per flag, and this "+
				"argument has no %q. The argument is not shown here because it may "+
				"be a credential", source, "=")
	}
	name := strings.TrimSpace(spec[:eq])
	if name == "" {
		return Header{}, fmt.Errorf(
			"%s: the header name is empty; write NAME=VALUE", source)
	}
	h := Header{Key: name, Value: spec[eq+1:], Origin: source}
	if err := validateHeader(h); err != nil {
		return Header{}, err
	}
	return h, nil
}

// validateHeader applies every rule that protects the credential headers and
// the wire format, to a header from ANY source. It is called once per source on
// purpose: a reserved name supplied through the environment or through
// config.yaml is exactly as dangerous as one supplied on the command line, and
// a rule only the flag path enforced would be a rule with a hole in it.
func validateHeader(h Header) error {
	if !validHeaderName(h.Key) {
		return fmt.Errorf(
			"%s: %q is not a valid HTTP header name. A name is a non-empty run of "+
				"letters, digits and !#$%%&'*+-.^_`|~ (RFC 7230 token); net/http "+
				"refuses the request outright if the name is not one of those",
			h.Origin, h.Key)
	}
	if IsReservedHeader(h.Key) {
		return fmt.Errorf(
			"%s: %q is reserved and cannot be set. The SDK attaches x-api-key, "+
				"authorization, x-timestamp and x-api-signature itself, and applies "+
				"extra headers afterwards with Set, so a header of the same name "+
				"would silently REPLACE the credential rather than add to it",
			h.Origin, h.Key)
	}
	if !validHeaderValue(h.Value) {
		return fmt.Errorf(
			"%s: the value for %q contains a byte net/http refuses to send in a "+
				"header value (any control character other than tab). The value is "+
				"not shown here because it may be a credential", h.Origin, h.Key)
	}
	return nil
}

// tchar is the RFC 7230 token alphabet, spelled out so the table below can be
// read against the specification rather than against the source.
//
//	tchar = "!" / "#" / "$" / "%" / "&" / "'" / "*" / "+" / "-" / "." /
//	        "^" / "_" / "`" / "|" / "~" / DIGIT / ALPHA
//
// net/http checks exactly this set before it dials: Transport.roundTrip calls
// validateHeaders, which reports `net/http: invalid header field name "..."` for
// a name outside it. Checking here instead means the operator finds out at
// flag-parse time, naming the flag, rather than at the first request — and the
// error text is one they can act on instead of a bare request failure.
const tchar = "!#$%&'*+-.^_`|~"

var headerNameTable = buildHeaderNameTable()

func buildHeaderNameTable() [256]bool {
	var t [256]bool
	for c := 'a'; c <= 'z'; c++ {
		t[c] = true
	}
	for c := 'A'; c <= 'Z'; c++ {
		t[c] = true
	}
	for c := '0'; c <= '9'; c++ {
		t[c] = true
	}
	for i := 0; i < len(tchar); i++ {
		t[tchar[i]] = true
	}
	return t
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !headerNameTable[name[i]] {
			return false
		}
	}
	return true
}

// validHeaderValue mirrors net/http's rule for a field value: every control
// character is refused except tab (space is not a control character and is
// allowed). This is what stops a value carrying a CR/LF from being one header
// on the wire and two in the operator's intent.
//
// Bytes at or above 0x80 are accepted, because net/http accepts them; refusing
// them here would be a rule this project invented, and an invented rule that
// rejects something the SDK would have sent is its own kind of surprise.
//
// The rule net/http applies is Transport.validateHeaders', whose own comment on
// the value check is worth repeating here because it is the same argument:
// "Don't include the value in the error, because it may be sensitive."
func validHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b < 0x20 && b != '\t' {
			return false
		}
		if b == 0x7f {
			return false
		}
	}
	return true
}

// resolveHeaders collapses the three sources into the header set to apply.
//
// Precedence, lowest first, which matches the rule the credentials already use
// ("the environment beats the file") extended by one step:
//
//	YAML  longbridge.headers:
//	env   LONGPORT_HEADER_<NAME>
//	flag  -header NAME=VALUE
//
// The sources are passed in that order and folded in that order, so a later
// source replaces an earlier one for the same name and — because the fold is
// per entry — a later entry replaces an earlier entry for the same name within
// one source too. `-header x-a=1 -header x-a=2` therefore sends 2, by the same
// rule that makes the environment beat the file.
//
// Names are lower-cased as they are collected, and that is not cosmetic. The
// SDK stores the set in a map[string]string and iterates it, calling
// req.Header.Set for each entry, so `x-a` and `X-A` would both be present and
// the winner would be whichever the map iteration happened to reach last. A
// header name that differs only in case is one header, so it is folded here
// where the result is deterministic; http.Header.Set canonicalises the name
// again before it goes on the wire, whatever spelling is stored.
func resolveHeaders(sources ...[]Header) []Header {
	index := map[string]int{}
	out := []Header{}
	for _, src := range sources {
		for _, h := range src {
			key := strings.ToLower(h.Key)
			h.Key = key
			if i, seen := index[key]; seen {
				out[i] = h
				continue
			}
			index[key] = len(out)
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// headersFromEnv reads every LONGPORT_HEADER_* variable in the process
// environment.
//
// The whole environment is walked rather than a fixed list, and that is what
// makes this a source an operator can extend without editing code: a header
// nobody anticipated can still be set. Every variable found is validated like
// any other header, so an unrepresentable one (LONGPORT_HEADER_=x, or a suffix
// containing a byte no header name may hold) is a startup error naming the
// variable — never a silent skip, which would be indistinguishable from the
// variable never having been read.
func headersFromEnv() ([]Header, error) {
	var out []Header
	for _, kv := range os.Environ() {
		eq := strings.Index(kv, "=")
		if eq < 0 {
			continue
		}
		name, value := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(name, HeaderEnvPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(name, HeaderEnvPrefix)
		h := Header{
			Key:    strings.ToLower(strings.ReplaceAll(suffix, "_", "-")),
			Value:  value,
			Origin: name,
		}
		if err := validateHeader(h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
