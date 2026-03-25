package main

import (
	"context"
	"fmt"
	"os"
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
		specPath    string
		format      string
		cutoffStr   string
		registryURL string
		outputPath  string
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

			// Parse cutoff date
			opts := locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: outputFormat,
				RegistryURL:  registryURL,
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
