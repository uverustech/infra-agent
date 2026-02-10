package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const version = "v1.8.0"

var (
	nodeID      string
	nodeType    string
	configDir   = "/etc/caddy"
	caddyfile   = "/etc/caddy/Caddyfile"
	controlURL  = "https://control.uvrs.xyz" // control dashboard
	heartbeatOK = false
	lastError   string

	// CLI flags
	showVersion = flag.Bool("version", false, "Show current and latest agent version")
	forceUpdate = flag.Bool("update", false, "Force immediate self-update to latest release")
	verbose     = flag.Bool("verbose", false, "Show detailed debug information")

	// Setup flags
	runSetup      = flag.Bool("setup", false, "Run system setup (e.g. install SSH keys)")
	autoConfirm   = flag.Bool("y", false, "Auto-confirm all actions during setup")
	githubToken   = flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token for private repo access (defaults to GITHUB_TOKEN env var)")
	sshKeyRepoUrl = flag.String("ssh-key-url", "https://github.com/uverustech/secrets/ssh-keys/uvr-ops/uvr_ops.pub", "URL to the SSH public key")
)

func main() {
	flag.StringVar(&nodeID, "node-id", os.Getenv("NODE_ID"), "Node ID (e.g. svr-gtw-nd1.uvrs.xyz)")
	flag.StringVar(&nodeType, "node-type", os.Getenv("NODE_TYPE"), "Node Type (e.g. gateway, server)")
	flag.Parse()

	// Handle --version, --update, --setup early — before any long-running logic
	if *showVersion {
		printVersionAndExit()
	}
	if *forceUpdate {
		forceSelfUpdateAndExit()
	}
	if *runSetup {
		doSetup()
		os.Exit(0)
	}

	if nodeID == "" {
		log.Fatal("NODE_ID not set. Use --node-id or NODE_ID env var")
	}

	log.Printf("infra-agent %s starting — node: %s", version, nodeID)

	if nodeType == "gateway" {
		gitPull()
		validateAndReload()
	}

	// Start log streaming in background
	go streamLogs()

	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		if nodeType == "gateway" {
			gitPull()
			validateAndReload()
		}
		sendHeartbeat()
	}
}

func streamLogs() {
	// Root is usually needed for journalctl access
	cmd := exec.Command("journalctl", "-f", "-o", "json", "-n", "0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[logs] failed to create stdout pipe for journalctl: %v", err)
		// Fallback to old behavior or retry
		time.Sleep(5 * time.Second)
		go streamLogs()
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[logs] failed to start journalctl: %v", err)
		time.Sleep(5 * time.Second)
		go streamLogs()
		return
	}

	log.Println("[logs] started streaming from system journal")

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		rawMsg, ok := entry["MESSAGE"].(string)
		if !ok {
			continue
		}

		// Enriched log payload
		payload := make(map[string]interface{})

		// 1. Try to parse MESSAGE as JSON (e.g. Caddy/Docker logs)
		if err := json.Unmarshal([]byte(rawMsg), &payload); err != nil {
			// Not JSON, just plain text
			payload["message"] = rawMsg
		}

		// 2. Add useful journal metadata
		if unit, ok := entry["_SYSTEMD_UNIT"].(string); ok {
			payload["unit"] = unit
			// If no logger/source is defined, use the unit name
			if _, exists := payload["logger"]; !exists {
				payload["logger"] = strings.TrimSuffix(unit, ".service")
			}
		}

		if priority, ok := entry["PRIORITY"].(string); ok {
			// Map syslog priority to levels
			levels := map[string]string{
				"0": "emergency", "1": "alert", "2": "critical", "3": "error",
				"4": "warning", "5": "notice", "6": "info", "7": "debug",
			}
			if level, exists := levels[priority]; exists && payload["level"] == nil {
				payload["level"] = level
			}
		}

		sendToControl(payload)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[logs] scanner error: %v", err)
	}

	cmd.Wait()
	log.Println("[logs] journalctl exited, restarting...")
	time.Sleep(2 * time.Second)
	go streamLogs()
}

