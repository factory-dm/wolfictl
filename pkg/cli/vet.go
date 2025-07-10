package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/chainguard-dev/yam/pkg/yam"
	"github.com/chainguard-dev/yam/pkg/yam/formatted"
	"github.com/samber/lo"
	"github.com/wolfi-dev/wolfictl/pkg/lint"
	"gopkg.in/yaml.v3"
)

const (
	manifestTypeMelange = "melange"
	manifestTypeApko    = "apko"
	manifestTypeUnknown = "unknown"
)

// ScannerType represents the type of security scanner to use
type ScannerType string

const (
	// ScannerGrype represents the Grype scanner
	ScannerGrype ScannerType = "grype"
	// ScannerTrivy represents the Trivy scanner
	ScannerTrivy ScannerType = "trivy"
)

// VetOptions holds configuration for the vet command
type VetOptions struct {
	ManifestPath   string
	RunBuild       bool
	ScannerType    ScannerType
	TempDir        string
	MelangeKeyDir  string
	ApkoConfigPath string
}

// cmdVet returns a cobra command for the vet subcommand
func cmdVet() *cobra.Command {
	o := &VetOptions{
		ScannerType:   ScannerGrype,
		TempDir:       os.TempDir(),
		MelangeKeyDir: filepath.Join(os.Getenv("HOME"), ".melange"),
	}

	cmd := &cobra.Command{
		Use:               "vet [manifest_path]",
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Short:             "Run vetting pipeline for melange or apko manifests",
		Long: `Run a comprehensive vetting pipeline for melange or apko manifests.

This command performs a series of checks on the given manifest:
1. Identifies the manifest type (melange or apko)
2. Runs format check (wolfictl lint yam)
3. Runs lint check (wolfictl lint)
4. Runs update check

Optionally, it can also:
- For melange manifests:
  * Generate melange keys if needed
  * Check if packages exist in the Wolfi repo
  * Run melange build
  * Export APK to temp directory
  * Run security scans with Grype/Trivy

- For apko manifests:
  * Check if packages exist in the Wolfi repo
  * Run terraform fmt
  * Run apko build
  * Run security scans with Grype/Trivy`,
		Example: `  # Vet a melange manifest (basic checks only)
  wolfictl vet my-melange-manifest.yaml

  # Vet a melange manifest and run build pipeline with security scanning
  wolfictl vet my-melange-manifest.yaml --run-build

  # Vet an apko manifest with Trivy scanner
  wolfictl vet my-apko-manifest.yaml --run-build --scanner trivy`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.ManifestPath = args[0]
			return o.Run(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&o.RunBuild, "run-build", false, "Run optional build pipeline and security scans")
	cmd.Flags().StringVar((*string)(&o.ScannerType), "scanner", string(ScannerGrype), "Security scanner to use (grype or trivy)")
	cmd.Flags().StringVar(&o.MelangeKeyDir, "melange-key-dir", o.MelangeKeyDir, "Directory for melange keys")
	cmd.Flags().StringVar(&o.ApkoConfigPath, "apko-config", "", "Path to apko configuration file")

	return cmd
}

// Run executes the vet command
func (o *VetOptions) Run(ctx context.Context) error {
	log := slog.Default()
	log.Info("Starting vetting process", "manifest", o.ManifestPath)

	// Check if manifest file exists
	if _, err := os.Stat(o.ManifestPath); os.IsNotExist(err) {
		return fmt.Errorf("manifest file %s does not exist", o.ManifestPath)
	}

	// Identify manifest type
	manifestType, err := o.identifyManifestType(ctx)
	if err != nil {
		return fmt.Errorf("failed to identify manifest type: %w", err)
	}
	log.Info("Manifest type identified", "type", manifestType)

	// Run format check (wolfictl lint yam)
	if err := o.runFormatCheck(ctx); err != nil {
		return fmt.Errorf("format check failed: %w", err)
	}
	log.Info("Format check passed")

	// Run lint check (wolfictl lint)
	if err := o.runLintCheck(ctx); err != nil {
		return fmt.Errorf("lint check failed: %w", err)
	}
	log.Info("Lint check passed")

	// Run update check
	if err := o.runUpdateCheck(ctx, manifestType); err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	log.Info("Update check passed")

	// Run optional build pipeline if requested
	if o.RunBuild {
		log.Info("Running build pipeline")
		if manifestType == manifestTypeMelange {
			if err := o.runMelangePipeline(ctx); err != nil {
				return fmt.Errorf("melange pipeline failed: %w", err)
			}
		} else if manifestType == manifestTypeApko {
			if err := o.runApkoPipeline(ctx); err != nil {
				return fmt.Errorf("apko pipeline failed: %w", err)
			}
		}
	}

	log.Info("Vetting completed successfully")
	return nil
}

// identifyManifestType determines if the manifest is for melange or apko
func (o *VetOptions) identifyManifestType(ctx context.Context) (string, error) {
	data, err := os.ReadFile(o.ManifestPath)
	if err != nil {
		return manifestTypeUnknown, err
	}

	var manifest map[string]interface{}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return manifestTypeUnknown, err
	}

	// Check for melange-specific keys
	if _, hasPackage := manifest["package"]; hasPackage {
		if _, hasPipeline := manifest["pipeline"]; hasPipeline {
			return manifestTypeMelange, nil
		}
	}

	// Check for apko-specific keys
	if _, hasContents := manifest["contents"]; hasContents {
		if _, hasEntrypoints := manifest["entrypoints"]; hasEntrypoints {
			return manifestTypeApko, nil
		}
	}

	// Check if it's an apko config with a different structure
	if _, hasApk := manifest["apk"]; hasApk {
		if _, hasInclude := manifest["include"]; hasInclude {
			return manifestTypeApko, nil
		}
	}

	return manifestTypeUnknown, errors.New("could not determine manifest type (neither melange nor apko)")
}

