package main

import (
	"os"
	"path/filepath"

	"github.com/jumoel/locksmith/npm"
	"gopkg.in/yaml.v3"
)

// discoverWorkspaceMembers auto-detects workspace members from the spec file.
// Returns nil if the project is not a workspace.
func discoverWorkspaceMembers(specPath string, specData []byte) (map[string][]byte, error) {
	specDir := filepath.Dir(specPath)

	// Try workspace globs from package.json first.
	globs, err := npm.ParseWorkspaceGlobs(specData)
	if err != nil {
		return nil, err
	}

	// If no workspaces in package.json, check for pnpm-workspace.yaml.
	if len(globs) == 0 {
		pnpmPath := filepath.Join(specDir, "pnpm-workspace.yaml")
		if data, err := os.ReadFile(pnpmPath); err == nil {
			globs, _ = parsePnpmWorkspaceYaml(data)
		}
	}

	if len(globs) == 0 {
		return nil, nil
	}

	members := make(map[string][]byte)
	for _, glob := range globs {
		pattern := filepath.Join(specDir, glob)
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			pkgPath := filepath.Join(match, "package.json")
			data, err := os.ReadFile(pkgPath)
			if err != nil {
				continue
			}
			relPath, _ := filepath.Rel(specDir, match)
			members[relPath] = data
		}
	}

	if len(members) == 0 {
		return nil, nil
	}
	return members, nil
}

// parsePnpmWorkspaceYaml extracts workspace globs from pnpm-workspace.yaml.
func parsePnpmWorkspaceYaml(data []byte) ([]string, error) {
	var config struct {
		Packages []string `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return config.Packages, nil
}
