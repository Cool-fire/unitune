package configure

import (
	"context"
	"fmt"

	"github.com/Cool-fire/unitune/pkg/aws"
	"github.com/Cool-fire/unitune/pkg/infra"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type ConfigureOptions struct {
	skipConfirm bool
	dryRun      bool
}

func (o *ConfigureOptions) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVarP(&o.skipConfirm, "yes", "y", false, "Skip confirmation prompt")
	fs.BoolVar(&o.dryRun, "dry-run", false, "Only show what would be deployed (cdk diff)")
}

func (o *ConfigureOptions) Run(c *cobra.Command, args []string) error {
	// Confirmation prompt
	if !o.skipConfirm {
		confirmationTxt := "unitune will provision cloud resources in your AWS account. This will incur charges. Continue?"

		var confirm bool
		huh.NewConfirm().
			Title(confirmationTxt).
			Affirmative("Yes, proceed").
			Negative("No, cancel").
			Value(&confirm).
			Run()

		if !confirm {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Step 1: Validate AWS permissions
	fmt.Println("\n🔐 Step 1/4: Validating AWS permissions...")
	validateSpinner := spinner.New().Title("Checking permissions...").ActionWithErr(func(ctx context.Context) error {
		return validatePermissions()
	})

	if err := validateSpinner.Run(); err != nil {
		return fmt.Errorf("permission validation failed: %w", err)
	}
	fmt.Println("   ✓ AWS permissions validated")

	// Step 2: Extract infrastructure
	fmt.Println("\n📦 Step 2/4: Preparing infrastructure...")
	var infraDir string
	extractSpinner := spinner.New().Title("Extracting CDK infrastructure...").ActionWithErr(func(ctx context.Context) error {
		var err error
		infraDir, err = infra.EnsureInfraExtracted()
		return err
	})

	if err := extractSpinner.Run(); err != nil {
		return fmt.Errorf("failed to extract infrastructure: %w", err)
	}
	fmt.Println("   ✓ Infrastructure ready")

	// Step 3: Install dependencies (only if needed)
	fmt.Println("\n📥 Step 3/4: Checking dependencies...")
	if err := infra.EnsureDependenciesInstalled(infraDir); err != nil {
		return fmt.Errorf("failed to install dependencies: %w", err)
	}
	fmt.Println("   ✓ Dependencies ready")

	// Step 4: Deploy infrastructure
	if o.dryRun {
		fmt.Println("\n📋 Step 4/4: Showing infrastructure diff (dry-run)...")
		if err := infra.RunCDK(infraDir, "diff", "--all"); err != nil {
			return fmt.Errorf("cdk diff failed: %w", err)
		}
		fmt.Println("\n✅ Dry-run complete. Run without --dry-run to deploy.")
		return nil
	}

	fmt.Println("\n🚀 Step 4/4: Deploying infrastructure...")

	// Bootstrap CDK (idempotent)
	fmt.Println("   → Bootstrapping CDK...")
	if err := infra.RunCDK(infraDir, "bootstrap"); err != nil {
		// Bootstrap might fail if already done, continue anyway
		fmt.Printf("   ⚠ Bootstrap warning (may already be bootstrapped)\n")
	}

	// Deploy all stacks
	fmt.Println("   → Deploying stacks...")
	if err := infra.RunCDK(infraDir, "deploy", "--all", "--require-approval", "broadening"); err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	fmt.Println("\n✅ Configuration complete! Your infrastructure is ready.")
	fmt.Println("   Run 'unitune deploy' to deploy your workloads.")

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

	return nil
}

func NewCommand() *cobra.Command {
	o := &ConfigureOptions{}

	c := &cobra.Command{
		Use:   "configure",
		Short: "Configure and deploy unitune infrastructure",
		Long: `Configure and deploy the unitune infrastructure to AWS.

This command will:
  1. Validate your AWS permissions
  2. Extract the CDK infrastructure
  3. Install dependencies (first time only)
  4. Deploy VPC, EKS cluster, and Karpenter

The infrastructure is cached in ~/.unitune/infra/ for faster subsequent runs.

Prerequisites:
  - AWS credentials configured (aws configure)
  - Node.js 18+ installed`,
		RunE: func(c *cobra.Command, args []string) error {
			return o.Run(c, args)
		},
	}

	o.BindFlags(c.Flags())
	return c
}
