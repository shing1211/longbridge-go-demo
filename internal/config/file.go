package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// candidateFiles are probed, in order, when no explicit path is given.
var candidateFiles = []string{"config.yaml", "config.local.yaml", "config.yml", "config.toml"}

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
// that a typo in --config does not silently fall back to env vars.
func loadFileInto(vals map[string]string, yamlKeys []string, explicit string) (string, error) {
	paths := candidateFiles
	if explicit != "" {
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
		// TOML is intentionally not parsed here. The SDK supports it via
		// WithFilePath; we only add YAML top-up so credentials can come from
		// either source with a single code path.
		if filepath.Ext(p) != ".toml" {
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
		}
		return p, nil
	}
	return "", nil
}