var (
	wsConn *websocket.Conn
	wsMu   sync.Mutex
)

func sendToControl(logData interface{}) {
	wsMu.Lock()
	defer wsMu.Unlock()

	if wsConn == nil {
		if err := connectWS(); err != nil {
			return
		}
	}

	msgJSON, _ := json.Marshal(logData)
	err := wsConn.WriteMessage(websocket.TextMessage, msgJSON)
	if err != nil {
		log.Printf("[logs] ws write error: %v, reconnecting...", err)
		wsConn.Close()
		wsConn = nil
	}
}

func connectWS() error {
	u := strings.Replace(controlURL, "https://", "wss://", 1) + "/api/logs/stream"
	header := http.Header{}
	header.Add("X-Node-ID", nodeID)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(u, header)
	if err != nil {
		// Silent fail, will retry on next log line
		return err
	}
	log.Printf("[logs] connected to control plane: %s", u)
	wsConn = conn
	return nil
}

func gitPull() {
	cmd := exec.Command("git", "-C", configDir, "pull", "--ff-only")

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Git pull failed: %v\n%s", err, string(output))
		return
	}

	if bytes.Contains(output, []byte("Already up to date")) {
		log.Println("Config already up to date")
		return
	}

	log.Printf("Config updated via git pull:\n%s", string(output))
}

func validateAndReload() {
	lastError = ""
	out, err := exec.Command("caddy", "validate", "--config", caddyfile).CombinedOutput()
	if err != nil {
		log.Printf("Validation failed: %v\n%s", err, string(out))
		lastError = string(out)
		heartbeatOK = false
		return
	}

	out, err = exec.Command("caddy", "reload", "--config", caddyfile).CombinedOutput()
	if err != nil {
		log.Printf("Reload failed: %v\n%s", err, string(out))
		lastError = string(out)
		heartbeatOK = false
		return
	}

	log.Println("Caddy reloaded successfully")
	heartbeatOK = true
}

func sendHeartbeat() {
	sha, _ := exec.Command("git", "-C", configDir, "rev-parse", "HEAD").Output()
	isHealthy, summary, healthData := getSystemMetrics()

	payload := map[string]interface{}{
		"node_id":        nodeID,
		"git_sha":        string(bytes.TrimSpace(sha)),
		"agent_version":  version,
		"caddy_version":  getCaddyVersion(),
		"last_reload_ok": heartbeatOK,
		"last_error":     lastError,
		"node_type":      nodeType,
		"is_healthy":     isHealthy,
		"health_summary": summary,
		"health_data":    healthData,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}
	jsonBody, _ := json.Marshal(payload)

	// Silently fail — control plane not required yet
	http.Post(controlURL+"/api/heartbeat", "application/json", bytes.NewReader(jsonBody))

	// After sending heartbeat, ask the control plane if we are outdated
	resp, err := http.Get("https://control.uvrs.xyz/api/agent/latest-version")
	if err != nil {
		log.Printf("[update] check failed: %v", err)
	} else if resp.StatusCode == 200 {
		var v struct {
			Version string `json:"version"`
		}
		if json.NewDecoder(resp.Body).Decode(&v) == nil && v.Version != "" && v.Version != version {
			tag := strings.TrimPrefix(v.Version, "v")
			log.Printf("[update] triggering update %s → %s", version, v.Version)
			go func() {
				if err := selfUpdate(tag); err != nil {
					log.Printf("[update] error: %v", err)
				}
			}()
		} else if *verbose {
			log.Printf("[update] no update needed or version check failed (latest: %s, current: %s)", v.Version, version)
		}
	}
	resp.Body.Close()
}

