package setup

import (
	"fmt"

	"github.com/spf13/cobra"
)

func RunFullSetup(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Starting Full System Setup ===")

	for _, step := range Steps {
		fmt.Printf("--- Step: %s ---\n", step.Name)
		if err := step.Run(cmd, args); err != nil {
			return fmt.Errorf("step %s failed: %w", step.Name, err)
		}
	}

	fmt.Println("=== Setup Complete ===")
	return nil
}
