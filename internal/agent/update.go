package agent

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func SelfUpdate(tag string, verbose bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}

	// Normalize tag (ensure it doesn't have 'v' prefix for the URL if needed,
	// but the user's release assets seem to be under /v1.x.x/infra-agent-linux-...)
	// Release artifact name: infra-agent-linux-amd64 or infra-agent-linux-arm64
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	assetName := fmt.Sprintf("infra-agent-linux-%s", arch)
	url := fmt.Sprintf("https://github.com/uverustech/infra-agent/releases/download/v%s/%s", tag, assetName)

	log.Printf("[update] downloading %s from %s", assetName, url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d (tag v%s might not exist yet)", resp.StatusCode, tag)
	}

	tmp := exe + ".NEW"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %w", tmp, err)
	}

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to copy download: %w", err)
	}
	f.Close()

	// Verify the new binary (basic check)
	verifyCmd := exec.Command(tmp, "version")
	if out, err := verifyCmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("downloaded binary failed verification: %v (%s)", err, string(out))
	}

	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	log.Printf("[update] successfully replaced binary → restarting service")
	// Using systemctl restart is best for systemd-managed services
	go func() {
		time.Sleep(1 * time.Second)
		if err := exec.Command("sudo", "systemctl", "restart", "infra-agent").Run(); err != nil {
			log.Printf("[update] failed to restart service via systemctl: %v (trying to exit instead)", err)
			os.Exit(0)
		}
	}()
	return nil
}
