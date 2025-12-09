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
)

const version = "v1.0.0"

var (
	nodeID      string
	configDir   = "/etc/caddy"
	caddyfile   = "/etc/caddy/Caddyfile"
	controlURL  = "https://control.gtw.uvrs.xyz" // control dashboard
	heartbeatOK = false
)

func main() {
	flag.StringVar(&nodeID, "node-id", os.Getenv("NODE_ID"), "Node ID (e.g. svr-gtw-nd1.uvrs.xyz)")
	flag.Parse()

	if nodeID == "" {
		log.Fatal("NODE_ID not set. Use --node-id or NODE_ID env var")
	}

	log.Printf("gtw-agent %s starting — node: %s", version, nodeID)

	// Initial pull & reload
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

func selfUpdate(tag string) {
    url := fmt.Sprintf("https://github.com/uverustech/gtw-agent/releases/download/%s/gtw-agent-linux-amd64", tag)
    resp, _ := http.Get(url)
	if resp == nil || resp.StatusCode != 200 {
		log.Printf("Failed to download new version from %s", url)
		return
	}
    defer resp.Body.Close()
    tmp := "/usr/local/bin/gtw-agent.NEW"
    f, _ := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
    io.Copy(f, resp.Body)
    f.Close()
    os.Rename(tmp, "/usr/local/bin/gtw-agent")
    log.Printf("Updated to %s → restarting", tag)
    exec.Command("systemctl", "restart", "gtw-agent").Run()
}

func getCaddyVersion() string {
	out, _ := exec.Command("caddy", "version").Output()
	return string(bytes.TrimSpace(out))
}
