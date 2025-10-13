package configure

import "github.com/spf13/cobra"


func NewCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "configure",
		Short: "Configure unitune infra",
		Long:  "Configure unitune infra",
	}

	return c
}