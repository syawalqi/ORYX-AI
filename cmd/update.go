package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const repoURL = "https://github.com/syawalqi/ORYX-AI"

// Update downloads the latest ORYX binary and replaces the running one.
func Update(currentVersion string) error {
	// Determine OS and arch
	osName := runtime.GOOS
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	} else {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	assetName := fmt.Sprintf("oryx-%s-%s", osName, arch)
	version := "latest"

	fmt.Printf("🔍 Current version: %s\n", currentVersion)
	fmt.Printf("📦 Downloading %s (%s/%s)...\n", version, osName, arch)

	// Download from GitHub releases
	downloadURL := fmt.Sprintf("%s/releases/download/%s/%s", repoURL, version, assetName)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d\n   URL: %s\n   Available releases: %s/releases", resp.StatusCode, downloadURL, repoURL)
	}

	// Get the path of the current binary
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find running binary: %w", err)
	}

	// Write to a temp file first
	tmpPath := binaryPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("can't create temp file: %w", err)
	}

	written, err := io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("download interrupted: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace failed (need sudo?): %w", err)
	}

	fmt.Printf("✅ Updated! (%d bytes)\n", written)
	fmt.Printf("   Binary: %s\n", binaryPath)
	fmt.Println("   Restart ORYX to use the new version.")
	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// normalizeVersion strips leading 'v' for comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}
