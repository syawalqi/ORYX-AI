// Package updatepkg provides the self-update logic for ORYX.
// Separated from cmd to avoid circular imports (cmd ↔ tui).
package updatepkg

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repoOwner = "syawalqi"
const repoName = "ORYX-AI"
const repoSlug = repoOwner + "/" + repoName
const repoWebURL = "https://github.com/" + repoSlug

// Track represents the update channel.
type Track string

const (
	TrackStable Track = "stable"
	TrackDev    Track = "dev"
	TrackPinned Track = "pinned"
)

// ReadTrack reads the saved install track from config.
func ReadTrack() Track {
	home, err := os.UserHomeDir()
	if err != nil {
		return TrackStable
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oryx", "update-track"))
	if err != nil {
		return TrackStable
	}
	switch strings.TrimSpace(string(data)) {
	case "dev":
		return TrackDev
	case "pinned":
		return TrackPinned
	default:
		return TrackStable
	}
}

// SaveTrack writes the update track to config.
func SaveTrack(track Track) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configDir := filepath.Join(home, ".config", "oryx")
	os.MkdirAll(configDir, 0755)
	os.WriteFile(filepath.Join(configDir, "update-track"), []byte(string(track)), 0644)
}

// releaseInfo holds the fields we need from GitHub API.
type releaseInfo struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
}

func fetchReleaseByTag(tag string) (*releaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repoSlug, tag)
	return doFetch(url)
}

func fetchLatestStable() (*releaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repoSlug)
	return doFetch(url)
}

func doFetch(url string) (*releaseInfo, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var info releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &info, nil
}

// extractCommitSHA extracts the commit SHA from a dev version string.
// e.g., "dev-abc1234" → "abc1234"
func extractCommitSHA(version string) string {
	if strings.HasPrefix(version, "dev-") {
		return strings.TrimPrefix(version, "dev-")
	}
	return ""
}

// extractBodySHA extracts the commit SHA from the release body.
// Body format: "Auto-built from commit abc1234: message"
func extractBodySHA(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "commit ") {
			parts := strings.SplitN(line, "commit ", 2)
			if len(parts) == 2 {
				sha := strings.TrimSpace(parts[1])
				if i := strings.IndexAny(sha, ": "); i > 0 {
					sha = sha[:i]
				}
				return sha
			}
		}
	}
	return ""
}

// Run performs the update using the saved track.
func Run(currentVersion string, force bool) error {
	track := ReadTrack()
	return RunWithTrack(currentVersion, track, force)
}

// RunWithTrack performs the update on a specific track.
func RunWithTrack(currentVersion string, track Track, force bool) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	assetName := fmt.Sprintf("oryx-%s-%s", osName, arch)

	fmt.Printf("🔍 Current version: %s (track: %s)\n", currentVersion, track)

	// Fetch remote version info
	var remoteTag string
	var downloadURL string

	switch track {
	case TrackDev:
		// Dev track: check the "latest" pre-release
		info, err := fetchReleaseByTag("latest")
		if err != nil {
			return fmt.Errorf("check remote: %w", err)
		}
		remoteSHA := extractBodySHA(info.Body)
		localSHA := extractCommitSHA(currentVersion)

		if !force && remoteSHA != "" && localSHA != "" && remoteSHA == localSHA {
			fmt.Printf("✅ Already up to date (dev-%s)\n", localSHA)
			return nil
		}
		remoteTag = info.TagName
		downloadURL = fmt.Sprintf("%s/releases/download/latest/%s", repoWebURL, assetName)

	case TrackPinned:
		// Pinned: always download the requested version
		downloadURL = fmt.Sprintf("%s/releases/download/%s/%s", repoWebURL, currentVersion, assetName)
		remoteTag = currentVersion

	default:
		// Stable track: check releases/latest (newest non-prerelease)
		info, err := fetchLatestStable()
		if err != nil {
			return fmt.Errorf("check remote: %w", err)
		}
		remoteTag = info.TagName

		if !force && remoteTag == currentVersion {
			fmt.Printf("✅ Already up to date (%s)\n", currentVersion)
			return nil
		}
		downloadURL = fmt.Sprintf("%s/releases/latest/download/%s", repoWebURL, assetName)
	}

	fmt.Printf("📦 Downloading %s (%s/%s)...\n", remoteTag, osName, arch)

	// Download
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d\n   URL: %s\n   Available releases: %s/releases", resp.StatusCode, downloadURL, repoWebURL)
	}

	// Get current binary path
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find running binary: %w", err)
	}

	// Write to temp file
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

	// Atomic replace — try rename first, fall back to cp+rm
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		cp := exec.Command("cp", tmpPath, binaryPath)
		if out, err := cp.CombinedOutput(); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("install failed: %v\n%s", err, out)
		}
		os.Remove(tmpPath)
	}

	// Save track
	SaveTrack(track)

	fmt.Printf("✅ Updated! %s → %s (%d bytes)\n", currentVersion, remoteTag, written)
	fmt.Println("   Restart ORYX to use the new version.")
	return nil
}
