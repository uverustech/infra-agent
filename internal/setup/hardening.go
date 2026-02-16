package setup

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

func RunHardening(cmd *cobra.Command, args []string) error {
	steps := []struct {
		name string
		fn   func(*cobra.Command, []string) error
	}{
		{"ensure-openssh-server", RunEnsureOpenSSH},
		{"setup-ssh-keys", RunSSH},
		{"disable-ssh-password-and-pam", RunDisableSSHPasswordAndPAM},
		{"install-and-configure-ufw", RunConfigureUFW},
		{"install-and-configure-fail2ban", RunConfigureFail2Ban},
		{"verify-hardening-status", RunVerifyHardening},
	}

	for _, s := range steps {
		fmt.Printf("--- Step: %s ---\n", s.name)
		if err := s.fn(cmd, nil); err != nil {
			return fmt.Errorf("step %s failed: %w", s.name, err)
		}
		fmt.Println()
	}

	return nil
}

func RunEnsureOpenSSH(cmd *cobra.Command, args []string) error {
	if isPackageInstalled("openssh-server") {
		fmt.Println("openssh-server is already installed.")
	} else {
		fmt.Println("Installing openssh-server...")
		if err := runCmd("apt-get", "update"); err != nil {
			return err
		}
		if err := runCmd("apt-get", "install", "-y", "openssh-server"); err != nil {
			return err
		}
	}

	fmt.Println("Enabling and starting ssh service...")
	if err := runCmd("systemctl", "enable", "--now", "ssh"); err != nil {
		return err
	}

	if err := runCmd("systemctl", "is-active", "--quiet", "ssh"); err != nil {
		return fmt.Errorf("ssh service is not active: %w", err)
	}

	fmt.Println("✓ SSH Server is running and enabled")
	return nil
}

func RunDisableSSHPasswordAndPAM(cmd *cobra.Command, args []string) error {
	confPath := "/etc/ssh/sshd_config.d/99-infra-hardening.conf"
	confContent := `PasswordAuthentication no
ChallengeResponseAuthentication no
UsePAM no
PermitRootLogin prohibit-password
PubkeyAuthentication yes
`
	fmt.Printf("Creating %s...\n", confPath)
	if err := os.MkdirAll("/etc/ssh/sshd_config.d", 0755); err != nil {
		return err
	}
	if err := os.WriteFile(confPath, []byte(confContent), 0644); err != nil {
		return err
	}

	cloudInitConf := "/etc/ssh/sshd_config.d/50-cloud-init.conf"
	if _, err := os.Stat(cloudInitConf); err == nil {
		fmt.Printf("Removing conflicting cloud-init override: %s\n", cloudInitConf)
		os.Remove(cloudInitConf)
	}

	fmt.Println("Restarting ssh service...")
	if err := runCmd("systemctl", "restart", "ssh"); err != nil {
		return err
	}

	fmt.Println("Verifying SSH configuration...")
	out, err := exec.Command("sshd", "-T").Output()
	if err != nil {
		return fmt.Errorf("failed to run sshd -T: %w", err)
	}
	settings := string(out)
	checks := []string{"passwordauthentication no", "usepam no", "challengeresponseauthentication no"}
	for _, check := range checks {
		if !strings.Contains(strings.ToLower(settings), check) {
			return fmt.Errorf("verification failed: %s not found in sshd -T", check)
		}
	}

	fmt.Println("✓ SSH Password and PAM disabled")
	return nil
}

func RunConfigureUFW(cmd *cobra.Command, args []string) error {
	if !isPackageInstalled("ufw") {
		fmt.Println("Installing ufw...")
		if err := runCmd("apt-get", "install", "-y", "ufw"); err != nil {
			return err
		}
	}

	fmt.Println("Configuring UFW...")
	runCmd("ufw", "default", "deny", "incoming")
	runCmd("ufw", "default", "allow", "outgoing")
	runCmd("ufw", "allow", "OpenSSH")

	fmt.Println("Enabling UFW...")
	if err := runCmd("ufw", "--force", "enable"); err != nil {
		return err
	}

	if err := runCmd("ufw", "reload"); err != nil {
		return err
	}

	fmt.Println("✓ UFW is active and configured")
	return nil
}

func RunConfigureFail2Ban(cmd *cobra.Command, args []string) error {
	if !isPackageInstalled("fail2ban") {
		fmt.Println("Installing fail2ban...")
		if err := runCmd("apt-get", "install", "-y", "fail2ban"); err != nil {
			return err
		}
	}

	fmt.Println("Enabling and starting fail2ban...")
	if err := runCmd("systemctl", "enable", "--now", "fail2ban"); err != nil {
		return err
	}

	jailPath := "/etc/fail2ban/jail.d/sshd-aggressive.local"
	jailContent := `[sshd]
enabled  = true
mode     = aggressive
maxretry = 4
findtime = 10m
bantime  = 3600
ignoreip = 127.0.0.1/8 ::1
`
	fmt.Printf("Creating %s...\n", jailPath)
	if err := os.WriteFile(jailPath, []byte(jailContent), 0644); err != nil {
		return err
	}

	fmt.Println("Reloading fail2ban...")
	if err := runCmd("fail2ban-client", "reload"); err != nil {
		return err
	}

	fmt.Println("✓ Fail2Ban is active with aggressive SSH jail")
	return nil
}

func RunVerifyHardening(cmd *cobra.Command, args []string) error {
	fmt.Println("--- Hardening Status Summary ---")

	checkStatus("SSH Service", "systemctl", "is-active", "--quiet", "ssh")
	checkStatus("UFW Service", "ufw", "status")
	checkStatus("Fail2Ban Service", "systemctl", "is-active", "--quiet", "fail2ban")

	fmt.Println("\nDetailed SSH Checks (sshd -T):")
	out, _ := exec.Command("sshd", "-T").Output()
	settings := strings.ToLower(string(out))
	results := map[string]bool{
		"PasswordAuthentication": strings.Contains(settings, "passwordauthentication no"),
		"UsePAM":                 strings.Contains(settings, "usepam no"),
		"PermitRootLogin":        strings.Contains(settings, "permitrootlogin prohibit-password") || strings.Contains(settings, "permitrootlogin no"),
	}

	for k, v := range results {
		status := "✘ RED"
		if v {
			status = "✓ GREEN"
		}
		fmt.Printf("  %-25s: %s\n", k, status)
	}

	return nil
}

// Helpers

func isPackageInstalled(pkg string) bool {
	err := exec.Command("dpkg", "-l", pkg).Run()
	return err == nil
}

func runCmd(name string, args ...string) error {
	if viper.GetBool(config.KeyVerbose) {
		fmt.Printf("Running: %s %s\n", name, strings.Join(args, " "))
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func checkStatus(label string, name string, args ...string) {
	err := exec.Command(name, args...).Run()
	status := "✘ RED"
	if err == nil {
		status = "✓ GREEN"
	}
	fmt.Printf("%-20s: %s\n", label, status)
}
