package setup

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

func RunHostname(cmd *cobra.Command, args []string) error {
	nodeID := viper.GetString(config.KeyNodeID)
	if nodeID == "" {
		return fmt.Errorf("node-id is not set. Cannot configure hostname")
	}

	staticHostname := nodeID
	prettyHostname := strings.Split(nodeID, ".")[0]

	fmt.Printf("Configuring hostname:\n  Static: %s\n  Pretty: %s\n", staticHostname, prettyHostname)

	if viper.GetBool(config.KeyAutoConfirm) || confirmAction(fmt.Sprintf("Update system hostname to %s?", staticHostname), false) {
		if err := runCmd("hostnamectl", "set-hostname", staticHostname); err != nil {
			return fmt.Errorf("failed to set static hostname: %w", err)
		}
		if err := runCmd("hostnamectl", "set-hostname", "--pretty", prettyHostname); err != nil {
			return fmt.Errorf("failed to set pretty hostname: %w", err)
		}
		fmt.Println("✓ Hostname updated successfully")
	} else {
		fmt.Println("Skipping hostname configuration")
	}

	return nil
}
