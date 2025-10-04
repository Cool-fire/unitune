import { Aws, Duration } from 'aws-cdk-lib';
import { Cluster, IdentityType, ServiceAccount } from 'aws-cdk-lib/aws-eks';
import { Rule } from 'aws-cdk-lib/aws-events';
import { SqsQueue } from 'aws-cdk-lib/aws-events-targets';
import { ManagedPolicy, Policy, PolicyStatement, Role, ServicePrincipal } from 'aws-cdk-lib/aws-iam';
import { Asset } from 'aws-cdk-lib/aws-s3-assets';
import { Queue, QueuePolicy } from 'aws-cdk-lib/aws-sqs';
import { Construct } from 'constructs/lib/construct';
import path from 'path';

const KARPENTER_VERISON = '1.6.3';

export interface KarpenterProps {
  readonly cluster: Cluster;
  readonly namespace?: string;
  readonly serviceAccountName?: string;
  readonly nodeRole?: Role;
}

export class Karpenter extends Construct {
  private readonly serviceAccountName: string;
  private readonly namespace: string;
  private readonly nodeRole: Role;
  private readonly cluster: Cluster;
  private readonly interruptionQueue: Queue;
  private readonly serviceAccount: ServiceAccount;

  constructor(scope: Construct, id: string, props: KarpenterProps) {
    super(scope, id);

    this.serviceAccountName = props.serviceAccountName ?? 'karpenter';
    this.namespace = props.namespace ?? 'kube-system';
    this.cluster = props.cluster;

    /**
     * Bootstrap the cluster to Enable Karpenter to create and manage nodes.
     *
     * Following the steps from the Karpenter documentation:
     * @link https://karpenter.sh/v0.32/reference/cloudformation/
     */

    this.nodeRole = props.nodeRole ?? this.createNodeRole(props.cluster.clusterName);

    this.cluster.awsAuth.addRoleMapping(this.nodeRole, {
      username: `system:node:{{EC2PrivateDNSName}}`,
      groups: ['system:bootstrappers', 'system:nodes'],
    });

    this.serviceAccount = this.cluster.addServiceAccount(`${this.cluster.clusterName}-ServiceAccount`, {
      namespace: this.namespace,
      name: this.serviceAccountName,
      identityType: IdentityType.POD_IDENTITY,
    });

    this.interruptionQueue = new Queue(this, `${this.cluster.clusterName}-KarpenterInterruptionQueue`, {
      queueName: `${this.cluster.clusterName}`,
      retentionPeriod: Duration.minutes(300), // 5 hours
    });

    this.addEventBridgeRules(this.interruptionQueue);
    const karpenterControllerPolicy = this.createKarpenterControllerPolicy();
    karpenterControllerPolicy.attachToRole(this.serviceAccount.role);

    this.cluster.addHelmChart('Karpeneter-chart', {
      chart: 'Karpenter',
      repository: 'oci://public.ecr.aws/karpenter/karpenter',
      namespace: this.namespace,
      wait: true,
      createNamespace: false,
      version: KARPENTER_VERISON,
      values: {
        settings: {
          clusterName: this.cluster.clusterName,
          clusterEndpoint: this.cluster.clusterEndpoint,
          interruptionQueue: this.interruptionQueue?.queueName,
        },
        serviceAccount: {
          create: false,
          name: this.serviceAccount.serviceAccountName,
          annotations: {
            'eks.amazonaws.com/role-arn': this.serviceAccount.role.roleArn,
          },
        },
      },
    });

    const karpenterConfigAsset = new Asset(this, 'KarpenterConfigAsset', {
      path: path.join(__dirname, '../../helm/karpenter-config'),
    });

    this.cluster.addHelmChart('KarpenterConfig-chart', {
      chartAsset: karpenterConfigAsset,
      namespace: this.namespace,
      wait: true,
      values: {
        clusterName: this.cluster.clusterName,
        nodeRoleName: this.nodeRole.roleName,
      },
    });
  }

  private addEventBridgeRules(interruptionQueue: Queue) {
    const rules = [
      new Rule(this, 'ScheduledChangeRule', {
        eventPattern: {
          source: ['aws.health'],
          detailType: ['AWS Health Event'],
        },
      }),
      new Rule(this, 'SpotInterruptionRule', {
        eventPattern: {
          source: ['aws.ec2'],
          detailType: ['EC2 Spot Instance Interruption Warning'],
        },
      }),
      new Rule(this, 'RebalanceRule', {
        eventPattern: {
          source: ['aws.ec2'],
          detailType: ['EC2 Instance Rebalance Recommendation'],
        },
      }),
      new Rule(this, 'InstanceStateChangeRule', {
        eventPattern: {
          source: ['aws.ec2'],
          detailType: ['EC2 Instance State-change Notification'],
        },
      }),
    ];

    for (const rule of rules) {
      rule.addTarget(new SqsQueue(interruptionQueue));
    }
  }

