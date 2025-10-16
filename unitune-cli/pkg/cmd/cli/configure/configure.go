package configure

import (
	"fmt"

	"github.com/Cool-fire/unitune/pkg/aws"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type ConfigureOptions struct {
	legacy bool
}

func (o *ConfigureOptions) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.legacy, "legacy", o.legacy, "Use legacy configuration format")
}

func (o *ConfigureOptions) Run(c *cobra.Command, args []string) error {
	confirmation_txt := "unitune configures cloud resources in your account and the initial setup will be charged, Please select yes if you wish to proceed"

	var confirm bool
	confirm_block := huh.NewConfirm().
		Title(confirmation_txt).
		Affirmative("Yes").
		Negative("No").
		Value(&confirm)

	confirm_block.Run()
	if !confirm {
		return fmt.Errorf("Configuration requires consent...")
	}

	validatePermissions()

	return nil
}

func validatePermissions() (bool, error) {
	_, err := aws.GetAwsConfig()
	if err != nil {
		return false, err
	}

	return true, nil
}

func NewCommand() *cobra.Command {
	o := &ConfigureOptions{}

	c := &cobra.Command{
		Use:   "configure",
		Short: "Configure unitune infra",
		Long:  "Configure unitune infra",
		RunE: func(c *cobra.Command, args []string) error {
			return o.Run(c, args)
		},
	}
	o.BindFlags(c.Flags())

	return c
}
