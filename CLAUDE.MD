# Unitune - AI Assistant Guide

## Project Overview

Unitune is a lightweight, open-source CLI tool for deploying and managing ML infrastructure on AWS EKS (Kubernetes). It automates the provisioning of GPU/CPU autoscaling clusters optimized for machine learning model finetuning and training workloads.

**Tech Stack:**
- Go (CLI application with Cobra framework)
- TypeScript + AWS CDK (Infrastructure as Code)
- Kubernetes (EKS) with Karpenter for autoscaling
- BuildKit for container building on Kubernetes

## Architecture

### Two-Component Design

1. **Unitune CLI (Go)**: Located in `unitune-cli/`
   - Command-line interface for infrastructure management
   - AWS service interactions (EKS, S3, ECR, IAM)
   - Kubernetes job orchestration
   - Embeds infrastructure code in the binary

2. **Unitune Infrastructure (TypeScript)**: Located in `unitune-infra/`
   - AWS CDK stacks for VPC and EKS
   - Karpenter setup for node autoscaling
   - Helm charts for Kubernetes resources

**Key Design Pattern**: Infrastructure code is embedded into the Go binary using `//go:embed`, ensuring single-binary distribution without external dependencies.

## Directory Structure

```
unitune/
├── unitune-cli/               # Go CLI application
│   ├── cmd/unitune/           # Main entry point
│   ├── pkg/
│   │   ├── aws/               # AWS service clients (EKS, S3, ECR, IAM)
│   │   ├── cmd/cli/           # Command implementations
│   │   │   ├── configure/     # Deploy infrastructure
│   │   │   ├── destroy/       # Teardown infrastructure
│   │   │   └── deploy/        # Build & push containers
│   │   ├── k8s/               # Kubernetes integration (BuildKit jobs)
│   │   └── infra/             # Infrastructure code embedding
│   ├── go.mod & go.sum        # Go dependencies
│
├── unitune-infra/             # TypeScript CDK infrastructure
│   ├── bin/unitune-infra.ts   # CDK app entry point
│   ├── lib/
│   │   ├── stacks/            # VPC and EKS stacks
│   │   ├── constructs/k8s/    # Karpenter constructs
│   │   └── helm/              # Karpenter Helm charts
│   ├── package.json           # NPM dependencies
│   ├── cdk.json               # CDK configuration
│
├── Makefile                   # Build automation
├── README.md
└── LICENSE (MIT)
```

## Key Commands

### CLI Commands

1. **`unitune configure`**: Deploy infrastructure
   - Validates AWS permissions
   - Extracts and deploys CDK stacks
   - Creates VPC, EKS cluster, and Karpenter
   - Flags: `--yes` (skip confirmation), `--dry-run` (preview)

2. **`unitune deploy`**: Build and push containers
   - Uploads build context to S3
   - Runs BuildKit job on EKS
   - Pushes image to ECR
   - Flags: `--dry-run` (print YAML), `--image-name` (ECR repo)

3. **`unitune destroy`**: Teardown infrastructure
   - Removes all AWS resources
   - Cleans up Karpenter resources
   - Flags: `--yes` (skip confirmation), `--dry-run` (preview)

### Build Commands (Makefile)

```bash
make setup       # Install dependencies
make build       # Build CLI for current platform
make build-all   # Cross-platform build
make install     # Install to /usr/local/bin
make test        # Run tests
make dev         # Development mode with live reload
```

## Working with the Code

### Go Code Patterns

- **Service-Based Architecture**: Separate packages for AWS, K8s, and CLI
- **Error Handling**: Use `fmt.Errorf("context: %w", err)` for wrapping
- **Command Structure**: Each command has an `*Options` struct with `BindFlags()` and `Run()` methods
- **Dependency Injection**: Pass clients as constructor arguments

### TypeScript/CDK Patterns

- **Construct Composition**: Reusable CDK constructs in `lib/constructs/`
- **Stack Dependencies**: EKS stack receives VPC stack via interface
- **Helm Integration**: Custom charts deployed as CDK assets
- **Resource Cleanup**: Explicit Lambda-based cleanup for Karpenter

### Kubernetes Patterns

- **Namespace**: Build jobs run in `unitune-build` namespace
- **Service Accounts**: Use EKS Pod Identity for IAM permissions
- **TTL Cleanup**: Jobs auto-delete 5 minutes after completion
- **Template Rendering**: Go templates in `pkg/k8s/templates/`

## Build & Deployment Flow

### Infrastructure Deployment

```
AWS Account → CDK Bootstrap
           → CloudFormation Stack
           → VPC (Multi-AZ NAT)
           → EKS Cluster (K8s 1.31)
           → Karpenter Controller
           → Node Pools (GPU/CPU)
```

### Container Build Flow

```
Local Directory → Tar archive → S3 upload
                              → BuildKit Job on EKS
                              → ECR push
                              → Image: {accountId}.dkr.ecr.{region}.amazonaws.com/{name}:{tag}
```

## Important Files

### Go Files

