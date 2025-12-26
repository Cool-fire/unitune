import * as cdk from 'aws-cdk-lib';
import { Aws } from 'aws-cdk-lib';
import { Code, Function, Runtime } from 'aws-cdk-lib/aws-lambda';
import { Cluster } from 'aws-cdk-lib/aws-eks';
import { KubectlV31Layer } from '@aws-cdk/lambda-layer-kubectl-v31';
import { Effect, PolicyStatement, Role, ServicePrincipal } from 'aws-cdk-lib/aws-iam';
import { CustomResource, Duration } from 'aws-cdk-lib';
import { Provider } from 'aws-cdk-lib/custom-resources';
import { Construct } from 'constructs/lib/construct';
import * as path from 'path';

export interface KarpenterCleanupProps {
  readonly cluster: Cluster;
  readonly clusterName: string;
}

export class KarpenterCleanup extends Construct {
  public readonly customResource: CustomResource;

  constructor(scope: Construct, id: string, props: KarpenterCleanupProps) {
    super(scope, id);

    // Get the region from the stack's environment (set via env in unitune-infra.ts)
    // The region comes from process.env.CDK_DEFAULT_REGION passed to the stack
    const stack = cdk.Stack.of(this);
    // Use the cluster's stack region (which has the env region) or fallback to Aws.REGION token
    const region = props.cluster.stack.region ?? stack.region ?? Aws.REGION;

    // Lambda function to cleanup Karpenter resources
    const cleanupFunction = new Function(this, 'KarpenterCleanupFunction', {
      runtime: Runtime.PYTHON_3_12,
      handler: 'index.handler',
      code: Code.fromAsset(path.join(__dirname, 'karpenter-cleanup')),
      timeout: Duration.minutes(10),
      memorySize: 512,
      layers: [new KubectlV31Layer(this, 'KubectlLayer')],
      environment: {
        CLUSTER_NAME: props.clusterName,
      },
    });

    // Grant permissions to the Lambda function to access EKS, EC2, and cleanup IAM resources
    cleanupFunction.addToRolePolicy(
      new PolicyStatement({
        effect: Effect.ALLOW,
        actions: [
          'eks:DescribeCluster',
          'eks:ListClusters',
          'ec2:DescribeInstances',
          'iam:ListInstanceProfilesForRole',
          'iam:RemoveRoleFromInstanceProfile',
          'iam:DeleteInstanceProfile',
        ],
        resources: ['*'],
      }),
    );

    // Grant the Lambda function permission to use kubectl on the cluster
    props.cluster.awsAuth.addMastersRole(cleanupFunction.role!);

    // Create the custom resource provider
    const provider = new Provider(this, 'KarpenterCleanupProvider', {
      onEventHandler: cleanupFunction,
    });

    // Create the custom resource
    // This resource will be deleted BEFORE other resources that depend on it
    // During deletion, the Lambda will run and clean up Karpenter resources
    this.customResource = new CustomResource(this, 'KarpenterCleanupResource', {
      serviceToken: provider.serviceToken,
      properties: {
        ClusterName: props.clusterName,
        Region: region,
      },
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    // Ensure the custom resource is deleted before the provider's Lambda
    // This keeps the Lambda available to handle the delete event
    this.customResource.node.addDependency(provider);
  }
}
