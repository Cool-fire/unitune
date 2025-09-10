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
  KubernetesVersion,
  NodegroupAmiType,
  TaintEffect,
} from 'aws-cdk-lib/aws-eks';
import { KubectlV31Layer } from '@aws-cdk/lambda-layer-kubectl-v31';
import { IVpc, SubnetType } from 'aws-cdk-lib/aws-ec2';
import { Construct } from 'constructs/lib/construct';
import {
  Effect,
  ManagedPolicy,
  OpenIdConnectPrincipal,
  PolicyStatement,
  Role,
  ServicePrincipal,
} from 'aws-cdk-lib/aws-iam';

export interface EksStackProps extends cdk.StackProps {
  clusterName?: string;
  vpc?: IVpc;
}

export class EksStack extends cdk.Stack {
  public cluster: Cluster;

  constructor(scope: Construct, id: string, props: EksStackProps) {
    super(scope, id, props);
    this.cluster = this.createCluster(props);
    this.installEksAddons(this.cluster);
  }

  private createCluster(props: EksStackProps): Cluster {
    const clusterName = props.clusterName ? `unitune-${props.clusterName}` : `unitune-cluster`;

    const cluster = new Cluster(this, clusterName, {
      clusterName: clusterName,
      version: KubernetesVersion.V1_31,
      kubectlLayer: new KubectlV31Layer(this, `${clusterName}-kubectl-layer`),
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
        'karpenter.sh/discovery': clusterName
      }
    });

    cluster.addNodegroupCapacity('default-node-group', {
      instanceTypes: [],
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

    // TODO: Change to new role
    cluster.grantAccess('clusterAdminAccess', `arn:aws:iam::${props.env?.account}:role/IibsAdminAccess-DO-NOT-DELETE`, [
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

    const eksPodAgentAddon = new CfnAddon(this, 'EKSPodAgentAddon', {
      addonName: 'eks-pod-identity-agent',
      clusterName: cluster.clusterName,
      resolveConflicts: 'OVERWRITE',
    });
  }
}
