package setup

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

func confirmAction(message string, autoConfirm bool) bool {
	if autoConfirm || viper.GetBool(config.KeyAutoConfirm) {
		return true
	}

	fmt.Printf("%s (y/n): ", message)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(strings.TrimSpace(response)) == "y"
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

func isPackageInstalled(pkg string) bool {
	err := exec.Command("dpkg", "-l", pkg).Run()
	return err == nil
}
