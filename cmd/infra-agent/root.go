package main

import (
	"fmt"
	"os"

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
			agent.Run()
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
