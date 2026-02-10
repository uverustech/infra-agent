package agent

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func SelfUpdate(tag string, verbose bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}

	if verbose {
		log.Printf("[update] current executable: %s", exe)
	}

	url := fmt.Sprintf("https://github.com/uverustech/infra-agent/releases/download/v%s/infra-agent-linux-amd64", tag)
	if verbose {
		log.Printf("[update] downloading from: %s", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
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

	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	log.Printf("[update] successfully replaced binary → restarting via systemd")
	// Non-blocking restart
	go func() {
		time.Sleep(500 * time.Millisecond)
		cmd := exec.Command("sudo", "systemctl", "restart", "infra-agent")
		if verbose {
			log.Printf("[update] executing: %s %v", cmd.Path, cmd.Args)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[update] failed to restart service: %v. Output: %s", err, string(out))
		}
	}()
	return nil
}
