import * as cdk from "aws-cdk-lib";
import { Addon, CfnAddon, Cluster, EndpointAccess, KubernetesVersion } from "aws-cdk-lib/aws-eks";
import { KubectlV32Layer } from "@aws-cdk/lambda-layer-kubectl-v32";
import { IVpc } from "aws-cdk-lib/aws-ec2";
import { Construct } from "constructs/lib/construct";
import { ManagedPolicy, PolicyStatement, Role, ServicePrincipal } from "aws-cdk-lib/aws-iam";

export interface EksStackProps extends cdk.StackProps {
    clusterName?: string;
    vpc?: IVpc;
}

export class EksStack extends cdk.Stack {
    public readonly cluster: Cluster;

    constructor(scope: Construct, id: string, props?: cdk.StackProps) {
        super(scope, id, props);

    }

    private createCluster(props: EksStackProps): Cluster {
        const clusterName = props.clusterName ? `unitune-${props.clusterName}` : `unitune-cluster`;

        const cluster = new Cluster(this, clusterName, {
            clusterName: clusterName,
            version: KubernetesVersion.V1_32,
            kubectlLayer: new KubectlV32Layer(this, `${clusterName}-kubectl-layer`),
            vpc: props.vpc,
            endpointAccess: EndpointAccess.PUBLIC_AND_PRIVATE,
        });


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

        const efsCsiAddon = new CfnAddon(this, 'EFSCSIAddon', {
            addonName: 'aws-efs-csi-driver',
            clusterName: cluster.clusterName,
            resolveConflicts: 'OVERWRITE',
        })
    }
}