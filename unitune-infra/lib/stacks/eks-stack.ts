import * as cdk from 'aws-cdk-lib';
import {
  AccessPolicy,
  AccessScopeType,
  Addon,
  AuthenticationMode,
  CfnAddon,
  Cluster,
  ClusterLoggingTypes,
  EndpointAccess,
  IdentityType,
  KubernetesVersion,
  NodegroupAmiType,
  TaintEffect,
} from 'aws-cdk-lib/aws-eks';
import { KubectlV31Layer } from '@aws-cdk/lambda-layer-kubectl-v31';
import { InstanceType, IVpc, SubnetType } from 'aws-cdk-lib/aws-ec2';
import { Construct } from 'constructs/lib/construct';
import {
  ArnPrincipal,
  Effect,
  ManagedPolicy,
  PolicyStatement,
  Role,
  ServicePrincipal,
} from 'aws-cdk-lib/aws-iam';
import { Repository } from 'aws-cdk-lib/aws-ecr';
import { BlockPublicAccess, Bucket, BucketEncryption } from 'aws-cdk-lib/aws-s3';
import { Karpenter } from '../constructs/k8s/karpenter';
import { KarpenterCleanup } from '../constructs/k8s/karpenter-cleanup';
import { AwsCustomResource, AwsCustomResourcePolicy, PhysicalResourceId } from 'aws-cdk-lib/custom-resources';

export interface EksStackProps extends cdk.StackProps {
  clusterName?: string;
  vpc?: IVpc;
}

export class EksStack extends cdk.Stack {
  public cluster: Cluster;
  private clusterName: string;

  constructor(scope: Construct, id: string, props: EksStackProps) {
    super(scope, id, props);
    this.cluster = this.createCluster(props);
    this.installEksAddons(this.cluster);
    this.configureBuildResources(this.cluster);

    // Create cleanup resource - this will run before Karpenter resources are deleted
    const karpenterCleanup = new KarpenterCleanup(this, 'KarpenterCleanup', {
      clusterName: this.clusterName,
    });

    // Create Karpenter
    const karpenter = new Karpenter(this, 'Karpenter', {
      clusterName: this.clusterName,
      cluster: this.cluster,
    });

    // REVERSE the dependency: cleanup depends on Karpenter
    // This ensures that during deletion:
    // 1. Cleanup custom resource is deleted first (triggering the delete handler)
    // 2. Cleanup Lambda runs and removes Karpenter resources
    // 3. Then Karpenter resources can be safely deleted
    karpenterCleanup.customResource.node.addDependency(karpenter);
  }

  private createCluster(props: EksStackProps): Cluster {
    this.clusterName = props.clusterName ? `unitune-${props.clusterName}` : `unitune-cluster`;

    const cluster = new Cluster(this, this.clusterName, {
      clusterName: this.clusterName,
      version: KubernetesVersion.V1_31,
      kubectlLayer: new KubectlV31Layer(this, `${this.clusterName}-kubectl-layer`),
      vpc: props.vpc,
      vpcSubnets: [
        {
          availabilityZones: props.vpc?.availabilityZones.filter((zone) => zone !== 'us-east-1e'),
        },
      ],
      defaultCapacity: 0,
      endpointAccess: EndpointAccess.PUBLIC_AND_PRIVATE,
      authenticationMode: AuthenticationMode.API_AND_CONFIG_MAP,
      clusterLogging: [
        ClusterLoggingTypes.API,
        ClusterLoggingTypes.AUDIT,
        ClusterLoggingTypes.AUTHENTICATOR,
        ClusterLoggingTypes.CONTROLLER_MANAGER,
        ClusterLoggingTypes.SCHEDULER,
      ],
      tags: {
        'karpenter.sh/discovery': this.clusterName,
      },
    });

    // Add a custom resource to tag the security group id's
    new AwsCustomResource(this, 'TagClusterSecurityGroupForKarpenter', {
      onCreate: {
        service: 'EC2',
        action: 'createTags',
        parameters: {
          Resources: [cluster.clusterSecurityGroupId],
          Tags: [{ Key: 'karpenter.sh/discovery', Value: this.clusterName }],
        },
        physicalResourceId: PhysicalResourceId.of(`${this.clusterName}-cluster-sg-tag`),
      },
      policy: AwsCustomResourcePolicy.fromStatements([
        new PolicyStatement({
          effect: Effect.ALLOW,
          actions: ['ec2:CreateTags'],
          resources: ['*'],
        }),
      ]),
    });

    // Tag VPC subnets for Karpenter discovery
    props.vpc?.publicSubnets.forEach((subnet, index) => {
      cdk.Tags.of(subnet).add('karpenter.sh/discovery', this.clusterName);
    });
    props.vpc?.privateSubnets.forEach((subnet, index) => {
      cdk.Tags.of(subnet).add('karpenter.sh/discovery', this.clusterName);
    });

    cluster.addNodegroupCapacity('default-node-group', {
      instanceTypes: [new InstanceType('m5.large')],
      minSize: 1,
      maxSize: 10,
      desiredSize: 2,
      amiType: NodegroupAmiType.BOTTLEROCKET_X86_64,
      subnets: props.vpc?.selectSubnets({
        subnetType: SubnetType.PUBLIC,
      }),
      // This taint allows to run addons and karpenter running on the control plane nodes
      taints: [
        {
          effect: TaintEffect.NO_SCHEDULE,
          key: 'CriticalAddonsOnly',
          value: 'true',
        },
      ],
    });

    // Create a dedicated role for cluster admin access
    const clusterAdminRole = new Role(this, 'ClusterAdminRole', {
      roleName: `${this.clusterName}-admin`,
      assumedBy: new ArnPrincipal(`arn:aws:iam::${this.account}:user/unitune`),
    });

    // Grant the role cluster admin access via Access Entry
    cluster.grantAccess('clusterAdminAccess', clusterAdminRole.roleArn, [
      AccessPolicy.fromAccessPolicyName('AmazonEKSClusterAdminPolicy', {
        accessScopeType: AccessScopeType.CLUSTER,
      }),
    ]);

    return cluster;
  }

