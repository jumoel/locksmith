package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumoel/locksmith"
	"github.com/spf13/cobra"
)

// version is set by ldflags at build time.
var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "locksmith",
		Short: "Generate lockfiles from package spec files",
	}
	root.AddCommand(generateCmd())
	root.AddCommand(versionCmd())
	return root
}

func generateCmd() *cobra.Command {
	var (
		specPath        string
		format          string
		cutoffStr       string
		registryURL     string
		outputPath      string
		platform        string
		nodeVersion     string
		scopeRegistries []string
		authTokens      []string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a lockfile from a spec file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read spec file
			specData, err := os.ReadFile(specPath)
			if err != nil {
				return fmt.Errorf("reading spec file: %w", err)
			}

			// Parse output format
			outputFormat := locksmith.OutputFormat(format)
			if !isValidFormat(outputFormat) {
				return fmt.Errorf("unknown format %q, valid formats: %s", format, validFormatsStr())
			}

			// Auto-discover workspace members.
			members, err := discoverWorkspaceMembers(specPath, specData)
			if err != nil {
				return fmt.Errorf("discovering workspace members: %w", err)
			}

			// Auto-discover pnpm catalogs from pnpm-workspace.yaml.
			catalogs, err := discoverPnpmCatalogs(specPath)
			if err != nil {
				return fmt.Errorf("discovering pnpm catalogs: %w", err)
			}

			// Parse scope registries.
			scopeRegs, err := parseKeyValuePairs(scopeRegistries, "scope-registry")
			if err != nil {
				return err
			}

			// Parse auth tokens.
			authToks, err := parseKeyValuePairs(authTokens, "auth-token")
			if err != nil {
				return err
			}

			// Parse cutoff date
			opts := locksmith.GenerateOptions{
				SpecFile:         specData,
				OutputFormat:     outputFormat,
				RegistryURL:      registryURL,
				ScopeRegistries:  scopeRegs,
				AuthTokens:       authToks,
				Platform:         platform,
				SpecDir:          filepath.Dir(specPath),
				WorkspaceMembers: members,
				NodeVersion:      nodeVersion,
				Catalogs:         catalogs,
			}
			if cutoffStr != "" {
				t, err := time.Parse(time.RFC3339, cutoffStr)
				if err != nil {
					// Try date-only format
					t, err = time.Parse("2006-01-02", cutoffStr)
					if err != nil {
						return fmt.Errorf("parsing cutoff date (use RFC3339 or YYYY-MM-DD): %w", err)
					}
				}
				opts.CutoffDate = &t
			}

			// Generate
			ctx := context.Background()
			result, err := locksmith.Generate(ctx, opts)
			if err != nil {
				return err
			}

			// Write output
			if outputPath == "" || outputPath == "-" {
				_, err = os.Stdout.Write(result.Lockfile)
				return err
			}
			return os.WriteFile(outputPath, result.Lockfile, 0o644)
		},
	}

	cmd.Flags().StringVarP(&specPath, "spec", "s", "", "path to spec file (e.g., package.json)")
	cmd.Flags().StringVarP(&format, "format", "f", "", "output format: "+validFormatsStr())
	cmd.Flags().StringVarP(&cutoffStr, "cutoff", "c", "", "cutoff date (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().StringVarP(&registryURL, "registry", "r", "", "registry URL override")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output file path (default: stdout)")
	cmd.Flags().StringVarP(&platform, "platform", "p", "", "target platform os/cpu (e.g., linux/x64) to filter incompatible optional deps")
	cmd.Flags().StringVar(&nodeVersion, "node-version", "", "target Node.js version for engines.node filtering (e.g., 18.0.0)")
	cmd.Flags().StringArrayVar(&scopeRegistries, "scope-registry", nil, "scope=url pairs for per-scope registry routing (e.g., @company=https://private.registry.com)")
	cmd.Flags().StringArrayVar(&authTokens, "auth-token", nil, "url=token pairs for per-registry Bearer auth (e.g., https://private.registry.com=secrettoken)")

	_ = cmd.MarkFlagRequired("spec")
	_ = cmd.MarkFlagRequired("format")

	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print locksmith version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("locksmith %s\n", version)
		},
	}
}

func isValidFormat(f locksmith.OutputFormat) bool {
	for _, valid := range locksmith.AllFormats() {
		if f == valid {
			return true
		}
	}
	return false
}

func validFormatsStr() string {
	formats := locksmith.AllFormats()
	strs := make([]string, len(formats))
	for i, f := range formats {
		strs[i] = string(f)
	}
	return strings.Join(strs, ", ")
}

// parseKeyValuePairs parses "key=value" strings into a map.
// Returns nil (not empty map) when the input slice is empty.
func parseKeyValuePairs(pairs []string, flagName string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		idx := strings.Index(pair, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("--%s value %q must be in key=value format", flagName, pair)
		}
		key := pair[:idx]
		value := pair[idx+1:]
		if value == "" {
			return nil, fmt.Errorf("--%s value %q has empty value", flagName, pair)
		}
		result[key] = value
	}
	return result, nil
}
