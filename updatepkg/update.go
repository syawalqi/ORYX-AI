// Package updatepkg provides the self-update logic for ORYX.
// Separated from cmd to avoid circular imports (cmd ↔ tui).
package updatepkg

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"
)

const repoURL = "https://github.com/syawalqi/ORYX-AI"

// Run downloads the latest ORYX binary and replaces the running one.
func Run(currentVersion string) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	assetName := fmt.Sprintf("oryx-%s-%s", osName, arch)

	fmt.Printf("🔍 Current version: %s\n", currentVersion)
	fmt.Printf("📦 Downloading latest (%s/%s)...\n", osName, arch)

	downloadURL := fmt.Sprintf("%s/releases/download/latest/%s", repoURL, assetName)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d\n   URL: %s\n   Available releases: %s/releases", resp.StatusCode, downloadURL, repoURL)
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find running binary: %w", err)
	}

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

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod failed: %w", err)
	}

	if err := os.Rename(tmpPath, binaryPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace failed (need sudo?): %w", err)
	}

	fmt.Printf("✅ Updated! (%d bytes)\n", written)
	fmt.Printf("   Binary: %s\n", binaryPath)
	fmt.Println("   Restart ORYX to use the new version.")
	return nil
}
