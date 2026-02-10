package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

var (
	heartbeatOK bool
	lastError   string
	wsConn      *websocket.Conn
	wsMu        sync.Mutex
)

var (
	currentVersion string
)

func Run(v string) {
	currentVersion = v
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Printf("Config file changed: %s. Re-applying settings...", e.Name)
	})
	viper.WatchConfig()

	nodeID := viper.GetString(config.KeyNodeID)
	nodeType := viper.GetString(config.KeyNodeType)

	if nodeID == "" {
		log.Fatal("node-id is required. Set it permanently with: infra-agent config set node-id <name>\nOr use --node-id once, or set INFRA_NODE_ID environment variable.")
	}

	log.Printf("infra-agent %s starting — node: %s", currentVersion, nodeID)

	if nodeType == "gateway" {
		GitPull()
		ValidateAndReload()
	}

	// Start log streaming in background
	go streamLogs()

	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		// Dynamic check: node type might have changed in config
		currNodeType := viper.GetString(config.KeyNodeType)
		if currNodeType == "gateway" && viper.GetBool(config.KeyAutoPull) {
			GitPull()
			ValidateAndReload()
		}
		sendHeartbeat()
	}
}

func streamLogs() {
	cmd := exec.Command("journalctl", "-f", "-o", "json", "-n", "0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[logs] failed to create stdout pipe: %v", err)
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

		payload := make(map[string]interface{})
		if err := json.Unmarshal([]byte(rawMsg), &payload); err != nil {
			payload["message"] = rawMsg
		}

		if unit, ok := entry["_SYSTEMD_UNIT"].(string); ok {
			payload["unit"] = unit
			if _, exists := payload["logger"]; !exists {
				payload["logger"] = strings.TrimSuffix(unit, ".service")
			}
		}

		if priority, ok := entry["PRIORITY"].(string); ok {
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

	cmd.Wait()
	log.Println("[logs] journalctl exited, restarting...")
	time.Sleep(2 * time.Second)
	go streamLogs()
}

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
	nodeID := viper.GetString(config.KeyNodeID)
	controlURL := viper.GetString(config.KeyControlURL)
	u := strings.Replace(controlURL, "https://", "wss://", 1) + "/api/logs/stream"
	header := http.Header{}
	header.Add("X-Node-ID", nodeID)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(u, header)
	if err != nil {
		log.Printf("[logs] ws connection failed: %v", err)
		return err
	}
	log.Printf("[logs] connected to control plane: %s", u)
	wsConn = conn
	return nil
}

func GitPull() {
	configDir := "/etc/caddy"
	cmd := exec.Command("git", "-C", configDir, "pull", "--ff-only")

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Git pull failed: %v\n%s", err, string(output))
		return
	}

	if bytes.Contains(output, []byte("Already up to date")) {
		return
	}

	log.Printf("Config updated via git pull:\n%s", string(output))
}

func ValidateAndReload() {
	caddyfile := "/etc/caddy/Caddyfile"
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
	nodeID := viper.GetString(config.KeyNodeID)
	nodeType := viper.GetString(config.KeyNodeType)
	controlURL := viper.GetString(config.KeyControlURL)
	configDir := "/etc/caddy"

	sha, _ := exec.Command("git", "-C", configDir, "rev-parse", "HEAD").Output()
	isHealthy, summary, healthData := getSystemMetrics(nodeType)

	payload := map[string]interface{}{
		"node_id":        nodeID,
		"git_sha":        string(bytes.TrimSpace(sha)),
		"agent_version":  currentVersion,
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
	resp, err := http.Post(controlURL+"/api/heartbeat", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		log.Printf("[heartbeat] failed: %v", err)
	} else {
		if resp.StatusCode != 200 {
			log.Printf("[heartbeat] server error: %s", resp.Status)
		}
		resp.Body.Close()
	}

	// Check for updates
	resp, err = http.Get(controlURL + "/api/agent/latest-version")
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		var v struct {
			Version string `json:"version"`
		}
		if json.NewDecoder(resp.Body).Decode(&v) == nil && v.Version != "" && v.Version != currentVersion {
			tag := strings.TrimPrefix(v.Version, "v")
			log.Printf("[update] triggering update %s → %s", currentVersion, v.Version)
			go func() {
				if err := SelfUpdate(tag, viper.GetBool(config.KeyVerbose)); err != nil {
					log.Printf("[update] error: %v", err)
				}
			}()
		}
		resp.Body.Close()
	}
}

func getSystemMetrics(nodeType string) (bool, string, map[string]interface{}) {
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

func getCaddyVersion() string {
	out, _ := exec.Command("caddy", "version").Output()
	return string(bytes.TrimSpace(out))
}

func GetLatestVersion(controlURL, currentVersion string) (string, error) {
	resp, err := http.Get(controlURL + "/api/agent/latest-version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Version, nil
}

func GetStatus() (map[string]interface{}, error) {
	nodeID := viper.GetString(config.KeyNodeID)
	nodeType := viper.GetString(config.KeyNodeType)
	configDir := "/etc/caddy"

	localSha, _ := exec.Command("git", "-C", configDir, "rev-parse", "HEAD").Output()
	localShaStr := string(bytes.TrimSpace(localSha))

	// Get remote SHA (ls-remote is fast and doesn't pull)
	remoteShaStr := "unknown"
	remoteCmd := exec.Command("git", "-C", configDir, "ls-remote", "origin", "HEAD")
	if out, err := remoteCmd.CombinedOutput(); err == nil {
		parts := strings.Fields(string(out))
		if len(parts) > 0 {
			remoteShaStr = parts[0]
		}
	} else {
		log.Printf("[status] failed to get remote sha: %v\n%s", err, string(out))
	}

	return map[string]interface{}{
		"node_id":        nodeID,
		"node_type":      nodeType,
		"agent_version":  currentVersion,
		"local_git_sha":  localShaStr,
		"remote_git_sha": remoteShaStr,
		"drift":          localShaStr != remoteShaStr && remoteShaStr != "unknown",
	}, nil
}
