import * as cdk from 'aws-cdk-lib';
import { Aws } from 'aws-cdk-lib';
import { Code, Function, Runtime } from 'aws-cdk-lib/aws-lambda';
import { Effect, PolicyStatement } from 'aws-cdk-lib/aws-iam';
import { CustomResource, Duration } from 'aws-cdk-lib';
import { Provider } from 'aws-cdk-lib/custom-resources';
import { Construct } from 'constructs/lib/construct';
import * as path from 'path';

export interface KarpenterCleanupProps {
  readonly clusterName: string;
  readonly region?: string;
}

export class KarpenterCleanup extends Construct {
  public readonly customResource: CustomResource;

  constructor(scope: Construct, id: string, props: KarpenterCleanupProps) {
    super(scope, id);

    const stack = cdk.Stack.of(this);
    const region = props.region ?? stack.region ?? Aws.REGION;

    // Lambda function to cleanup Karpenter resources (EC2 instances and IAM instance profiles)
    const cleanupFunction = new Function(this, 'KarpenterCleanupFunction', {
      runtime: Runtime.PYTHON_3_12,
      handler: 'index.handler',
      code: Code.fromAsset(path.join(__dirname, 'karpenter-cleanup')),
      timeout: Duration.minutes(10),
      memorySize: 256,
      environment: {
        CLUSTER_NAME: props.clusterName,
      },
    });

    // Grant permissions to the Lambda function to cleanup Karpenter resources
    cleanupFunction.addToRolePolicy(
      new PolicyStatement({
        effect: Effect.ALLOW,
        actions: [
          'ec2:DescribeInstances',
          'ec2:TerminateInstances',
          'iam:ListInstanceProfilesForRole',
          'iam:RemoveRoleFromInstanceProfile',
          'iam:DeleteInstanceProfile',
        ],
        resources: ['*'],
      }),
    );

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
  }
}
