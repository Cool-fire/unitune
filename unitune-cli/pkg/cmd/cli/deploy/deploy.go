package deploy

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type DeployOptions struct {
}

func (o *DeployOptions) BindFlags(fs *pflag.FlagSet) {

}

func (o *DeployOptions) Run(cmd *cobra.Command, args []string) error {
	return nil
}

func AddCommand() *cobra.Command {
	o := &DeployOptions{}

	c := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the unitune infrastructure",
		Long:  "Deploy the unitune infrastructure",
		RunE:  func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd, args)
		},
	}

	o.BindFlags(c.Flags())
	return c
}