func getSystemMetrics() (bool, string, map[string]interface{}) {
	isHealthy := true
	summaryParts := []string{}
	data := make(map[string]interface{})

	// 1. Disk Usage
	diskUsage, err := getDiskUsage("/")
	if err == nil {
		data["disk_usage"] = diskUsage
		if diskUsage > 90 {
			isHealthy = false
			summaryParts = append(summaryParts, "Disk space critical")
		}
	}

	// 2. Memory Usage
	memUsage, err := getMemoryUsage()
	if err == nil {
		data["mem_usage"] = memUsage
		if memUsage > 95 {
			isHealthy = false
			summaryParts = append(summaryParts, "Memory usage critical")
		}
	}

	// 3. CPU Usage
	cpuUsage, err := getCPUUsage()
	if err == nil {
		data["cpu_usage"] = cpuUsage
		if cpuUsage > 98 {
			isHealthy = false
			summaryParts = append(summaryParts, "CPU load critical")
		}
	}

	// 4. Uptime
	uptime, err := getUptime()
	if err == nil {
		data["uptime"] = uptime
	}

	// 5. Node Type specific checks
	if nodeType == "gateway" {
		data["caddy_ok"] = heartbeatOK
		if !heartbeatOK {
			isHealthy = false
			summaryParts = append(summaryParts, "Caddy reload failed")
		}
	}

	summary := "All systems nominal"
	if len(summaryParts) > 0 {
		summary = strings.Join(summaryParts, ", ")
	}

	return isHealthy, summary, data
}

func getDiskUsage(path string) (float64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0, err
	}
	all := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := all - free
	if all == 0 {
		return 0, nil
	}
	return float64(used) / float64(all) * 100, nil
}

func getMemoryUsage() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	var total, free, available uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &total)
		} else if strings.HasPrefix(line, "MemFree:") {
			fmt.Sscanf(line, "MemFree: %d", &free)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %d", &available)
		}
	}
	if total == 0 {
		return 0, nil
	}
	// Available is more accurate than Free on Linux
	used := total - available
	return float64(used) / float64(total) * 100, nil
}

func getCPUUsage() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	var load1 float64
	fmt.Sscanf(string(data), "%f", &load1)

	// Since we don't know the core count easily without more cgo/library,
	// we'll just report the raw 1m load for now.
	// In a real agent, we'd read /proc/stat twice.
	return load1, nil
}

func getUptime() (string, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", err
	}
	var seconds float64
	fmt.Sscanf(string(data), "%f", &seconds)

	days := int(seconds) / (24 * 3600)
	seconds = seconds - float64(days*24*3600)
	hours := int(seconds) / 3600
	seconds = seconds - float64(hours*3600)
	minutes := int(seconds) / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes), nil
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes), nil
	}
	return fmt.Sprintf("%dm", minutes), nil
}
func selfUpdate(tag string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}

	if *verbose {
		log.Printf("[update] current executable: %s", exe)
	}

	url := fmt.Sprintf("https://github.com/uverustech/infra-agent/releases/download/v%s/infra-agent-linux-amd64", tag)
	if *verbose {
		log.Printf("[update] downloading from: %s", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if *verbose {
		log.Printf("[update] response status: %s", resp.Status)
		for k, v := range resp.Header {
			log.Printf("[update] header %s: %v", k, v)
		}
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmp := exe + ".NEW"
	if *verbose {
		log.Printf("[update] creating temp file: %s", tmp)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %w", tmp, err)
	}

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to copy download to %s: %w", tmp, err)
	}
	f.Close()

	if *verbose {
		log.Printf("[update] downloaded %d bytes to %s", n, tmp)
	}

	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to replace binary %s with %s: %w", exe, tmp, err)
	}

	log.Printf("[update] successfully replaced binary → restarting via systemd")
	// Non-blocking restart
	go func() {
		time.Sleep(500 * time.Millisecond) // Give log time to flush
		cmd := exec.Command("sudo", "systemctl", "restart", "infra-agent")
		if *verbose {
			log.Printf("[update] executing: %s %v", cmd.Path, cmd.Args)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[update] failed to restart service: %v. Output: %s", err, string(out))
		}
	}()
	return nil
}

func getCaddyVersion() string {
	out, _ := exec.Command("caddy", "version").Output()
	return string(bytes.TrimSpace(out))
}

// getLatestAgentVersion contacts the control plane (or fallback directly to GitHub API)
func getLatestAgentVersion() (string, error) {
	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	req, _ := http.NewRequest("GET", controlURL+"/api/agent/latest-version", nil)
	req.Header.Set("User-Agent", "infra-agent/"+version)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Version == "" || payload.Version == "unknown" {
		return "", fmt.Errorf("empty version received")
	}
	return payload.Version, nil
}

