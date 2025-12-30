package unitune

import (
	"github.com/Cool-fire/unitune/pkg/cmd/cli/configure"
	"github.com/Cool-fire/unitune/pkg/cmd/cli/deploy"
	"github.com/Cool-fire/unitune/pkg/cmd/cli/destroy"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "unitune",
		Short: "A lightweight tool for deploying and managing Machine learning infrastructure on K8s",
		Long: `Unitune - Deploy ML infrastructure on Kubernetes with ease.

Unitune provisions optimized Kubernetes clusters with:
  • EKS with Karpenter for GPU/CPU autoscaling
  • Pre-configured node pools for ML workloads
  • VPC with proper networking setup`,
	}

	c.AddCommand(
		configure.NewCommand(),
		destroy.NewCommand(),
		deploy.AddCommand(),
	)

	return c
}
