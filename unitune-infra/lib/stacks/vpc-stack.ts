import { Construct } from 'constructs/lib/construct';
import * as cdk from 'aws-cdk-lib';
import { FlowLogDestination, FlowLogTrafficType, Vpc } from 'aws-cdk-lib/aws-ec2';
import { Tags } from 'aws-cdk-lib';

export class VpcStack extends cdk.Stack {
  public readonly vpc: Vpc;
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    this.vpc = new Vpc(this, 'VpcStack', {
      vpcName: 'UnituneVpc',
      maxAzs: 99, // Maximum number of Availability Zones to use
      natGateways: 2,
      flowLogs: {
        VpcFlowlogs: {
          trafficType: FlowLogTrafficType.ALL,
          destination: FlowLogDestination.toCloudWatchLogs(),
        },
      },
    });

    this.vpc.privateSubnets.forEach((subnet) => {
      Tags.of(subnet).add('karpenter.sh/discovery', 'true');
    });
  }
}
