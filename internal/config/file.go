package config

import (
	"fmt"
	"os"
	"path/filepath"
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
// `longbridge:` block. Only credentials are read here; every other field is
// left to the SDK's own parser, which is invoked later with WithFilePath.
type fileCreds struct {
	Longbridge *struct {
		AppKey      string `yaml:"app_key"`
		AppSecret   string `yaml:"app_secret"`
		AccessToken string `yaml:"access_token"`
	} `yaml:"longbridge"`
}

// loadFileInto fills any still-empty entry in vals from a YAML file, and
// returns the path actually used ("" if none).
//
// The file is optional. An explicit path that does not exist IS an error, so
// that a typo in --config does not silently fall back to env vars. An explicit
// path that is not a .yaml file is also an error, and is reported as a plain
// error rather than as a credential problem: nothing is missing, the request
// itself is one this loader cannot serve.
func loadFileInto(vals map[string]string, explicit string) (string, error) {
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
		}
		return p, nil
	}
	return "", nil
}
