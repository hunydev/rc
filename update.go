package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	githubReleaseURL  = "https://api.github.com/repos/hunydev/rc/releases/latest"
	updateHTTPTimeout = 60 * time.Second
)

// restartChan signals the main goroutine to gracefully restart the process.
var restartChan = make(chan struct{}, 1)

// updateMu prevents concurrent update operations.
var updateMu sync.Mutex

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type updateCheckResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseNotes    string `json:"release_notes,omitempty"`
	AssetName       string `json:"asset_name,omitempty"`
	AssetSize       int64  `json:"asset_size,omitempty"`
}

// checkLatestRelease queries the GitHub API for the latest release info.
func checkLatestRelease() (*releaseInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", githubReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}
	return &release, nil
}

// findAsset locates the archive matching the current OS and architecture.
func findAsset(release *releaseInfo) *releaseAsset {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	target := fmt.Sprintf("rc_%s_%s_%s%s", release.TagName, goos, goarch, ext)
	for i := range release.Assets {
		if release.Assets[i].Name == target {
			return &release.Assets[i]
		}
	}
	return nil
}

// compareVersions compares two semver strings (with optional "v" prefix).
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func compareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}
	return 0
}

// handleUpdateCheck returns current vs latest version info.
func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	release, err := checkLatestRelease()
	if err != nil {
		writeUpdateJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	resp := updateCheckResponse{
		CurrentVersion: version,
		LatestVersion:  release.TagName,
		ReleaseNotes:   release.Body,
	}

	if version == "dev" {
		resp.UpdateAvailable = true
	} else {
		resp.UpdateAvailable = compareVersions(version, release.TagName) < 0
	}

	if asset := findAsset(release); asset != nil {
		resp.AssetName = asset.Name
		resp.AssetSize = asset.Size
	}

	writeUpdateJSON(w, http.StatusOK, resp)
}

// handleUpdateApply downloads the latest binary, replaces the current one, and triggers a restart.
func handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if !updateMu.TryLock() {
		writeUpdateJSON(w, http.StatusConflict, map[string]string{"error": "update already in progress"})
		return
	}
	defer updateMu.Unlock()

	// Resolve current executable path
	execPath, err := os.Executable()
	if err != nil {
		writeUpdateJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot determine executable path"})
		return
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		writeUpdateJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot resolve executable path"})
		return
	}

	// Check write permission
	if err := checkWritable(execPath); err != nil {
		writeUpdateJSON(w, http.StatusForbidden, map[string]string{"error": "no write permission to binary: " + err.Error()})
		return
	}

	// Get latest release
	release, err := checkLatestRelease()
	if err != nil {
		writeUpdateJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to check release: " + err.Error()})
		return
	}

	asset := findAsset(release)
	if asset == nil {
		writeUpdateJSON(w, http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH),
		})
		return
	}

	// Download and install
	if err := downloadAndInstall(asset.BrowserDownloadURL, execPath); err != nil {
		writeUpdateJSON(w, http.StatusInternalServerError, map[string]string{"error": "update failed: " + err.Error()})
		return
	}

	log.Printf("Update installed: %s → %s", version, release.TagName)

	writeUpdateJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"version": release.TagName,
		"message": "Update installed. Restarting...",
	})

	// Trigger restart (non-blocking send)
	select {
	case restartChan <- struct{}{}:
	default:
	}
}

// downloadAndInstall downloads the release archive and replaces the current binary.
func downloadAndInstall(url, execPath string) error {
	client := &http.Client{Timeout: updateHTTPTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	if runtime.GOOS == "windows" {
		return fmt.Errorf("auto-update on Windows is not yet supported")
	}

	return extractTarGzBinary(resp.Body, execPath)
}

// extractTarGzBinary extracts the "rc" binary from a tar.gz archive into destPath.
func extractTarGzBinary(r io.Reader, destPath string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip open failed: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary 'rc' not found in archive")
		}
		if err != nil {
			return fmt.Errorf("tar read failed: %w", err)
		}

		if header.Name == "rc" && header.Typeflag == tar.TypeReg {
			// Write to temp file in same directory for atomic rename
			dir := filepath.Dir(destPath)
			tmp, err := os.CreateTemp(dir, ".rc-update-*")
			if err != nil {
				return fmt.Errorf("cannot create temp file: %w", err)
			}
			tmpPath := tmp.Name()

			if _, err := io.Copy(tmp, tr); err != nil {
				tmp.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("write failed: %w", err)
			}
			tmp.Close()

			if err := os.Chmod(tmpPath, 0755); err != nil {
				os.Remove(tmpPath)
				return fmt.Errorf("chmod failed: %w", err)
			}

			// Atomic rename
			if err := os.Rename(tmpPath, destPath); err != nil {
				os.Remove(tmpPath)
				return fmt.Errorf("rename failed: %w", err)
			}

			return nil
		}
	}
}

// performRestart gracefully shuts down the server and re-execs the current binary with same args/env.
func performRestart(server *http.Server, closeFn func()) {
	time.Sleep(1 * time.Second) // allow HTTP response to flush

	log.Println("Restarting for update...")

	// Close sessions/resources
	if closeFn != nil {
		closeFn()
	}

	// Graceful HTTP shutdown
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	server.Shutdown(ctx)

	// Re-exec with same arguments and environment
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Restart failed: cannot determine executable: %v", err)
		os.Exit(1)
	}
	execPath, _ = filepath.EvalSymlinks(execPath)

	log.Printf("Restarting: %s %v", execPath, os.Args[1:])
	if err := syscall.Exec(execPath, os.Args, os.Environ()); err != nil {
		log.Printf("Restart failed: exec error: %v", err)
		os.Exit(1)
	}
}

func checkWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func writeUpdateJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
