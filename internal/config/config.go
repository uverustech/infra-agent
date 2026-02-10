package config

import (
	"fmt"
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
}

func Load() error {
	viper.SetConfigName("infra-agent")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/infra-agent")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config: %w", err)
		}
	}
	return nil
}

func Save() error {
	filename := viper.ConfigFileUsed()
	if filename == "" {
		filename = "infra-agent.yaml"
	}
	return viper.WriteConfigAs(filename)
}

func MaskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "...." + s[len(s)-4:]
}
