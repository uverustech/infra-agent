package setup

import "github.com/spf13/cobra"

type Step struct {
	Name string
	Run  func(cmd *cobra.Command, args []string) error
}

var Steps = []Step{
	{Name: "ssh", Run: RunSSH},
	{Name: "hardening", Run: RunHardening},
	{Name: "packages", Run: RunPackages},
	{Name: "timezone", Run: RunTimezone},
}
