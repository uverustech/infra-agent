package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

func (a *Agent) sendHeartbeat() {
	nodeID := viper.GetString(config.KeyNodeID)
	nodeType := viper.GetString(config.KeyNodeType)
	controlURL := viper.GetString(config.KeyControlURL)

	sha, _ := exec.Command("git", "-C", configDir, "rev-parse", "HEAD").Output()

	a.reloadMu.RLock()
	reloadOK := a.lastReloadOK
	lastErr := a.lastError
	a.reloadMu.RUnlock()

	isHealthy, summary, healthData := a.getSystemMetrics(nodeType)

	payload := map[string]interface{}{
		"node_id":        nodeID,
		"git_sha":        string(bytes.TrimSpace(sha)),
		"agent_version":  a.version,
		"caddy_version":  getCaddyVersion(),
		"last_reload_ok": reloadOK,
		"last_error":     lastErr,
		"node_type":      nodeType,
		"is_healthy":     isHealthy,
		"health_summary": summary,
		"health_data":    healthData,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}
	jsonBody, _ := json.Marshal(payload)

	resp, err := a.httpClient.Post(controlURL+"/api/infra/heartbeat", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		log.Printf("[heartbeat] failed: %v", err)
	} else {
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			log.Printf("[heartbeat] server error: %s", resp.Status)
		}
		resp.Body.Close()
	}

	a.checkForUpdate(controlURL)
}

func (a *Agent) checkForUpdate(controlURL string) {
	if a.updateRunning.Load() {
		return
	}

	latest, err := GetLatestVersion(controlURL)
	if err != nil || latest == "" || latest == a.version {
		return
	}

	tag := strings.TrimPrefix(latest, "v")
	log.Printf("[update] triggering update %s → %s", a.version, latest)

	a.updateRunning.Store(true)
	go func() {
		defer a.updateRunning.Store(false)
		if err := SelfUpdate(tag, viper.GetBool(config.KeyVerbose)); err != nil {
			log.Printf("[update] error: %v", err)
		}
	}()
}

// GetLatestVersion fetches the latest agent version string from the control plane.
func GetLatestVersion(controlURL string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(controlURL + "/api/infra/agent/latest-version")
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
	return payload.Version, nil
}

func (a *Agent) getSystemMetrics(nodeType string) (bool, string, map[string]interface{}) {
	isHealthy := true
	var issues []string
	data := make(map[string]interface{})

	if v, err := getDiskUsage("/"); err == nil {
		data["disk_usage"] = v
		if v > 90 {
			isHealthy = false
			issues = append(issues, "Disk space critical")
		}
	}

	if v, err := getMemoryUsage(); err == nil {
		data["mem_usage"] = v
		if v > 95 {
			isHealthy = false
			issues = append(issues, "Memory usage critical")
		}
	}

	if v, err := getLoadAverage(); err == nil {
		data["load_avg"] = v
		// Critical when load exceeds 2× the number of logical cores.
		if v > float64(runtime.NumCPU())*2 {
			isHealthy = false
			issues = append(issues, fmt.Sprintf("Load average critical (%.2f)", v))
		}
	}

	if v, err := getUptime(); err == nil {
		data["uptime"] = v
	}

	if nodeType == "gateway" {
		a.reloadMu.RLock()
		reloadOK := a.lastReloadOK
		a.reloadMu.RUnlock()
		data["caddy_ok"] = reloadOK
		if !reloadOK {
			isHealthy = false
			issues = append(issues, "Caddy reload failed")
		}
	}

	summary := "All systems nominal"
	if len(issues) > 0 {
		summary = strings.Join(issues, ", ")
	}
	return isHealthy, summary, data
}

func getDiskUsage(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	all := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	if all == 0 {
		return 0, nil
	}
	return float64(all-free) / float64(all) * 100, nil
}

func getMemoryUsage() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	var total, available uint64
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			fmt.Sscanf(line, "MemTotal: %d", &total)
		case strings.HasPrefix(line, "MemAvailable:"):
			fmt.Sscanf(line, "MemAvailable: %d", &available)
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(total-available) / float64(total) * 100, nil
}

// getLoadAverage returns the 1-minute load average from /proc/loadavg.
func getLoadAverage() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	var load float64
	fmt.Sscanf(string(data), "%f", &load)
	return load, nil
}

func getUptime() (string, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", err
	}
	var secs float64
	fmt.Sscanf(string(data), "%f", &secs)
	days := int(secs) / 86400
	secs -= float64(days * 86400)
	hours := int(secs) / 3600
	secs -= float64(hours * 3600)
	minutes := int(secs) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes), nil
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes), nil
	}
	return fmt.Sprintf("%dm", minutes), nil
}
