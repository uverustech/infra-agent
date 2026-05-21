package agent

import (
	"bytes"
	"log"
	"os/exec"
	"strings"

	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)


// GitPull runs a fast-forward pull on the gateway config repo.
func GitPull() {
	output, err := exec.Command("git", "-C", configDir, "pull", "--ff-only").CombinedOutput()
	if err != nil {
		log.Printf("Git pull failed: %v\n%s", err, string(output))
		return
	}
	if !bytes.Contains(output, []byte("Already up to date")) {
		log.Printf("Config updated via git pull:\n%s", string(output))
	}
}

// GetStatus returns node and git drift info for the CLI status command.
func GetStatus() (map[string]interface{}, error) {
	nodeID := viper.GetString(config.KeyNodeID)
	nodeType := viper.GetString(config.KeyNodeType)

	localSha, _ := exec.Command("git", "-C", configDir, "rev-parse", "HEAD").Output()
	localShaStr := string(bytes.TrimSpace(localSha))

	remoteShaStr := "unknown"
	if out, err := exec.Command("git", "-C", configDir, "ls-remote", "origin", "HEAD").CombinedOutput(); err == nil {
		if parts := strings.Fields(string(out)); len(parts) > 0 {
			remoteShaStr = parts[0]
		}
	} else {
		log.Printf("[status] failed to get remote sha: %v\n%s", err, string(out))
	}

	drift := localShaStr != remoteShaStr && remoteShaStr != "unknown"

	return map[string]interface{}{
		"node_id":        nodeID,
		"node_type":      nodeType,
		"local_git_sha":  localShaStr,
		"remote_git_sha": remoteShaStr,
		"drift":          drift,
	}, nil
}
