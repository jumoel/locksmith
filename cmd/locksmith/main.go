package main

import (
	"context"
	"fmt"
	"io"
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
		timeoutDuration time.Duration
		verbose         bool
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

			// Generate with optional timeout. The deadline gives the resolver
			// a budget to fall back on when a packument fetch wedges on a
			// slow registry, instead of letting the process spin until an
			// external killer (CI timeout, OOM, shell ^C) terminates it and
			// leaves no output behind.
			ctx := context.Background()
			if timeoutDuration > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeoutDuration)
				defer cancel()
			}

			stderr := cmd.ErrOrStderr()
			var heartbeatStop chan struct{}
			if verbose {
				heartbeatStop = startHeartbeat(stderr, ctx, time.Now())
				defer close(heartbeatStop)
			}

			result, err := locksmith.Generate(ctx, opts)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
					return fmt.Errorf("generate timed out after %s: %w", timeoutDuration, err)
				}
				return err
			}

			// Write output. Atomic write to a temp file in the same directory
			// followed by rename: a SIGKILL mid-write (CI timeout, OOM) leaves
			// either nothing or a complete file, never a partial/zero-byte one.
			if outputPath == "" || outputPath == "-" {
				_, err = os.Stdout.Write(result.Lockfile)
				return err
			}
			if err := atomicWriteFile(outputPath, result.Lockfile, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", outputPath, err)
			}
			if verbose {
				fmt.Fprintf(stderr, "locksmith: wrote %d bytes to %s\n", len(result.Lockfile), outputPath)
			}
			return nil
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
	cmd.Flags().DurationVar(&timeoutDuration, "timeout", 0, "abort generation after this duration (e.g. 5m, 30s); 0 means no timeout")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "emit a heartbeat to stderr every 5s so long-running runs are observable")

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

// atomicWriteFile writes data to a temp file in the same directory as path,
// fsyncs it, and renames it over path. Either the destination doesn't exist
// (we never got that far) or it contains the full payload - never the
// half-written / zero-byte file os.WriteFile leaves behind when the process
// is killed mid-write.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".locksmith-"+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below this point fails.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// startHeartbeat prints a single-line stderr update every 5s until the
// returned channel is closed or the context is cancelled. Lets the operator
// distinguish "locksmith is making progress" from "locksmith is wedged" on
// large dependency trees that take minutes to resolve.
func startHeartbeat(w io.Writer, ctx context.Context, start time.Time) chan struct{} {
	return startHeartbeatEvery(w, ctx, start, 5*time.Second)
}

// startHeartbeatEvery is the testable variant that takes a tick interval.
func startHeartbeatEvery(w io.Writer, ctx context.Context, start time.Time, interval time.Duration) chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Fprintf(w, "locksmith: still working (elapsed %s)\n", time.Since(start).Round(time.Second))
			}
		}
	}()
	return stop
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