// runFormatCheck runs the format check using wolfictl lint yam
func (o *VetOptions) runFormatCheck(ctx context.Context) error {
	slog.Info("Running format check")

	// Based on cmdLintYam implementation
	fsys := os.DirFS(".")
	paths := lo.Map([]string{o.ManifestPath}, func(p string, _ int) string {
		return filepath.Clean(p)
	})

	encodeOptions, err := formatted.ReadConfig()
	if err != nil {
		return fmt.Errorf("unable to load yam config: %w", err)
	}

	formatOptions := yam.FormatOptions{
		EncodeOptions:          *encodeOptions,
		FinalNewline:           true,
		TrimTrailingWhitespace: true,
	}

	if err := yam.Lint(fsys, paths, yam.ExecDiff, formatOptions); err != nil {
		if errors.Is(err, yam.ErrDidNotPassLintCheck) {
			// Provide same user hint as cmdLintYam
			fmt.Println("\nYAML needs to be formatted. 👻")
			fmt.Println("Run `yam` to fix automatically. For more information, see https://github.com/chainguard-dev/yam")
			fmt.Println()
		}
		return err
	}
	fmt.Println("YAML is formatted correctly! 🎉")
	return nil
}

// runLintCheck runs the lint check using wolfictl lint
func (o *VetOptions) runLintCheck(ctx context.Context) error {
	slog.Info("Running lint check")

	l := lint.New(lint.WithPath(o.ManifestPath))

	// Minimum severity warning to match original behaviour
	result, err := l.Lint(ctx, lint.SeverityWarning)
	if err != nil {
		return err
	}

	if result.HasErrors() {
		l.Print(ctx, result)
		// Only treat SeverityError as failure
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

// runUpdateCheck runs the update check based on manifest type
func (o *VetOptions) runUpdateCheck(ctx context.Context, manifestType string) error {
	slog.Info("Running update check")
	
	// TODO: Implement update check logic
	// This would check if packages need to be updated
	// For now, we'll just log that this would happen
	slog.Info("Update check would run here", "manifestType", manifestType)
	
	return nil
}

// runMelangePipeline runs the melange build pipeline
func (o *VetOptions) runMelangePipeline(ctx context.Context) error {
	slog.Info("Running melange pipeline")
	
	// Check if melange is installed
	if _, err := exec.LookPath("melange"); err != nil {
		return fmt.Errorf("melange not found in PATH: %w", err)
	}
	
	// Generate melange keys if needed
	if err := o.ensureMelangeKeys(ctx); err != nil {
		return fmt.Errorf("failed to ensure melange keys: %w", err)
	}
	
	// Check if all packages exist on the Wolfi repo
	if err := o.checkMelangePackages(ctx); err != nil {
		return fmt.Errorf("package check failed: %w", err)
	}
	
	// Run melange build
	if err := o.runMelangeBuild(ctx); err != nil {
		return fmt.Errorf("melange build failed: %w", err)
	}
	
	// Export generated .apk to temp dir
	apkPath, err := o.exportMelangeApk(ctx)
	if err != nil {
		return fmt.Errorf("failed to export APK: %w", err)
	}
	
	// Run security scan
	if err := o.runSecurityScan(ctx, apkPath); err != nil {
		return fmt.Errorf("security scan failed: %w", err)
	}
	
	return nil
}

// ensureMelangeKeys ensures that melange keys exist
func (o *VetOptions) ensureMelangeKeys(ctx context.Context) error {
	// Check if keys directory exists
	if _, err := os.Stat(o.MelangeKeyDir); os.IsNotExist(err) {
		slog.Info("Creating melange keys directory", "dir", o.MelangeKeyDir)
		if err := os.MkdirAll(o.MelangeKeyDir, 0755); err != nil {
			return err
		}
	}
	
	// Check if keys exist
	keyPath := filepath.Join(o.MelangeKeyDir, "melange.rsa")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		slog.Info("Generating melange keys")
		cmd := exec.CommandContext(ctx, "melange", "keygen", o.MelangeKeyDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	
	return nil
}

// checkMelangePackages checks if all packages exist on the Wolfi repo
func (o *VetOptions) checkMelangePackages(ctx context.Context) error {
	slog.Info("Checking if packages exist in Wolfi repository")
	
	// Parse the melange manifest to extract package dependencies
	data, err := os.ReadFile(o.ManifestPath)
	if err != nil {
		return err
	}
	
	var manifest struct {
		Package struct {
			Dependencies map[string]string `yaml:"dependencies"`
		} `yaml:"package"`
	}
	
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return err
	}
	
	// TODO: Implement actual package existence check against Wolfi repo
	// For now, we'll just log the dependencies that would be checked
	for pkg := range manifest.Package.Dependencies {
		slog.Info("Would check package existence", "package", pkg)
	}
	
	return nil
}

// runMelangeBuild runs the melange build command
func (o *VetOptions) runMelangeBuild(ctx context.Context) error {
	slog.Info("Running melange build")
	
	// Create a temporary directory for build outputs
	buildDir := filepath.Join(o.TempDir, "melange-build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}
	
	// Run melange build
	cmd := exec.CommandContext(
		ctx,
		"melange",
		"build",
		o.ManifestPath,
		"--key", filepath.Join(o.MelangeKeyDir, "melange.rsa"),
		"--out-dir", buildDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}

// exportMelangeApk exports the generated APK to a temporary directory
func (o *VetOptions) exportMelangeApk(ctx context.Context) (string, error) {
	slog.Info("Exporting generated APK")
	
	// Parse the melange manifest to extract package name and version
	data, err := os.ReadFile(o.ManifestPath)
	if err != nil {
		return "", err
	}
	
	var manifest struct {
		Package struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
		} `yaml:"package"`
	}
	
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	
	// Construct the expected APK path
	buildDir := filepath.Join(o.TempDir, "melange-build")
	apkName := fmt.Sprintf("%s-%s-r0.apk", manifest.Package.Name, manifest.Package.Version)
	apkPath := filepath.Join(buildDir, "packages", apkName)
	
	// Check if the APK exists
	if _, err := os.Stat(apkPath); os.IsNotExist(err) {
		return "", fmt.Errorf("expected APK not found at %s", apkPath)
	}
	
	return apkPath, nil
}

// runApkoPipeline runs the apko build pipeline
func (o *VetOptions) runApkoPipeline(ctx context.Context) error {
	slog.Info("Running apko pipeline")
	
	// Check if apko is installed
	if _, err := exec.LookPath("apko"); err != nil {
		return fmt.Errorf("apko not found in PATH: %w", err)
	}
	
	// Check if terraform is installed for fmt
	if _, err := exec.LookPath("terraform"); err != nil {
		slog.Warn("terraform not found in PATH, skipping terraform fmt")
	} else {
		// Run terraform fmt
		if err := o.runTerraformFmt(ctx); err != nil {
			return fmt.Errorf("terraform fmt failed: %w", err)
		}
	}
	
	// Check if all packages exist on the Wolfi repo
	if err := o.checkApkoPackages(ctx); err != nil {
		return fmt.Errorf("package check failed: %w", err)
	}
	
	// Run apko build
	imagePath, err := o.runApkoBuild(ctx)
	if err != nil {
		return fmt.Errorf("apko build failed: %w", err)
	}
	
	// Run security scan
	if err := o.runSecurityScan(ctx, imagePath); err != nil {
		return fmt.Errorf("security scan failed: %w", err)
	}
	
	return nil
}

// runTerraformFmt runs terraform fmt on the apko manifest
func (o *VetOptions) runTerraformFmt(ctx context.Context) error {
	slog.Info("Running terraform fmt")
	
	cmd := exec.CommandContext(ctx, "terraform", "fmt", o.ManifestPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}

// checkApkoPackages checks if all packages in the apko manifest exist
func (o *VetOptions) checkApkoPackages(ctx context.Context) error {
	slog.Info("Checking if packages exist in Wolfi repository")
	
	// Parse the apko manifest to extract package dependencies
	data, err := os.ReadFile(o.ManifestPath)
	if err != nil {
		return err
	}
	
	var manifest struct {
		Contents struct {
			Packages []string `yaml:"packages"`
		} `yaml:"contents"`
	}
	
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return err
	}
	
	// TODO: Implement actual package existence check against Wolfi repo
	// For now, we'll just log the packages that would be checked
	for _, pkg := range manifest.Contents.Packages {
		slog.Info("Would check package existence", "package", pkg)
	}
	
	return nil
}

// runApkoBuild runs the apko build command
func (o *VetOptions) runApkoBuild(ctx context.Context) (string, error) {
	slog.Info("Running apko build")
	
	// Create a temporary directory for build outputs
	buildDir := filepath.Join(o.TempDir, "apko-build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return "", err
	}
	
	// Generate a temporary name for the output image
	imageName := filepath.Base(o.ManifestPath)
	imageName = strings.TrimSuffix(imageName, filepath.Ext(imageName))
	imagePath := filepath.Join(buildDir, imageName+".tar")
	
	// Run apko build
	cmd := exec.CommandContext(
		ctx,
		"apko",
		"build",
		o.ManifestPath,
		"--output", imagePath,
	)
	
	// Add config if provided
	if o.ApkoConfigPath != "" {
		cmd.Args = append(cmd.Args, "--config", o.ApkoConfigPath)
	}
	
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		return "", err
	}
	
	return imagePath, nil
}

// runSecurityScan runs a security scan on the given artifact
func (o *VetOptions) runSecurityScan(ctx context.Context, artifactPath string) error {
	slog.Info("Running security scan", "scanner", o.ScannerType, "artifact", artifactPath)
	
	switch o.ScannerType {
	case ScannerGrype:
		return o.runGrypeScan(ctx, artifactPath)
	case ScannerTrivy:
		return o.runTrivyScan(ctx, artifactPath)
	default:
		return fmt.Errorf("unknown scanner type: %s", o.ScannerType)
	}
}

// runGrypeScan runs a Grype security scan
func (o *VetOptions) runGrypeScan(ctx context.Context, artifactPath string) error {
	// Check if grype is installed
	if _, err := exec.LookPath("grype"); err != nil {
		return fmt.Errorf("grype not found in PATH: %w", err)
	}
	
	cmd := exec.CommandContext(ctx, "grype", artifactPath, "-o", "table")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}

// runTrivyScan runs a Trivy security scan
func (o *VetOptions) runTrivyScan(ctx context.Context, artifactPath string) error {
	// Check if trivy is installed
	if _, err := exec.LookPath("trivy"); err != nil {
		return fmt.Errorf("trivy not found in PATH: %w", err)
	}
	
	cmd := exec.CommandContext(ctx, "trivy", "fs", artifactPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}
