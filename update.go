package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	githubReleaseURL  = "https://api.github.com/repos/hunydev/rc/releases/latest"
	updateHTTPTimeout = 60 * time.Second
)

// restartChan carries the executable path for the main goroutine to re-exec.
var restartChan = make(chan string, 1)

// serverLn is the active net.Listener, set by main and used by performRestart for recovery.
var serverLn net.Listener

// restarting is set to 1 when a restart is in progress; main's serve loop checks this.
var restarting int32

// restartReady signals the main serve loop to re-serve after a failed restart recovery.
var restartReady = make(chan struct{}, 1)

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

	// Verify new binary is executable
	out, err := exec.Command(execPath, "-v").CombinedOutput()
	if err != nil {
		writeUpdateJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("new binary verification failed: %v (%s)", err, strings.TrimSpace(string(out))),
		})
		return
	}
	newVersion := strings.TrimSpace(string(out))
	log.Printf("Update verified: %s → %s", version, newVersion)

	writeUpdateJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"version": release.TagName,
		"message": "Update installed. Restarting...",
	})

	// Trigger restart with the known exec path (non-blocking send)
	select {
	case restartChan <- execPath:
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

// performRestart closes the listener, starts the new binary, and either exits or recovers.
// Flow: close listener (free port) → start new process → verify it's alive → exit old.
// If the new process fails to start or exits immediately, re-listen and resume serving.
func performRestart(server *http.Server, closeFn func(), execPath string) {
	time.Sleep(1 * time.Second) // allow HTTP response to flush

	log.Println("Restarting for update...")
	atomic.StoreInt32(&restarting, 1)

	// Close listener to free the port (server.Serve returns in main loop)
	addr := serverLn.Addr().String()
	serverLn.Close()

	// Start new process fully detached
	cmd := exec.Command(execPath, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = nil
	cmd.SysProcAttr = daemonSysProcAttr()

	log.Printf("Starting new process: %s %v", execPath, os.Args[1:])
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start new process: %v — recovering", err)
		recoverListener(server, addr)
		return
	}

	// Wait briefly to check if the new process stays alive
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// New process exited immediately — failed
		log.Printf("New process exited immediately: %v — recovering", err)
		recoverListener(server, addr)
		return
	case <-time.After(2 * time.Second):
		// Still running after 2s — success
	}

	log.Printf("New process running (pid=%d), exiting old process.", cmd.Process.Pid)
	if closeFn != nil {
		closeFn()
	}
	os.Exit(0)
}

// recoverListener re-creates the listener and signals the main serve loop to resume.
func recoverListener(server *http.Server, addr string) {
	newLn, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Cannot recover listener on %s: %v — exiting", addr, err)
		os.Exit(1)
	}
	serverLn = newLn
	atomic.StoreInt32(&restarting, 0)
	restartReady <- struct{}{}
	log.Printf("Recovered: listening on %s again", addr)
}

// checkWritable verifies we can create/rename files in the directory containing path.
// We check the directory (not the binary itself) because Linux returns ETXTBSY
// when opening a running executable for writing. The actual update uses atomic
// rename which only requires directory write permission.
func checkWritable(path string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".rc-write-check-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	tmp.Close()
	os.Remove(name)
	return nil
}

func writeUpdateJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// runCLIUpdate checks for updates from the CLI and installs if available.
func runCLIUpdate() error {
	fmt.Printf("Current version: %s\n", version)
	fmt.Println("Checking for updates...")

	release, err := checkLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	upToDate := version != "dev" && compareVersions(version, release.TagName) >= 0
	if upToDate {
		fmt.Printf("Already up to date (%s).\n", version)
		return nil
	}

	fmt.Printf("Update available: %s → %s\n", version, release.TagName)

	asset := findAsset(release)
	if asset == nil {
		return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, release.TagName)
	}
	fmt.Printf("Downloading %s (%.1f MB)...\n", asset.Name, float64(asset.Size)/(1024*1024))

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	if err := checkWritable(execPath); err != nil {
		return fmt.Errorf("no write permission to %s: %w", execPath, err)
	}

	if err := downloadAndInstall(asset.BrowserDownloadURL, execPath); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Printf("Updated successfully: %s → %s\n", version, release.TagName)
	return nil
}
