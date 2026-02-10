package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/agent"
	"github.com/uverustech/infra-agent/internal/config"
	"github.com/uverustech/infra-agent/internal/setup"
)

var (
	RootCmd = &cobra.Command{
		Use:   "infra-agent",
		Short: "Infra Agent - uverustech infrastructure management",
		Run: func(cmd *cobra.Command, args []string) {
			agent.Run(version)
		},
	}

	setupCmd = &cobra.Command{
		Use:   "setup",
		Short: "Run system setup tasks",
		RunE:  setup.RunFullSetup,
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show current version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("infra-agent %s\n", version)
		},
	}

	updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Self-update the agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Logic to get latest tag and call agent.SelfUpdate
			return nil
		},
	}

	configCmd = &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Run: func(cmd *cobra.Command, args []string) {
			settings := viper.AllSettings()
			if len(settings) == 0 {
				fmt.Println("No configuration found.")
				return
			}
			fmt.Println("Current Configuration (Precedence: Flag > Env > Config > Default):")
			for k, v := range settings {
				source := "Default"
				if viper.InConfig(k) {
					source = "Config File"
				}
				if cmd.Flags().Changed(k) {
					source = "Flag"
				}
				// Check env? Viper doesn't make it easy to see if it came from env specifically

				if strings.Contains(k, "token") {
					fmt.Printf("  %-15s: %-20s (%s)\n", k, config.MaskSecret(fmt.Sprintf("%v", v)), source)
				} else {
					fmt.Printf("  %-15s: %-20s (%s)\n", k, v, source)
				}
			}
			fmt.Printf("\nConfig file used: %s\n", viper.ConfigFileUsed())
		},
	}

	configSetCmd = &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			viper.Set(args[0], args[1])
			return config.Save()
		},
	}

	configGetCmd = &cobra.Command{
		Use:   "get [key]",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(viper.Get(args[0]))
		},
	}

	gatewayCmd = &cobra.Command{
		Use:   "gateway",
		Short: "Gateway specific actions",
	}

	gatewayPullCmd = &cobra.Command{
		Use:   "pull",
		Short: "Pull latest geometry config",
		Run: func(cmd *cobra.Command, args []string) {
			agent.GitPull()
		},
	}

	gatewayReloadCmd = &cobra.Command{
		Use:   "reload",
		Short: "Validate and reload Caddy",
		Run: func(cmd *cobra.Command, args []string) {
			agent.ValidateAndReload()
		},
	}

	statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show gateway status and drift information",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := agent.GetStatus()
			if err != nil {
				return err
			}
			fmt.Printf("Node ID:        %s\n", status["node_id"])
			fmt.Printf("Node Type:      %s\n", status["node_type"])
			fmt.Printf("Agent Version:  %s\n", status["agent_version"])
			fmt.Printf("Local Git SHA:  %s\n", status["local_git_sha"])
			fmt.Printf("Remote Git SHA: %s\n", status["remote_git_sha"])
			if status["drift"].(bool) {
				fmt.Println("Status:         DRAINED (Drift detected! Run 'gateway pull' to sync)")
			} else {
				fmt.Println("Status:         HEALTHY (Up to date)")
			}
			return nil
		},
	}
)

func init() {
	config.Init()

	RootCmd.PersistentFlags().StringP(config.KeyNodeID, "i", "", "Node ID")
	RootCmd.PersistentFlags().StringP(config.KeyNodeType, "t", "server", "Node Type (gateway, server)")
	RootCmd.PersistentFlags().BoolP(config.KeyVerbose, "v", false, "Verbose output")
	RootCmd.PersistentFlags().BoolP(config.KeyAutoConfirm, "y", false, "Auto-confirm setup steps")

	viper.BindPFlag(config.KeyNodeID, RootCmd.PersistentFlags().Lookup(config.KeyNodeID))
	viper.BindPFlag(config.KeyNodeType, RootCmd.PersistentFlags().Lookup(config.KeyNodeType))
	viper.BindPFlag(config.KeyVerbose, RootCmd.PersistentFlags().Lookup(config.KeyVerbose))
	viper.BindPFlag(config.KeyAutoConfirm, RootCmd.PersistentFlags().Lookup(config.KeyAutoConfirm))

	RootCmd.AddCommand(versionCmd)
	RootCmd.AddCommand(setupCmd)
	RootCmd.AddCommand(updateCmd)
	RootCmd.AddCommand(configCmd)
	RootCmd.AddCommand(gatewayCmd)

	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	gatewayCmd.AddCommand(gatewayPullCmd)
	gatewayCmd.AddCommand(gatewayReloadCmd)
	gatewayCmd.AddCommand(statusCmd)

	// Add setup subcommands
	for _, step := range setup.Steps {
		stepCopy := step // capture loop var
		sub := &cobra.Command{
			Use:   stepCopy.Name,
			Short: fmt.Sprintf("Run %s setup step", stepCopy.Name),
			RunE:  stepCopy.Run,
		}
		setupCmd.AddCommand(sub)
	}
}

func Execute() {
	if err := config.Load(); err != nil {
		fmt.Printf("Warning: error loading config: %v\n", err)
	}

	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
