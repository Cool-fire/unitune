package unitune

import (
	"github.com/Cool-fire/unitune/pkg/cmd/cli/configure"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "unitune",
		Short: "A lightweight tool for deploying and managing Machine learning infrastructure on K8s",
	}

	c.AddCommand(
		configure.NewCommand(),
	)

	return c
}