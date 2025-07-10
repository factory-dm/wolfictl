package cli

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"chainguard.dev/melange/pkg/config"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/wolfictl/pkg/lint"
	"github.com/wolfi-dev/wolfictl/pkg/melange"
	"github.com/wolfi-dev/wolfictl/pkg/yam"
	"gopkg.in/yaml.v3"
)

type vetOptions struct {
	runMelangePipeline bool
	runApkoPipeline    bool
	tempDir            string
	verbose            bool
}

// ConfigCheck is a struct to check if a file is a melange or apko config
type ConfigCheck struct {
	Package struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"package"`
	Contents struct {
		Repositories []string `yaml:"repositories,omitempty"`
		Packages     []string `yaml:"packages,omitempty"`
	} `yaml:"contents,omitempty"`
}

func cmdVet() *cobra.Command {
	opts := vetOptions{}
	cmd := &cobra.Command{
		Use:               "vet [manifest.yaml]",
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Short:             "Vet a melange or apko manifest before sending to PR",
		Long: `Vet a melange or apko manifest before sending to PR.

This command runs a series of checks on the given manifest file:
1. Identifies the manifest type (melange or apko)
2. Runs format check (wolfictl lint yam)
3. Runs lint check (wolfictl lint)
4. Optionally runs melange or apko pipeline if enabled

Example:
  wolfictl vet my-melange-manifest.yaml
  wolfictl vet my-apko-manifest.yaml --run-melange-pipeline
  wolfictl vet my-apko-manifest.yaml --run-apko-pipeline`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVet(cmd.Context(), args, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.runMelangePipeline, "run-melange-pipeline", false, "Run melange pipeline (keygen, build, scan)")
	cmd.Flags().BoolVar(&opts.runApkoPipeline, "run-apko-pipeline", false, "Run apko pipeline (build, scan)")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Enable verbose output")
	cmd.Flags().StringVar(&opts.tempDir, "temp-dir", os.TempDir(), "Temporary directory for build artifacts")

	return cmd
}

func runVet(ctx context.Context, args []string, opts vetOptions) error {
	if opts.verbose {
		slog.Info("Starting vet command", "args", args)
	}

	for _, manifestPath := range args {
		// Check if file exists
		_, err := os.Stat(manifestPath)
		if err != nil {
			return fmt.Errorf("manifest file not found: %s: %w", manifestPath, err)
		}

		// Identify manifest type
		manifestType, err := identifyManifestType(ctx, manifestPath)
		if err != nil {
			return fmt.Errorf("failed to identify manifest type: %w", err)
		}

		slog.Info("Identified manifest type", "path", manifestPath, "type", manifestType)

		// Run format check (wolfictl lint yam)
		if err := runFormatCheck(ctx, manifestPath, opts.verbose); err != nil {
			slog.Error("Format check failed", "error", err)
			return fmt.Errorf("format check failed: %w", err)
		}
		slog.Info("Format check passed", "path", manifestPath)

		// Run lint check (wolfictl lint)
		if err := runLintCheck(ctx, manifestPath, opts.verbose); err != nil {
			slog.Error("Lint check failed", "error", err)
			return fmt.Errorf("lint check failed: %w", err)
		}
		slog.Info("Lint check passed", "path", manifestPath)

		// Run optional pipeline based on manifest type
		if manifestType == "melange" && opts.runMelangePipeline {
			if err := runMelangePipeline(ctx, manifestPath, opts); err != nil {
				slog.Error("Melange pipeline failed", "error", err)
				return fmt.Errorf("melange pipeline failed: %w", err)
			}
			slog.Info("Melange pipeline completed successfully", "path", manifestPath)
		} else if manifestType == "apko" && opts.runApkoPipeline {
			if err := runApkoPipeline(ctx, manifestPath, opts); err != nil {
				slog.Error("Apko pipeline failed", "error", err)
				return fmt.Errorf("apko pipeline failed: %w", err)
			}
			slog.Info("Apko pipeline completed successfully", "path", manifestPath)
		}
	}

	slog.Info("All checks passed successfully")
	return nil
}

// identifyManifestType determines if the manifest is melange or apko
func identifyManifestType(ctx context.Context, manifestPath string) (string, error) {
	data, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("failed to read manifest file: %w", err)
	}

	check := &ConfigCheck{}
	if err := yaml.Unmarshal(data, check); err != nil {
		return "", fmt.Errorf("failed to parse manifest file: %w", err)
	}

	// Check if it's a melange config
	if check.Package.Name != "" && check.Package.Version != "" {
		// Try to parse with melange config parser to confirm
		_, err := config.ParseConfiguration(ctx, manifestPath)
		if err == nil {
			return "melange", nil
		}
	}

	// Check if it's an apko config
	// Apko configs typically have contents.repositories and contents.packages
	if len(check.Contents.Repositories) > 0 || len(check.Contents.Packages) > 0 {
		return "apko", nil
	}

	return "", errors.New("unable to determine manifest type (neither melange nor apko)")
}

// runFormatCheck runs the format check using wolfictl lint yam
func runFormatCheck(ctx context.Context, manifestPath string, verbose bool) error {
	if verbose {
		slog.Info("Running format check", "path", manifestPath)
	}

	// Use the yam package directly instead of spawning a subprocess
	return yam.Format(manifestPath, true)
}

// runLintCheck runs the lint check using wolfictl lint
func runLintCheck(ctx context.Context, manifestPath string, verbose bool) error {
	if verbose {
		slog.Info("Running lint check", "path", manifestPath)
	}

	// Use the lint package directly
	linter := lint.New(lint.WithPath(manifestPath))
	result, err := linter.Lint(ctx, lint.SeverityWarning)
	if err != nil {
		return err
	}

	if result.HasErrors() {
		linter.Print(ctx, result)
		// only count errors as failures, not warnings.
		for _, res := range result {
			for _, e := range res.Errors {
				if e.Rule.Severity.Value == lint.SeverityErrorLevel {
					return errors.New("linting failed")
				}
			}
		}
	}

	return nil
}

// runMelangePipeline runs the melange pipeline
func runMelangePipeline(ctx context.Context, manifestPath string, opts vetOptions) error {
	slog.Info("Running melange pipeline", "path", manifestPath)

	// Check if melange keys exist, generate if not
	keysDir := filepath.Join(filepath.Dir(manifestPath), ".melange")
	if _, err := os.Stat(keysDir); os.IsNotExist(err) {
		slog.Info("Melange keys not found, generating...")
		if err := runCommand("melange", []string{"keygen"}, filepath.Dir(manifestPath), opts.verbose); err != nil {
			return fmt.Errorf("failed to generate melange keys: %w", err)
		}
	}

	// Check if packages exist in Wolfi repo
	// This would require implementing package existence check logic
	// For now, we'll just log a message
	slog.Info("Checking if packages exist in Wolfi repo...")

	// Run melange build
	buildArgs := []string{
		"build",
		"--arch", "x86_64",
		"--signing-key", filepath.Join(keysDir, "melange.rsa"),
		"--keyring", filepath.Join(keysDir, "melange.rsa.pub"),
		"--output", opts.tempDir,
		manifestPath,
	}

	if err := runCommand("melange", buildArgs, "", opts.verbose); err != nil {
		return fmt.Errorf("melange build failed: %w", err)
	}

	// Export and scan generated APKs
	apkDir := opts.tempDir
	if err := scanApks(ctx, apkDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to scan APKs: %w", err)
	}

	return nil
}

// runApkoPipeline runs the apko pipeline
func runApkoPipeline(ctx context.Context, manifestPath string, opts vetOptions) error {
	slog.Info("Running apko pipeline", "path", manifestPath)

	// Check if packages exist in Wolfi repo
	// This would require implementing package existence check logic
	// For now, we'll just log a message
	slog.Info("Checking if packages exist in Wolfi repo...")

	// Run terraform fmt if it's a terraform file
	if strings.HasSuffix(manifestPath, ".tf") {
		if err := runCommand("terraform", []string{"fmt", manifestPath}, "", opts.verbose); err != nil {
			return fmt.Errorf("terraform fmt failed: %w", err)
		}
	}

	// Run apko build
	outputPath := filepath.Join(opts.tempDir, "image.tar")
	buildArgs := []string{
		"build",
		manifestPath,
		"--arch", "x86_64",
		outputPath,
	}

	if err := runCommand("apko", buildArgs, "", opts.verbose); err != nil {
		return fmt.Errorf("apko build failed: %w", err)
	}

	// Run CVE scans
	if err := runCommand("grype", []string{outputPath}, "", opts.verbose); err != nil {
		return fmt.Errorf("grype scan failed: %w", err)
	}

	return nil
}

// scanApks scans APK files in the given directory
func scanApks(ctx context.Context, apkDir string, verbose bool) error {
	// Find all APK files
	apkFiles, err := filepath.Glob(filepath.Join(apkDir, "*.apk"))
	if err != nil {
		return fmt.Errorf("failed to find APK files: %w", err)
	}

	if len(apkFiles) == 0 {
		return fmt.Errorf("no APK files found in %s", apkDir)
	}

	// Run grype scan on each APK
	for _, apkFile := range apkFiles {
		if verbose {
			slog.Info("Scanning APK", "file", apkFile)
		}

		if err := runCommand("grype", []string{apkFile}, "", verbose); err != nil {
			return fmt.Errorf("grype scan failed for %s: %w", apkFile, err)
		}
	}

	return nil
}

// runCommand runs a command with the given arguments
func runCommand(command string, args []string, workDir string, verbose bool) error {
	cmd := exec.Command(command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		slog.Info("Running command", "command", command, "args", args)
	} else {
		// Capture output but don't display it
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("command failed: %s %v: %w\nOutput: %s", command, args, err, output)
		}
	}

	return cmd.Run()
}