  private createKarpenterControllerPolicy(): ManagedPolicy {
    const policyName = `KarpenterControllerPolicy-${this.cluster.clusterName}`;

    const allowScopedEC2InstanceActions = new PolicyStatement({
      actions: ['ec2:CreateFleet', 'ec2:RunInstances'],
      resources: [
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}::image/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}::snapshot/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:spot-instances-request/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:security-group/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:subnet/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:launch-template/*`,
      ],
    });

    const allowScopedEC2InstanceActionsWithTags = new PolicyStatement({
      actions: ['ec2:RunInstances', 'ec2:CreateFleet', 'ec2:CreateLaunchTemplate'],
      resources: [
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:fleet/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:instance/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:volume/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:network-interface/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:launch-template/*`,
      ],
      conditions: {
        StringEquals: {
          [`aws:RequestTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
          'aws:RequestTag/eks:eks-cluster-name': `${this.cluster.clusterName}`,
        },
        StringLike: {
          'aws:RequestTag/karpenter.sh/nodepool': '*',
        },
      },
    });

    const allowedScopedResourceCreationTagging = new PolicyStatement({
      actions: ['ec2:CreateTags'],
      resources: [
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:instance/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:volume/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:network-interface/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:fleet/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:launch-template/*`,
      ],
      conditions: {
        StringEquals: {
          [`aws:RequestTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
          'aws:RequestTag/eks:eks-cluster-name': `${this.cluster.clusterName}`,
          'ec2:CreateAction': ['RunInstances', 'CreateFleet', 'CreateLaunchTemplate'],
        },
        StringLike: {
          'aws:RequestTag/karpenter.sh/nodepool': '*',
        },
      },
    });

    const allowScopedResourceTagging = new PolicyStatement({
      sid: 'AllowScopedResourceTagging',
      resources: [`arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:instance/*`],
      actions: ['ec2:CreateTags'],
      conditions: {
        StringEquals: {
          [`aws:ResourceTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
        },
        StringLike: {
          'aws:ResourceTag/karpenter.sh/nodepool': '*',
        },
        'ForAllValues:StringEquals': {
          'aws:TagKeys': ['karpenter.sh/nodeclaim', 'Name'],
        },
      },
    });

    const allowScopedDeletion = new PolicyStatement({
      sid: 'AllowScopedDeletion',
      resources: [
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:instance/*`,
        `arn:${Aws.PARTITION}:ec2:${Aws.REGION}:*:launch-template/*`,
      ],
      actions: ['ec2:TerminateInstances', 'ec2:DeleteLaunchTemplate'],
      conditions: {
        StringEquals: {
          [`aws:ResourceTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
        },
        StringLike: {
          'aws:ResourceTag/karpenter.sh/nodepool': '*',
        },
      },
    });

    const allowRegionalReadActions = new PolicyStatement({
      sid: 'AllowRegionalReadActions',
      resources: ['*'],
      actions: [
        'ec2:DescribeAvailabilityZones',
        'ec2:DescribeImages',
        'ec2:DescribeInstances',
        'ec2:DescribeInstanceTypeOfferings',
        'ec2:DescribeInstanceTypes',
        'ec2:DescribeLaunchTemplates',
        'ec2:DescribeSecurityGroups',
        'ec2:DescribeSpotPriceHistory',
        'ec2:DescribeSubnets',
      ],
      conditions: {
        StringEquals: {
          'aws:RequestedRegion': Aws.REGION,
        },
      },
    });

    const allowSSMReadActions = new PolicyStatement({
      sid: 'AllowSSMReadActions',
      resources: [`arn:${Aws.PARTITION}:ssm:${Aws.REGION}::parameter/aws/service/*`],
      actions: ['ssm:GetParameter'],
    });

    const allowPricingReadActions = new PolicyStatement({
      sid: 'AllowPricingReadActions',
      resources: ['*'],
      actions: ['pricing:GetProducts'],
    });

    const allowPassingInstanceRole = new PolicyStatement({
      sid: 'AllowPassingInstanceRole',
      resources: [`arn:${Aws.PARTITION}:iam::${Aws.ACCOUNT_ID}:role/KarpenterNodeRole-${this.cluster.clusterName}`],
      actions: ['iam:PassRole'],
      conditions: {
        StringEquals: {
          'iam:PassedToService': 'ec2.amazonaws.com',
        },
      },
    });

    const allowScopedInstanceProfileCreationActions = new PolicyStatement({
      sid: 'AllowScopedInstanceProfileCreationActions',
      resources: ['*'],
      actions: ['iam:CreateInstanceProfile'],
      conditions: {
        StringEquals: {
          [`aws:RequestTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
          'aws:RequestTag/topology.kubernetes.io/region': Aws.REGION,
        },
        StringLike: {
          'aws:RequestTag/karpenter.k8s.aws/ec2nodeclass': '*',
        },
      },
    });

    const allowScopedInstanceProfileTagActions = new PolicyStatement({
      sid: 'AllowScopedInstanceProfileTagActions',
      resources: ['*'],
      actions: ['iam:TagInstanceProfile'],
      conditions: {
        StringEquals: {
          [`aws:ResourceTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
          'aws:ResourceTag/topology.kubernetes.io/region': Aws.REGION,
          [`aws:RequestTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
          'aws:RequestTag/topology.kubernetes.io/region': Aws.REGION,
        },
        StringLike: {
          'aws:ResourceTag/karpenter.k8s.aws/ec2nodeclass': '*',
          'aws:RequestTag/karpenter.k8s.aws/ec2nodeclass': '*',
        },
      },
    });

    const allowScopedInstanceProfileActions = new PolicyStatement({
      sid: 'AllowScopedInstanceProfileActions',
      resources: ['*'],
      actions: ['iam:AddRoleToInstanceProfile', 'iam:RemoveRoleFromInstanceProfile', 'iam:DeleteInstanceProfile'],
      conditions: {
        StringEquals: {
          [`aws:ResourceTag/kubernetes.io/cluster/${this.cluster.clusterName}`]: 'owned',
          'aws:ResourceTag/topology.kubernetes.io/region': Aws.REGION,
        },
        StringLike: {
          'aws:ResourceTag/karpenter.k8s.aws/ec2nodeclass': '*',
        },
      },
    });

    const allowInstanceProfileReadActions = new PolicyStatement({
      sid: 'AllowInstanceProfileReadActions',
      resources: ['*'],
      actions: ['iam:GetInstanceProfile'],
    });

    const allowAPIServerEndpointDiscovery = new PolicyStatement({
      sid: 'AllowAPIServerEndpointDiscovery',
      resources: [`arn:${Aws.PARTITION}:eks:${Aws.REGION}:${Aws.ACCOUNT_ID}:cluster/${this.cluster.clusterName}`],
      actions: ['eks:DescribeCluster'],
    });

    const controllerPolicies = [
      allowScopedEC2InstanceActions,
      allowScopedEC2InstanceActionsWithTags,
      allowedScopedResourceCreationTagging,
      allowScopedResourceTagging,
      allowScopedDeletion,
      allowRegionalReadActions,
      allowSSMReadActions,
      allowPricingReadActions,
      allowPassingInstanceRole,
      allowScopedInstanceProfileCreationActions,
      allowScopedInstanceProfileTagActions,
      allowScopedInstanceProfileActions,
      allowInstanceProfileReadActions,
      allowAPIServerEndpointDiscovery,
    ];

    if (this.interruptionQueue) {
      const allowSQSReadActions = new PolicyStatement({
        sid: 'AllowSQSReadActions',
        resources: [this.interruptionQueue.queueArn],
        actions: ['sqs:ReceiveMessage', 'sqs:DeleteMessage', 'sqs:GetQueueAttributes'],
      });

      controllerPolicies.push(allowSQSReadActions);
    }

    return new ManagedPolicy(this, `${this.cluster.clusterName}-KarpenterControllerPolicy`, {
      managedPolicyName: policyName,
      statements: controllerPolicies,
    });
  }

  private createNodeRole(clusterName: string): Role {
    return new Role(this, `${clusterName}-KarpenterNodeRole`, {
      assumedBy: new ServicePrincipal('ec2.amazonaws.com'),
      managedPolicies: [
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEKS_CNI_Policy'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEC2ContainerRegistryReadOnly'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonSSMManagedInstanceCore'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEKSWorkerNodePolicy'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEC2FullAccess'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonS3FullAccess'),
        ManagedPolicy.fromAwsManagedPolicyName('CloudWatchAgentServerPolicy'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonAPIGatewayInvokeFullAccess'),
      ],
    });
  }
}