// printVersionAndExit fetches latest version from control plane and prints both
func printVersionAndExit() {
	latest, err := getLatestAgentVersion()
	if err != nil {
		fmt.Printf("infra-agent %s (latest: error fetching – %v)\n", version, err)
		os.Exit(1)
	}

	fmt.Printf("infra-agent current:  %s\n", version)
	fmt.Printf("infra-agent latest:   %s\n", latest)

	if latest != version {
		fmt.Printf("↑ Update available! Run: infra-agent --update\n")
	} else {
		fmt.Println("You are running the latest version")
	}
	os.Exit(0)
}

// forceSelfUpdateAndExit triggers update immediately and restarts the service
func forceSelfUpdateAndExit() {
	latest, err := getLatestAgentVersion()
	if err != nil {
		log.Fatalf("Failed to fetch latest version: %v", err)
	}
	if latest == version {
		fmt.Printf("Already on latest version: %s\n", version)
		os.Exit(0)
	}

	fmt.Printf("Updating infra-agent %s → %s ...\n", version, latest)
	tag := strings.TrimPrefix(latest, "v")

	if err := selfUpdate(tag); err != nil {
		log.Fatalf("Self-update failed: %v", err)
	}
	// selfUpdate already restarts via systemctl — we never return
	os.Exit(0)
}

// doSetup performs system initializations like installing SSH keys
func doSetup() {
	fmt.Println("=== Starting Infra Agent Setup ===")

	if *githubToken == "" {
		log.Fatal("Error: GitHub token is required for setup. Set --github-token or GITHUB_TOKEN env var.")
	}

	fmt.Printf("Fetching SSH public key from: %s\n", *sshKeyRepoUrl)
	pubKey, err := fetchGithubFile(*sshKeyRepoUrl, *githubToken)
	if err != nil {
		log.Fatalf("Failed to fetch SSH key: %v", err)
	}
	pubKey = strings.TrimSpace(pubKey)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Could not determine home directory: %v", err)
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	authKeysFile := filepath.Join(sshDir, "authorized_keys")

	// Ensure .ssh exists
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		if confirmAction(fmt.Sprintf("Create directory %s?", sshDir)) {
			if err := os.MkdirAll(sshDir, 0700); err != nil {
				log.Fatalf("Failed to create %s: %v", sshDir, err)
			}
			fmt.Printf("Created %s\n", sshDir)
		} else {
			fmt.Println("Skipping SSH key installation.")
			return
		}
	}

	// Check if key already exists
	content, _ := os.ReadFile(authKeysFile)
	if strings.Contains(string(content), pubKey) {
		fmt.Println("SSH key already exists in authorized_keys. Skipping.")
		return
	}

	if confirmAction(fmt.Sprintf("Add SSH key to %s?", authKeysFile)) {
		f, err := os.OpenFile(authKeysFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("Failed to open %s: %v", authKeysFile, err)
		}
		defer f.Close()

		if _, err := f.WriteString("\n" + pubKey + "\n"); err != nil {
			log.Fatalf("Failed to write to %s: %v", authKeysFile, err)
		}
		fmt.Printf("Successfully added SSH key to %s\n", authKeysFile)
	} else {
		fmt.Println("Skipping SSH key installation.")
	}

	fmt.Println("=== Setup complete ===")
}

func confirmAction(message string) bool {
	if *autoConfirm {
		return true
	}

	fmt.Printf("%s (y/n): ", message)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(strings.TrimSpace(response)) == "y"
}

func fetchGithubFile(urlStr, token string) (string, error) {
	// Expected urlStr: https://github.com/owner/repo/path/to/file.pub
	trimmed := strings.TrimPrefix(urlStr, "https://github.com/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid GitHub URL format: %s", urlStr)
	}

	owner := parts[0]
	repo := parts[1]
	filePath := strings.Join(parts[2:], "/")

	apiUrl := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, filePath)
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned %s: %s", resp.Status, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
