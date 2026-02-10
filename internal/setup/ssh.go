package setup

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

func RunSSH(cmd *cobra.Command, args []string) error {
	githubToken := viper.GetString(config.KeyGithubToken)
	sshKeyURL := viper.GetString(config.KeySSHKeyURL)
	autoConfirm := viper.GetBool(config.KeyAutoConfirm)

	if githubToken == "" {
		return fmt.Errorf("GitHub token is required for SSH setup. Set --github-token or GITHUB_TOKEN env var")
	}

	fmt.Printf("Fetching SSH public key from: %s\n", sshKeyURL)
	pubKey, err := fetchGithubFile(sshKeyURL, githubToken)
	if err != nil {
		return fmt.Errorf("failed to fetch SSH key: %w", err)
	}
	pubKey = strings.TrimSpace(pubKey)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	authKeysFile := filepath.Join(sshDir, "authorized_keys")

	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		if confirmAction(fmt.Sprintf("Create directory %s?", sshDir), autoConfirm) {
			if err := os.MkdirAll(sshDir, 0700); err != nil {
				return fmt.Errorf("failed to create %s: %w", sshDir, err)
			}
			fmt.Printf("Created %s\n", sshDir)
		} else {
			fmt.Println("Skipping SSH key installation")
			return nil
		}
	}

	content, _ := os.ReadFile(authKeysFile)
	if strings.Contains(string(content), pubKey) {
		fmt.Println("SSH key already exists in authorized_keys. Skipping")
		return nil
	}

	if confirmAction(fmt.Sprintf("Add SSH key to %s?", authKeysFile), autoConfirm) {
		f, err := os.OpenFile(authKeysFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", authKeysFile, err)
		}
		defer f.Close()

		if _, err := f.WriteString("\n" + pubKey + "\n"); err != nil {
			return fmt.Errorf("failed to write to %s: %w", authKeysFile, err)
		}
		fmt.Printf("Successfully added SSH key to %s\n", authKeysFile)
	} else {
		fmt.Println("Skipping SSH key installation")
	}

	return nil
}

func fetchGithubFile(urlStr, token string) (string, error) {
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

func confirmAction(message string, autoConfirm bool) bool {
	if autoConfirm {
		return true
	}

	fmt.Printf("%s (y/n): ", message)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(strings.TrimSpace(response)) == "y"
}
