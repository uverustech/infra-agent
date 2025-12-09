package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
	"fmt"
	"io"
	"strings"
	"crypto/tls"
)

const version = "v1.1.0"

var (
	nodeID      string
	configDir   = "/etc/caddy"
	caddyfile   = "/etc/caddy/Caddyfile"
	controlURL  = "https://control.gtw.uvrs.xyz" // control dashboard
	heartbeatOK = false
	
	// CLI flags
	showVersion = flag.Bool("version", false, "Show current and latest agent version")
	forceUpdate = flag.Bool("update", false, "Force immediate self-update to latest release")
)

func main() {
	flag.StringVar(&nodeID, "node-id", os.Getenv("NODE_ID"), "Node ID (e.g. svr-gtw-nd1.uvrs.xyz)")
	flag.Parse()

	// Handle --version and --update early — before any long-running logic
	if *showVersion {
		printVersionAndExit()
	}
	if *forceUpdate {
		forceSelfUpdateAndExit()
	}

	if nodeID == "" {
		log.Fatal("NODE_ID not set. Use --node-id or NODE_ID env var")
	}

	log.Printf("gtw-agent %s starting — node: %s", version, nodeID)

	gitPull()
	validateAndReload()

	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		gitPull()
		validateAndReload()
		sendHeartbeat()
	}
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
	if err := exec.Command("caddy", "validate", "--config", caddyfile).Run(); err != nil {
		log.Printf("Validation failed: %v", err)
		heartbeatOK = false
		return
	}
	if err := exec.Command("caddy", "reload", "--config", caddyfile).Run(); err != nil {
		log.Printf("Reload failed: %v", err)
		heartbeatOK = false
		return
	}
	log.Println("Caddy reloaded successfully")
	heartbeatOK = true
}

func sendHeartbeat() {
	sha, _ := exec.Command("git", "-C", configDir, "rev-parse", "HEAD").Output()
	payload := map[string]interface{}{
		"node_id":         nodeID,
		"git_sha":         string(bytes.TrimSpace(sha)),
		"agent_version":   version,
		"caddy_version":   getCaddyVersion(),
		"last_reload_ok":  heartbeatOK,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	}
	jsonBody, _ := json.Marshal(payload)

	// Silently fail — control plane not required yet
	http.Post(controlURL+"/api/heartbeat", "application/json", bytes.NewReader(jsonBody))

	// After sending heartbeat, ask the control plane if we are outdated
	resp, err := http.Get("https://control.gtw.uvrs.xyz/api/agent/latest-version")
	if err != nil {
		log.Printf("[update] check failed: %v", err)
	} else if resp.StatusCode == 200 {
		var v struct{ Version string `json:"version"` }
		if json.NewDecoder(resp.Body).Decode(&v) == nil && v.Version != "" && v.Version != version {
			log.Printf("Updating gtw-agent %s → %s", version, v.Version)
			tag := strings.TrimPrefix(v.Version, "v")
			go selfUpdate(tag)
		}
	}
	resp.Body.Close()
}
func selfUpdate(tag string) error {
	url := fmt.Sprintf("https://github.com/uverustech/gtw-agent/releases/download/%s/gtw-agent-linux-amd64", tag)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	tmp := "/usr/local/bin/gtw-agent.NEW"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	if err := os.Rename(tmp, "/usr/local/bin/gtw-agent"); err != nil {
		return err
	}

	log.Printf("Successfully updated to %s → restarting via systemd", tag)
	// Non-blocking restart
	go exec.Command("systemctl", "restart", "gtw-agent").Run()
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

	req, _ := http.NewRequest("GET", controlURL+"/api/agent-latest-version", nil)
	req.Header.Set("User-Agent", "gtw-agent/"+version)

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
		fmt.Printf("gtw-agent %s (latest: error fetching – %v)\n", version, err)
		os.Exit(1)
	}

	fmt.Printf("gtw-agent current:  %s\n", version)
	fmt.Printf("gtw-agent latest:   %s\n", latest)

	if latest != version {
		fmt.Printf("↑ Update available! Run: gtw-agent --update\n")
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

	fmt.Printf("Updating gtw-agent %s → %s ...\n", version, latest)
	tag := strings.TrimPrefix(latest, "v")

	if err := selfUpdate(tag); err != nil {
		log.Fatalf("Self-update failed: %v", err)
	}
	// selfUpdate already restarts via systemctl — we never return
	os.Exit(0)
}

