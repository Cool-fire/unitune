package destroy

import (
	"context"
	"fmt"

	"github.com/Cool-fire/unitune/pkg/infra"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type DestroyOptions struct {
	skipConfirm bool
	dryRun      bool
}

func (o *DestroyOptions) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVarP(&o.skipConfirm, "yes", "y", false, "Skip confirmation prompt")
	fs.BoolVar(&o.dryRun, "dry-run", false, "Only show what would be destroyed (cdk diff)")
}

func (o *DestroyOptions) Run(c *cobra.Command, args []string) error {
	// Confirmation prompt
	if !o.skipConfirm {
		fmt.Println("\n⚠️  WARNING: This will destroy ALL unitune infrastructure!")
		fmt.Println("   This includes: VPC, EKS cluster, Karpenter, and all associated resources.")
		fmt.Println("   This action cannot be undone.\n")

		var confirm bool
		huh.NewConfirm().
			Title("Are you sure you want to destroy all infrastructure?").
			Affirmative("Yes, destroy everything").
			Negative("No, cancel").
			Value(&confirm).
			Run()

		if !confirm {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Step 1: Check if infrastructure is already extracted, extract only if needed
	fmt.Println("\n📦 Step 1/3: Preparing infrastructure...")
	extracted, infraDir, err := infra.IsInfraExtracted()
	if err != nil {
		return fmt.Errorf("failed to check infrastructure: %w", err)
	}

	if extracted {
		fmt.Println("   ✓ Infrastructure already extracted")
	} else {
		extractSpinner := spinner.New().Title("Extracting CDK infrastructure...").ActionWithErr(func(ctx context.Context) error {
			var err error
			infraDir, err = infra.EnsureInfraExtracted()
			return err
		})

		if err := extractSpinner.Run(); err != nil {
			return fmt.Errorf("failed to extract infrastructure: %w", err)
		}
		fmt.Println("   ✓ Infrastructure ready")
	}

	// Step 2: Install dependencies
	fmt.Println("\n📥 Step 2/3: Checking dependencies...")
	if err := infra.EnsureDependenciesInstalled(infraDir); err != nil {
		return fmt.Errorf("failed to install dependencies: %w", err)
	}
	fmt.Println("   ✓ Dependencies ready")

	// Step 3: Destroy infrastructure
	if o.dryRun {
		fmt.Println("\n📋 Step 3/3: Showing infrastructure to be destroyed (dry-run)...")
		if err := infra.RunCDK(infraDir, "diff", "--all"); err != nil {
			return fmt.Errorf("cdk diff failed: %w", err)
		}
		fmt.Println("\n✅ Dry-run complete. Run without --dry-run to destroy.")
		return nil
	}

	fmt.Println("\n🗑️  Step 3/3: Destroying infrastructure...")
	fmt.Println("   → Destroying all stacks...")

	// Build destroy command args
	destroyArgs := []string{"destroy", "--all"}
	if o.skipConfirm {
		destroyArgs = append(destroyArgs, "--force")
	}

	if err := infra.RunCDK(infraDir, destroyArgs...); err != nil {
		return fmt.Errorf("destroy failed: %w", err)
	}

	// Clean up local cache
	fmt.Println("\n🧹 Cleaning up local cache...")
	if err := infra.CleanInfraCache(); err != nil {
		fmt.Printf("   ⚠ Warning: failed to clean cache: %v\n", err)
	} else {
		fmt.Println("   ✓ Local cache cleaned")
	}

	fmt.Println("\n✅ Infrastructure destroyed successfully.")

	return nil
}

func NewCommand() *cobra.Command {
	o := &DestroyOptions{}

	c := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy all unitune infrastructure",
		Long: `Destroy all unitune infrastructure from AWS.

This command will destroy VPC, EKS cluster, Karpenter, and all associated resources.

⚠️  WARNING: This action is irreversible and will delete:
  - The EKS cluster and all workloads running on it
  - The VPC and all networking resources
  - Karpenter and all provisioned nodes
  - Any data stored in cluster-local volumes

Make sure to backup any important data before proceeding.`,
		RunE: func(c *cobra.Command, args []string) error {
			return o.Run(c, args)
		},
	}

	o.BindFlags(c.Flags())
	return c
}
