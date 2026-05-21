package agent

import (
	"bytes"
	"log"
	"os/exec"
)

// ValidateAndReload is the package-level function used by the CLI gateway reload command.
// Returns (ok, errMsg).
func ValidateAndReload() (bool, string) {
	out, err := exec.Command("caddy", "validate", "--config", caddyfile).CombinedOutput()
	if err != nil {
		log.Printf("Validation failed: %v\n%s", err, string(out))
		return false, string(out)
	}

	out, err = exec.Command("caddy", "reload", "--config", caddyfile).CombinedOutput()
	if err != nil {
		log.Printf("Reload failed: %v\n%s", err, string(out))
		return false, string(out)
	}

	log.Println("Caddy reloaded successfully")
	return true, ""
}

// applyConfig runs ValidateAndReload and updates the agent's shared state under the reload lock.
func (a *Agent) applyConfig() {
	ok, errStr := ValidateAndReload()
	a.reloadMu.Lock()
	a.lastReloadOK = ok
	a.lastError = errStr
	a.reloadMu.Unlock()
}

func getCaddyVersion() string {
	out, _ := exec.Command("caddy", "version").Output()
	return string(bytes.TrimSpace(out))
}
