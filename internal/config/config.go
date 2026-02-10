package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
)

func Init() {
	viper.SetEnvPrefix("INFRA")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Default values
	viper.SetDefault(KeyControlURL, "https://control.uvrs.xyz")
	viper.SetDefault(KeyNodeType, "server")
	viper.SetDefault(KeySSHKeyURL, "https://github.com/uverustech/secrets/ssh-keys/uvr-ops/uvr_ops.pub")
	viper.SetDefault(KeyAutoPull, true)
}

func Load() error {
	viper.SetConfigName("infra-agent")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/infra-agent")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to read config: %w", err)
	}
	log.Printf("Using config file: %s", viper.ConfigFileUsed())
	return nil
}

func Save() error {
	filename := viper.ConfigFileUsed()
	if filename == "" {
		// Try to use /etc/infra-agent/infra-agent.yaml if writable, otherwise local
		const defaultPath = "/etc/infra-agent"
		const defaultFile = defaultPath + "/infra-agent.yaml"

		if err := os.MkdirAll(defaultPath, 0755); err == nil {
			filename = defaultFile
		} else {
			filename = "infra-agent.yaml"
		}
	}
	log.Printf("Saving configuration to: %s", filename)
	return viper.WriteConfigAs(filename)
}

func MaskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "...." + s[len(s)-4:]
}