- `unitune-cli/cmd/unitune/unitune.go`: CLI entry point
- `unitune-cli/pkg/aws/service.go`: AWS client configuration
- `unitune-cli/pkg/aws/eks_service.go`: EKS cluster interaction
- `unitune-cli/pkg/aws/permissions/permissions.json`: Required AWS permissions
- `unitune-cli/pkg/k8s/build_job.go`: BuildKit job orchestration
- `unitune-cli/pkg/k8s/templates/buildkit_job.yaml`: Job template
- `unitune-cli/pkg/infra/embed.go`: Infrastructure embedding logic

### TypeScript Files

- `unitune-infra/bin/unitune-infra.ts`: CDK app entry
- `unitune-infra/lib/stacks/vpc-stack.ts`: VPC with NAT and flow logs
- `unitune-infra/lib/stacks/eks-stack.ts`: EKS cluster configuration
- `unitune-infra/lib/constructs/k8s/karpenter.ts`: Karpenter setup
- `unitune-infra/lib/helm/karpenter-config/templates/nodepool-cpu.yaml`: Node pool config

### Configuration Files

- `Makefile`: Build automation and cross-compilation
- `unitune-infra/cdk.json`: CDK feature flags and config
- `unitune-cli/go.mod`: Go dependencies
- `unitune-infra/package.json`: NPM dependencies

## AWS Resources Created

- **VPC**: Multi-AZ with NAT gateways and flow logs
- **EKS Cluster**: Kubernetes 1.31 with full logging
- **Karpenter**: Autoscaling controller with EventBridge rules
- **IAM Roles**: Cluster role, node role, Karpenter controller role
- **S3 Bucket**: Build context storage (per account/region)
- **ECR Repository**: Container image registry
- **Lambda**: Cleanup function for Karpenter resources
- **SQS Queue**: EC2 interruption notifications

## Testing & Development

### Running Tests

```bash
cd unitune-cli && go test ./...
cd unitune-infra && npm test
```

### Local Development

```bash
make dev  # Runs with live reload
```

### Building Locally

```bash
make setup  # First time only
make build  # Creates binary in unitune-cli/bin/
```

## Common Tasks for AI Assistants

### Adding a New CLI Command

1. Create new package in `unitune-cli/pkg/cmd/cli/`
2. Define `*Options` struct with flags
3. Implement `BindFlags()` and `Run()` methods
4. Register command in parent command's `init()`

### Modifying Infrastructure

1. Update CDK code in `unitune-infra/lib/`
2. Test with `cdk diff` in the infra directory
3. Changes auto-deploy via `unitune configure`

### Adding AWS Service Integration

1. Create new file in `unitune-cli/pkg/aws/`
2. Follow service pattern (client initialization, methods)
3. Add required permissions to `permissions.json`

### Working with Kubernetes Jobs

1. Update template in `pkg/k8s/templates/buildkit_job.yaml`
2. Modify rendering logic in `pkg/k8s/template_renderer.go`
3. Test job submission with `--dry-run` flag

## Dependencies & Versions

### Key Go Dependencies

- `github.com/spf13/cobra` v1.10.1 (CLI framework)
- `github.com/aws/aws-sdk-go-v2` v1.41.1 (AWS SDK)
- `k8s.io/client-go` v0.29.0 (Kubernetes client)
- `github.com/charmbracelet/huh` v0.8.0 (Interactive prompts)

### Key TypeScript Dependencies

- `aws-cdk-lib` 2.208.0 (CDK framework)
- `typescript` ~5.6.3

### Infrastructure Versions

- Kubernetes: 1.31
- Karpenter: v1.6.3
- Bottlerocket OS: latest

## Security & Permissions

### AWS Permissions Required

The CLI validates permissions before deployment. See `unitune-cli/pkg/aws/permissions/permissions.json` for the full list.

Key services:
- CloudFormation (stack management)
- IAM (roles and policies)
- EKS (cluster operations)
- EC2 (VPC, security groups, instances)
- S3 (build context storage)
- ECR (container registry)

### Best Practices

- Never commit AWS credentials
- Use IAM roles for EKS workloads (Pod Identity)
- Validate permissions before deployment
- Clean up resources with `unitune destroy`

## Troubleshooting

### Common Issues

1. **Permission Denied**: Run `unitune configure` to validate AWS permissions
2. **Build Failures**: Check BuildKit job logs in EKS
3. **Infrastructure Deploy Fails**: Check CloudFormation events in AWS Console
4. **Embedded Files Missing**: Run `make prepare-embed` before building

### Debugging

- Use `--dry-run` flag to preview operations
- Check CloudWatch Logs for EKS cluster logs
- Use `kubectl logs` for BuildKit job logs
- Enable verbose logging with `-v` flag (if implemented)

## Git Branch Workflow

All development happens on feature branches:
- Branch naming: `claude/*` prefix with session ID suffix
- Always push to the specified branch
- Create PRs against the main branch when ready

## License

MIT License - Copyright 2025 Upendra Reddy

## Notes for AI Assistants

- **Prefer editing over creating**: Always modify existing files rather than creating new ones
- **Test before committing**: Run `make test` for Go changes, `npm test` for infrastructure changes
- **Follow patterns**: Maintain consistency with existing code structure
- **Validate permissions**: Infrastructure changes may require new AWS permissions
- **Embedded files**: After modifying infrastructure code, run `make prepare-embed` before building
- **Cross-platform**: CLI should work on macOS (arm64/amd64) and Linux (amd64/arm64)
