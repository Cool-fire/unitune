package configure

import (
	"context"
	"fmt"

	"github.com/Cool-fire/unitune/pkg/aws"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"

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

	validatePermissionSpinner := spinner.New().Title("Validating Permissions...").ActionWithErr(func(ctx context.Context) error {
		return validatePermissions()
	})

	if err := validatePermissionSpinner.Run(); err != nil {
		fmt.Printf("Failed to validate permissions: %v", err)
	}

	return nil
}

func validatePermissions() error {
	cfg, err := aws.GetAwsConfig()
	if err != nil {
		return fmt.Errorf("failed to get AWS config: %v", err)
	}

	sourceArn, err := aws.GetPolicySourceArn(cfg)
	if err != nil {
		return fmt.Errorf("failed to get policy source ARN: %v", err)
	}

	hasSimulatePermission, err := aws.HasSimulatePrincipalPolicyPermission(cfg, sourceArn)
	if err != nil {
		return fmt.Errorf("failed to check simulate permission: %v", err)
	}
	if !hasSimulatePermission {
		return fmt.Errorf("missing iam:SimulatePrincipalPolicy permission")
	}

	if err := aws.CheckRequiredPermissions(cfg); err != nil {
		return fmt.Errorf("permission validation failed: %v", err)
	}

	fmt.Printf("Testing succeded")

	return nil
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
