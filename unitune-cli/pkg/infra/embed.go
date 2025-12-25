package infra

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed all:embedded
var embeddedInfra embed.FS

// GetInfraDir returns the path to the unitune infra directory (~/.unitune/infra)
func GetInfraDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".unitune", "infra"), nil
}

// EnsureInfraExtracted ensures the CDK infrastructure is extracted to ~/.unitune/infra/
// Returns the path to the infra directory. Always re-extracts to ensure the latest version.
func EnsureInfraExtracted() (string, error) {
	infraDir, err := GetInfraDir()
	if err != nil {
		return "", err
	}

	// Always extract to ensure we have the latest embedded infrastructure
	if err := extractToDir(infraDir); err != nil {
		return "", err
	}

	return infraDir, nil
}

// extractToDir extracts the embedded infrastructure to the specified directory
func extractToDir(targetDir string) error {
	// Clean up existing directory if partial extraction
	os.RemoveAll(targetDir)

	// Create the directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create infra directory: %w", err)
	}

	// Walk and extract all embedded files
	err := fs.WalkDir(embeddedInfra, "embedded", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path (strip "embedded/" prefix)
		relPath, err := filepath.Rel("embedded", path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// Read embedded file
		content, err := embeddedInfra.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		// Write to target
		return os.WriteFile(targetPath, content, 0644)
	})

	if err != nil {
		os.RemoveAll(targetDir)
		return fmt.Errorf("failed to extract infra: %w", err)
	}

	return nil
}

func EnsureDependenciesInstalled(dir string) error {
	fmt.Println("📦 Installing dependencies...")
	cmd := exec.Command("npm", "install", "--prefer-offline", "--no-audit")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunCDK executes a CDK command in the given directory
func RunCDK(dir string, args ...string) error {
	cdkArgs := append([]string{"cdk"}, args...)
	cmd := exec.Command("npx", cdkArgs...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// CleanInfraCache removes the cached infrastructure directory
func CleanInfraCache() error {
	infraDir, err := GetInfraDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(infraDir)
}
