// Package yarnrc parses just enough of a yarn `.yarnrc.yml` to extract the
// settings locksmith needs for byte-accurate lockfile generation.
//
// Right now that's only `compressionLevel`, which determines the v8
// lockfile's `cacheKey` suffix. More settings (e.g. `enableHardenedMode`,
// `defaultProtocol`) can be added here as locksmith starts honoring them.
package yarnrc

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ReadCompressionLevel reads `compressionLevel` from the .yarnrc.yml at path
// and returns it as a string. Returns ("", nil) when the key is absent or
// the value is null. Integer values are stringified ("0", "9"); string values
// are returned as-is (e.g. "mixed").
func ReadCompressionLevel(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg struct {
		CompressionLevel any `yaml:"compressionLevel"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	switch v := cfg.CompressionLevel.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		// YAML decodes large numbers as float64; only accept integer values.
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return "", fmt.Errorf("compressionLevel must be integer or \"mixed\", got %v", v)
	default:
		return "", fmt.Errorf("compressionLevel must be integer or string, got %T", v)
	}
}