  private installEksAddons(cluster: Cluster): void {
    const coreDnsAddon = new CfnAddon(this, 'CoreDNSAddon', {
      addonName: 'coredns',
      clusterName: cluster.clusterName,
      resolveConflicts: 'OVERWRITE',
    });

    const kubeProxyAddon = new CfnAddon(this, 'KubeProxyAddon', {
      addonName: 'kube-proxy',
      clusterName: cluster.clusterName,
      resolveConflicts: 'OVERWRITE',
    });

    const trustRelationShipStatement = new PolicyStatement({
      effect: Effect.ALLOW,
      principals: [new ServicePrincipal('pods.eks.amazonaws.com')],
      actions: ['sts:AssumeRole', 'sts:TagSession'],
    });

    const vpcRole = new Role(this, 'vpcCniRole', {
      roleName: `vpcCniRole-${cluster.clusterName}`,
      assumedBy: new ServicePrincipal('pods.eks.amazonaws.com'),
      managedPolicies: [
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEKS_CNI_Policy'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEKSClusterPolicy'),
      ],
    });
    vpcRole.assumeRolePolicy?.addStatements(trustRelationShipStatement);

    const vpcAddon = new CfnAddon(this, 'VPCCNIAddon', {
      addonName: 'vpc-cni',
      clusterName: cluster.clusterName,
      resolveConflicts: 'OVERWRITE',
      podIdentityAssociations: [
        {
          roleArn: vpcRole.roleArn,
          serviceAccount: 'aws-node',
        },
      ],
    });

    const efsCsiRole = new Role(this, 'efiCsiRole', {
      roleName: `efsCsiRole-${this.cluster.clusterName}`,
      assumedBy: new ServicePrincipal('pods.eks.amazonaws.com'),
      managedPolicies: [
        ManagedPolicy.fromAwsManagedPolicyName('service-role/AmazonEFSCSIDriverPolicy'),
        ManagedPolicy.fromAwsManagedPolicyName('AmazonEKSClusterPolicy'),
      ],
    });

    efsCsiRole.assumeRolePolicy?.addStatements(trustRelationShipStatement);

    const efsCsiAddon = new CfnAddon(this, 'EFSCSIAddon', {
      addonName: 'aws-efs-csi-driver',
      clusterName: cluster.clusterName,
      resolveConflicts: 'OVERWRITE',
      podIdentityAssociations: [
        {
          roleArn: efsCsiRole.roleArn,
          serviceAccount: 'efs-csi-controller-sa',
        },
      ],
    });

    // const eksPodAgentAddon = new CfnAddon(this, 'EKSPodAgentAddon', {
    //   addonName: 'eks-pod-identity-agent',
    //   clusterName: cluster.clusterName,
    //   resolveConflicts: 'OVERWRITE',
    // });
  }

  /**
   * Provision shared build resources used by the deploy command:
   *  - unique S3 bucket for build contexts
   *  - single ECR repository for images
   *  - builder namespace + service account using Pod Identity with scoped perms
   *  - stack outputs for the CLI to discover
   */
  private configureBuildResources(cluster: Cluster): void {
    // S3 bucket – leave bucketName undefined so CFN generates a unique name
    const contextBucket = new Bucket(this, 'UnituneBuildContextBucket', {
      bucketName: `unitune-buildctx-${cdk.Aws.ACCOUNT_ID}-${cdk.Aws.REGION}`,
      blockPublicAccess: BlockPublicAccess.BLOCK_ALL,
      encryption: BucketEncryption.S3_MANAGED,
      lifecycleRules: [
        {
          prefix: 'contexts/',
          expiration: cdk.Duration.days(7),
        },
      ],
      removalPolicy: cdk.RemovalPolicy.DESTROY,
      autoDeleteObjects: true,
    });

    // Single shared ECR repository
    const repository = new Repository(this, 'UnituneEcrRepository', {
      repositoryName: 'unitune',
      imageScanOnPush: true,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
      emptyOnDelete: true,
    });

    // Namespace + Service Account for privileged BuildKit Jobs
    const buildNamespace = 'unitune-build';
    cluster.addManifest('UnituneBuildNamespace', {
      apiVersion: 'v1',
      kind: 'Namespace',
      metadata: { name: buildNamespace },
    });

    const builderServiceAccount = cluster.addServiceAccount('UnituneBuilderServiceAccount', {
      namespace: buildNamespace,
      name: 'unitune-builder',
      identityType: IdentityType.POD_IDENTITY,
    });

    // Grant minimal S3 read access to contexts/*
    contextBucket.grantRead(builderServiceAccount, 'contexts/*');

    // Grant ECR push/pull and auth token
    repository.grantPullPush(builderServiceAccount);
    builderServiceAccount.addToPrincipalPolicy(
      new PolicyStatement({
        actions: ['ecr:GetAuthorizationToken'],
        resources: ['*'],
      }),
    );

    new cdk.CfnOutput(this, 'UnituneBuildContextBucketName', {
      value: contextBucket.bucketName,
    });

    new cdk.CfnOutput(this, 'UnituneEcrRepositoryUri', {
      value: repository.repositoryUri,
    });

    new cdk.CfnOutput(this, 'UnituneBuildNamespace', {
      value: buildNamespace,
    });

    new cdk.CfnOutput(this, 'UnituneBuilderServiceAccount', {
      value: builderServiceAccount.serviceAccountName,
    });
  }
}
