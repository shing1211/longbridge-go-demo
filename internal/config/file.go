package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlExt is the only file extension this package loads. The SDK can also read
// .toml and .env, but it only ever sees the path this package hands it, and
// the credential top-up below is YAML-only — so advertising the other formats
// in the candidate list produced files that either failed startup (config.yml,
// which the SDK's configTypeMap does not know) or silently supplied no
// credential at all (config.toml, which is not parsed here). YAML is the format
// this project can actually honour, so it is the only one it offers.
const yamlExt = ".yaml"

// candidateFiles are probed, in order, when no explicit path is given.
var candidateFiles = []string{"config.yaml", "config.local.yaml"}

// fileCreds mirrors the credential fields the SDK understands inside a
// `longbridge:` block, plus the one block this demo adds for itself. Only
// credentials and headers are read here; every other field is left to the SDK's
// own parser, which is invoked later with WithFilePath.
//
// The headers mapping is NOT a field of the SDK's configuration struct, so the
// SDK's own reader ignores it — verified in openapi-go v0.25.2 config/config.go:
// the struct has no yaml tag on ExtraHeaders and no `headers` key, and yaml.v3
// ignores keys it does not know. The headers are applied afterwards through the
// SDK's own WithHeader, which is the only route that lets this project refuse a
// reserved name before it reaches the wire.
type fileCreds struct {
	Longbridge *struct {
		AppKey      string            `yaml:"app_key"`
		AppSecret   string            `yaml:"app_secret"`
		AccessToken string            `yaml:"access_token"`
		Headers     map[string]string `yaml:"headers"`
	} `yaml:"longbridge"`
}

// loadFileInto fills any still-empty entry in vals from a YAML file, appends the
// file's `headers:` mapping to headers, and returns the path actually used ("" if
// none).
//
// headers may be nil, which means "the caller does not want them"; nothing is
// collected and nothing is validated in that case. It is an out-parameter rather
// than a second return value so that the credentials and the headers come out of
// one parse of one file — a second read of the same file would exist only to
// keep this signature at two arguments.
//
// The file is optional. An explicit path that does not exist IS an error, so
// that a typo in --config does not silently fall back to env vars. An explicit
// path that is not a .yaml file is also an error, and is reported as a plain
// error rather than as a credential problem: nothing is missing, the request
// itself is one this loader cannot serve.
//
// A header from the file is validated exactly like one from the command line,
// and its Origin is the path rather than a fixed word, so a rejection quotes the
// file to go and look in.
func loadFileInto(vals map[string]string, headers *[]Header, explicit string) (string, error) {
	paths := candidateFiles
	if explicit != "" {
		// Checked before the stat below, so a wrong format is reported as a
		// wrong format even when the file does not exist.
		if ext := filepath.Ext(explicit); ext != yamlExt {
			return "", fmt.Errorf(
				"config file %q: this demo reads YAML only, so the file name must end in %s (got %q). "+
					"The Longbridge SDK can also parse TOML and .env, but that reader is not wired up here. "+
					"Convert the file (see config.example.yaml), rename it to %s, "+
					"or supply LONGBRIDGE_APP_KEY, LONGBRIDGE_APP_SECRET and LONGBRIDGE_ACCESS_TOKEN in the environment",
				explicit, yamlExt, ext, strings.TrimSuffix(filepath.Base(explicit), ext)+yamlExt)
		}
		paths = []string{explicit}
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			if explicit != "" {
				return "", fmt.Errorf("config file %q: %w", p, err)
			}
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("reading config file %q: %w", p, err)
		}
		var parsed fileCreds
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			if explicit != "" {
				return "", fmt.Errorf("parsing YAML config %q: %w", p, err)
			}
			continue
		}
		if parsed.Longbridge != nil {
			for k, v := range map[string]string{
				"app_key":      parsed.Longbridge.AppKey,
				"app_secret":   parsed.Longbridge.AppSecret,
				"access_token": parsed.Longbridge.AccessToken,
			} {
				if strings.TrimSpace(vals[k]) == "" && strings.TrimSpace(v) != "" {
					vals[k] = v
				}
			}
			if err := appendFileHeaders(headers, p, parsed.Longbridge.Headers); err != nil {
				return "", err
			}
		}
		return p, nil
	}
	return "", nil
}

// appendFileHeaders validates and collects the `headers:` mapping.
//
// An entry whose value is blank is refused rather than sent as an empty header:
// sending one and sending none look identical to the operator and are not the
// same thing to the far end, and a blank here is a mistake worth naming.
//
// Names are walked in sorted order so that a file with two broken entries
// always reports the same one first.
func appendFileHeaders(out *[]Header, path string, in map[string]string) error {
	if out == nil || len(in) == 0 {
		return nil
	}
	names := make([]string, 0, len(in))
	for k := range in {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := in[k]
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("config file %q: longbridge.headers[%q] has no value; "+
				"remove the entry or give it one", path, k)
		}
		h := Header{Key: k, Value: v, Origin: path}
		if err := validateHeader(h); err != nil {
			return err
		}
		*out = append(*out, h)
	}
	return nil
}
